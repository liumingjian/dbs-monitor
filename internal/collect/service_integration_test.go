package collect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestServerDirectCollectionAndAlertLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_collect_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}

	instanceID := uuid.New()
	pgID := pgtype.UUID{Bytes: instanceID, Valid: true}
	_, err = instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgID, Name: "target", Host: env("PGHOST", "localhost"),
		Port: int32(envInt("PGPORT", 55432)), DatabaseName: env("PGDATABASE", "dbs_monitor"),
		Username: env("PGUSER", "dbs_monitor"), Password: env("PGPASSWORD", "dbs_monitor"),
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	dialer := &countingDialer{}
	collector := New(platform, dialer, clock.Real{})
	eval := evaluator.New(platform, clock.Real{})

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

	for range 3 {
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("collect breaching sample: %v", err)
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate breach: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.FIRING)
	if dialer.calls != 3 {
		t.Fatalf("dial count after two runs = %d, want 3 (two fresh probes and one cached query connection)", dialer.calls)
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
	if successfulTasks != 2 {
		t.Fatalf("successful task count = %d, want 2", successfulTasks)
	}
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
	if statementTimeout != "4s" {
		t.Fatalf("statement_timeout = %q, want 4s", statementTimeout)
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
	for range 2 {
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("record unreachable target: %v", err)
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate no data: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.NO_DATA)
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
	for range 2 {
		if err := collector.RunOnce(ctx); err != nil {
			t.Fatalf("collect recovery sample: %v", err)
		}
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate recovery: %v", err)
		}
	}
	assertStatus(t, ctx, pool, pgID, alerting.RECOVERED)
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
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB); err != nil {
		t.Fatalf("migrate isolation test database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open isolation platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	observedAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if err := metric.EnsurePartitions(ctx, platform, observedAt); err != nil {
		t.Fatalf("ensure isolation test partitions: %v", err)
	}
	for _, host := range []string{"slow.invalid", "fast.invalid"} {
		id := uuid.New()
		if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
			ID: pgtype.UUID{Bytes: id, Valid: true}, Name: host, Host: host, Port: 5432,
			DatabaseName: "postgres", Username: "monitor", Password: "secret",
		}); err != nil {
			t.Fatalf("create %s instance: %v", host, err)
		}
	}
	service, err := NewWithConfig(platform, delayedFailureDialer{}, fixedClock{now: observedAt}, Config{ProbeConcurrency: 2, QueryConcurrency: 1})
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

func assertLifecycleEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID pgtype.UUID) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT DISTINCT event.kind, event.rule_version, event.rule_snapshot
		FROM alert_event event
		JOIN alert_instance instance ON instance.id = event.alert_instance_id
		WHERE instance.instance_id = $1`, instanceID)
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
		FROM alert_instance WHERE instance_id = $1`, instanceID).Scan(&got, &breach, &recovery, &noData, &before)
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

func (dialer *countingDialer) Dial(ctx context.Context, connectionString string) (*monitorpg.TargetConn, error) {
	dialer.calls++
	return (monitorpg.DirectDialer{}).Dial(ctx, connectionString)
}

type delayedFailureDialer struct{}

func (delayedFailureDialer) Dial(ctx context.Context, connectionString string) (*monitorpg.TargetConn, error) {
	target, _ := url.Parse(connectionString)
	if target.Hostname() == "slow.invalid" {
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
