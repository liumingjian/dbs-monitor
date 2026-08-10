package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestCreatedAlertRuleFiresOnNextEvaluationCycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_alert_rule_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
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
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, true)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	currentClock := fixedClock{now: now}

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}

	instanceID := uuid.New()
	pgInstanceID := pgtype.UUID{Bytes: instanceID, Valid: true}
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused")
	if err != nil {
		t.Fatalf("encrypt instance credential: %v", err)
	}
	if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgInstanceID, Name: "target", Host: "localhost", Port: 5432,
		DatabaseName: "postgres", Username: "postgres", PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", map[string]any{
		"name":                       "High active connections",
		"metric_id":                  "pg.connection.active",
		"aggregation":                "latest",
		"operator":                   ">=",
		"threshold":                  10,
		"recovery_operator":          "<",
		"recovery_threshold":         5,
		"window_seconds":             60,
		"consecutive_count":          2,
		"recovery_consecutive_count": 2,
		"severity":                   "warning",
		"no_data_policy":             "mark_no_data",
		"enabled":                    true,
	}, "")
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create rule status = %d, want 201", created.StatusCode)
	}
	var createdRule struct {
		ID      uuid.UUID `json:"id"`
		Version int       `json:"version"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if createdRule.ID == uuid.Nil || createdRule.Version != 1 {
		t.Fatalf("created rule = %+v, want id and version 1", createdRule)
	}

	queries := metric.New(pool)
	seriesID, err := queries.UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgInstanceID, MetricID: "pg.connection.active",
		Labels: []byte(`{}`), LabelsKey: "{}", LastSeen: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("create metric series: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, pool, now); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 12)", seriesID, now); err != nil {
		t.Fatalf("insert breaching sample: %v", err)
	}

	eval := evaluator.New(platform, currentClock)
	for range 2 {
		if err := eval.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate rule: %v", err)
		}
	}

	var status, eventKind string
	var ruleVersion int
	var snapshot []byte
	err = pool.QueryRow(ctx, `SELECT instance.status, event.kind, event.rule_version, event.rule_snapshot
		FROM alert_instance instance
		JOIN alert_event event ON event.alert_instance_id = instance.id
		WHERE instance.rule_id = $1 AND instance.instance_id = $2 AND event.kind = 'FIRED'`,
		createdRule.ID, instanceID).Scan(&status, &eventKind, &ruleVersion, &snapshot)
	if err != nil {
		t.Fatalf("read firing state and event: %v", err)
	}
	if status != "FIRING" || eventKind != "FIRED" || ruleVersion != 1 || !json.Valid(snapshot) {
		t.Fatalf("state/event = %s/%s version=%d snapshot=%s", status, eventKind, ruleVersion, snapshot)
	}
	var ruleSnapshot struct {
		MetricID          string  `json:"metric_id"`
		Threshold         float64 `json:"threshold"`
		RecoveryThreshold float64 `json:"recovery_threshold"`
		Severity          string  `json:"severity"`
		Version           int     `json:"version"`
	}
	if err := json.Unmarshal(snapshot, &ruleSnapshot); err != nil {
		t.Fatalf("decode rule snapshot: %v", err)
	}
	if ruleSnapshot.MetricID != "pg.connection.active" || ruleSnapshot.Threshold != 10 ||
		ruleSnapshot.RecoveryThreshold != 5 || ruleSnapshot.Severity != "warning" || ruleSnapshot.Version != 1 {
		t.Fatalf("rule snapshot = %+v", ruleSnapshot)
	}
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

func (clock fixedClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

var _ clock.Clock = fixedClock{}
