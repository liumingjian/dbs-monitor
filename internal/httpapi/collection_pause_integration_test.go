package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestCollectionPauseEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_pause_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
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
	currentClock := newCurrentFixedClock()
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	var adminID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM app_user WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}

	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, env("PGPASSWORD", "dbs_monitor"))
	if err != nil {
		t.Fatalf("encrypt target password: %v", err)
	}
	instanceQueries := instance.New(pool)
	if _, err := instanceQueries.CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "pause target",
		Host: env("PGHOST", "localhost"), Port: int32(envInt("PGPORT", 55432)),
		DatabaseName: env("PGDATABASE", "dbs_monitor"), Username: env("PGUSER", "dbs_monitor"),
		PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create target instance: %v", err)
	}
	agentToken := "pause-agent-token"
	agentTokenHash := sha256.Sum256([]byte(agentToken))
	if _, err := instanceQueries.RegisterAgent(ctx, instance.RegisterAgentParams{
		ID:                 pgtype.UUID{Bytes: instanceID, Valid: true},
		AgentTokenHash:     agentTokenHash[:],
		AgentTokenIssuedAt: pgtype.Timestamptz{Time: currentClock.now, Valid: true},
	}); err != nil {
		t.Fatalf("register agent: %v", err)
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

	collector := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("initial collection: %v", err)
	}
	var watermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, instanceID).Scan(&watermark); err != nil {
		t.Fatalf("read initial watermark: %v", err)
	}
	currentClock.Advance(30 * time.Second)

	ruleResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", alertRuleInput(instanceID), "")
	if ruleResponse.StatusCode != http.StatusCreated {
		ruleResponse.Body.Close()
		t.Fatalf("create alert rule status = %d, want 201", ruleResponse.StatusCode)
	}
	var createdRule struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.NewDecoder(ruleResponse.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode alert rule: %v", err)
	}
	ruleResponse.Body.Close()
	seriesID := createAlertTestSeries(t, ctx, pool, instanceID, currentClock.now)
	eval := evaluator.New(platform, currentClock, collector.WithTriggerSnapshotConnection)
	for range 2 {
		insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
		runAlertEvaluation(t, ctx, eval)
		currentClock.Advance(30 * time.Second)
	}
	var alertID uuid.UUID
	var initialAlertStatus string
	if err := pool.QueryRow(ctx, `SELECT id, status FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2 AND status <> 'RECOVERED'`, createdRule.ID, instanceID).Scan(&alertID, &initialAlertStatus); err != nil {
		t.Fatalf("read firing alert: %v", err)
	}
	if initialAlertStatus != "FIRING" {
		t.Fatalf("alert status after two breaches = %s, want FIRING", initialAlertStatus)
	}

	pauseURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/pause", server.URL, instanceID)
	paused := requestJSON(t, client, http.MethodPut, pauseURL, map[string]any{"paused": true, "reason": "planned retirement"}, "")
	if paused.StatusCode != http.StatusOK {
		paused.Body.Close()
		t.Fatalf("pause status = %d, want 200", paused.StatusCode)
	}
	var pauseBody struct {
		Paused    bool       `json:"paused"`
		Reason    string     `json:"reason"`
		UpdatedBy uuid.UUID  `json:"updated_by"`
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(paused.Body).Decode(&pauseBody); err != nil {
		t.Fatalf("decode pause response: %v", err)
	}
	paused.Body.Close()
	if !pauseBody.Paused || pauseBody.Reason != "planned retirement" || pauseBody.UpdatedBy != adminID ||
		pauseBody.UpdatedAt == nil || !pauseBody.UpdatedAt.Equal(currentClock.now) {
		t.Fatalf("pause response = %+v", pauseBody)
	}
	assertPauseEvent(t, ctx, pool, alertID, adminID, "FROZEN", 1)
	pausedRuleResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", alertRuleInput(instanceID), "")
	if pausedRuleResponse.StatusCode != http.StatusCreated {
		pausedRuleResponse.Body.Close()
		t.Fatalf("create rule while paused status = %d, want 201", pausedRuleResponse.StatusCode)
	}
	var pausedRule struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.NewDecoder(pausedRuleResponse.Body).Decode(&pausedRule); err != nil {
		t.Fatalf("decode rule created while paused: %v", err)
	}
	pausedRuleResponse.Body.Close()

	instances := getResponse(t, client, server.URL+"/api/v1/instances")
	var instanceList []struct {
		CollectionPause struct {
			Paused bool `json:"paused"`
		} `json:"collection_pause"`
	}
	if err := json.NewDecoder(instances.Body).Decode(&instanceList); err != nil {
		t.Fatalf("decode instances: %v", err)
	}
	instances.Body.Close()
	pausedCount := 0
	for _, found := range instanceList {
		if found.CollectionPause.Paused {
			pausedCount++
		}
	}
	if pausedCount != 1 {
		t.Fatalf("paused instance count = %d, want 1", pausedCount)
	}

	currentClock.Advance(time.Minute)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("paused collection pass: %v", err)
	}
	runAlertEvaluation(t, ctx, eval)
	targets, err := instance.New(pool).ListCollectionTargets(ctx)
	if err != nil || len(targets) != 0 {
		t.Fatalf("paused collection targets = %d, err %v", len(targets), err)
	}
	var pausedWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, instanceID).Scan(&pausedWatermark); err != nil {
		t.Fatalf("read paused watermark: %v", err)
	}
	if !pausedWatermark.Equal(watermark) {
		t.Fatalf("paused watermark = %s, want unchanged %s", pausedWatermark, watermark)
	}
	var frozenID uuid.UUID
	var frozenStatus string
	var frozenNoData int
	if err := pool.QueryRow(ctx, "SELECT id, status, no_data_count FROM alert_instance WHERE id = $1", alertID).
		Scan(&frozenID, &frozenStatus, &frozenNoData); err != nil {
		t.Fatalf("read frozen alert: %v", err)
	}
	if frozenID != alertID || frozenStatus != "FIRING" || frozenNoData != 0 {
		t.Fatalf("frozen alert = %s %s no-data %d", frozenID, frozenStatus, frozenNoData)
	}
	var pausedRuleAlerts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2`, pausedRule.ID, instanceID).Scan(&pausedRuleAlerts); err != nil {
		t.Fatalf("count alerts for rule created while paused: %v", err)
	}
	if pausedRuleAlerts != 0 {
		t.Fatalf("rule created while paused produced %d alert instances", pausedRuleAlerts)
	}

	seriesURL := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.active&from=%s&to=%s&step=raw",
		server.URL, instanceID,
		url.QueryEscape(currentClock.now.Add(-time.Minute).Format(time.RFC3339)),
		url.QueryEscape(currentClock.now.Add(time.Minute).Format(time.RFC3339)))
	assertUnavailability(t, client, seriesURL, "COLLECTION_PAUSED")

	var samplesBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'host.cpu.usage_percent'`, instanceID).Scan(&samplesBefore); err != nil {
		t.Fatalf("count Agent samples before pause report: %v", err)
	}
	agentReport := requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/v1/report", map[string]any{
		"instance_id": instanceID, "agent_version": "1.0.0", "timestamp": currentClock.now,
		"metrics": []map[string]any{{"metric": "host.cpu.usage_percent", "value": 42}},
	}, agentToken)
	agentReport.Body.Close()
	if agentReport.StatusCode != http.StatusNoContent {
		t.Fatalf("paused Agent report status = %d, want 204", agentReport.StatusCode)
	}
	var lastReportAt time.Time
	var samplesAfter int
	if err := pool.QueryRow(ctx, `SELECT last_report_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'AGENT'`, instanceID).Scan(&lastReportAt); err != nil {
		t.Fatalf("read paused Agent heartbeat: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'host.cpu.usage_percent'`, instanceID).Scan(&samplesAfter); err != nil {
		t.Fatalf("count Agent samples after pause report: %v", err)
	}
	if !lastReportAt.Equal(currentClock.now) || samplesAfter != samplesBefore {
		t.Fatalf("paused Agent report heartbeat %s samples %d, want %s and %d", lastReportAt, samplesAfter, currentClock.now, samplesBefore)
	}

	unpaused := requestJSON(t, client, http.MethodPut, pauseURL, map[string]any{"paused": false}, "")
	unpaused.Body.Close()
	if unpaused.StatusCode != http.StatusOK {
		t.Fatalf("unpause status = %d, want 200", unpaused.StatusCode)
	}
	assertPauseEvent(t, ctx, pool, alertID, adminID, "UNFROZEN", 1)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	runAlertEvaluation(t, ctx, eval)
	var continuedID uuid.UUID
	var alertRows int
	if err := pool.QueryRow(ctx, `SELECT min(id::text)::uuid, count(*) FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, instanceID).Scan(&continuedID, &alertRows); err != nil {
		t.Fatalf("read continued alert: %v", err)
	}
	if continuedID != alertID || alertRows != 1 {
		t.Fatalf("unfrozen alert id/count = %s/%d, want %s/1", continuedID, alertRows, alertID)
	}

	currentClock.Advance(30 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("resumed collection: %v", err)
	}
	var resumedWatermark time.Time
	if err := pool.QueryRow(ctx, `SELECT last_success_at FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, instanceID).Scan(&resumedWatermark); err != nil {
		t.Fatalf("read resumed watermark: %v", err)
	}
	if !resumedWatermark.After(watermark) {
		t.Fatalf("resumed watermark = %s, want after %s", resumedWatermark, watermark)
	}
}

func assertPauseEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, alertID, actorID uuid.UUID, kind string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_event
		WHERE alert_instance_id = $1 AND actor_id = $2 AND kind = $3
		  AND from_state = to_state`, alertID, actorID, kind).Scan(&count); err != nil {
		t.Fatalf("count %s events: %v", kind, err)
	}
	if count != want {
		t.Fatalf("%s events = %d, want %d", kind, count, want)
	}
}
