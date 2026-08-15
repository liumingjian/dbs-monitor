package migrations_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/migrations"
	"github.com/pressly/goose/v3"
)

func TestMigrationsAndPartitionFailureCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	admin := openDatabase(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()

	databaseName := fmt.Sprintf("dbs_monitor_test_%d", os.Getpid())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop stale test database: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})

	database := openDatabase(t, databaseName)
	defer database.Close()

	keyringDirectory := filepath.Join(t.TempDir(), "credentials")
	applied, err := migrations.Up(ctx, database, keyringDirectory)
	if err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if applied == 0 {
		t.Fatal("first migration run applied no migrations")
	}
	firstKey, err := os.ReadFile(filepath.Join(keyringDirectory, "master-key-v1"))
	if err != nil {
		t.Fatalf("read generated migration key: %v", err)
	}
	applied, err = migrations.Up(ctx, database, keyringDirectory)
	if err != nil {
		t.Fatalf("second migration run: %v", err)
	}
	if applied != 0 {
		t.Fatalf("second migration run applied %d migrations, want 0", applied)
	}
	secondKey, err := os.ReadFile(filepath.Join(keyringDirectory, "master-key-v1"))
	if err != nil {
		t.Fatalf("reread generated migration key: %v", err)
	}
	if !bytes.Equal(firstKey, secondKey) {
		t.Fatal("second migration run replaced the master key")
	}

	lockConnection, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve migration lock connection: %v", err)
	}
	defer lockConnection.Close()
	const (
		migrationLockID  int64 = 0x4442534d
		migrationLockSQL       = `SELECT pg_advisory_lock(
			((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
		)`
		migrationUnlockSQL = `SELECT pg_advisory_unlock(
			((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
		)`
	)
	if _, err := lockConnection.ExecContext(ctx, migrationLockSQL, migrationLockID); err != nil {
		t.Fatalf("hold migration advisory lock: %v", err)
	}
	type migrationResult struct {
		applied int
		err     error
	}
	result := make(chan migrationResult, 1)
	go func() {
		applied, err := migrations.Up(ctx, database, keyringDirectory)
		result <- migrationResult{applied: applied, err: err}
	}()
	select {
	case completed := <-result:
		t.Fatalf("concurrent migration completed while advisory lock was held: %+v", completed)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := lockConnection.ExecContext(ctx, migrationUnlockSQL, migrationLockID); err != nil {
		t.Fatalf("release migration advisory lock: %v", err)
	}
	select {
	case completed := <-result:
		if completed.err != nil {
			t.Fatalf("migration after advisory lock release: %v", completed.err)
		}
		if completed.applied != 0 {
			t.Fatalf("migration after advisory lock release applied %d migrations, want 0", completed.applied)
		}
	case <-ctx.Done():
		t.Fatalf("migration did not continue after advisory lock release: %v", ctx.Err())
	}

	var provider string
	if err := database.QueryRowContext(ctx, "SELECT datlocprovider FROM pg_database WHERE datname = current_database()").Scan(&provider); err != nil {
		t.Fatalf("query locale provider: %v", err)
	}
	if provider == "i" {
		t.Fatal("test database uses ICU locale provider")
	}

	var partitioned bool
	if err := database.QueryRowContext(ctx, "SELECT relkind = 'p' FROM pg_class WHERE oid = 'metric_sample'::regclass").Scan(&partitioned); err != nil {
		t.Fatalf("query metric_sample kind: %v", err)
	}
	if !partitioned {
		t.Fatal("metric_sample is not partitioned")
	}

	var seriesID int64
	err = database.QueryRowContext(ctx, `
		WITH created_identity AS (
			INSERT INTO instance_identity (id, name)
			VALUES ('00000000-0000-0000-0000-000000000001', 'test')
			RETURNING id
		), created_instance AS (
			INSERT INTO instance (id, name, host, port, database_name, username, password_ciphertext, password_key_version)
			SELECT id, 'test', 'localhost', 5432, 'postgres', 'postgres', '\\x01', 1 FROM created_identity
			RETURNING id
		)
		INSERT INTO metric_series (instance_id, metric_id, labels_key, last_seen)
		SELECT id, 'pg.connection.total', '{}', now() FROM created_instance
		RETURNING series_id`).Scan(&seriesID)
	if err != nil {
		t.Fatalf("create test series: %v", err)
	}
	_, err = database.ExecContext(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, now(), 1)", seriesID)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		t.Fatalf("missing partition error = %v, want PgError", err)
	}
	if pgError.Code != "23514" {
		t.Fatalf("missing partition SQLSTATE = %s, want 23514", pgError.Code)
	}

	target, err := pgx.Connect(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pgx connection: %v", err)
	}
	defer target.Close(ctx)
	if err := metric.EnsurePartitions(ctx, target, time.Now()); err != nil {
		t.Fatalf("ensure partitions: %v", err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, now(), 1)", seriesID); err != nil {
		t.Fatalf("insert after partition repair: %v", err)
	}

	for _, table := range []string{
		"collection_task_config", "instance_collection_config",
		"instance_collection_task_state", "instance_collection_connection_state",
		"instance_capability_snapshot",
		"long_query_sample_snapshot", "long_query_sample",
		"instance_session_snapshot", "instance_session_snapshot_entry",
		"query_statistics_snapshot", "query_statistics_snapshot_entry",
		"alert_rule", "alert_rule_version", "alert_rule_scope_instance",
		"alert_rule_evaluation_state", "alert_event",
		"alert_trigger_snapshot", "alert_trigger_snapshot_session",
		"performance_event",
	} {
		var exists bool
		if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("check collection plan table %q: %v", table, err)
		}
		if !exists {
			t.Fatalf("collection plan table %q is missing", table)
		}
	}
	var sqlTextColumns int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('long_query_sample', 'instance_session_snapshot_entry',
		                     'query_statistics_snapshot_entry')
		  AND column_name IN ('query', 'sql', 'query_text', 'sql_text')`).Scan(&sqlTextColumns); err != nil {
		t.Fatalf("inspect activity snapshot columns: %v", err)
	}
	if sqlTextColumns != 0 {
		t.Fatalf("snapshot SQL text columns = %d, want 0", sqlTextColumns)
	}
	var pauseColumns int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'instance_collection_config'
		  AND column_name IN ('collection_paused', 'collection_pause_updated_by',
		                      'collection_pause_updated_at', 'collection_pause_reason')`).Scan(&pauseColumns); err != nil {
		t.Fatalf("inspect collection pause columns: %v", err)
	}
	if pauseColumns != 4 {
		t.Fatalf("collection pause columns = %d, want 4", pauseColumns)
	}
	var lifecycleColumns int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'instance' AND is_nullable = 'NO'
		AND column_name IN ('agent_expected')`).Scan(&lifecycleColumns); err != nil {
		t.Fatalf("inspect Agent lifecycle columns: %v", err)
	}
	if lifecycleColumns != 1 {
		t.Fatalf("required Agent lifecycle columns = %d, want 1", lifecycleColumns)
	}
	var tokenIndexUnique bool
	if err := database.QueryRowContext(ctx, `SELECT indisunique FROM pg_index
		WHERE indexrelid = 'instance_agent_token_hash_unique_idx'::regclass`).Scan(&tokenIndexUnique); err != nil {
		t.Fatalf("inspect Agent token hash index: %v", err)
	}
	if !tokenIndexUnique {
		t.Fatal("Agent token hash index is not unique")
	}
	var seedMetric, seedScope string
	var seedVersion int
	var seedEvaluationInterval int
	var seedSnapshot []byte
	if err := database.QueryRowContext(ctx, `SELECT rule.metric_id, rule.scope, rule.evaluation_interval_seconds,
		version.version, version.snapshot
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = rule.version
		WHERE rule.id = '00000000-0000-0000-0000-000000000061'`).Scan(
		&seedMetric, &seedScope, &seedEvaluationInterval, &seedVersion, &seedSnapshot); err != nil {
		t.Fatalf("read seeded tracer rule: %v", err)
	}
	if seedMetric != "pg.connection.total" || seedScope != "ALL" || seedEvaluationInterval != 5 || seedVersion != 1 || len(seedSnapshot) == 0 {
		t.Fatalf("seeded tracer rule = metric %q scope %q interval %d version %d snapshot %q",
			seedMetric, seedScope, seedEvaluationInterval, seedVersion, seedSnapshot)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO collection_task_config (instance_id, task_id, interval_seconds)
		VALUES ('00000000-0000-0000-0000-000000000001', 'pg.stat_database', 4)`)
	if !errors.As(err, &pgError) || pgError.Code != "23514" {
		t.Fatalf("4-second collection interval error = %v, want check violation", err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO instance_collection_task_state
		(instance_id, task_id, consecutive_failures, last_result)
		VALUES ('00000000-0000-0000-0000-000000000001', 'pg.probe', 0, 'UNKNOWN')`)
	if !errors.As(err, &pgError) || pgError.Code != "23514" {
		t.Fatalf("unknown task result error = %v, want check violation", err)
	}
}

func TestAlertingSeedsAreIdempotentAndTemplatesAreReplaced(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	admin := openDatabase(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	databaseName := fmt.Sprintf("dbs_monitor_alerting_seed_%d", os.Getpid())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	database := openDatabase(t, databaseName)
	defer database.Close()
	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	if _, err := migrations.Up(ctx, database, credentialDirectory); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	type ruleSeed struct {
		identifier, metricID, aggregation, operator, recoveryOperator, severity string
		threshold, recoveryThreshold                                            float64
		windowSeconds, consecutiveCount, evaluationIntervalSeconds              int
	}
	wantRules := []ruleSeed{
		{"agent_offline", "agent.status", "latest", "=", "=", "critical", 0, 1, 30, 3, 30},
		{"data_stale", "collector.last_success_time", "latest", ">", "<", "warning", 600, 450, 60, 2, 60},
		{"database_unreachable", "pg.availability.reachable", "latest", "=", "=", "critical", 0, 1, 30, 3, 30},
	}
	ruleRows, err := database.QueryContext(ctx, `SELECT builtin_identifier, metric_id, aggregation, operator,
		threshold, recovery_operator, recovery_threshold, severity, window_seconds,
		consecutive_count, evaluation_interval_seconds
		FROM alert_rule WHERE builtin_identifier IS NOT NULL ORDER BY builtin_identifier`)
	if err != nil {
		t.Fatalf("list built-in alert rules: %v", err)
	}
	defer ruleRows.Close()
	var gotRules []ruleSeed
	for ruleRows.Next() {
		var got ruleSeed
		if err := ruleRows.Scan(&got.identifier, &got.metricID, &got.aggregation, &got.operator,
			&got.threshold, &got.recoveryOperator, &got.recoveryThreshold, &got.severity,
			&got.windowSeconds, &got.consecutiveCount, &got.evaluationIntervalSeconds); err != nil {
			t.Fatalf("scan built-in alert rule: %v", err)
		}
		gotRules = append(gotRules, got)
	}
	if !reflect.DeepEqual(gotRules, wantRules) {
		t.Fatalf("built-in alert rules = %+v, want %+v", gotRules, wantRules)
	}

	var policyIdentifier, policyName string
	var defaultPolicyCount int
	if err := database.QueryRowContext(ctx, `SELECT min(identifier), min(name), count(*)
		FROM notification_policy WHERE is_default`).Scan(&policyIdentifier, &policyName, &defaultPolicyCount); err != nil {
		t.Fatalf("read default notification policy: %v", err)
	}
	if policyIdentifier != "default" || policyName != "默认策略" || defaultPolicyCount != 1 {
		t.Fatalf("default notification policy = %q/%q count %d", policyIdentifier, policyName, defaultPolicyCount)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM notification_policy WHERE is_default`); err == nil {
		t.Fatal("deleting the default notification policy succeeded")
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO notification_policy (id, identifier, name, is_default)
		VALUES ('00000000-0000-0000-0000-000000063099', 'other-default', 'other', true)`); err == nil {
		t.Fatal("creating a second default notification policy succeeded")
	}

	type templateSeed struct {
		identifier, metricID, aggregation, operator, recoveryOperator, severity string
		threshold, recoveryThreshold                                            float64
		consecutiveCount, evaluationIntervalSeconds                             int
	}
	wantTemplates := []templateSeed{
		{"active_connections_high", "pg.connection.active", "max", ">=", "<=", "warning", 100, 80, 3, 30},
		{"blocked_sessions", "pg.session.blocked_count", "latest", ">", "=", "critical", 0, 0, 3, 30},
		{"connections_high", "pg.connection.total", "max", ">=", "<=", "warning", 500, 400, 3, 30},
		{"cpu_high", "host.cpu.usage_percent", "avg", ">=", "<=", "warning", 80, 70, 5, 60},
		{"disk_usage_high", "host.disk.usage_percent", "latest", ">=", "<=", "critical", 90, 85, 3, 60},
		{"idle_in_transaction_high", "pg.connection.idle_in_transaction", "latest", ">=", "<=", "warning", 10, 5, 3, 30},
		{"lock_waiting", "pg.lock.waiting_count", "latest", ">", "=", "warning", 0, 0, 3, 30},
		{"long_queries_high", "pg.query.long_running_count", "latest", ">=", "<=", "warning", 5, 2, 3, 30},
		{"long_transaction", "pg.transaction.max_duration_sec", "max", ">=", "<=", "warning", 300, 60, 2, 30},
		{"memory_high", "host.memory.usage_percent", "avg", ">=", "<=", "warning", 85, 75, 5, 60},
		{"prepared_xacts", "pg.prepared_xacts.count", "latest", ">", "=", "info", 0, 0, 3, 60},
		{"probe_latency_high", "pg.probe.latency_ms", "avg", ">=", "<=", "warning", 500, 300, 3, 30},
		{"replication_slot_backlog", "pg.replication_slot.retained_wal_bytes", "latest", ">=", "<=", "warning", 1073741824, 536870912, 3, 60},
		{"replication_wal_lag", "pg.replication.wal_lag_bytes", "avg", ">=", "<=", "warning", 104857600, 52428800, 3, 60},
		{"temp_bytes_high", "pg.temp.bytes_per_sec", "avg", ">=", "<=", "warning", 10485760, 5242880, 5, 60},
	}
	templateRows, err := database.QueryContext(ctx, `SELECT identifier, metric_id, aggregation, operator,
		threshold, recovery_operator, recovery_threshold, severity, consecutive_count,
		evaluation_interval_seconds
		FROM alert_rule_template ORDER BY identifier`)
	if err != nil {
		t.Fatalf("list alert rule templates: %v", err)
	}
	defer templateRows.Close()
	var gotTemplates []templateSeed
	for templateRows.Next() {
		var got templateSeed
		if err := templateRows.Scan(&got.identifier, &got.metricID, &got.aggregation, &got.operator,
			&got.threshold, &got.recoveryOperator, &got.recoveryThreshold, &got.severity,
			&got.consecutiveCount, &got.evaluationIntervalSeconds); err != nil {
			t.Fatalf("scan alert rule template: %v", err)
		}
		gotTemplates = append(gotTemplates, got)
	}
	if !reflect.DeepEqual(gotTemplates, wantTemplates) {
		t.Fatalf("alert rule templates = %+v, want %+v", gotTemplates, wantTemplates)
	}

	if _, err := database.ExecContext(ctx, `UPDATE alert_rule SET name = 'user name', threshold = 9
		WHERE builtin_identifier = 'database_unreachable'`); err != nil {
		t.Fatalf("customize built-in alert rule: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE alert_rule_template SET threshold = -1
		WHERE identifier = 'cpu_high'`); err != nil {
		t.Fatalf("modify read-only template fixture: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO alert_rule_template
		(identifier, version, name, metric_id, aggregation, operator, threshold,
		recovery_operator, recovery_threshold, window_seconds, consecutive_count,
		recovery_consecutive_count, severity, no_data_policy, evaluation_interval_seconds)
		SELECT 'obsolete', version, name, metric_id, aggregation, operator, threshold,
		recovery_operator, recovery_threshold, window_seconds, consecutive_count,
		recovery_consecutive_count, severity, no_data_policy, evaluation_interval_seconds
		FROM alert_rule_template LIMIT 1`); err != nil {
		t.Fatalf("insert obsolete template fixture: %v", err)
	}
	if applied, err := migrations.Up(ctx, database, credentialDirectory); err != nil || applied != 0 {
		t.Fatalf("repeat migration = applied %d, error %v", applied, err)
	}
	var customizedName string
	var customizedThreshold float64
	if err := database.QueryRowContext(ctx, `SELECT name, threshold FROM alert_rule
		WHERE builtin_identifier = 'database_unreachable'`).Scan(&customizedName, &customizedThreshold); err != nil {
		t.Fatalf("read customized built-in rule: %v", err)
	}
	if customizedName != "user name" || customizedThreshold != 9 {
		t.Fatalf("customized built-in rule was overwritten: %q threshold %v", customizedName, customizedThreshold)
	}
	var templateCount int
	var cpuThreshold float64
	if err := database.QueryRowContext(ctx, `SELECT count(*), min(threshold) FILTER (WHERE identifier = 'cpu_high')
		FROM alert_rule_template`).Scan(&templateCount, &cpuThreshold); err != nil {
		t.Fatalf("read replaced templates: %v", err)
	}
	if templateCount != 15 || cpuThreshold != 80 {
		t.Fatalf("replaced templates = count %d cpu threshold %v, want 15/80", templateCount, cpuThreshold)
	}
}

func TestPlaintextCredentialMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	databaseName := fmt.Sprintf("dbs_monitor_credential_migration_%d", os.Getpid())
	admin := openDatabase(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop stale test database: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	database := openDatabase(t, databaseName)
	defer database.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, database, os.DirFS("."))
	if err != nil {
		t.Fatalf("create legacy migration provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("migrate encrypted credential schema: %v", err)
	}
	instanceID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	password := "issue-123-legacy-password"
	legacyAgentTokenHash := sha256.Sum256([]byte("legacy-agent-token"))
	if _, err := database.ExecContext(ctx, `INSERT INTO instance
		(id, name, host, port, database_name, username, password, agent_token_hash)
		VALUES ($1, 'legacy', 'localhost', 5432, 'postgres', 'postgres', $2, $3)`, instanceID, password, legacyAgentTokenHash[:]); err != nil {
		t.Fatalf("insert legacy instance: %v", err)
	}

	keyringDirectory := filepath.Join(t.TempDir(), "credentials")
	if _, err := migrations.Up(ctx, database, keyringDirectory); err != nil {
		t.Fatalf("migrate plaintext credential: %v", err)
	}
	var plaintextColumnExists bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'instance' AND column_name = 'password'
	)`).Scan(&plaintextColumnExists); err != nil {
		t.Fatalf("inspect instance schema: %v", err)
	}
	if plaintextColumnExists {
		t.Fatal("plaintext password column remains after migration")
	}
	var requiredCipherColumns int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'instance' AND is_nullable = 'NO'
		AND ((column_name = 'password_ciphertext' AND data_type = 'bytea')
		  OR (column_name = 'password_key_version' AND data_type = 'integer'))`).Scan(&requiredCipherColumns); err != nil {
		t.Fatalf("inspect encrypted credential columns: %v", err)
	}
	if requiredCipherColumns != 2 {
		t.Fatalf("required encrypted credential columns = %d, want 2", requiredCipherColumns)
	}
	var ciphertext []byte
	var keyVersion int32
	var credentialVersion int64
	if err := database.QueryRowContext(ctx, `SELECT password_ciphertext, password_key_version, credential_version
		FROM instance WHERE id = $1`, instanceID).Scan(&ciphertext, &keyVersion, &credentialVersion); err != nil {
		t.Fatalf("read migrated credential: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(password)) {
		t.Fatal("migrated ciphertext contains the plaintext password")
	}
	if credentialVersion != 1 {
		t.Fatalf("credential version = %d, want 1", credentialVersion)
	}
	var agentExpected bool
	var tokenIssuedAt, firstRegisteredAt time.Time
	var tokenRevokedAt sql.NullTime
	if err := database.QueryRowContext(ctx, `SELECT agent_expected, agent_token_issued_at,
		agent_token_revoked_at, agent_first_registered_at FROM instance WHERE id = $1`, instanceID).
		Scan(&agentExpected, &tokenIssuedAt, &tokenRevokedAt, &firstRegisteredAt); err != nil {
		t.Fatalf("read migrated Agent lifecycle: %v", err)
	}
	if !agentExpected || tokenIssuedAt.IsZero() || firstRegisteredAt.IsZero() || tokenRevokedAt.Valid {
		t.Fatalf("migrated Agent lifecycle = expected %t issued %s revoked %v first %s",
			agentExpected, tokenIssuedAt, tokenRevokedAt, firstRegisteredAt)
	}
	keyring, err := instance.OpenCredentialKeyring(keyringDirectory, true)
	if err != nil {
		t.Fatalf("reopen migration keyring: %v", err)
	}
	decrypted, err := keyring.DecryptPassword(instanceID, ciphertext, keyVersion)
	if err != nil {
		t.Fatalf("decrypt migrated credential: %v", err)
	}
	if decrypted != password {
		t.Fatal("migrated credential did not round-trip")
	}
}

func openDatabase(t *testing.T, database string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", connectionString(database))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping database: %v", err)
	}
	return db
}

func connectionString(database string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		env("PGHOST", "localhost"), env("PGPORT", "55432"), env("PGUSER", "dbs_monitor"),
		env("PGPASSWORD", "dbs_monitor"), database)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
