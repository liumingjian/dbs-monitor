package collect_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/instance"
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

	collector := collect.New(platform, monitorpg.DirectDialer{}, clock.Real{})
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
