//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

func TestAcceptance_AC_03_S1(t *testing.T) {
	started := time.Now()
	defer func() {
		status := resultPassed
		actualResult := "disposition switching, attribution, recovery, and lifecycle reset passed through generated APIs"
		if t.Failed() {
			status = resultFailed
			actualResult = "AC-03-S1 alert disposition acceptance failed; see go test output"
		}
		acceptanceReport.record("AC-03-S1", status, actualResult, time.Since(started))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	platform := openDiagnosticAcceptanceDatabase(t, ctx)
	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(credentialDirectory, 0o700); err != nil {
		t.Fatalf("create acceptance credential directory: %v", err)
	}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open acceptance credential keyring: %v", err)
	}
	currentClock := &dispositionAcceptanceClock{now: time.Now().UTC().Truncate(time.Second)}
	if err := httpapi.SeedAdmin(ctx, platform, "disposition-admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed bootstrap administrator: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandlerWithVersion(platform, currentClock, keyring, "3.0.0").Routes())
	defer server.Close()
	client := diagnosticAcceptanceClient(t, server)
	loginDiagnosticAcceptanceUser(t, ctx, client, "disposition-admin", "correct horse battery staple")

	createdInstance, err := client.CreateInstanceWithResponse(ctx, api.InstanceCreateInput{
		Name:     "AC-03-S1 disposition target",
		Host:     diagnosticAcceptanceEnv("PGHOST", "localhost"),
		Port:     dispositionAcceptancePort(t),
		Database: diagnosticAcceptanceEnv("PGDATABASE", "dbs_monitor"),
		Username: diagnosticAcceptanceEnv("PGUSER", "dbs_monitor"),
		Password: diagnosticAcceptanceEnv("PGPASSWORD", "dbs_monitor"),
	})
	if err != nil {
		t.Fatalf("create monitored instance through API: %v", err)
	}
	if createdInstance.StatusCode() != http.StatusCreated || createdInstance.JSON201 == nil {
		t.Fatalf("create monitored instance status/body = %d/%s", createdInstance.StatusCode(), createdInstance.Body)
	}
	instanceID := createdInstance.JSON201.Instance.Id

	registered, err := client.RegisterAgentWithResponse(ctx, instanceID)
	if err != nil {
		t.Fatalf("register Agent through API: %v", err)
	}
	if registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
		t.Fatalf("register Agent status/body = %d/%s", registered.StatusCode(), registered.Body)
	}
	agentToken := *registered.JSON200.AgentToken

	recoveryThreshold := 50.0
	recoveryCount := 1
	createdRule, err := client.CreateAlertRuleWithResponse(ctx, api.AlertRuleInput{
		Name:                      "AC-03-S1 high CPU",
		MetricId:                  string(api.AgentMetricMetricHostCpuUsagePercent),
		Aggregation:               api.Latest,
		Operator:                  api.GreaterThanEqual,
		Threshold:                 80,
		RecoveryOperator:          api.LessThan,
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             60,
		ConsecutiveCount:          1,
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  api.Warning,
		NoDataPolicy:              api.MarkNoData,
		Scope:                     api.INSTANCES,
		InstanceIds:               []uuid.UUID{instanceID},
		EvaluationIntervalSeconds: 5,
		Enabled:                   true,
	})
	if err != nil {
		t.Fatalf("create alert rule through API: %v", err)
	}
	if createdRule.StatusCode() != http.StatusCreated || createdRule.JSON201 == nil {
		t.Fatalf("create alert rule status/body = %d/%s", createdRule.StatusCode(), createdRule.Body)
	}
	ruleID := createdRule.JSON201.Id
	alertEvaluator := evaluator.New(platform, currentClock, nil)

	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := alertEvaluator.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate firing transition: %v", err)
	}
	initialAlert := findCurrentDispositionAlert(t, ctx, client, instanceID, ruleID)
	if initialAlert.Status != api.FIRING || initialAlert.Disposition != api.AlertDispositionNONE {
		t.Fatalf("initial alert state/disposition = %s/%s, want FIRING/NONE", initialAlert.Status, initialAlert.Disposition)
	}

	assertDispositionValidationError(t, ctx, client, initialAlert.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionIGNORED,
	}, "ignore_reason_code")
	blankDetail := "  "
	otherReason := api.OTHER
	assertDispositionValidationError(t, ctx, client, initialAlert.Id, api.AlertDispositionInput{
		Disposition:        api.AlertDispositionIGNORED,
		IgnoreReasonCode:   &otherReason,
		IgnoreReasonDetail: &blankDetail,
	}, "ignore_reason_detail")

	note := "Investigating"
	acked := updateDisposition(t, ctx, client, initialAlert.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionACKED,
		Note:        &note,
	})
	if !acked.StopsRepeatNotifications || acked.ExcludedFromHealthRollup {
		t.Fatalf("ACKED projected facts = stop repeat %t, excluded health %t", acked.StopsRepeatNotifications, acked.ExcludedFromHealthRollup)
	}
	currentClock.Advance(time.Second)
	reasonDetail := "Expected maintenance load"
	ignored := updateDisposition(t, ctx, client, initialAlert.Id, api.AlertDispositionInput{
		Disposition:        api.AlertDispositionIGNORED,
		IgnoreReasonCode:   &otherReason,
		IgnoreReasonDetail: &reasonDetail,
	})
	if ignored.StopsRepeatNotifications || !ignored.ExcludedFromHealthRollup {
		t.Fatalf("IGNORED projected facts = stop repeat %t, excluded health %t", ignored.StopsRepeatNotifications, ignored.ExcludedFromHealthRollup)
	}
	currentClock.Advance(time.Second)
	updateDisposition(t, ctx, client, initialAlert.Id, api.AlertDispositionInput{Disposition: api.AlertDispositionACKED})

	dispositionDetail := getDispositionDetail(t, ctx, client, initialAlert.Id)
	assertDispositionHistory(t, dispositionDetail, createdRule.JSON201.Version)

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := alertEvaluator.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate disposed alert: %v", err)
	}
	stillFiring := getDispositionAlertDetail(t, ctx, client, initialAlert.Id)
	if stillFiring.Status != api.FIRING || stillFiring.Disposition != api.AlertDispositionACKED {
		t.Fatalf("disposed alert state/disposition = %s/%s, want FIRING/ACKED", stillFiring.Status, stillFiring.Disposition)
	}

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 20)
	if err := alertEvaluator.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	recovered := getDispositionAlertDetail(t, ctx, client, initialAlert.Id)
	if recovered.Status != api.RECOVERED || recovered.Disposition != api.AlertDispositionACKED {
		t.Fatalf("recovered alert state/disposition = %s/%s, want RECOVERED/ACKED", recovered.Status, recovered.Disposition)
	}

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := alertEvaluator.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate new lifecycle: %v", err)
	}
	nextAlert := findCurrentDispositionAlert(t, ctx, client, instanceID, ruleID)
	if nextAlert.Id == initialAlert.Id || nextAlert.Status != api.FIRING || nextAlert.Disposition != api.AlertDispositionNONE {
		t.Fatalf("new lifecycle = id %s state/disposition %s/%s, want a new FIRING/NONE alert", nextAlert.Id, nextAlert.Status, nextAlert.Disposition)
	}
}

type dispositionAcceptanceClock struct {
	now time.Time
}

func (current *dispositionAcceptanceClock) Now() time.Time { return current.now }

func (*dispositionAcceptanceClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

func (current *dispositionAcceptanceClock) Advance(elapsed time.Duration) {
	current.now = current.now.Add(elapsed)
}

func dispositionAcceptancePort(t *testing.T) int {
	t.Helper()
	port, err := strconv.Atoi(diagnosticAcceptanceEnv("PGPORT", "55432"))
	if err != nil {
		t.Fatalf("parse PGPORT: %v", err)
	}
	return port
}

func reportDispositionSample(t *testing.T, ctx context.Context, client *api.ClientWithResponses, agentToken string, instanceID uuid.UUID, reportedAt time.Time, value float64) {
	t.Helper()
	response, err := client.ReportAgentMetricsWithResponse(ctx, api.AgentReport{
		InstanceId:   instanceID,
		AgentVersion: "3.0.0",
		Timestamp:    reportedAt,
		Metrics: []api.AgentMetric{{
			Metric: api.AgentMetricMetricHostCpuUsagePercent,
			Value:  value,
		}},
	}, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+agentToken)
		return nil
	})
	if err != nil {
		t.Fatalf("report Agent sample through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("report Agent sample status/body = %d/%s", response.StatusCode(), response.Body)
	}
}

func findCurrentDispositionAlert(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) api.AlertObservation {
	t.Helper()
	response, err := client.ListCurrentAlertsWithResponse(ctx, &api.ListCurrentAlertsParams{InstanceId: &instanceID})
	if err != nil {
		t.Fatalf("list current alerts through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("list current alerts status/body = %d/%s", response.StatusCode(), response.Body)
	}
	for _, alert := range response.JSON200.Items {
		if alert.RuleId == ruleID {
			return alert
		}
	}
	t.Fatalf("current alert for rule %s not found in %+v", ruleID, response.JSON200.Items)
	return api.AlertObservation{}
}

func getDispositionAlertDetail(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID) api.AlertDetail {
	t.Helper()
	response, err := client.GetAlertDetailWithResponse(ctx, alertID)
	if err != nil {
		t.Fatalf("get alert detail through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("get alert detail status/body = %d/%s", response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func assertDispositionValidationError(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID, input api.AlertDispositionInput, expectedField string) {
	t.Helper()
	response, err := client.UpdateAlertDispositionWithResponse(ctx, alertID, input)
	if err != nil {
		t.Fatalf("send invalid disposition through API: %v", err)
	}
	if response.StatusCode() != http.StatusBadRequest || response.JSON400 == nil || response.JSON400.Error.Code != api.VALIDATIONFAILED {
		t.Fatalf("invalid disposition status/body = %d/%s", response.StatusCode(), response.Body)
	}
	if response.JSON400.Error.FieldErrors == nil {
		t.Fatalf("invalid disposition has no field errors: %s", response.Body)
	}
	for _, fieldError := range *response.JSON400.Error.FieldErrors {
		if fieldError.Field == expectedField {
			return
		}
	}
	t.Fatalf("invalid disposition field errors = %+v, want %s", *response.JSON400.Error.FieldErrors, expectedField)
}

func updateDisposition(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID, input api.AlertDispositionInput) api.AlertDispositionDetail {
	t.Helper()
	response, err := client.UpdateAlertDispositionWithResponse(ctx, alertID, input)
	if err != nil {
		t.Fatalf("update disposition through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("update disposition status/body = %d/%s", response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func getDispositionDetail(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID) api.AlertDispositionDetail {
	t.Helper()
	response, err := client.GetAlertDispositionWithResponse(ctx, alertID)
	if err != nil {
		t.Fatalf("get disposition through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("get disposition status/body = %d/%s", response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func assertDispositionHistory(t *testing.T, detail api.AlertDispositionDetail, ruleVersion int) {
	t.Helper()
	expectedTransitions := []struct {
		from api.AlertDisposition
		to   api.AlertDisposition
	}{
		{from: api.AlertDispositionNONE, to: api.AlertDispositionACKED},
		{from: api.AlertDispositionACKED, to: api.AlertDispositionIGNORED},
		{from: api.AlertDispositionIGNORED, to: api.AlertDispositionACKED},
	}

	if detail.Disposition != api.AlertDispositionACKED {
		t.Fatalf("current disposition = %s, want ACKED", detail.Disposition)
	}
	if detail.DispositionBy == nil || detail.DispositionAt == nil {
		t.Fatalf("current disposition attribution = actor %v at %v, want both populated", detail.DispositionBy, detail.DispositionAt)
	}
	if len(detail.History) != len(expectedTransitions) {
		t.Fatalf("disposition history length = %d, want %d: %+v", len(detail.History), len(expectedTransitions), detail.History)
	}
	expectedActorID := *detail.DispositionBy

	for index, event := range detail.History {
		expected := expectedTransitions[index]
		if event.FromDisposition != expected.from || event.ToDisposition != expected.to {
			t.Fatalf("disposition history event %d transition = %s -> %s, want %s -> %s", index, event.FromDisposition, event.ToDisposition, expected.from, expected.to)
		}
		if event.ActorId != expectedActorID {
			t.Fatalf("disposition history event %d actor = %s, want %s", index, event.ActorId, expectedActorID)
		}
		if event.RuleVersion != ruleVersion {
			t.Fatalf("disposition history event %d rule version = %d, want %d", index, event.RuleVersion, ruleVersion)
		}
		if event.CurrentValue == nil {
			t.Fatalf("disposition history event %d current value is absent", index)
		}
		if *event.CurrentValue != 90 {
			t.Fatalf("disposition history event %d current value = %v, want 90", index, *event.CurrentValue)
		}
		snapshotThreshold := event.RuleSnapshot["threshold"]
		threshold, hasThreshold := snapshotThreshold.(float64)
		if !hasThreshold || threshold != 80 {
			t.Fatalf("disposition history event %d threshold = %v, want 80", index, snapshotThreshold)
		}
		if event.EvaluatedAt.IsZero() || event.ActedAt.IsZero() {
			t.Fatalf("disposition history event %d timestamps = evaluated %s, acted %s, want both populated", index, event.EvaluatedAt, event.ActedAt)
		}
	}

	acknowledged := detail.History[0]
	if acknowledged.Note == nil || *acknowledged.Note != "Investigating" {
		t.Fatalf("acknowledged disposition payload = %+v, want note Investigating", acknowledged)
	}
	ignored := detail.History[1]
	if ignored.IgnoreReasonCode == nil || *ignored.IgnoreReasonCode != api.OTHER {
		t.Fatalf("ignored disposition payload = %+v, want reason OTHER", ignored)
	}
	if ignored.IgnoreReasonDetail == nil || *ignored.IgnoreReasonDetail != "Expected maintenance load" {
		t.Fatalf("ignored disposition payload = %+v, want detail Expected maintenance load", ignored)
	}
}
