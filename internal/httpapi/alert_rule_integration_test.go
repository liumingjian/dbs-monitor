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
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestAlertRuleVersionEnablementNoDataAndDedupSemantics(t *testing.T) {
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
	currentClock := &fixedClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}

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

	targetID := createAlertTestInstance(t, ctx, pool, keyring, "target")
	otherID := createAlertTestInstance(t, ctx, pool, keyring, "out-of-scope")
	invalidInput := alertRuleInput(targetID)
	delete(invalidInput, "recovery_threshold")
	invalid := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", invalidInput, "")
	defer invalid.Body.Close()
	var invalidBody struct {
		Error struct {
			Code        string `json:"code"`
			FieldErrors []struct {
				Field string `json:"field"`
			} `json:"field_errors"`
		} `json:"error"`
	}
	if invalid.StatusCode != http.StatusBadRequest || json.NewDecoder(invalid.Body).Decode(&invalidBody) != nil ||
		invalidBody.Error.Code != "VALIDATION_FAILED" || len(invalidBody.Error.FieldErrors) != 1 ||
		invalidBody.Error.FieldErrors[0].Field != "recovery_threshold" {
		t.Fatalf("missing recovery threshold response = status %d body %+v", invalid.StatusCode, invalidBody)
	}

	ruleInput := alertRuleInput(targetID)
	delete(ruleInput, "recovery_consecutive_count")
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", ruleInput, "")
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create rule status = %d, want 201", created.StatusCode)
	}
	var createdRule struct {
		ID                       uuid.UUID `json:"id"`
		Version                  int       `json:"version"`
		RecoveryConsecutiveCount int       `json:"recovery_consecutive_count"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if createdRule.ID == uuid.Nil || createdRule.Version != 1 || createdRule.RecoveryConsecutiveCount != 2 {
		t.Fatalf("created rule = %+v, want id and version 1", createdRule)
	}

	seriesID := createAlertTestSeries(t, ctx, pool, targetID, currentClock.now)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	snapshotConnections := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	eval := evaluator.New(platform, currentClock, snapshotConnections.WithTriggerSnapshotConnection)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "PENDING", 1, 0, 0, 1)

	// A second scheduler pass at the same instant is not another rule evaluation.
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "PENDING", 1, 0, 0, 1)

	currentClock.Advance(30 * time.Second)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	runAlertEvaluation(t, ctx, eval)
	var firstAlertID uuid.UUID
	var firstTriggeredAt time.Time
	if err := pool.QueryRow(ctx, `SELECT id, first_triggered_at
		FROM alert_instance WHERE rule_id = $1 AND instance_id = $2 AND status = 'FIRING'`,
		createdRule.ID, targetID).Scan(&firstAlertID, &firstTriggeredAt); err != nil {
		t.Fatalf("read firing alert: %v", err)
	}
	if !firstTriggeredAt.Equal(currentClock.now) {
		t.Fatalf("first_triggered_at = %s, want %s", firstTriggeredAt, currentClock.now)
	}
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 2, 0, 0, 1)
	var failedSnapshotID uuid.UUID
	var failedSnapshotReason string
	if err := pool.QueryRow(ctx, `SELECT snapshot.id, snapshot.failure_reason
		FROM alert_trigger_snapshot snapshot
		WHERE snapshot.alert_instance_id = $1 AND snapshot.result = 'FAILED'`, firstAlertID).
		Scan(&failedSnapshotID, &failedSnapshotReason); err != nil {
		t.Fatalf("read failed trigger snapshot: %v", err)
	}
	if failedSnapshotID == uuid.Nil || failedSnapshotReason == "" {
		t.Fatalf("failed trigger snapshot = %s reason %q, want persisted reason", failedSnapshotID, failedSnapshotReason)
	}
	var firedSnapshotID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT trigger_snapshot_id FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'FIRED'`, firstAlertID).Scan(&firedSnapshotID); err != nil {
		t.Fatalf("read fired event trigger snapshot: %v", err)
	}
	if firedSnapshotID != failedSnapshotID {
		t.Fatalf("FIRED snapshot = %s, want %s", firedSnapshotID, failedSnapshotID)
	}
	assertTriggerSnapshotAPIResult(t, client, server.URL, firstAlertID, "FAILED")
	var nonApplicableAlertID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM alert_instance
		WHERE instance_id = $1 AND metric_id = 'pg.connection.total' ORDER BY updated_at DESC LIMIT 1`, targetID).
		Scan(&nonApplicableAlertID); err != nil {
		t.Fatalf("read non-applicable alert: %v", err)
	}
	assertTriggerSnapshotAPIResult(t, client, server.URL, nonApplicableAlertID, "NOT_APPLICABLE")
	var nonApplicableSnapshotCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_trigger_snapshot WHERE alert_instance_id = $1`, nonApplicableAlertID).
		Scan(&nonApplicableSnapshotCount); err != nil {
		t.Fatalf("count non-applicable snapshots: %v", err)
	}
	if nonApplicableSnapshotCount != 0 {
		t.Fatalf("non-applicable snapshot count = %d, want 0", nonApplicableSnapshotCount)
	}
	var nonApplicableEventReferences int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'FIRED' AND trigger_snapshot_id IS NOT NULL`, nonApplicableAlertID).
		Scan(&nonApplicableEventReferences); err != nil {
		t.Fatalf("count non-applicable event snapshot references: %v", err)
	}
	if nonApplicableEventReferences != 0 {
		t.Fatalf("non-applicable FIRED snapshot references = %d, want 0", nonApplicableEventReferences)
	}

	currentClock.Advance(30 * time.Second)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 4)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 1, 0, 1)

	ruleInput["name"] = "High active connections v2"
	updated := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/alert-rules/"+createdRule.ID.String(), ruleInput, "")
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update rule status = %d, want 200", updated.StatusCode)
	}
	var updatedRule struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(updated.Body).Decode(&updatedRule); err != nil || updatedRule.Version != 2 {
		t.Fatalf("updated rule = %+v, error = %v", updatedRule, err)
	}
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 1, 0, 1)

	disabled := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/alert-rules/"+createdRule.ID.String()+"/enabled", map[string]any{"enabled": false}, "")
	defer disabled.Body.Close()
	if disabled.StatusCode != http.StatusOK {
		t.Fatalf("disable rule status = %d, want 200", disabled.StatusCode)
	}
	var disabledRule struct {
		Enabled          bool       `json:"enabled"`
		Version          int        `json:"version"`
		EnabledUpdatedBy *uuid.UUID `json:"enabled_updated_by"`
		EnabledUpdatedAt *time.Time `json:"enabled_updated_at"`
	}
	if err := json.NewDecoder(disabled.Body).Decode(&disabledRule); err != nil {
		t.Fatalf("decode disabled rule: %v", err)
	}
	if disabledRule.Enabled || disabledRule.Version != 2 || disabledRule.EnabledUpdatedBy == nil || disabledRule.EnabledUpdatedAt == nil {
		t.Fatalf("disabled rule audit = %+v", disabledRule)
	}

	currentClock.Advance(30 * time.Second)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 1, 0, 1)

	enabled := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/alert-rules/"+createdRule.ID.String()+"/enabled", map[string]any{"enabled": true}, "")
	enabled.Body.Close()
	if enabled.StatusCode != http.StatusOK {
		t.Fatalf("enable rule status = %d, want 200", enabled.StatusCode)
	}
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 0, 0, 2)
	assertAlertIdentity(t, ctx, pool, createdRule.ID, targetID, firstAlertID, firstTriggeredAt)

	// Sustained anomalies update the unresolved lifecycle instead of inserting duplicates.
	currentClock.Advance(30 * time.Second)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	runAlertEvaluation(t, ctx, eval)
	assertAlertIdentity(t, ctx, pool, createdRule.ID, targetID, firstAlertID, firstTriggeredAt)
	var triggerSnapshotCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_trigger_snapshot WHERE alert_instance_id = $1`, firstAlertID).Scan(&triggerSnapshotCount); err != nil {
		t.Fatalf("count trigger snapshots: %v", err)
	}
	if triggerSnapshotCount != 1 {
		t.Fatalf("trigger snapshots after sustained firing = %d, want 1", triggerSnapshotCount)
	}

	// Two due evaluations without a window sample enter NO_DATA without closing the firing lifecycle.
	currentClock.Advance(90 * time.Second)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 0, 1, 2)
	currentClock.Advance(30 * time.Second)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "NO_DATA", 0, 0, 2, 2)
	assertAlertIdentity(t, ctx, pool, createdRule.ID, targetID, firstAlertID, firstTriggeredAt)

	currentClock.Advance(30 * time.Second)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
	runAlertEvaluation(t, ctx, eval)
	assertAlertState(t, ctx, pool, createdRule.ID, targetID, "FIRING", 0, 0, 0, 2)

	for recoveryStep := range 2 {
		currentClock.Advance(30 * time.Second)
		insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 4)
		runAlertEvaluation(t, ctx, eval)
		var status string
		var recoveryCount int
		var stepRecoveredAt *time.Time
		if err := pool.QueryRow(ctx, `SELECT status, recovery_count, recovered_at FROM alert_instance WHERE id = $1`, firstAlertID).
			Scan(&status, &recoveryCount, &stepRecoveredAt); err != nil {
			t.Fatalf("read recovery step %d: %v", recoveryStep+1, err)
		}
		wantStatus := "FIRING"
		if recoveryStep == 1 {
			wantStatus = "RECOVERED"
		}
		if status != wantStatus || recoveryCount != recoveryStep+1 || (recoveryStep == 1 && stepRecoveredAt == nil) {
			t.Fatalf("recovery step %d = status %s count %d recovered_at %v, want %s count %d",
				recoveryStep+1, status, recoveryCount, stepRecoveredAt, wantStatus, recoveryStep+1)
		}
	}
	var firstRuleVersion, finalRuleVersion int
	var firstSnapshot, finalSnapshot []byte
	var recoveredAt time.Time
	if err := pool.QueryRow(ctx, `SELECT first_rule_version, first_rule_snapshot, rule_version, rule_snapshot, recovered_at
		FROM alert_instance WHERE id = $1`, firstAlertID).
		Scan(&firstRuleVersion, &firstSnapshot, &finalRuleVersion, &finalSnapshot, &recoveredAt); err != nil {
		t.Fatalf("read recovered lifecycle snapshots: %v", err)
	}
	if firstRuleVersion != 1 || finalRuleVersion != 2 || !json.Valid(firstSnapshot) || !json.Valid(finalSnapshot) || !recoveredAt.Equal(currentClock.now) {
		t.Fatalf("lifecycle snapshots = first %d/%s final %d/%s recovered %s", firstRuleVersion, firstSnapshot, finalRuleVersion, finalSnapshot, recoveredAt)
	}
	var recoveredEventVersion int
	var recoveredEventSnapshot []byte
	if err := pool.QueryRow(ctx, `SELECT rule_version, rule_snapshot FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'RECOVERED'`, firstAlertID).
		Scan(&recoveredEventVersion, &recoveredEventSnapshot); err != nil {
		t.Fatalf("read recovered event snapshot: %v", err)
	}
	if recoveredEventVersion != 2 || !json.Valid(recoveredEventSnapshot) {
		t.Fatalf("recovered event snapshot = version %d snapshot %s", recoveredEventVersion, recoveredEventSnapshot)
	}

	// A breach after recovery creates a new lifecycle; the old one remains immutable history.
	for range 2 {
		currentClock.Advance(30 * time.Second)
		insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
		runAlertEvaluation(t, ctx, eval)
	}
	var totalLifecycles, unresolvedLifecycles int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status <> 'RECOVERED')
		FROM alert_instance WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, targetID).
		Scan(&totalLifecycles, &unresolvedLifecycles); err != nil {
		t.Fatalf("count alert lifecycles: %v", err)
	}
	if totalLifecycles != 2 || unresolvedLifecycles != 1 {
		t.Fatalf("alert lifecycles = total %d unresolved %d, want 2/1", totalLifecycles, unresolvedLifecycles)
	}
	var outOfScopeAlerts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_instance WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, otherID).Scan(&outOfScopeAlerts); err != nil {
		t.Fatalf("count out-of-scope alerts: %v", err)
	}
	if outOfScopeAlerts != 0 {
		t.Fatalf("out-of-scope alert rows = %d, want 0", outOfScopeAlerts)
	}
}

func alertRuleInput(instanceID uuid.UUID) map[string]any {
	return map[string]any{
		"name": "High active connections", "metric_id": "pg.connection.active",
		"aggregation": "latest", "operator": ">=", "threshold": 10,
		"recovery_operator": "<", "recovery_threshold": 5,
		"window_seconds": 60, "consecutive_count": 2, "recovery_consecutive_count": 2,
		"severity": "warning", "no_data_policy": "mark_no_data",
		"scope": "INSTANCES", "instance_ids": []uuid.UUID{instanceID},
		"evaluation_interval_seconds": 30, "enabled": true,
	}
}

func createAlertTestInstance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, keyring *instance.CredentialKeyring, name string) uuid.UUID {
	t.Helper()
	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused")
	if err != nil {
		t.Fatalf("encrypt instance credential: %v", err)
	}
	if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: name, Host: "localhost", Port: 5432,
		DatabaseName: "postgres", Username: "postgres", PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create instance %q: %v", name, err)
	}
	return instanceID
}

func createAlertTestSeries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, now time.Time) int64 {
	t.Helper()
	seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true}, MetricID: "pg.connection.active",
		Labels: []byte(`{}`), LabelsKey: "{}", LastSeen: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("create metric series: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, pool, now); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	return seriesID
}

func insertAlertTestSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seriesID int64, at time.Time, value float64) {
	t.Helper()
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, at, value); err != nil {
		t.Fatalf("insert alert test sample: %v", err)
	}
}

func assertTriggerSnapshotAPIResult(t *testing.T, client *http.Client, serverURL string, alertInstanceID uuid.UUID, want string) {
	t.Helper()
	response := requestJSON(t, client, http.MethodGet, serverURL+"/api/v1/alert-instances/"+alertInstanceID.String()+"/trigger-snapshot", nil, "")
	defer response.Body.Close()
	var body struct {
		Result        string `json:"result"`
		FailureReason string `json:"failure_reason"`
		Sessions      []any  `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode trigger snapshot response: %v", err)
	}
	if response.StatusCode != http.StatusOK || body.Result != want || body.Sessions == nil {
		t.Fatalf("trigger snapshot response = status %d body %+v, want 200 result %s", response.StatusCode, body, want)
	}
	if want == "FAILED" && body.FailureReason == "" {
		t.Fatal("failed trigger snapshot response has no reason")
	}
}

func runAlertEvaluation(t *testing.T, ctx context.Context, service *evaluator.Service) {
	t.Helper()
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate alert rules: %v", err)
	}
}

func assertAlertState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ruleID, instanceID uuid.UUID, status string, breach, recovery, noData, version int) {
	t.Helper()
	var gotStatus string
	var gotBreach, gotRecovery, gotNoData, gotVersion int
	if err := pool.QueryRow(ctx, `SELECT status, breach_count, recovery_count, no_data_count, rule_version
		FROM alert_instance WHERE rule_id = $1 AND instance_id = $2 AND status <> 'RECOVERED'`, ruleID, instanceID).
		Scan(&gotStatus, &gotBreach, &gotRecovery, &gotNoData, &gotVersion); err != nil {
		t.Fatalf("read alert state: %v", err)
	}
	if gotStatus != status || gotBreach != breach || gotRecovery != recovery || gotNoData != noData || gotVersion != version {
		t.Fatalf("alert state = %s counts %d/%d/%d version %d, want %s %d/%d/%d version %d",
			gotStatus, gotBreach, gotRecovery, gotNoData, gotVersion, status, breach, recovery, noData, version)
	}
}

func assertAlertIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ruleID, instanceID, wantID uuid.UUID, wantFirstTriggeredAt time.Time) {
	t.Helper()
	var gotID uuid.UUID
	var gotFirstTriggeredAt time.Time
	var unresolved int
	if err := pool.QueryRow(ctx, `SELECT min(id::text)::uuid, min(first_triggered_at), count(*)
		FROM alert_instance WHERE rule_id = $1 AND instance_id = $2 AND status <> 'RECOVERED'`, ruleID, instanceID).
		Scan(&gotID, &gotFirstTriggeredAt, &unresolved); err != nil {
		t.Fatalf("read alert identity: %v", err)
	}
	if unresolved != 1 || gotID != wantID || !gotFirstTriggeredAt.Equal(wantFirstTriggeredAt) {
		t.Fatalf("alert identity = %s at %s count %d, want %s at %s count 1", gotID, gotFirstTriggeredAt, unresolved, wantID, wantFirstTriggeredAt)
	}
}

type fixedClock struct {
	now time.Time
}

func (clock *fixedClock) Now() time.Time { return clock.now }

func (clock *fixedClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

func (clock *fixedClock) Advance(duration time.Duration) { clock.now = clock.now.Add(duration) }

var _ clock.Clock = (*fixedClock)(nil)
