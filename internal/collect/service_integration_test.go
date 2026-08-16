package collect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/capability"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestAcceptance_AC_09_F5_ServerDirectCollectionAndAlertLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_collect_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Fatalf("install pg_stat_statements in monitored target: %v", err)
	}
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}

	instanceID := uuid.New()
	pgID := pgtype.UUID{Bytes: instanceID, Valid: true}
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, env("PGPASSWORD", "dbs_monitor"))
	if err != nil {
		t.Fatalf("encrypt instance password: %v", err)
	}
	_, err = instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgID, Name: "target", Host: env("PGHOST", "localhost"),
		Port: int32(envInt("PGPORT", 55432)), DatabaseName: env("PGDATABASE", "dbs_monitor"),
		Username: env("PGUSER", "dbs_monitor"), PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	dialer := &countingDialer{}
	currentClock := &fixedClock{now: time.Now().UTC()}
	if err := metric.EnsurePartitions(ctx, platform, currentClock.now); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	collector := New(platform, dialer, currentClock, keyring)
	health := platformhealth.NewStore("3.0.0", currentClock.now.Add(-time.Hour), log.New(io.Discard, "", 0))
	collector.SetPlatformHealth(health)
	eval := evaluator.New(platform, currentClock, collector.WithTriggerSnapshotConnection)

	extra := make([]*pgx.Conn, 25)
	for index := range extra {
		extra[index], err = pgx.Connect(ctx, connectionString(env("PGDATABASE", "dbs_monitor")))
		if err != nil {
			t.Fatalf("open extra target connection: %v", err)
		}
	}
	defer func() {
		for _, conn := range extra {
			if conn != nil {
				conn.Close(context.Background())
			}
		}
	}()

	for cycle := range 3 {
		if cycle > 0 {
			currentClock.Advance(30 * time.Second)
		}
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("collect breaching sample: %v", err)
		}
		if cycle == 0 {
			var result string
			var code, message sql.NullString
			if err := pool.QueryRow(ctx, `SELECT last_result, last_error_code, last_error_message
				FROM instance_collection_task_state WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, pgID).
				Scan(&result, &code, &message); err != nil {
				t.Fatalf("read initial activity task result: %v", err)
			}
			if result != "SUCCESS" {
				t.Fatalf("initial activity task result=%s code=%s message=%s", result, code.String, message.String)
			}
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate breach: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.FIRING)
	if dialer.calls != 4 {
		t.Fatalf("dial count after three runs = %d, want 4 (three fresh probes and one cached query connection)", dialer.calls)
	}
	var healthyWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&healthyWatermark); err != nil {
		t.Fatalf("read healthy integrity watermark: %v", err)
	}
	var successfulTasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM instance_collection_task_state
		WHERE instance_id = $1 AND last_result = 'SUCCESS'`, pgID).Scan(&successfulTasks); err != nil {
		t.Fatalf("count successful collection tasks: %v", err)
	}
	wantSuccessfulTasks := len(scheduledTasks())
	if successfulTasks != wantSuccessfulTasks {
		t.Fatalf("successful task count = %d, want %d", successfulTasks, wantSuccessfulTasks)
	}
	var samplesBeforeEmergency int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1`, pgID).Scan(&samplesBeforeEmergency); err != nil {
		t.Fatalf("count samples before storage emergencies: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE metric_sample_20000101 PARTITION OF metric_sample
		FOR VALUES FROM ('2000-01-01T00:00:00Z') TO ('2000-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("create retention sentinel partition: %v", err)
	}
	partitionsBeforeEmergency := metricSamplePartitionNames(t, ctx, pool)
	var alertsBeforeEmergency int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alert_instance WHERE instance_id = $1", pgID).Scan(&alertsBeforeEmergency); err != nil {
		t.Fatalf("count alerts before disk emergency: %v", err)
	}
	health.Update(currentClock.now, platformhealth.DiskSource(
		96, platformhealth.DiskNormal, platformhealth.DefaultDiskThresholds(),
	))
	currentClock.Advance(30 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect at local disk emergency: %v", err)
	}
	var samplesAtDiskEmergency int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1`, pgID).Scan(&samplesAtDiskEmergency); err != nil {
		t.Fatalf("count samples at local disk emergency: %v", err)
	}
	if samplesAtDiskEmergency <= samplesBeforeEmergency {
		t.Fatalf("samples at local disk emergency = %d, want more than %d", samplesAtDiskEmergency, samplesBeforeEmergency)
	}
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&healthyWatermark); err != nil {
		t.Fatalf("refresh healthy integrity watermark at local disk emergency: %v", err)
	}
	samplesBeforeEmergency = samplesAtDiskEmergency

	capacityBudget := int64(1)
	if err := collector.SetPlatformDatabaseCapacityMonitor(&capacityBudget, platformhealth.DefaultDiskThresholds()); err != nil {
		t.Fatalf("configure platform database capacity monitor: %v", err)
	}
	(&centralScheduler{service: collector}).refreshPlatformDatabaseCapacityHealth(ctx, currentClock.now)
	capacitySource := health.Source(platformhealth.SourcePlatformDatabaseCapacity)
	if capacitySource.Status != platformhealth.StatusFailed || capacitySource.Code != "PLATFORM_DATABASE_CAPACITY_EMERGENCY_WATERMARK" {
		t.Fatalf("platform database capacity source = %+v, want emergency failure", capacitySource)
	}
	if health.Current().Status != platformhealth.StatusFailed {
		t.Fatalf("aggregate platform health = %s, want FAILED", health.Current().Status)
	}
	currentClock.Advance(30 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect at platform database capacity emergency: %v", err)
	}
	if err := eval.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate naturally at platform database capacity emergency: %v", err)
	}
	var samplesAfterEmergency, alertsAfterEmergency int
	var emergencyWatermark time.Time
	var emergencyResult, emergencyCode, emergencyMessage string
	var retentionSentinelExists bool
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1`, pgID).Scan(&samplesAfterEmergency); err != nil {
		t.Fatalf("count samples after disk emergency: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&emergencyWatermark); err != nil {
		t.Fatalf("read capacity emergency integrity watermark: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_result, last_error_code, last_error_message
		FROM instance_collection_task_state WHERE instance_id = $1 AND task_id = 'pg.probe'`, pgID).
		Scan(&emergencyResult, &emergencyCode, &emergencyMessage); err != nil {
		t.Fatalf("read capacity emergency task state: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT to_regclass('metric_sample_20000101') IS NOT NULL").Scan(&retentionSentinelExists); err != nil {
		t.Fatalf("check retention sentinel partition: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alert_instance WHERE instance_id = $1", pgID).Scan(&alertsAfterEmergency); err != nil {
		t.Fatalf("count alerts after capacity emergency: %v", err)
	}
	if samplesAfterEmergency != samplesBeforeEmergency {
		t.Fatalf("samples after capacity emergency = %d, want unchanged %d", samplesAfterEmergency, samplesBeforeEmergency)
	}
	if !emergencyWatermark.Equal(healthyWatermark) {
		t.Fatalf("watermark after capacity emergency = %s, want unchanged %s", emergencyWatermark, healthyWatermark)
	}
	if emergencyResult != "FAILED" || emergencyCode != errorCodePlatformDatabaseCapacityEmergency || emergencyMessage == "" {
		t.Fatalf("task state after capacity emergency = %s/%s/%q, want FAILED/%s/non-empty message",
			emergencyResult, emergencyCode, emergencyMessage, errorCodePlatformDatabaseCapacityEmergency)
	}
	if !retentionSentinelExists {
		t.Fatal("retention sentinel partition was removed during capacity emergency")
	}
	if partitionsAfterEmergency := metricSamplePartitionNames(t, ctx, pool); !slices.Equal(partitionsAfterEmergency, partitionsBeforeEmergency) {
		t.Fatalf("metric sample partitions changed during capacity emergency: before=%v after=%v",
			partitionsBeforeEmergency, partitionsAfterEmergency)
	}
	if alertsAfterEmergency != alertsBeforeEmergency {
		t.Fatalf("alerts after capacity emergency = %d, want unchanged %d", alertsAfterEmergency, alertsBeforeEmergency)
	}
	if _, err := pool.Exec(ctx, "UPDATE instance SET name = 'emergency control write' WHERE id = $1", pgID); err != nil {
		t.Fatalf("control-plane write during capacity emergency: %v", err)
	}
	health.Update(currentClock.now, platformhealth.PlatformDatabaseCapacitySource(
		77, 100, health.PlatformDatabaseCapacityLevel(), platformhealth.DefaultDiskThresholds(),
	))
	collector.queryConnectionMu.Lock()
	cached := collector.queryConnections[instanceID.String()]
	collector.queryConnectionMu.Unlock()
	var statementTimeout string
	if cached.conn == nil {
		t.Fatal("collection query connection was not retained")
	}
	if err := cached.conn.QueryRow(ctx, "SHOW statement_timeout").Scan(&statementTimeout); err != nil {
		t.Fatalf("read server-side statement timeout: %v", err)
	}
	if statementTimeout != "10s" {
		t.Fatalf("statement_timeout = %q, want 10s", statementTimeout)
	}
	targets, err := instance.New(pool).ListCollectionTargets(ctx)
	if err != nil || len(targets) != 1 {
		t.Fatalf("list target for backpressure check: targets=%d error=%v", len(targets), err)
	}
	var probeTask metric.Task
	for _, task := range scheduledTasks() {
		if task.ID == metric.TaskProbe {
			probeTask = task
		}
	}
	var unreachableBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.availability.reachable' AND sample.value = 0`, pgID).Scan(&unreachableBefore); err != nil {
		t.Fatalf("count unreachable samples before skip: %v", err)
	}
	skipped := newScheduledRun(targets[0], probeTask, 0, time.Now().UTC())
	if err := collector.recordUnmet(ctx, skipped, resultSkippedBackpressure, time.Time{}); err != nil {
		t.Fatalf("record probe backpressure skip: %v", err)
	}
	var skippedResult string
	if err := pool.QueryRow(ctx, `SELECT last_result FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.probe'`, pgID).Scan(&skippedResult); err != nil {
		t.Fatalf("read skipped probe state: %v", err)
	}
	var unreachableAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.availability.reachable' AND sample.value = 0`, pgID).Scan(&unreachableAfter); err != nil {
		t.Fatalf("count unreachable samples after skip: %v", err)
	}
	if skippedResult != "SKIPPED_BACKPRESSURE" || unreachableAfter != unreachableBefore {
		t.Fatalf("probe skip result=%s unreachable samples=%d, want SKIPPED_BACKPRESSURE/%d", skippedResult, unreachableAfter, unreachableBefore)
	}
	var skippedWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&skippedWatermark); err != nil {
		t.Fatalf("read skipped integrity watermark: %v", err)
	}
	if !skippedWatermark.Equal(healthyWatermark) {
		t.Fatalf("integrity watermark advanced from %s to %s after backpressure skip", healthyWatermark, skippedWatermark)
	}
	currentClock.Advance(30 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("recover after backpressure skip: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&healthyWatermark); err != nil {
		t.Fatalf("refresh healthy integrity watermark after skip recovery: %v", err)
	}

	_, err = pool.Exec(ctx, "UPDATE instance SET port = 1 WHERE id = $1", pgID)
	if err != nil {
		t.Fatalf("make target unreachable: %v", err)
	}
	for range 3 {
		currentClock.Advance(30 * time.Second)
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("record unreachable target: %v", err)
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate no data: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.NO_DATA)
	assertBuiltinStatus(t, ctx, pool, pgID, "database_unreachable", alerting.FIRING)
	var failedWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&failedWatermark); err != nil {
		t.Fatalf("read failed integrity watermark: %v", err)
	}
	if !failedWatermark.Equal(healthyWatermark) {
		t.Fatalf("integrity watermark advanced from %s to %s while tasks were unmet", healthyWatermark, failedWatermark)
	}
	var probeResult, queryResult string
	if err := pool.QueryRow(ctx, `SELECT last_result FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.probe'`, pgID).Scan(&probeResult); err != nil {
		t.Fatalf("read failed probe state: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_result FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, pgID).Scan(&queryResult); err != nil {
		t.Fatalf("read backed-off query state: %v", err)
	}
	if probeResult != "FAILED" || queryResult != "BACKOFF" {
		t.Fatalf("unreachable task results = probe %s, query %s; want FAILED/BACKOFF", probeResult, queryResult)
	}

	for index, conn := range extra {
		conn.Close(ctx)
		extra[index] = nil
	}
	_, err = pool.Exec(ctx, "UPDATE instance SET port = $2 WHERE id = $1", pgID, envInt("PGPORT", 55432))
	if err != nil {
		t.Fatalf("restore target port: %v", err)
	}
	for range 3 {
		currentClock.Advance(30 * time.Second)
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("collect recovery sample: %v", err)
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate recovery: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.RECOVERED)
	assertBuiltinStatus(t, ctx, pool, pgID, "database_unreachable", alerting.RECOVERED)
	var recoveredWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, pgID).Scan(&recoveredWatermark); err != nil {
		t.Fatalf("read recovered integrity watermark: %v", err)
	}
	if recoveredWatermark.Before(healthyWatermark) {
		t.Fatalf("recovered integrity watermark = %s, before healthy watermark %s", recoveredWatermark, healthyWatermark)
	}
	assertLifecycleEvents(t, ctx, pool, pgID)

	var points int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.connection.total'`, pgID).Scan(&points); err != nil {
		t.Fatalf("count metric points: %v", err)
	}
	if points == 0 {
		t.Fatal("pg.connection.total has no points")
	}
	var metricSampledAt, longQuerySampledAt, sessionSampledAt time.Time
	if err := pool.QueryRow(ctx, `SELECT max(sample.ts) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.query.long_running_count'`, pgID).Scan(&metricSampledAt); err != nil {
		t.Fatalf("read activity metric snapshot time: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT max(sampled_at) FROM long_query_sample_snapshot WHERE instance_id = $1", pgID).Scan(&longQuerySampledAt); err != nil {
		t.Fatalf("read long query snapshot time: %v", err)
	}
	var sessionCount int
	var sessionsTruncated bool
	if err := pool.QueryRow(ctx, `SELECT sampled_at, original_count, truncated
		FROM instance_session_snapshot WHERE instance_id = $1`, pgID).Scan(&sessionSampledAt, &sessionCount, &sessionsTruncated); err != nil {
		t.Fatalf("read latest session snapshot: %v", err)
	}
	var storedSessions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM instance_session_snapshot_entry WHERE instance_id = $1", pgID).Scan(&storedSessions); err != nil {
		t.Fatalf("count latest session snapshot entries: %v", err)
	}
	if !metricSampledAt.Equal(longQuerySampledAt) || !metricSampledAt.Equal(sessionSampledAt) {
		t.Fatalf("activity snapshot times = metric %s long-query %s sessions %s, want one snapshot",
			metricSampledAt, longQuerySampledAt, sessionSampledAt)
	}
	if storedSessions > sessionSnapshotLimit || sessionCount < storedSessions || sessionsTruncated != (sessionCount > sessionSnapshotLimit) {
		t.Fatalf("latest session snapshot = stored %d original %d truncated %t", storedSessions, sessionCount, sessionsTruncated)
	}
	var queryStatisticsEntries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM query_statistics_snapshot_entry
		WHERE instance_id = $1 AND sampled_at = (
			SELECT max(sampled_at) FROM query_statistics_snapshot WHERE instance_id = $1
		)`, pgID).Scan(&queryStatisticsEntries); err != nil {
		t.Fatalf("count query statistics snapshot entries: %v", err)
	}
	if queryStatisticsEntries == 0 || queryStatisticsEntries > queryStatisticsSnapshotLimit {
		t.Fatalf("query statistics snapshot entries = %d, want 1..%d", queryStatisticsEntries, queryStatisticsSnapshotLimit)
	}
	assertReplicationSlotSemantics(t, ctx, admin, platform, collector, targets[0], pgID, currentClock)
}

func metricSamplePartitionNames(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT child.relname
		FROM pg_inherits
		JOIN pg_class parent ON parent.oid = inhparent
		JOIN pg_class child ON child.oid = inhrelid
		WHERE parent.relname = 'metric_sample'
		ORDER BY child.relname`)
	if err != nil {
		t.Fatalf("list metric sample partitions: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan metric sample partition: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read metric sample partitions: %v", err)
	}
	return names
}

func assertReplicationSlotSemantics(
	t *testing.T,
	ctx context.Context,
	admin *sql.DB,
	platform *db.Pool,
	collector *Service,
	target instance.ListCollectionTargetsRow,
	instanceID pgtype.UUID,
	currentClock *fixedClock,
) {
	t.Helper()
	states := storedCapabilityStates(t, ctx, platform.Pool, instanceID)
	if states[metric.CapabilityTopologyHasSlot] != metric.CapabilityNotApplicable {
		t.Fatalf("initial slot capability = %s, want NOT_APPLICABLE", states[metric.CapabilityTopologyHasSlot])
	}

	slotName := fmt.Sprintf("dbs_monitor_test_%d", os.Getpid())
	admin.ExecContext(ctx, "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1", slotName)
	var createdSlot string
	if err := admin.QueryRowContext(ctx, "SELECT slot_name FROM pg_create_physical_replication_slot($1, true)", slotName).Scan(&createdSlot); err != nil {
		t.Fatalf("create physical replication slot: %v", err)
	}
	defer func() {
		admin.ExecContext(context.Background(), "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1", slotName)
	}()

	conn, err := collector.queryConnection(ctx, target)
	if err != nil {
		t.Fatalf("open target connection for slot collection: %v", err)
	}
	complete, err := capability.ProbeAndStoreSnapshot(ctx, ctx, platform, conn, instanceID, currentClock.Now().UTC())
	if err != nil || !complete {
		t.Fatalf("probe slot capability: complete=%t error=%v", complete, err)
	}
	states = storedCapabilityStates(t, ctx, platform.Pool, instanceID)
	if states[metric.CapabilityTopologyHasSlot] != metric.CapabilityPresent {
		t.Fatalf("created slot capability = %s, want PRESENT", states[metric.CapabilityTopologyHasSlot])
	}

	var slotTask metric.Task
	for _, task := range scheduledTasks() {
		if task.ID == metric.TaskReplicationSlot {
			slotTask = task
			break
		}
	}
	run := newScheduledRun(target, slotTask, 0, currentClock.Now().UTC())
	run.startedAt = currentClock.Now().UTC()
	if recorded, err := collector.recordStarted(ctx, run); err != nil || !recorded {
		t.Fatalf("record slot task start: recorded=%t error=%v", recorded, err)
	}
	var advancedLSN string
	if err := admin.QueryRowContext(ctx,
		"SELECT end_lsn::text FROM pg_replication_slot_advance($1, pg_current_wal_lsn())", slotName).Scan(&advancedLSN); err != nil {
		t.Fatalf("advance physical replication slot: %v", err)
	}
	batch, err := collector.collectQueryTask(ctx, conn, run)
	if err != nil {
		t.Fatalf("collect replication slot: %v", err)
	}
	if err := collector.recordSuccess(ctx, run, batch); err != nil {
		t.Fatalf("persist replication slot: %v", err)
	}
	var value float64
	var labels []byte
	if err := platform.QueryRow(ctx, `SELECT sample.value, series.labels
		FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.replication_slot.retained_wal_bytes'
		ORDER BY sample.ts DESC LIMIT 1`, instanceID).Scan(&value, &labels); err != nil {
		t.Fatalf("read replication slot sample: %v", err)
	}
	var decodedLabels map[string]string
	if err := json.Unmarshal(labels, &decodedLabels); err != nil {
		t.Fatalf("decode replication slot labels: %v", err)
	}
	if value < 0 || decodedLabels["slot"] != slotName {
		t.Fatalf("slot sample = value %v labels %v, want nonnegative value/slot %s", value, decodedLabels, slotName)
	}

	if _, err := admin.ExecContext(ctx, "SELECT pg_drop_replication_slot($1)", slotName); err != nil {
		t.Fatalf("drop physical replication slot: %v", err)
	}
	complete, err = capability.ProbeAndStoreSnapshot(ctx, ctx, platform, conn, instanceID, currentClock.Now().UTC())
	if err != nil || !complete {
		t.Fatalf("re-probe absent slot capability: complete=%t error=%v", complete, err)
	}
	states = storedCapabilityStates(t, ctx, platform.Pool, instanceID)
	reason, blocked := metric.MetricCapabilityBlockReason(metric.MetricReplicationSlotRetainedWAL, states)
	if states[metric.CapabilityTopologyHasSlot] != metric.CapabilityNotApplicable || !blocked || reason != metric.CapabilityBlockNotApplicableRole {
		t.Fatalf("absent slot = capability %s blocked %t reason %s", states[metric.CapabilityTopologyHasSlot], blocked, reason)
	}
}

func TestSlowProbeDoesNotBlockAnotherInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_collect_isolation_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create isolation test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })
	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate isolation test database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open isolation platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open isolation credential keyring: %v", err)
	}
	observedAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if err := metric.EnsurePartitions(ctx, platform, observedAt); err != nil {
		t.Fatalf("ensure isolation test partitions: %v", err)
	}
	for _, host := range []string{"slow.invalid", "fast.invalid"} {
		id := uuid.New()
		ciphertext, keyVersion, err := keyring.EncryptPassword(id, "secret")
		if err != nil {
			t.Fatalf("encrypt %s credential: %v", host, err)
		}
		if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
			ID: pgtype.UUID{Bytes: id, Valid: true}, Name: host, Host: host, Port: 5432,
			DatabaseName: "postgres", Username: "monitor", PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
		}); err != nil {
			t.Fatalf("create %s instance: %v", host, err)
		}
	}
	service, err := NewWithConfig(platform, delayedFailureDialer{}, fixedClock{now: observedAt}, keyring, Config{ProbeConcurrency: 2, QueryConcurrency: 1})
	if err != nil {
		t.Fatalf("create isolation collector: %v", err)
	}
	targets, err := instance.New(pool).ListCollectionTargets(ctx)
	if err != nil {
		t.Fatalf("list isolation targets: %v", err)
	}
	var probe metric.Task
	for _, task := range scheduledTasks() {
		if task.ID == metric.TaskProbe {
			probe = task
		}
	}
	scheduler := newCentralScheduler(service)
	for _, target := range targets {
		if err := service.ensureTaskStates(ctx, target.ID); err != nil {
			t.Fatalf("initialize isolation task states: %v", err)
		}
		scheduler.pending.put(newScheduledRun(target, probe, 0, observedAt))
	}
	scheduler.dispatch(ctx)
	select {
	case outcome := <-scheduler.completed:
		if outcome.run.target.Host != "fast.invalid" {
			t.Fatalf("first completed probe = %s, want fast.invalid", outcome.run.target.Host)
		}
		if outcome.err != nil {
			t.Fatalf("persist fast probe failure: %v", outcome.err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("fast probe was blocked by slow instance")
	}
	select {
	case outcome := <-scheduler.completed:
		if outcome.run.target.Host != "slow.invalid" || outcome.err != nil {
			t.Fatalf("slow probe outcome = host %s error %v", outcome.run.target.Host, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow probe did not complete")
	}
}

func TestCapabilityProbeGatesTasksAndFailsAtomically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_capability_%d", os.Getpid())
	roleName := fmt.Sprintf("dbs_capability_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	databaseIdentifier := pgx.Identifier{databaseName}.Sanitize()
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+databaseIdentifier+" WITH (FORCE)")
	admin.ExecContext(ctx, "DROP ROLE IF EXISTS "+roleIdentifier)
	if _, err := admin.ExecContext(ctx, "CREATE ROLE "+roleIdentifier+" LOGIN PASSWORD 'capability-secret'"); err != nil {
		t.Fatalf("create capability test role: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "GRANT pg_monitor TO "+roleIdentifier); err != nil {
		t.Fatalf("grant pg_monitor: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+databaseIdentifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create capability platform database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", connectionString(env("PGDATABASE", "dbs_monitor")))
		if err != nil {
			return
		}
		defer cleanup.Close()
		cleanup.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+databaseIdentifier+" WITH (FORCE)")
		cleanup.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+roleIdentifier)
	})

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate capability platform database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open capability platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open capability credential keyring: %v", err)
	}
	instanceID := uuid.New()
	pgID := pgtype.UUID{Bytes: instanceID, Valid: true}
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "capability-secret")
	if err != nil {
		t.Fatalf("encrypt capability credential: %v", err)
	}
	if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgID, Name: "capability-target", Host: env("PGHOST", "localhost"),
		Port: int32(envInt("PGPORT", 55432)), DatabaseName: databaseName,
		Username: roleName, PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create capability target: %v", err)
	}
	base := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if err := metric.EnsurePartitions(ctx, platform, base); err != nil {
		t.Fatalf("ensure capability test partitions: %v", err)
	}

	presentCollector := New(platform, monitorpg.DirectDialer{}, fixedClock{now: base}, keyring)
	if err := presentCollector.RunOnce(ctx); err != nil {
		t.Fatalf("collect with pg_monitor: %v", err)
	}
	presentCollector.closeQueryConnections()
	present := storedCapabilityStates(t, ctx, pool, pgID)
	if present[metric.CapabilityRolePGMonitor] != metric.CapabilityPresent {
		t.Fatalf("pg_monitor status = %s, want PRESENT", present[metric.CapabilityRolePGMonitor])
	}
	if present[metric.CapabilityExtensionPGStatStatements] != metric.CapabilityMissing {
		t.Fatalf("pg_stat_statements status = %s, want MISSING", present[metric.CapabilityExtensionPGStatStatements])
	}
	var queryStatisticsStarted pgtype.Timestamptz
	var queryStatisticsError string
	if err := pool.QueryRow(ctx, `SELECT last_started_at, last_error_code
		FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.stat_statements'`, pgID).Scan(&queryStatisticsStarted, &queryStatisticsError); err != nil {
		t.Fatalf("read gated query statistics task: %v", err)
	}
	var queryStatisticsSnapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM query_statistics_snapshot
		WHERE instance_id = $1`, pgID).Scan(&queryStatisticsSnapshots); err != nil {
		t.Fatalf("count gated query statistics snapshots: %v", err)
	}
	if queryStatisticsStarted.Valid {
		t.Fatalf("gated query statistics started at %v, want no start time", queryStatisticsStarted)
	}
	if queryStatisticsError != "EXTENSION_MISSING" {
		t.Fatalf("gated query statistics error = %q, want EXTENSION_MISSING", queryStatisticsError)
	}
	if queryStatisticsSnapshots != 0 {
		t.Fatalf("gated query statistics snapshots = %d, want 0", queryStatisticsSnapshots)
	}
	var samplesBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.connection.total'`, pgID).Scan(&samplesBefore); err != nil {
		t.Fatalf("count samples before revoke: %v", err)
	}
	if samplesBefore == 0 {
		t.Fatal("pg.stat_activity did not execute while pg_monitor was present")
	}
	var startedBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT last_started_at FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, pgID).Scan(&startedBefore); err != nil {
		t.Fatalf("read task start before revoke: %v", err)
	}

	if _, err := admin.ExecContext(ctx, "REVOKE pg_monitor FROM "+roleIdentifier); err != nil {
		t.Fatalf("revoke pg_monitor: %v", err)
	}
	missingCollector := New(platform, monitorpg.DirectDialer{}, fixedClock{now: base.Add(6 * time.Minute)}, keyring)
	if err := missingCollector.RunOnce(ctx); err != nil {
		t.Fatalf("collect after pg_monitor revoke: %v", err)
	}
	missingCollector.closeQueryConnections()
	missing := storedCapabilityStates(t, ctx, pool, pgID)
	if missing[metric.CapabilityRolePGMonitor] != metric.CapabilityMissing {
		t.Fatalf("pg_monitor status after revoke = %s, want MISSING", missing[metric.CapabilityRolePGMonitor])
	}
	var samplesAfter int
	var startedAfter time.Time
	var errorCode string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'pg.connection.total'`, pgID).Scan(&samplesAfter); err != nil {
		t.Fatalf("count samples after revoke: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_started_at, last_error_code FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, pgID).Scan(&startedAfter, &errorCode); err != nil {
		t.Fatalf("read gated task state: %v", err)
	}
	if samplesAfter != samplesBefore || !startedAfter.Equal(startedBefore) || errorCode != "PERMISSION_DENIED" {
		t.Fatalf("gated task samples/start/error = %d/%s/%s, want %d/%s/PERMISSION_DENIED",
			samplesAfter, startedAfter, errorCode, samplesBefore, startedBefore)
	}

	probeIndex := -1
	for index := range metric.Capabilities {
		if metric.Capabilities[index].ID == metric.CapabilityExtensionPGStatStatements {
			probeIndex = index
			break
		}
	}
	if probeIndex < 0 {
		t.Fatal("pg_stat_statements capability declaration is missing")
	}
	originalProbe := metric.Capabilities[probeIndex].Probe
	metric.Capabilities[probeIndex].Probe = "SELECT missing_column FROM pg_extension LIMIT 1"
	defer func() { metric.Capabilities[probeIndex].Probe = originalProbe }()
	unknownCollector := New(platform, monitorpg.DirectDialer{}, fixedClock{now: base.Add(12 * time.Minute)}, keyring)
	if err := unknownCollector.RunOnce(ctx); err != nil {
		t.Fatalf("collect with failed capability probe: %v", err)
	}
	unknownCollector.closeQueryConnections()
	unknown := storedCapabilityStates(t, ctx, pool, pgID)
	for _, declaration := range metric.Capabilities {
		if unknown[declaration.ID] != metric.CapabilityUnknown {
			t.Errorf("status after partial probe failure for %s = %s, want UNKNOWN", declaration.ID, unknown[declaration.ID])
		}
	}

	metric.Capabilities[probeIndex].Probe = originalProbe
	targets, err := instance.New(pool).ListCollectionTargets(ctx)
	if err != nil || len(targets) != 1 {
		t.Fatalf("list capability target: targets=%d error=%v", len(targets), err)
	}
	timeoutCollector := New(platform, monitorpg.DirectDialer{}, fixedClock{now: base.Add(18 * time.Minute)}, keyring)
	conn, err := timeoutCollector.queryConnection(ctx, targets[0])
	if err != nil {
		t.Fatalf("open capability probe connection: %v", err)
	}
	defer timeoutCollector.closeQueryConnections()
	probeCtx, cancelProbe := context.WithCancel(ctx)
	cancelProbe()
	complete, err := capability.ProbeAndStoreSnapshot(probeCtx, ctx, platform, conn, pgID, base.Add(18*time.Minute))
	if err != nil {
		t.Fatalf("persist canceled capability probe: %v", err)
	}
	if complete {
		t.Fatal("canceled capability probe was reported complete")
	}
	unknown = storedCapabilityStates(t, ctx, pool, pgID)
	for _, declaration := range metric.Capabilities {
		if unknown[declaration.ID] != metric.CapabilityUnknown {
			t.Errorf("status after canceled probe for %s = %s, want UNKNOWN", declaration.ID, unknown[declaration.ID])
		}
	}
}

func storedCapabilityStates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID pgtype.UUID) map[metric.CapabilityID]metric.CapabilityStatus {
	t.Helper()
	var encoded []byte
	if err := pool.QueryRow(ctx, "SELECT states FROM instance_capability_snapshot WHERE instance_id = $1", instanceID).Scan(&encoded); err != nil {
		t.Fatalf("read stored capability snapshot: %v", err)
	}
	states, err := metric.DecodeCapabilitySnapshot(encoded)
	if err != nil {
		t.Fatalf("decode stored capability snapshot: %v", err)
	}
	return states
}

func assertLifecycleEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID pgtype.UUID) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT DISTINCT event.kind, event.rule_version, event.rule_snapshot
		FROM alert_event event
		JOIN alert_instance instance ON instance.id = event.alert_instance_id
		WHERE instance.instance_id = $1
		  AND instance.metric_id = 'pg.connection.total'`, instanceID)
	if err != nil {
		t.Fatalf("read alert events: %v", err)
	}
	defer rows.Close()
	want := map[string]bool{
		"PENDING_STARTED": false, "FIRED": false, "UPDATED": false,
		"RECOVERED": false, "NO_DATA_ENTERED": false, "NO_DATA_EXITED": false,
	}
	for rows.Next() {
		var kind string
		var version int
		var snapshot []byte
		if err := rows.Scan(&kind, &version, &snapshot); err != nil {
			t.Fatalf("scan alert event: %v", err)
		}
		if version != 1 || !json.Valid(snapshot) {
			t.Errorf("event %s version=%d snapshot=%s, want version 1 and JSON", kind, version, snapshot)
		}
		var ruleSnapshot struct {
			MetricID          string  `json:"metric_id"`
			Threshold         float64 `json:"threshold"`
			RecoveryThreshold float64 `json:"recovery_threshold"`
			Severity          string  `json:"severity"`
			Version           int     `json:"version"`
		}
		if err := json.Unmarshal(snapshot, &ruleSnapshot); err != nil {
			t.Errorf("decode event %s rule snapshot: %v", kind, err)
		} else if ruleSnapshot.MetricID != "pg.connection.total" || ruleSnapshot.Threshold != 20 ||
			ruleSnapshot.RecoveryThreshold != 15 || ruleSnapshot.Severity != "critical" || ruleSnapshot.Version != 1 {
			t.Errorf("event %s rule snapshot = %+v", kind, ruleSnapshot)
		}
		if _, exists := want[kind]; exists {
			want[kind] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate alert events: %v", err)
	}
	for kind, found := range want {
		if !found {
			t.Errorf("alert event %s was not recorded", kind)
		}
	}
}

func assertStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID pgtype.UUID, want alerting.State) {
	t.Helper()
	var got string
	var breach, recovery, noData int
	var before pgtype.Text
	err := pool.QueryRow(ctx, `SELECT status, breach_count, recovery_count, no_data_count, state_before_no_data
		FROM alert_instance WHERE instance_id = $1 AND metric_id = 'pg.connection.total'`, instanceID).
		Scan(&got, &breach, &recovery, &noData, &before)
	if err != nil {
		t.Fatalf("get alert status: %v", err)
	}
	if alerting.State(got) != want {
		var latest float64
		pool.QueryRow(ctx, `SELECT sample.value FROM metric_sample sample
			JOIN metric_series series ON series.series_id = sample.series_id
			WHERE series.instance_id = $1 AND series.metric_id = 'pg.connection.total'
			ORDER BY sample.ts DESC LIMIT 1`, instanceID).Scan(&latest)
		t.Fatalf("alert status = %s, want %s (breach=%d recovery=%d no_data=%d before=%q latest=%v)",
			got, want, breach, recovery, noData, before.String, latest)
	}
}

func assertBuiltinStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID pgtype.UUID, identifier string, want alerting.State) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT alert.status
		FROM alert_instance alert
		JOIN alert_rule rule ON rule.id = alert.rule_id
		WHERE alert.instance_id = $1 AND rule.builtin_identifier = $2`, instanceID, identifier).Scan(&got); err != nil {
		t.Fatalf("get built-in alert status: %v", err)
	}
	if alerting.State(got) != want {
		t.Fatalf("built-in alert %s status = %s, want %s", identifier, got, want)
	}
}

func openSQL(t *testing.T, database string) *sql.DB {
	t.Helper()
	databaseHandle, err := sql.Open("pgx", connectionString(database))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := databaseHandle.Ping(); err != nil {
		databaseHandle.Close()
		t.Fatalf("ping database: %v", err)
	}
	return databaseHandle
}

func connectionString(database string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		env("PGHOST", "localhost"), envInt("PGPORT", 55432), env("PGUSER", "dbs_monitor"),
		env("PGPASSWORD", "dbs_monitor"), database)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func createTestCredentialDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create test credential directory: %v", err)
	}
	return directory
}

func envInt(name string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(os.Getenv(name), "%d", &value); err == nil && value > 0 {
		return value
	}
	return fallback
}

type countingDialer struct {
	calls int
}

func (dialer *countingDialer) Dial(ctx context.Context, config *pgx.ConnConfig) (*monitorpg.TargetConn, error) {
	dialer.calls++
	return (monitorpg.DirectDialer{}).Dial(ctx, config)
}

type delayedFailureDialer struct{}

func (delayedFailureDialer) Dial(ctx context.Context, config *pgx.ConnConfig) (*monitorpg.TargetConn, error) {
	if config.Host == "slow.invalid" {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("dial failed")
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

func (clock fixedClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

func (clock *fixedClock) Advance(duration time.Duration) { clock.now = clock.now.Add(duration) }
