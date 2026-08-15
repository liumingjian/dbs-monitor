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
	evaluation := evaluator.New(platform, currentClock, nil)

	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate firing transition: %v", err)
	}
	firing := currentDispositionAlert(t, ctx, client, instanceID, ruleID)
	if firing.Status != api.FIRING || firing.Disposition != api.AlertDispositionNONE {
		t.Fatalf("initial alert state/disposition = %s/%s, want FIRING/NONE", firing.Status, firing.Disposition)
	}

	assertDispositionValidation(t, ctx, client, firing.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionIGNORED,
	}, "ignore_reason_code")
	blankDetail := "  "
	otherReason := api.OTHER
	assertDispositionValidation(t, ctx, client, firing.Id, api.AlertDispositionInput{
		Disposition:        api.AlertDispositionIGNORED,
		IgnoreReasonCode:   &otherReason,
		IgnoreReasonDetail: &blankDetail,
	}, "ignore_reason_detail")

	note := "Investigating"
	acked := updateDisposition(t, ctx, client, firing.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionACKED,
		Note:        &note,
	})
	if !acked.StopsRepeatNotifications || acked.ExcludedFromHealthRollup {
		t.Fatalf("ACKED projected facts = stop repeat %t, excluded health %t", acked.StopsRepeatNotifications, acked.ExcludedFromHealthRollup)
	}
	currentClock.Advance(time.Second)
	detail := "Expected maintenance load"
	ignored := updateDisposition(t, ctx, client, firing.Id, api.AlertDispositionInput{
		Disposition:        api.AlertDispositionIGNORED,
		IgnoreReasonCode:   &otherReason,
		IgnoreReasonDetail: &detail,
	})
	if ignored.StopsRepeatNotifications || !ignored.ExcludedFromHealthRollup {
		t.Fatalf("IGNORED projected facts = stop repeat %t, excluded health %t", ignored.StopsRepeatNotifications, ignored.ExcludedFromHealthRollup)
	}
	currentClock.Advance(time.Second)
	updateDisposition(t, ctx, client, firing.Id, api.AlertDispositionInput{Disposition: api.AlertDispositionACKED})

	disposition := getDisposition(t, ctx, client, firing.Id)
	assertDispositionHistory(t, disposition, createdRule.JSON201.Version)

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate disposed alert: %v", err)
	}
	stillFiring := getDispositionAlert(t, ctx, client, firing.Id)
	if stillFiring.Status != api.FIRING || stillFiring.Disposition != api.AlertDispositionACKED {
		t.Fatalf("disposed alert state/disposition = %s/%s, want FIRING/ACKED", stillFiring.Status, stillFiring.Disposition)
	}

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 20)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	recovered := getDispositionAlert(t, ctx, client, firing.Id)
	if recovered.Status != api.RECOVERED || recovered.Disposition != api.AlertDispositionACKED {
		t.Fatalf("recovered alert state/disposition = %s/%s, want RECOVERED/ACKED", recovered.Status, recovered.Disposition)
	}

	currentClock.Advance(5 * time.Second)
	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate new lifecycle: %v", err)
	}
	newLifecycle := currentDispositionAlert(t, ctx, client, instanceID, ruleID)
	if newLifecycle.Id == firing.Id || newLifecycle.Status != api.FIRING || newLifecycle.Disposition != api.AlertDispositionNONE {
		t.Fatalf("new lifecycle = id %s state/disposition %s/%s, want a new FIRING/NONE alert", newLifecycle.Id, newLifecycle.Status, newLifecycle.Disposition)
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

func reportDispositionSample(t *testing.T, ctx context.Context, client *api.ClientWithResponses, token string, instanceID uuid.UUID, at time.Time, value float64) {
	t.Helper()
	response, err := client.ReportAgentMetricsWithResponse(ctx, api.AgentReport{
		InstanceId:   instanceID,
		AgentVersion: "3.0.0",
		Timestamp:    at,
		Metrics: []api.AgentMetric{{
			Metric: api.AgentMetricMetricHostCpuUsagePercent,
			Value:  value,
		}},
	}, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		t.Fatalf("report Agent sample through API: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("report Agent sample status/body = %d/%s", response.StatusCode(), response.Body)
	}
}

func currentDispositionAlert(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) api.AlertObservation {
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

func getDispositionAlert(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID) api.AlertDetail {
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

func assertDispositionValidation(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID, input api.AlertDispositionInput, field string) {
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
		if fieldError.Field == field {
			return
		}
	}
	t.Fatalf("invalid disposition field errors = %+v, want %s", *response.JSON400.Error.FieldErrors, field)
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

func getDisposition(t *testing.T, ctx context.Context, client *api.ClientWithResponses, alertID uuid.UUID) api.AlertDispositionDetail {
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
	wantFrom := []api.AlertDisposition{api.AlertDispositionNONE, api.AlertDispositionACKED, api.AlertDispositionIGNORED}
	wantTo := []api.AlertDisposition{api.AlertDispositionACKED, api.AlertDispositionIGNORED, api.AlertDispositionACKED}
	if detail.Disposition != api.AlertDispositionACKED || detail.DispositionBy == nil || detail.DispositionAt == nil || len(detail.History) != len(wantTo) {
		t.Fatalf("current disposition/history = %+v", detail)
	}
	for index, event := range detail.History {
		threshold, hasThreshold := event.RuleSnapshot["threshold"].(float64)
		if event.FromDisposition != wantFrom[index] || event.ToDisposition != wantTo[index] ||
			event.ActorId != *detail.DispositionBy || event.RuleVersion != ruleVersion || event.CurrentValue == nil || *event.CurrentValue != 90 ||
			event.EvaluatedAt.IsZero() || event.ActedAt.IsZero() || !hasThreshold || threshold != 80 {
			t.Fatalf("disposition history event %d = %+v", index, event)
		}
	}
	if detail.History[0].Note == nil || *detail.History[0].Note != "Investigating" ||
		detail.History[1].IgnoreReasonCode == nil || *detail.History[1].IgnoreReasonCode != api.OTHER ||
		detail.History[1].IgnoreReasonDetail == nil || *detail.History[1].IgnoreReasonDetail != "Expected maintenance load" {
		t.Fatalf("disposition history payloads = %+v", detail.History)
	}
}
