package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
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
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
	defer server.Close()
	client := loginAlertTestUser(t, server, "admin", "correct horse battery staple")
	var actorID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM app_user WHERE username = 'admin'").Scan(&actorID); err != nil {
		t.Fatalf("read alert rule actor: %v", err)
	}

	templatesResponse := getResponse(t, client, server.URL+"/api/v1/alert-rule-templates")
	defer templatesResponse.Body.Close()
	var templates []api.AlertRuleTemplate
	if templatesResponse.StatusCode != http.StatusOK || json.NewDecoder(templatesResponse.Body).Decode(&templates) != nil {
		t.Fatalf("list templates status = %d", templatesResponse.StatusCode)
	}
	if len(templates) != 15 {
		t.Fatalf("alert rule templates = %d, want 15", len(templates))
	}
	var cpuTemplate api.AlertRuleTemplate
	for _, template := range templates {
		if template.Id == "cpu_high" {
			cpuTemplate = template
		}
	}
	if cpuTemplate.MetricId != "host.cpu.usage_percent" || cpuTemplate.Aggregation != api.Avg ||
		cpuTemplate.Threshold != 80 || cpuTemplate.RecoveryThreshold != 70 || cpuTemplate.ConsecutiveCount != 5 ||
		cpuTemplate.EvaluationIntervalSeconds != 60 || cpuTemplate.Severity != api.Warning {
		t.Fatalf("CPU template = %+v", cpuTemplate)
	}

	// 模板的引擎归属：CPU 是引擎无关的（处处可见、不占位），连接数填的是 connections 这个位
	// （一份两用），Slot 积压既不无关又没有位，是 PostgreSQL 私有。
	templateByID := make(map[string]api.AlertRuleTemplate, len(templates))
	for _, template := range templates {
		templateByID[template.Id] = template
	}
	assertTemplateOwnership(t, templateByID["cpu_high"], api.MetricEngineAgnostic, "")
	assertTemplateOwnership(t, templateByID["connections_high"], api.MetricEnginePostgreSQL, api.SlotConnections)
	assertTemplateOwnership(t, templateByID["replication_slot_backlog"], api.MetricEnginePostgreSQL, "")

	// 按引擎筛选：PostgreSQL 上十五条全在（今天唯一接入的引擎，行为与从前一致）；
	// 换一个什么位都还没绑定的引擎，只剩下三条引擎无关的模板——引擎私有的模板不在那里露面。
	// 第二个引擎只存在于这个测试里，生产代码里的引擎全集仍然只有 PostgreSQL。
	if listed := listTemplateIDsForEngine(t, client, server.URL, "POSTGRESQL"); len(listed) != 15 {
		t.Fatalf("templates on POSTGRESQL = %d, want 15", len(listed))
	}
	elsewhere := listTemplateIDsForEngine(t, client, server.URL, "ENGINE_UNDER_TEST")
	if !reflect.DeepEqual(elsewhere, []string{"cpu_high", "disk_usage_high", "memory_high"}) {
		t.Fatalf("templates on an engine that binds no slot = %v, want the three engine-agnostic ones", elsewhere)
	}

	fromTemplateResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/api/v1/alert-rule-templates/cpu_high/alert-rules",
		map[string]any{"name": "Custom CPU", "threshold": 90, "severity": "critical"}, "")
	defer fromTemplateResponse.Body.Close()
	var fromTemplate api.AlertRule
	if fromTemplateResponse.StatusCode != http.StatusCreated || json.NewDecoder(fromTemplateResponse.Body).Decode(&fromTemplate) != nil {
		t.Fatalf("create from template status = %d", fromTemplateResponse.StatusCode)
	}
	if fromTemplate.Name != "Custom CPU" || fromTemplate.Threshold != 90 || fromTemplate.RecoveryThreshold != 70 ||
		fromTemplate.SourceTemplateId == nil || *fromTemplate.SourceTemplateId != "cpu_high" ||
		fromTemplate.SourceTemplateVersion == nil || *fromTemplate.SourceTemplateVersion != 1 ||
		fromTemplate.IsBuiltin || fromTemplate.EffectiveNotificationPolicyName != "默认策略（继承）" {
		t.Fatalf("rule created from template = %+v", fromTemplate)
	}
	if fromTemplate.CreatedBy == nil || *fromTemplate.CreatedBy != actorID ||
		fromTemplate.UpdatedBy == nil || *fromTemplate.UpdatedBy != actorID {
		t.Fatalf("rule created from template attribution = created %v, updated %v; want actor %s",
			fromTemplate.CreatedBy, fromTemplate.UpdatedBy, actorID)
	}

	copyResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/api/v1/alert-rules/"+fromTemplate.Id.String()+"/copies",
		map[string]any{"name": "Copied CPU"}, "")
	defer copyResponse.Body.Close()
	var copied api.AlertRule
	if copyResponse.StatusCode != http.StatusCreated || json.NewDecoder(copyResponse.Body).Decode(&copied) != nil {
		t.Fatalf("copy alert rule status = %d", copyResponse.StatusCode)
	}
	if copied.Id == fromTemplate.Id || copied.Name != "Copied CPU" || copied.Threshold != fromTemplate.Threshold ||
		copied.SourceTemplateId != nil || copied.SourceTemplateVersion != nil {
		t.Fatalf("copied alert rule = %+v", copied)
	}
	if copied.CreatedBy == nil || *copied.CreatedBy != actorID || copied.UpdatedBy == nil || *copied.UpdatedBy != actorID {
		t.Fatalf("copied alert rule attribution = created %v, updated %v; want actor %s",
			copied.CreatedBy, copied.UpdatedBy, actorID)
	}
	assertAlertRuleAttribution(t, ctx, pool, fromTemplate.Id, actorID, currentClock.now, currentClock.now, nil)
	assertAlertRuleAttribution(t, ctx, pool, copied.Id, actorID, currentClock.now, currentClock.now, nil)
	deletedAt := currentClock.now
	for _, ruleID := range []uuid.UUID{copied.Id, fromTemplate.Id} {
		deleted := requestJSON(t, client, http.MethodDelete, server.URL+"/api/v1/alert-rules/"+ruleID.String(), nil, "")
		deleted.Body.Close()
		if deleted.StatusCode != http.StatusNoContent {
			t.Fatalf("delete copied/template rule status = %d, want 204", deleted.StatusCode)
		}
		assertAlertRuleAttribution(t, ctx, pool, ruleID, actorID, currentClock.now, currentClock.now, &deletedAt)
	}

	rulesResponse := getResponse(t, client, server.URL+"/api/v1/alert-rules")
	defer rulesResponse.Body.Close()
	var rules []api.AlertRule
	if rulesResponse.StatusCode != http.StatusOK || json.NewDecoder(rulesResponse.Body).Decode(&rules) != nil {
		t.Fatalf("list alert rules status = %d", rulesResponse.StatusCode)
	}
	var databaseRule api.AlertRule
	builtinCount := 0
	for _, rule := range rules {
		if rule.Id == copied.Id || rule.Id == fromTemplate.Id {
			t.Fatalf("deleted alert rule remains visible: %+v", rule)
		}
		if !rule.IsBuiltin {
			continue
		}
		builtinCount++
		if rule.BuiltinIdentifier != nil && *rule.BuiltinIdentifier == "database_unreachable" {
			databaseRule = rule
		}
	}
	if builtinCount != 3 || databaseRule.Id == uuid.Nil || !databaseRule.Enabled || databaseRule.Scope != api.ALL ||
		databaseRule.Severity != api.Critical || databaseRule.EffectiveNotificationPolicyName != "默认策略（继承）" ||
		databaseRule.CurrentAlertCount != 0 || databaseRule.LastTriggeredAt != nil {
		t.Fatalf("seeded built-in rules = count %d database rule %+v", builtinCount, databaseRule)
	}
	detailResponse := getResponse(t, client, server.URL+"/api/v1/alert-rules/"+databaseRule.Id.String())
	defer detailResponse.Body.Close()
	if detailResponse.StatusCode != http.StatusOK {
		t.Fatalf("get alert rule status = %d, want 200", detailResponse.StatusCode)
	}
	var detailedRule api.AlertRule
	if err := json.NewDecoder(detailResponse.Body).Decode(&detailedRule); err != nil {
		t.Fatalf("decode alert rule detail: %v", err)
	}
	if !reflect.DeepEqual(detailedRule, databaseRule) {
		t.Fatalf("alert rule detail = %+v, want %+v", detailedRule, databaseRule)
	}
	missingDetailResponse := getResponse(t, client, server.URL+"/api/v1/alert-rules/"+uuid.New().String())
	assertRuleErrorCode(t, missingDetailResponse, http.StatusNotFound, api.NOTFOUND)

	disabledBuiltin := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/alert-rules/"+databaseRule.Id.String()+"/enabled", map[string]any{"enabled": false}, "")
	assertRuleErrorCode(t, disabledBuiltin, http.StatusBadRequest, api.BUILTINRULEDISABLEFORBIDDEN)
	deletedBuiltin := requestJSON(t, client, http.MethodDelete,
		server.URL+"/api/v1/alert-rules/"+databaseRule.Id.String(), nil, "")
	assertRuleErrorCode(t, deletedBuiltin, http.StatusConflict, api.BUILTINRULEDELETEFORBIDDEN)
	infoInput := map[string]any{
		"name": databaseRule.Name, "metric_id": databaseRule.MetricId,
		"aggregation": databaseRule.Aggregation, "operator": databaseRule.Operator, "threshold": databaseRule.Threshold,
		"recovery_operator": databaseRule.RecoveryOperator, "recovery_threshold": databaseRule.RecoveryThreshold,
		"window_seconds": databaseRule.WindowSeconds, "consecutive_count": databaseRule.ConsecutiveCount,
		"recovery_consecutive_count": databaseRule.RecoveryConsecutiveCount, "severity": "info",
		"no_data_policy": databaseRule.NoDataPolicy, "scope": databaseRule.Scope,
		"instance_ids": databaseRule.InstanceIds, "evaluation_interval_seconds": databaseRule.EvaluationIntervalSeconds,
		"enabled": databaseRule.Enabled,
	}
	infoBuiltin := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/alert-rules/"+databaseRule.Id.String(), infoInput, "")
	assertRuleErrorCode(t, infoBuiltin, http.StatusBadRequest, api.BUILTINRULESEVERITYTOOLOW)

	targetID := createAlertTestInstance(t, ctx, pool, keyring, "target")
	otherID := createAlertTestInstance(t, ctx, pool, keyring, "out-of-scope")
	reachabilitySeriesID := createAlertTestSeriesForMetric(t, ctx, pool, targetID, currentClock.now, "pg.availability.reachable")
	snapshotConnections := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	seededEvaluator := evaluator.New(platform, currentClock, snapshotConnections.WithTriggerSnapshotConnection)
	for step := range 3 {
		insertAlertTestSample(t, ctx, pool, reachabilitySeriesID, currentClock.now, 0)
		runAlertEvaluation(t, ctx, seededEvaluator)
		if step < 2 {
			currentClock.Advance(30 * time.Second)
		}
	}
	assertAlertState(t, ctx, pool, databaseRule.Id, targetID, "FIRING", 3, 0, 0, 1)
	updatedRulesResponse := getResponse(t, client, server.URL+"/api/v1/alert-rules")
	defer updatedRulesResponse.Body.Close()
	var updatedRules []api.AlertRule
	if updatedRulesResponse.StatusCode != http.StatusOK || json.NewDecoder(updatedRulesResponse.Body).Decode(&updatedRules) != nil {
		t.Fatalf("list updated alert rules status = %d", updatedRulesResponse.StatusCode)
	}
	foundDatabaseRule := false
	for _, rule := range updatedRules {
		if rule.Id != databaseRule.Id {
			continue
		}
		foundDatabaseRule = true
		if rule.CurrentAlertCount != 2 || rule.LastTriggeredAt == nil || !rule.LastTriggeredAt.Equal(currentClock.now) {
			t.Fatalf("database rule list projection = %+v", rule)
		}
		break
	}
	if !foundDatabaseRule {
		t.Fatal("database rule missing from updated alert rule list")
	}

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
		ID                       uuid.UUID  `json:"id"`
		Version                  int        `json:"version"`
		RecoveryConsecutiveCount int        `json:"recovery_consecutive_count"`
		CreatedBy                *uuid.UUID `json:"created_by"`
		UpdatedBy                *uuid.UUID `json:"updated_by"`
		CreatedAt                time.Time  `json:"created_at"`
		UpdatedAt                time.Time  `json:"updated_at"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if createdRule.ID == uuid.Nil || createdRule.Version != 1 || createdRule.RecoveryConsecutiveCount != 2 {
		t.Fatalf("created rule = %+v, want an ID, version 1, and default recovery count 2", createdRule)
	}
	var adminID, createdBy, updatedBy, createdVersionBy uuid.UUID
	var adminPasswordHash []byte
	var createdAt, updatedAt, createdVersionAt time.Time
	if err := pool.QueryRow(ctx, `SELECT id, password_hash FROM app_user WHERE username = 'admin'`).Scan(&adminID, &adminPasswordHash); err != nil {
		t.Fatalf("read rule actor: %v", err)
	}
	if createdRule.CreatedBy == nil || *createdRule.CreatedBy != adminID ||
		createdRule.UpdatedBy == nil || *createdRule.UpdatedBy != adminID ||
		!createdRule.CreatedAt.Equal(currentClock.now) || !createdRule.UpdatedAt.Equal(currentClock.now) {
		t.Fatalf("created rule API attribution = %+v, want actor %s at %s", createdRule, adminID, currentClock.now)
	}
	if err := pool.QueryRow(ctx, `SELECT rule.created_by, rule.updated_by, rule.created_at, rule.updated_at,
		version.created_by, version.created_at
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = 1
		WHERE rule.id = $1`, createdRule.ID).Scan(
		&createdBy, &updatedBy, &createdAt, &updatedAt, &createdVersionBy, &createdVersionAt,
	); err != nil {
		t.Fatalf("read created rule attribution: %v", err)
	}
	if createdBy != adminID || updatedBy != adminID || createdVersionBy != adminID ||
		!createdAt.Equal(currentClock.now) || !updatedAt.Equal(currentClock.now) || !createdVersionAt.Equal(currentClock.now) {
		t.Fatalf("created rule attribution = actors %s/%s/%s at %s/%s/%s, want %s at %s",
			createdBy, updatedBy, createdVersionBy, createdAt, updatedAt, createdVersionAt, adminID, currentClock.now)
	}

	seriesID := createAlertTestSeries(t, ctx, pool, targetID, currentClock.now)
	insertAlertTestSample(t, ctx, pool, seriesID, currentClock.now, 12)
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
	dispositionURL := server.URL + "/api/v1/alert-instances/" + firstAlertID.String() + "/disposition"
	for _, invalidDisposition := range []map[string]any{
		{"disposition": "IGNORED"},
		{"disposition": "IGNORED", "ignore_reason_code": "OTHER", "ignore_reason_detail": "  "},
	} {
		response := requestJSON(t, client, http.MethodPut, dispositionURL, invalidDisposition, "")
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid disposition status = %d, want 400", response.StatusCode)
		}
	}
	for _, action := range []struct {
		input                        map[string]any
		wantDisposition              string
		wantStopsRepeat              bool
		wantExcludedFromHealthRollup bool
	}{
		{map[string]any{"disposition": "ACKED", "note": "Investigating"}, "ACKED", true, false},
		{map[string]any{"disposition": "IGNORED", "ignore_reason_code": "OTHER", "ignore_reason_detail": "Expected maintenance load"}, "IGNORED", false, true},
		{map[string]any{"disposition": "ACKED"}, "ACKED", true, false},
	} {
		response := requestJSON(t, client, http.MethodPut, dispositionURL, action.input, "")
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("update disposition status = %d, want 200", response.StatusCode)
		}
		var summary struct {
			Disposition              string `json:"disposition"`
			StopsRepeatNotifications bool   `json:"stops_repeat_notifications"`
			ExcludedFromHealthRollup bool   `json:"excluded_from_health_rollup"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&summary)
		response.Body.Close()
		if decodeErr != nil || summary.Disposition != action.wantDisposition ||
			summary.StopsRepeatNotifications != action.wantStopsRepeat ||
			summary.ExcludedFromHealthRollup != action.wantExcludedFromHealthRollup {
			t.Fatalf("updated disposition summary = %+v, error = %v", summary, decodeErr)
		}
	}
	dispositionResponse := requestJSON(t, client, http.MethodGet, dispositionURL, nil, "")
	defer dispositionResponse.Body.Close()
	var dispositionDetail struct {
		Disposition              string     `json:"disposition"`
		DispositionBy            *uuid.UUID `json:"disposition_by"`
		DispositionAt            *time.Time `json:"disposition_at"`
		StopsRepeatNotifications bool       `json:"stops_repeat_notifications"`
		ExcludedFromHealthRollup bool       `json:"excluded_from_health_rollup"`
		History                  []struct {
			Kind               string          `json:"kind"`
			FromDisposition    string          `json:"from_disposition"`
			ToDisposition      string          `json:"to_disposition"`
			ActorID            uuid.UUID       `json:"actor_id"`
			Note               *string         `json:"note"`
			IgnoreReasonCode   *string         `json:"ignore_reason_code"`
			IgnoreReasonDetail *string         `json:"ignore_reason_detail"`
			RuleVersion        int             `json:"rule_version"`
			CurrentValue       *float64        `json:"current_value"`
			RuleSnapshot       json.RawMessage `json:"rule_snapshot"`
			EvaluatedAt        time.Time       `json:"evaluated_at"`
			ActedAt            time.Time       `json:"acted_at"`
		} `json:"history"`
	}
	if dispositionResponse.StatusCode != http.StatusOK || json.NewDecoder(dispositionResponse.Body).Decode(&dispositionDetail) != nil {
		t.Fatalf("read disposition response status = %d", dispositionResponse.StatusCode)
	}
	if dispositionDetail.Disposition != "ACKED" || dispositionDetail.DispositionBy == nil || dispositionDetail.DispositionAt == nil ||
		!dispositionDetail.StopsRepeatNotifications || dispositionDetail.ExcludedFromHealthRollup {
		t.Fatalf("current disposition = %+v", dispositionDetail)
	}
	wantKinds := []string{"ACKED", "IGNORED", "ACKED"}
	wantFrom := []string{"NONE", "ACKED", "IGNORED"}
	for index, event := range dispositionDetail.History {
		if index >= len(wantKinds) || event.Kind != wantKinds[index] || event.FromDisposition != wantFrom[index] ||
			event.ToDisposition != wantKinds[index] || event.ActorID != *dispositionDetail.DispositionBy ||
			event.RuleVersion != 1 || event.CurrentValue == nil || *event.CurrentValue != 12 ||
			!json.Valid(event.RuleSnapshot) || !event.EvaluatedAt.Equal(firstTriggeredAt) || !event.ActedAt.Equal(currentClock.now) {
			t.Fatalf("disposition event %d = %+v", index, event)
		}
	}
	if len(dispositionDetail.History) != len(wantKinds) || dispositionDetail.History[0].Note == nil ||
		*dispositionDetail.History[0].Note != "Investigating" || dispositionDetail.History[1].IgnoreReasonCode == nil ||
		*dispositionDetail.History[1].IgnoreReasonCode != "OTHER" || dispositionDetail.History[1].IgnoreReasonDetail == nil ||
		*dispositionDetail.History[1].IgnoreReasonDetail != "Expected maintenance load" {
		t.Fatalf("disposition history = %+v", dispositionDetail.History)
	}
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

	editorID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
		VALUES ($1, 'rule-editor', $2, 'ALERT_ADMIN')`, editorID, adminPasswordHash); err != nil {
		t.Fatalf("create rule editor: %v", err)
	}
	editorClient := loginAlertTestUser(t, server, "rule-editor", "correct horse battery staple")

	ruleInput["name"] = "High active connections v2"
	updated := requestJSON(t, editorClient, http.MethodPut, server.URL+"/api/v1/alert-rules/"+createdRule.ID.String(), ruleInput, "")
	defer updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update rule status = %d, want 200", updated.StatusCode)
	}
	var updatedRule struct {
		Version   int        `json:"version"`
		CreatedBy *uuid.UUID `json:"created_by"`
		UpdatedBy *uuid.UUID `json:"updated_by"`
		UpdatedAt time.Time  `json:"updated_at"`
	}
	if err := json.NewDecoder(updated.Body).Decode(&updatedRule); err != nil {
		t.Fatalf("decode updated rule: %v", err)
	}
	if updatedRule.CreatedBy == nil || *updatedRule.CreatedBy != adminID ||
		updatedRule.UpdatedBy == nil || *updatedRule.UpdatedBy != editorID || !updatedRule.UpdatedAt.Equal(currentClock.now) {
		t.Fatalf("updated rule API attribution = %+v, want creator %s and editor %s at %s",
			updatedRule, adminID, editorID, currentClock.now)
	}
	var storedUpdatedBy, versionCreatedBy uuid.UUID
	var storedUpdatedAt, versionCreatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT rule.updated_by, rule.updated_at, version.created_by, version.created_at
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = 2
		WHERE rule.id = $1`, createdRule.ID).Scan(
		&storedUpdatedBy, &storedUpdatedAt, &versionCreatedBy, &versionCreatedAt,
	); err != nil {
		t.Fatalf("read updated rule attribution: %v", err)
	}
	if storedUpdatedBy != editorID || versionCreatedBy != editorID ||
		!storedUpdatedAt.Equal(currentClock.now) || !versionCreatedAt.Equal(currentClock.now) {
		t.Fatalf("updated rule attribution = actors %s/%s at %s/%s, want %s at %s",
			storedUpdatedBy, versionCreatedBy, storedUpdatedAt, versionCreatedAt, editorID, currentClock.now)
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
	if disabledRule.Enabled || disabledRule.Version != 2 || disabledRule.EnabledUpdatedBy == nil ||
		*disabledRule.EnabledUpdatedBy != adminID || disabledRule.EnabledUpdatedAt == nil ||
		!disabledRule.EnabledUpdatedAt.Equal(currentClock.now) {
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
	var recoveredDisposition, unresolvedDisposition string
	if err := pool.QueryRow(ctx, `SELECT disposition FROM alert_instance WHERE id = $1`, firstAlertID).Scan(&recoveredDisposition); err != nil {
		t.Fatalf("read recovered disposition: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT disposition FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2 AND status <> 'RECOVERED'`, createdRule.ID, targetID).Scan(&unresolvedDisposition); err != nil {
		t.Fatalf("read new lifecycle disposition: %v", err)
	}
	if recoveredDisposition != "ACKED" || unresolvedDisposition != "NONE" {
		t.Fatalf("lifecycle dispositions = recovered %s unresolved %s, want ACKED/NONE", recoveredDisposition, unresolvedDisposition)
	}
	var outOfScopeAlerts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_instance WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, otherID).Scan(&outOfScopeAlerts); err != nil {
		t.Fatalf("count out-of-scope alerts: %v", err)
	}
	if outOfScopeAlerts != 0 {
		t.Fatalf("out-of-scope alert rows = %d, want 0", outOfScopeAlerts)
	}

	configurationBatch := &pgx.Batch{}
	configurationBatch.Queue(`INSERT INTO collection_task_config (instance_id, task_id, interval_seconds)
		VALUES ($1, 'pg.stat_activity', 30)`, targetID)
	configurationBatch.Queue(`INSERT INTO instance_collection_task_state (instance_id, task_id, consecutive_failures)
		VALUES ($1, 'pg.stat_activity', 0)`, targetID)
	configurationBatch.Queue(`INSERT INTO instance_collection_connection_state (instance_id, consecutive_failures)
		VALUES ($1, 0)`, targetID)
	configurationBatch.Queue(`INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
		VALUES ($1, $2, '{}')`, targetID, currentClock.now)
	configurationBatch.Queue(`UPDATE instance
		SET agent_expected = true,
		    agent_token_hash = decode(repeat('ab', 32), 'hex'),
		    agent_token_issued_at = $2,
		    agent_first_registered_at = $2
		WHERE id = $1`, targetID, currentClock.now)
	if err := pool.SendBatch(ctx, configurationBatch).Close(); err != nil {
		t.Fatalf("seed removable instance configuration: %v", err)
	}
	retainedHistory := readInstanceHistoryCounts(t, ctx, pool, targetID)

	removed := requestJSON(t, client, http.MethodDelete, server.URL+"/api/v1/instances/"+targetID.String(), nil, "")
	removed.Body.Close()
	if removed.StatusCode != http.StatusNoContent {
		t.Fatalf("remove instance status = %d, want 204", removed.StatusCode)
	}
	var liveConfigurationRows int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM instance WHERE id = $1) +
		(SELECT count(*) FROM instance_collection_config WHERE instance_id = $1) +
		(SELECT count(*) FROM collection_task_config WHERE instance_id = $1) +
		(SELECT count(*) FROM instance_collection_task_state WHERE instance_id = $1) +
		(SELECT count(*) FROM instance_collection_connection_state WHERE instance_id = $1) +
		(SELECT count(*) FROM instance_collect_state WHERE instance_id = $1) +
		(SELECT count(*) FROM instance_capability_snapshot WHERE instance_id = $1) +
		(SELECT count(*) FROM alert_rule_scope_instance WHERE instance_id = $1) +
		(SELECT count(*) FROM alert_rule_evaluation_state WHERE instance_id = $1)`, targetID).Scan(&liveConfigurationRows); err != nil {
		t.Fatalf("count configuration after instance removal: %v", err)
	}
	if liveConfigurationRows != 0 {
		t.Fatalf("live configuration rows after instance removal = %d, want 0", liveConfigurationRows)
	}

	var removedName string
	var removedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT name, removed_at FROM instance_identity WHERE id = $1", targetID).Scan(&removedName, &removedAt); err != nil {
		t.Fatalf("read removed instance identity: %v", err)
	}
	if removedName != "target" || !removedAt.Equal(currentClock.now) {
		t.Fatalf("removed instance identity = %q at %s, want target at %s", removedName, removedAt, currentClock.now)
	}
	historyAfterRemoval := readInstanceHistoryCounts(t, ctx, pool, targetID)
	wantHistoryAfterRemoval := retainedHistory
	wantHistoryAfterRemoval.unresolvedAlerts = 0
	wantHistoryAfterRemoval.events += retainedHistory.unresolvedAlerts
	if historyAfterRemoval != wantHistoryAfterRemoval {
		t.Fatalf("history after instance removal = %+v, want %+v", historyAfterRemoval, wantHistoryAfterRemoval)
	}
	var removalEvents, firingRemovalEvents, recoveredRemovalEvents, removalActors int
	if err := pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE event.from_state = 'FIRING'),
		count(*) FILTER (WHERE event.to_state = 'RECOVERED'),
		count(DISTINCT event.actor_id)
		FROM alert_event event
		JOIN alert_instance alert ON alert.id = event.alert_instance_id
		WHERE alert.instance_id = $1 AND event.kind = 'INSTANCE_REMOVED'`, targetID).
		Scan(&removalEvents, &firingRemovalEvents, &recoveredRemovalEvents, &removalActors); err != nil {
		t.Fatalf("read instance removal alert event: %v", err)
	}
	if removalEvents != retainedHistory.unresolvedAlerts || firingRemovalEvents == 0 || recoveredRemovalEvents != removalEvents || removalActors != 1 {
		t.Fatalf("instance removal alert events = total %d firing %d recovered %d actors %d, want %d/all/one actor",
			removalEvents, firingRemovalEvents, recoveredRemovalEvents, removalActors, retainedHistory.unresolvedAlerts)
	}
	var dispositionEventsAfterRemoval int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alert_event WHERE alert_instance_id = $1 AND kind IN ('ACKED', 'IGNORED')", firstAlertID).
		Scan(&dispositionEventsAfterRemoval); err != nil {
		t.Fatalf("count retained disposition events: %v", err)
	}
	if dispositionEventsAfterRemoval != len(wantKinds) {
		t.Fatalf("disposition events after removal = %d, want %d", dispositionEventsAfterRemoval, len(wantKinds))
	}
	retainedDisposition := requestJSON(t, client, http.MethodGet, dispositionURL, nil, "")
	retainedDisposition.Body.Close()
	if retainedDisposition.StatusCode != http.StatusOK {
		t.Fatalf("retained disposition status = %d, want 200", retainedDisposition.StatusCode)
	}
	assertTriggerSnapshotAPIResult(t, client, server.URL, firstAlertID, "FAILED")

	replacementID := createAlertTestInstance(t, ctx, pool, keyring, "target")
	if replacementID == targetID {
		t.Fatal("re-onboarding reused the removed instance identity")
	}
	replacementHistory := readInstanceHistoryCounts(t, ctx, pool, replacementID)
	if replacementHistory.alerts != 0 || replacementHistory.series != 0 {
		t.Fatalf("replacement inherited alerts/series = %d/%d, want 0/0", replacementHistory.alerts, replacementHistory.series)
	}
}

type instanceHistoryCounts struct {
	samples          int
	series           int
	alerts           int
	unresolvedAlerts int
	events           int
	snapshots        int
}

func readInstanceHistoryCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) instanceHistoryCounts {
	t.Helper()
	var counts instanceHistoryCounts
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM metric_sample sample JOIN metric_series series ON series.series_id = sample.series_id WHERE series.instance_id = $1),
		(SELECT count(*) FROM metric_series WHERE instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1 AND status <> 'RECOVERED'),
		(SELECT count(*) FROM alert_event event JOIN alert_instance alert ON alert.id = event.alert_instance_id WHERE alert.instance_id = $1),
		(SELECT count(*) FROM alert_trigger_snapshot snapshot JOIN alert_instance alert ON alert.id = snapshot.alert_instance_id WHERE alert.instance_id = $1)`, instanceID).Scan(
		&counts.samples,
		&counts.series,
		&counts.alerts,
		&counts.unresolvedAlerts,
		&counts.events,
		&counts.snapshots,
	); err != nil {
		t.Fatalf("count instance history: %v", err)
	}
	return counts
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

func loginAlertTestUser(t *testing.T, server *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Transport: server.Client().Transport, Jar: jar}
	response := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": username, "password": password,
	}, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("login %q status = %d, want 204", username, response.StatusCode)
	}
	return client
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
		Engine: string(instance.EnginePostgreSQL),
		DatabaseName: instance.BootstrapDatabaseColumn("postgres"), Username: "postgres", PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create instance %q: %v", name, err)
	}
	return instanceID
}

func createAlertTestSeries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, now time.Time) int64 {
	return createAlertTestSeriesForMetric(t, ctx, pool, instanceID, now, "pg.connection.active")
}

func createAlertTestSeriesForMetric(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, now time.Time, metricID string) int64 {
	t.Helper()
	seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true}, MetricID: metricID,
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

func assertRuleErrorCode(t *testing.T, response *http.Response, wantStatus int, wantCode api.ErrorErrorCode) {
	t.Helper()
	defer response.Body.Close()
	var body api.Error
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode alert rule error: %v", err)
	}
	if response.StatusCode != wantStatus || body.Error.Code != wantCode {
		t.Fatalf("alert rule error = status %d code %q, want %d/%q", response.StatusCode, body.Error.Code, wantStatus, wantCode)
	}
}

func assertAlertRuleAttribution(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	ruleID uuid.UUID,
	wantActor uuid.UUID,
	wantCreatedAt time.Time,
	wantUpdatedAt time.Time,
	wantDeletedAt *time.Time,
) {
	t.Helper()
	var createdBy, updatedBy uuid.UUID
	var deletedBy *uuid.UUID
	var createdAt, updatedAt time.Time
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT created_by, updated_by, deleted_by,
		created_at, updated_at, deleted_at
		FROM alert_rule WHERE id = $1`, ruleID).Scan(
		&createdBy, &updatedBy, &deletedBy, &createdAt, &updatedAt, &deletedAt,
	); err != nil {
		t.Fatalf("read alert rule attribution for %s: %v", ruleID, err)
	}
	if createdBy != wantActor || updatedBy != wantActor || !createdAt.Equal(wantCreatedAt) || !updatedAt.Equal(wantUpdatedAt) {
		t.Fatalf("alert rule %s attribution = created %s at %s, updated %s at %s; want actor %s",
			ruleID, createdBy, createdAt, updatedBy, updatedAt, wantActor)
	}
	if wantDeletedAt != nil {
		if deletedBy == nil || *deletedBy != wantActor || deletedAt == nil || !deletedAt.Equal(*wantDeletedAt) {
			t.Fatalf("deleted alert rule %s attribution = actor %v at %v; want actor %s at %s",
				ruleID, deletedBy, deletedAt, wantActor, *wantDeletedAt)
		}
		return
	}
	if deletedBy != nil || deletedAt != nil {
		t.Fatalf("live alert rule %s has deletion attribution %v at %v", ruleID, deletedBy, deletedAt)
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

func newCurrentFixedClock() *fixedClock {
	return &fixedClock{now: time.Now().UTC().Truncate(time.Microsecond)}
}

func (clock *fixedClock) Now() time.Time { return clock.now }

func (clock *fixedClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

func (clock *fixedClock) Advance(duration time.Duration) { clock.now = clock.now.Add(duration) }

var _ clock.Clock = (*fixedClock)(nil)

func assertTemplateOwnership(t *testing.T, template api.AlertRuleTemplate, engine api.MetricEngine, slot api.SemanticSlot) {
	t.Helper()
	if template.Engine != engine {
		t.Errorf("template %q engine = %q, want %q", template.Id, template.Engine, engine)
	}
	got, err := template.SemanticSlot.Get()
	if slot == "" {
		if err == nil {
			t.Errorf("template %q fills slot %q, want none", template.Id, got)
		}
		return
	}
	if err != nil || got != slot {
		t.Errorf("template %q slot = %q (%v), want %q", template.Id, got, err, slot)
	}
}

func listTemplateIDsForEngine(t *testing.T, client *http.Client, baseURL string, engine string) []string {
	t.Helper()
	response := getResponse(t, client, baseURL+"/api/v1/alert-rule-templates?engine="+engine)
	defer response.Body.Close()
	var templates []api.AlertRuleTemplate
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&templates) != nil {
		t.Fatalf("list templates for engine %q status = %d", engine, response.StatusCode)
	}
	identifiers := make([]string, 0, len(templates))
	for _, template := range templates {
		identifiers = append(identifiers, template.Id)
	}
	return identifiers
}
