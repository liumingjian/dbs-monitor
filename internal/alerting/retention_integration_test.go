package alerting_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestDeleteRecoveredAlertHistoryAtNinetyDayBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_alert_retention_%d", os.Getpid())
	admin := openRetentionDatabase(t, retentionEnv("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	migrationDatabase := openRetentionDatabase(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDatabase, filepath.Join(t.TempDir(), "credentials")); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDatabase.Close()

	pool, err := pgxpool.New(ctx, retentionConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()

	instanceID := uuid.New()
	if _, err := pool.Exec(ctx, `WITH identity AS (
		INSERT INTO instance_identity (id, name) VALUES ($1, 'retention-target') RETURNING id
	)
	INSERT INTO instance (id, name, host, port, database_name, username, password_ciphertext, password_key_version)
	SELECT id, 'retention-target', 'localhost', 5432, 'postgres', 'postgres', '\x01', 1 FROM identity`, instanceID); err != nil {
		t.Fatalf("create retention target: %v", err)
	}

	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-90 * 24 * time.Hour)
	boundaryID := insertRetentionAlert(t, ctx, pool, instanceID, "boundary", "RECOVERED", cutoff)
	newerID := insertRetentionAlert(t, ctx, pool, instanceID, "newer", "RECOVERED", cutoff.Add(time.Microsecond))
	unresolvedID := insertRetentionAlert(t, ctx, pool, instanceID, "unresolved", "FIRING", cutoff.Add(-24*time.Hour))
	if _, err := pool.Exec(ctx, `INSERT INTO alert_event
		(alert_instance_id, rule_id, rule_version, kind, from_state, to_state, rule_snapshot, evaluated_at)
		VALUES ($1, '00000000-0000-0000-0000-000000000061', 1, 'RECOVERED', 'FIRING', 'RECOVERED', '{}', $2)`, boundaryID, cutoff); err != nil {
		t.Fatalf("create retained alert event: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_trigger_snapshot
		(alert_instance_id, captured_at, result) VALUES ($1, $2, 'SUCCESS')`, boundaryID, cutoff); err != nil {
		t.Fatalf("create retained trigger snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO performance_event
		(alert_instance_id, event_type, derived_at) VALUES ($1, 'LOCK_BLOCKING', $2)`, boundaryID, cutoff); err != nil {
		t.Fatalf("create retained performance event: %v", err)
	}

	deleted, err := alerting.DeleteRecoveredAlertHistory(ctx, &db.Pool{Pool: pool}, now)
	if err != nil {
		t.Fatalf("delete recovered alert history: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted alert instances = %d, want 1", deleted)
	}

	for _, retainedID := range []uuid.UUID{newerID, unresolvedID} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM alert_instance WHERE id = $1)", retainedID).Scan(&exists); err != nil {
			t.Fatalf("check retained alert %s: %v", retainedID, err)
		}
		if !exists {
			t.Errorf("alert %s was deleted before reaching the retention boundary", retainedID)
		}
	}
	for _, table := range []string{"alert_instance", "alert_event", "alert_trigger_snapshot", "performance_event"} {
		var count int
		column := "alert_instance_id"
		if table == "alert_instance" {
			column = "id"
		}
		if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s = $1", table, column), boundaryID).Scan(&count); err != nil {
			t.Fatalf("count deleted %s rows: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows after cascade = %d, want 0", table, count)
		}
	}
}

func insertRetentionAlert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, dimension, status string, changedAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var recoveredAt any
	if status == "RECOVERED" {
		recoveredAt = changedAt
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(id, instance_id, metric_id, status, updated_at, rule_id, rule_version, severity,
		 rule_snapshot, metric_dimension_key, first_triggered_at, first_rule_version,
		 first_rule_snapshot, recovered_at)
		VALUES ($1, $2, 'pg.connection.total', $3, $4,
		 '00000000-0000-0000-0000-000000000061', 1, 'critical', '{}', $5, $4, 1, '{}', $6)`,
		id, instanceID, status, changedAt, dimension, recoveredAt); err != nil {
		t.Fatalf("insert %s alert: %v", dimension, err)
	}
	return id
}

func openRetentionDatabase(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", retentionConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		t.Fatalf("ping database: %v", err)
	}
	return database
}

func retentionConnectionString(databaseName string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		retentionEnv("PGHOST", "localhost"), retentionEnv("PGPORT", "55432"), retentionEnv("PGUSER", "dbs_monitor"),
		retentionEnv("PGPASSWORD", "dbs_monitor"), databaseName)
}

func retentionEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
