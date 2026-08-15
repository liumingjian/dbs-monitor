//go:build acceptance

package acceptance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

const instanceRemovalEntryID = "AC-08-S6"

func TestAcceptance_AC_08_S6(t *testing.T) {
	started := time.Now()
	defer func() {
		status, actualResult := resultPassed, "instance removal deleted configuration, retained attributed alert and sample history, and isolated re-onboarding"
		if t.Failed() {
			status, actualResult = resultFailed, "AC-08-S6 instance removal acceptance failed; see go test output"
		}
		acceptanceReport.record(instanceRemovalEntryID, status, actualResult, time.Since(started))
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
	if err := httpapi.SeedAdmin(ctx, platform, "removal-admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed bootstrap administrator: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandlerWithVersion(platform, currentClock, keyring, "3.0.0").Routes())
	defer server.Close()
	client := diagnosticAcceptanceClient(t, server)
	loginDiagnosticAcceptanceUser(t, ctx, client, "removal-admin", "correct horse battery staple")

	input := api.InstanceCreateInput{
		Name:     "AC-08-S6 removal target",
		Host:     diagnosticAcceptanceEnv("PGHOST", "localhost"),
		Port:     dispositionAcceptancePort(t),
		Database: diagnosticAcceptanceEnv("PGDATABASE", "dbs_monitor"),
		Username: diagnosticAcceptanceEnv("PGUSER", "dbs_monitor"),
		Password: diagnosticAcceptanceEnv("PGPASSWORD", "dbs_monitor"),
	}
	created, err := client.CreateInstanceWithResponse(ctx, input)
	if err != nil {
		t.Fatalf("create removable instance through API: %v", err)
	}
	if created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create removable instance status/body = %d/%s", created.StatusCode(), created.Body)
	}
	instanceID := created.JSON201.Instance.Id

	configured, err := client.UpdateCollectionTaskIntervalWithResponse(ctx, instanceID, "pg.probe", api.CollectionTaskIntervalInput{IntervalSeconds: 10})
	if err != nil {
		t.Fatalf("configure collection plan through API: %v", err)
	}
	if configured.StatusCode() != http.StatusOK || configured.JSON200 == nil {
		t.Fatalf("configure collection plan status/body = %d/%s", configured.StatusCode(), configured.Body)
	}
	registered, err := client.RegisterAgentWithResponse(ctx, instanceID)
	if err != nil {
		t.Fatalf("register removable Agent through API: %v", err)
	}
	if registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
		t.Fatalf("register removable Agent status/body = %d/%s", registered.StatusCode(), registered.Body)
	}
	agentToken := *registered.JSON200.AgentToken

	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	acknowledgedRuleID := createRemovalAlertRule(t, ctx, client, instanceID, "acknowledged", 80, api.Warning)
	createRemovalAlertRule(t, ctx, client, instanceID, "unacknowledged", 85, api.Critical)
	if err := evaluator.New(platform, currentClock, nil).RunOnce(ctx); err != nil {
		t.Fatalf("evaluate removable alert: %v", err)
	}
	alert := findCurrentDispositionAlert(t, ctx, client, instanceID, acknowledgedRuleID)
	note := "Retain this disposition after instance removal"
	dispositionBefore := updateDisposition(t, ctx, client, alert.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionACKED,
		Note:        &note,
	})
	if dispositionBefore.DispositionBy == nil {
		t.Fatal("acknowledged alert has no attributed actor")
	}

	before := readRemovalFacts(t, ctx, platform, instanceID)
	if before.credentialAndAgent != 1 || before.collectionConfig != 1 || before.collectionTaskOverrides != 1 ||
		before.agentCollectionState != 1 || before.ruleTargets != 2 || before.series != 1 || before.samples != 1 ||
		before.alerts < 2 || before.unresolvedAlerts != before.alerts || before.removalEvents != 0 {
		t.Fatalf("pre-removal facts = %+v, want complete configuration, one sample, and every alert unresolved", before)
	}

	removed, err := client.DeleteInstanceWithResponse(ctx, instanceID)
	if err != nil {
		t.Fatalf("remove instance through API: %v", err)
	}
	if removed.StatusCode() != http.StatusNoContent {
		t.Fatalf("remove instance status/body = %d/%s", removed.StatusCode(), removed.Body)
	}

	assertRemovedInstanceAbsent(t, ctx, client, instanceID)
	assertRemovedAgentTokenRejected(t, ctx, client, instanceID, agentToken, currentClock.now.Add(time.Second))
	after := readRemovalFacts(t, ctx, platform, instanceID)
	if after.credentialAndAgent != 0 || after.collectionConfig != 0 || after.collectionTaskOverrides != 0 ||
		after.agentCollectionState != 0 || after.ruleTargets != 0 {
		t.Fatalf("configuration facts after removal = %+v, want all active configuration deleted", after)
	}
	if after.identities != before.identities || after.removedIdentities != 1 || after.series != before.series ||
		after.samples != before.samples || after.alerts != before.alerts || after.unresolvedAlerts != 0 ||
		after.events != before.events+before.unresolvedAlerts || after.removalEvents != before.unresolvedAlerts {
		t.Fatalf("history facts after removal = %+v, before %+v", after, before)
	}
	assertRemovalHistory(t, ctx, platform, client, instanceID, alert.Id, input.Name, *dispositionBefore.DispositionBy, len(dispositionBefore.History), before.alerts)

	replacement, err := client.CreateInstanceWithResponse(ctx, input)
	if err != nil {
		t.Fatalf("re-onboard removed database through API: %v", err)
	}
	if replacement.StatusCode() != http.StatusCreated || replacement.JSON201 == nil {
		t.Fatalf("re-onboard removed database status/body = %d/%s", replacement.StatusCode(), replacement.Body)
	}
	replacementID := replacement.JSON201.Instance.Id
	if replacementID == instanceID {
		t.Fatal("re-onboarding reused the removed instance identity")
	}
	assertReplacementHasNoHistory(t, ctx, client, replacementID)
	replacementFacts := readRemovalFacts(t, ctx, platform, replacementID)
	if replacementFacts.credentialAndAgent != 0 || replacementFacts.identities != 1 || replacementFacts.removedIdentities != 0 || replacementFacts.collectionConfig != 1 ||
		replacementFacts.series != 0 || replacementFacts.samples != 0 || replacementFacts.alerts != 0 {
		t.Fatalf("replacement facts = %+v, want fresh configuration without inherited history", replacementFacts)
	}
}

func createRemovalAlertRule(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID, suffix string, threshold float64, severity api.AlertSeverity) uuid.UUID {
	t.Helper()
	recoveryThreshold := 50.0
	recoveryCount := 1
	created, err := client.CreateAlertRuleWithResponse(ctx, api.AlertRuleInput{
		Name:                      "AC-08-S6 " + suffix + " high CPU",
		MetricId:                  string(api.AgentMetricMetricHostCpuUsagePercent),
		Aggregation:               api.Latest,
		Operator:                  api.GreaterThanEqual,
		Threshold:                 threshold,
		RecoveryOperator:          api.LessThan,
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             60,
		ConsecutiveCount:          1,
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  severity,
		NoDataPolicy:              api.MarkNoData,
		Scope:                     api.INSTANCES,
		InstanceIds:               []uuid.UUID{instanceID},
		EvaluationIntervalSeconds: 5,
		Enabled:                   true,
	})
	if err != nil {
		t.Fatalf("create removable alert rule through API: %v", err)
	}
	if created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create removable alert rule status/body = %d/%s", created.StatusCode(), created.Body)
	}
	return created.JSON201.Id
}

type removalFacts struct {
	credentialAndAgent      int
	identities              int
	removedIdentities       int
	collectionConfig        int
	collectionTaskOverrides int
	agentCollectionState    int
	ruleTargets             int
	series                  int
	samples                 int
	alerts                  int
	unresolvedAlerts        int
	events                  int
	removalEvents           int
}

func readRemovalFacts(t *testing.T, ctx context.Context, platform *db.Pool, instanceID uuid.UUID) removalFacts {
	t.Helper()
	var facts removalFacts
	err := platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM instance WHERE id = $1 AND password_ciphertext IS NOT NULL AND agent_token_hash IS NOT NULL),
		(SELECT count(*) FROM instance_identity WHERE id = $1),
		(SELECT count(*) FROM instance_identity WHERE id = $1 AND removed_at IS NOT NULL),
		(SELECT count(*) FROM instance_collection_config WHERE instance_id = $1),
		(SELECT count(*) FROM collection_task_config WHERE instance_id = $1),
		(SELECT count(*) FROM instance_collect_state WHERE instance_id = $1 AND source = 'AGENT'),
		(SELECT count(*) FROM alert_rule_scope_instance WHERE instance_id = $1),
		(SELECT count(*) FROM metric_series WHERE instance_id = $1),
		(SELECT count(*) FROM metric_sample sample JOIN metric_series series ON series.series_id = sample.series_id WHERE series.instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1 AND status <> 'RECOVERED'),
		(SELECT count(*) FROM alert_event event JOIN alert_instance alert ON alert.id = event.alert_instance_id WHERE alert.instance_id = $1),
		(SELECT count(*) FROM alert_event event JOIN alert_instance alert ON alert.id = event.alert_instance_id WHERE alert.instance_id = $1 AND event.kind = 'INSTANCE_REMOVED')`, instanceID).Scan(
		&facts.credentialAndAgent,
		&facts.identities,
		&facts.removedIdentities,
		&facts.collectionConfig,
		&facts.collectionTaskOverrides,
		&facts.agentCollectionState,
		&facts.ruleTargets,
		&facts.series,
		&facts.samples,
		&facts.alerts,
		&facts.unresolvedAlerts,
		&facts.events,
		&facts.removalEvents,
	)
	if err != nil {
		t.Fatalf("read instance removal facts: %v", err)
	}
	return facts
}

func assertRemovedInstanceAbsent(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	response, err := client.ListInstancesWithResponse(ctx)
	if err != nil {
		t.Fatalf("list instances after removal: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("list instances after removal status/body = %d/%s", response.StatusCode(), response.Body)
	}
	for _, candidate := range *response.JSON200 {
		if candidate.Id == instanceID {
			t.Fatalf("removed instance %s remains in active instance list", instanceID)
		}
	}
}

func assertRemovedAgentTokenRejected(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID, token string, reportedAt time.Time) {
	t.Helper()
	response, err := client.ReportAgentMetricsWithResponse(ctx, api.AgentReport{
		InstanceId: instanceID, AgentVersion: "3.0.0", Timestamp: reportedAt,
		Metrics: []api.AgentMetric{{Metric: api.AgentMetricMetricHostCpuUsagePercent, Value: 91}},
	}, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		t.Fatalf("report with removed Agent token: %v", err)
	}
	if response.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("removed Agent token status/body = %d/%s, want 401", response.StatusCode(), response.Body)
	}
}

func assertRemovalHistory(t *testing.T, ctx context.Context, platform *db.Pool, client *api.ClientWithResponses, instanceID, alertID uuid.UUID, instanceName string, actorID uuid.UUID, dispositionEventCount, alertCount int) {
	t.Helper()
	history, err := client.ListAlertHistoryWithResponse(ctx, &api.ListAlertHistoryParams{InstanceId: &instanceID})
	if err != nil {
		t.Fatalf("list retained alert history: %v", err)
	}
	if history.StatusCode() != http.StatusOK || history.JSON200 == nil || history.JSON200.Total != alertCount || len(history.JSON200.Items) != alertCount {
		t.Fatalf("retained alert history status/body = %d/%s", history.StatusCode(), history.Body)
	}
	var retained api.AlertObservation
	for _, candidate := range history.JSON200.Items {
		if candidate.Id == alertID {
			retained = candidate
			break
		}
	}
	if retained.Id != alertID || retained.Status != api.RECOVERED || retained.RecoveredAt == nil || retained.InstanceName != instanceName {
		t.Fatalf("retained alert = %+v, want recovered alert %s attributed to %q", retained, alertID, instanceName)
	}
	disposition := getDispositionDetail(t, ctx, client, alertID)
	if disposition.Disposition != api.AlertDispositionACKED || disposition.DispositionBy == nil || *disposition.DispositionBy != actorID ||
		len(disposition.History) != dispositionEventCount {
		t.Fatalf("retained disposition = %+v, want ACKED with actor %s and %d events", disposition, actorID, dispositionEventCount)
	}
	var removalActor uuid.UUID
	if err := platform.QueryRow(ctx, `SELECT actor_id FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'INSTANCE_REMOVED'`, alertID).Scan(&removalActor); err != nil {
		t.Fatalf("read attributed INSTANCE_REMOVED event: %v", err)
	}
	if removalActor != actorID {
		t.Fatalf("INSTANCE_REMOVED actor = %s, want %s", removalActor, actorID)
	}
}

func assertReplacementHasNoHistory(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	current, err := client.ListCurrentAlertsWithResponse(ctx, &api.ListCurrentAlertsParams{InstanceId: &instanceID})
	if err != nil {
		t.Fatalf("list replacement current alerts: %v", err)
	}
	if current.StatusCode() != http.StatusOK || current.JSON200 == nil || current.JSON200.Total != 0 {
		t.Fatalf("replacement current alerts status/body = %d/%s", current.StatusCode(), current.Body)
	}
	history, err := client.ListAlertHistoryWithResponse(ctx, &api.ListAlertHistoryParams{InstanceId: &instanceID})
	if err != nil {
		t.Fatalf("list replacement alert history: %v", err)
	}
	if history.StatusCode() != http.StatusOK || history.JSON200 == nil || history.JSON200.Total != 0 {
		t.Fatalf("replacement alert history status/body = %d/%s", history.StatusCode(), history.Body)
	}
}
