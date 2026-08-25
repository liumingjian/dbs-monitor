package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// unreachableDialer fails every dial immediately, so a dispatched run finishes
// without touching a monitored instance. This test is about what the scheduler
// decides, not about what a collection task returns.
type unreachableDialer struct{}

func (unreachableDialer) Dial(context.Context, *pgx.ConnConfig) (*monitorpg.TargetConn, error) {
	return nil, errors.New("dial refused by test")
}

// The collection loop is driven entirely through the clock seam: a
// clock.Manual fires the ticker, and nothing in this test waits on wall time.
// A platform that stalls past several dues must coalesce them, running the
// newest due once and recording every due it missed as a backpressure skip.
func TestRunCoalescesMissedDuesIntoBackpressureSkips(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const probeInterval = time.Minute
	const idleInterval = time.Hour
	const stall = 3 * time.Minute

	platform, keyring := newRunTestPlatform(t, ctx)
	instanceID := uuid.New()
	targetID := pgtype.UUID{Bytes: instanceID, Valid: true}
	createRunTestInstance(t, ctx, platform, keyring, instanceID)

	// Only the probe accrues inside the stall; every other task is pushed out
	// of the window so the expected skip count comes from one task.
	for _, task := range scheduledTasks() {
		interval := idleInterval
		if task.ID == metric.TaskProbe {
			interval = probeInterval
		}
		if _, err := platform.Exec(ctx, `INSERT INTO collection_task_config (instance_id, task_id, interval_seconds)
			VALUES ($1, $2, $3) ON CONFLICT (instance_id, task_id) DO UPDATE SET interval_seconds = EXCLUDED.interval_seconds`,
			targetID, task.ID, int32(interval/time.Second)); err != nil {
			t.Fatalf("configure %s interval: %v", task.ID, err)
		}
	}

	base := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	currentClock := clock.NewManual(base)
	if err := metric.EnsurePartitions(ctx, platform, base); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	collector := New(platform, unreachableDialer{}, currentClock, keyring)
	health := platformhealth.NewStore("3.0.0", base.Add(-time.Hour), log.New(io.Discard, "", 0))
	collector.SetPlatformHealth(health)

	cycled := make(chan struct{})
	collector.cycled = cycled
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go collector.Run(runCtx, time.Second)
	if err := currentClock.AwaitTicker(ctx, 1); err != nil {
		t.Fatalf("the collection loop never subscribed to the clock: %v", err)
	}

	currentClock.Advance(stall)
	select {
	case <-cycled:
	case <-ctx.Done():
		t.Fatal("the collection loop never completed a cycle: the ticker did not fire")
	}

	firstDue := nextDueAfter(base, initialPhase(instanceID.String(), metric.TaskProbe, probeInterval), probeInterval)
	var accrued int64
	for due := firstDue; !due.After(base.Add(stall)); due = due.Add(probeInterval) {
		accrued++
	}
	if accrued < 2 {
		t.Fatalf("the stall crossed %d probe dues, want at least 2 for a coalescing test", accrued)
	}
	// The newest due survives as the pending run; every earlier one is a skip.
	wantSkipped := accrued - 1

	source := health.Source(platformhealth.SourceCollectionScheduler)
	if source.SkippedBackpressure == nil || *source.SkippedBackpressure != wantSkipped {
		t.Fatalf("skipped_backpressure = %v, want %d after a %s stall across %d dues",
			source.SkippedBackpressure, wantSkipped, stall, accrued)
	}

	var lastResult string
	var lastDueAt time.Time
	if err := platform.QueryRow(ctx, `SELECT last_result, last_due_at FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = $2`, targetID, metric.TaskProbe).Scan(&lastResult, &lastDueAt); err != nil {
		t.Fatalf("read probe task state: %v", err)
	}
	// The state row carries the newest missed due, not the oldest: a stall
	// leaves one row per task, never a row per missed due. PostgreSQL stores
	// timestamps to the microsecond, so the expectation is truncated to match.
	wantDue := firstDue.Add(time.Duration(wantSkipped-1) * probeInterval).Truncate(time.Microsecond)
	if lastDueAt.UTC().Before(wantDue) {
		t.Fatalf("last_due_at = %s, want at least the newest skipped due %s", lastDueAt.UTC(), wantDue)
	}
}

// A cycle that crosses no due records no skip. Without this control the count
// above would pass for a scheduler that skipped everything.
func TestRunWithoutAMissedDueRecordsNoBackpressureSkip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	platform, keyring := newRunTestPlatform(t, ctx)
	instanceID := uuid.New()
	targetID := pgtype.UUID{Bytes: instanceID, Valid: true}
	createRunTestInstance(t, ctx, platform, keyring, instanceID)

	for _, task := range scheduledTasks() {
		if _, err := platform.Exec(ctx, `INSERT INTO collection_task_config (instance_id, task_id, interval_seconds)
			VALUES ($1, $2, 3600) ON CONFLICT (instance_id, task_id) DO UPDATE SET interval_seconds = 3600`,
			targetID, task.ID); err != nil {
			t.Fatalf("configure %s interval: %v", task.ID, err)
		}
	}

	base := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	currentClock := clock.NewManual(base)
	if err := metric.EnsurePartitions(ctx, platform, base); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	collector := New(platform, unreachableDialer{}, currentClock, keyring)
	health := platformhealth.NewStore("3.0.0", base.Add(-time.Hour), log.New(io.Discard, "", 0))
	collector.SetPlatformHealth(health)

	cycled := make(chan struct{})
	collector.cycled = cycled
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go collector.Run(runCtx, time.Second)
	if err := currentClock.AwaitTicker(ctx, 1); err != nil {
		t.Fatalf("the collection loop never subscribed to the clock: %v", err)
	}

	currentClock.Advance(90 * time.Second)
	select {
	case <-cycled:
	case <-ctx.Done():
		t.Fatal("the collection loop never completed a cycle: the ticker did not fire")
	}

	source := health.Source(platformhealth.SourceCollectionScheduler)
	if source.SkippedBackpressure == nil || *source.SkippedBackpressure != 0 {
		t.Fatalf("skipped_backpressure = %v, want 0 when no due was missed", source.SkippedBackpressure)
	}
}

func newRunTestPlatform(t *testing.T, ctx context.Context) (*db.Pool, *instance.CredentialKeyring) {
	t.Helper()
	databaseName := fmt.Sprintf("dbs_monitor_run_%d_%s", os.Getpid(), uuid.New().String()[:8])
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
		defer admin.Close()
		admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})

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
	t.Cleanup(pool.Close)
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	return &db.Pool{Pool: pool}, keyring
}

func createRunTestInstance(t *testing.T, ctx context.Context, platform *db.Pool, keyring *instance.CredentialKeyring, instanceID uuid.UUID) {
	t.Helper()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, env("PGPASSWORD", "dbs_monitor"))
	if err != nil {
		t.Fatalf("encrypt instance password: %v", err)
	}
	if _, err := instance.New(platform.Pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "target", Host: env("PGHOST", "localhost"),
		Port: int32(envInt("PGPORT", 55432)), DatabaseName: env("PGDATABASE", "dbs_monitor"),
		Username: env("PGUSER", "dbs_monitor"), PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
}
