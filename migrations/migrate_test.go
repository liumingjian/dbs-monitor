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
	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	instanceID := uuid.MustParse("00000000-0000-0000-0000-000000000067")
	password := env("PGPASSWORD", "dbs_monitor")
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
