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

	instanceInput := api.InstanceCreateInput{
		Name:     "AC-08-S6 removal target",
		Host:     diagnosticAcceptanceEnv("PGHOST", "localhost"),
		Port:     dispositionAcceptancePort(t),
		Database: diagnosticAcceptanceEnv("PGDATABASE", "dbs_monitor"),
		Username: diagnosticAcceptanceEnv("PGUSER", "dbs_monitor"),
		Password: diagnosticAcceptanceEnv("PGPASSWORD", "dbs_monitor"),
	}
	createdInstance, err := client.CreateInstanceWithResponse(ctx, instanceInput)
	if err != nil {
		t.Fatalf("create removable instance through API: %v", err)
	}
	if createdInstance.StatusCode() != http.StatusCreated || createdInstance.JSON201 == nil {
		t.Fatalf("create removable instance status/body = %d/%s", createdInstance.StatusCode(), createdInstance.Body)
	}
	instanceID := createdInstance.JSON201.Instance.Id

	configuredInterval, err := client.UpdateCollectionTaskIntervalWithResponse(ctx, instanceID, "pg.probe", api.CollectionTaskIntervalInput{IntervalSeconds: 10})
	if err != nil {
		t.Fatalf("configure collection plan through API: %v", err)
	}
	if configuredInterval.StatusCode() != http.StatusOK || configuredInterval.JSON200 == nil {
		t.Fatalf("configure collection plan status/body = %d/%s", configuredInterval.StatusCode(), configuredInterval.Body)
	}
	registeredAgent, err := client.RegisterAgentWithResponse(ctx, instanceID)
	if err != nil {
		t.Fatalf("register removable Agent through API: %v", err)
	}
	if registeredAgent.StatusCode() != http.StatusOK || registeredAgent.JSON200 == nil || registeredAgent.JSON200.AgentToken == nil {
		t.Fatalf("register removable Agent status/body = %d/%s", registeredAgent.StatusCode(), registeredAgent.Body)
	}
	agentToken := *registeredAgent.JSON200.AgentToken

	reportDispositionSample(t, ctx, client, agentToken, instanceID, currentClock.now, 90)
	acknowledgedRuleID := createRemovalAlertRule(t, ctx, client, instanceID, "AC-08-S6 acknowledged high CPU", 80, api.Warning)
	createRemovalAlertRule(t, ctx, client, instanceID, "AC-08-S6 unacknowledged high CPU", 85, api.Critical)
	if err := evaluator.New(platform, currentClock, nil).RunOnce(ctx); err != nil {
		t.Fatalf("evaluate removable alert: %v", err)
	}
	acknowledgedAlert := findCurrentDispositionAlert(t, ctx, client, instanceID, acknowledgedRuleID)
	note := "Retain this disposition after instance removal"
	dispositionBeforeRemoval := updateDisposition(t, ctx, client, acknowledgedAlert.Id, api.AlertDispositionInput{
		Disposition: api.AlertDispositionACKED,
		Note:        &note,
	})
	if dispositionBeforeRemoval.DispositionBy == nil {
		t.Fatal("acknowledged alert has no attributed actor")
	}

	factsBeforeRemoval := readRemovalFacts(t, ctx, platform, instanceID)
	if factsBeforeRemoval.credentialAndAgentCount != 1 || factsBeforeRemoval.collectionConfigCount != 1 || factsBeforeRemoval.collectionTaskOverrideCount != 1 ||
		factsBeforeRemoval.agentCollectionStateCount != 1 || factsBeforeRemoval.ruleTargetCount != 2 || factsBeforeRemoval.seriesCount != 1 || factsBeforeRemoval.sampleCount != 1 ||
		factsBeforeRemoval.alertCount < 2 || factsBeforeRemoval.unresolvedAlertCount != factsBeforeRemoval.alertCount || factsBeforeRemoval.removalEventCount != 0 {
		t.Fatalf("pre-removal facts = %+v, want complete configuration, one sample, and every alert unresolved", factsBeforeRemoval)
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
	factsAfterRemoval := readRemovalFacts(t, ctx, platform, instanceID)
	if factsAfterRemoval.credentialAndAgentCount != 0 || factsAfterRemoval.collectionConfigCount != 0 || factsAfterRemoval.collectionTaskOverrideCount != 0 ||
		factsAfterRemoval.agentCollectionStateCount != 0 || factsAfterRemoval.ruleTargetCount != 0 {
		t.Fatalf("configuration facts after removal = %+v, want all active configuration deleted", factsAfterRemoval)
	}
	if factsAfterRemoval.identityCount != factsBeforeRemoval.identityCount || factsAfterRemoval.removedIdentityCount != 1 || factsAfterRemoval.seriesCount != factsBeforeRemoval.seriesCount ||
		factsAfterRemoval.sampleCount != factsBeforeRemoval.sampleCount || factsAfterRemoval.alertCount != factsBeforeRemoval.alertCount || factsAfterRemoval.unresolvedAlertCount != 0 ||
		factsAfterRemoval.eventCount != factsBeforeRemoval.eventCount+factsBeforeRemoval.unresolvedAlertCount || factsAfterRemoval.removalEventCount != factsBeforeRemoval.unresolvedAlertCount {
		t.Fatalf("history facts after removal = %+v, before %+v", factsAfterRemoval, factsBeforeRemoval)
	}
	assertRemovalHistory(t, ctx, platform, client, removalHistoryExpectation{
		instanceID:              instanceID,
		alertID:                 acknowledgedAlert.Id,
		instanceName:            instanceInput.Name,
		actorID:                 *dispositionBeforeRemoval.DispositionBy,
		dispositionHistoryCount: len(dispositionBeforeRemoval.History),
		alertCount:              factsBeforeRemoval.alertCount,
	})

	replacementInstance, err := client.CreateInstanceWithResponse(ctx, instanceInput)
	if err != nil {
		t.Fatalf("re-onboard removed database through API: %v", err)
	}
	if replacementInstance.StatusCode() != http.StatusCreated || replacementInstance.JSON201 == nil {
		t.Fatalf("re-onboard removed database status/body = %d/%s", replacementInstance.StatusCode(), replacementInstance.Body)
	}
	replacementID := replacementInstance.JSON201.Instance.Id
	if replacementID == instanceID {
		t.Fatal("re-onboarding reused the removed instance identity")
	}
	assertReplacementHasNoHistory(t, ctx, client, replacementID)
	replacementFacts := readRemovalFacts(t, ctx, platform, replacementID)
	if replacementFacts.credentialAndAgentCount != 0 || replacementFacts.identityCount != 1 || replacementFacts.removedIdentityCount != 0 || replacementFacts.collectionConfigCount != 1 ||
		replacementFacts.seriesCount != 0 || replacementFacts.sampleCount != 0 || replacementFacts.alertCount != 0 {
		t.Fatalf("replacement facts = %+v, want fresh configuration without inherited history", replacementFacts)
	}
}

func createRemovalAlertRule(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID, name string, threshold float64, severity api.AlertSeverity) uuid.UUID {
	t.Helper()
	recoveryThreshold := 50.0
	recoveryCount := 1
	created, err := client.CreateAlertRuleWithResponse(ctx, api.AlertRuleInput{
		Name:                      name,
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
	credentialAndAgentCount     int
	identityCount               int
	removedIdentityCount        int
	collectionConfigCount       int
	collectionTaskOverrideCount int
	agentCollectionStateCount   int
	ruleTargetCount             int
	seriesCount                 int
	sampleCount                 int
	alertCount                  int
	unresolvedAlertCount        int
	eventCount                  int
	removalEventCount           int
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
		(SELECT count(*) FROM metric_sample AS sample
			JOIN metric_series AS series ON series.series_id = sample.series_id
			WHERE series.instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1),
		(SELECT count(*) FROM alert_instance WHERE instance_id = $1 AND status <> 'RECOVERED'),
		(SELECT count(*) FROM alert_event AS event
			JOIN alert_instance AS alert ON alert.id = event.alert_instance_id
			WHERE alert.instance_id = $1),
		(SELECT count(*) FROM alert_event AS event
			JOIN alert_instance AS alert ON alert.id = event.alert_instance_id
			WHERE alert.instance_id = $1 AND event.kind = 'INSTANCE_REMOVED')`, instanceID).Scan(
		&facts.credentialAndAgentCount,
		&facts.identityCount,
		&facts.removedIdentityCount,
		&facts.collectionConfigCount,
		&facts.collectionTaskOverrideCount,
		&facts.agentCollectionStateCount,
		&facts.ruleTargetCount,
		&facts.seriesCount,
		&facts.sampleCount,
		&facts.alertCount,
		&facts.unresolvedAlertCount,
		&facts.eventCount,
		&facts.removalEventCount,
	)
	if err != nil {
		t.Fatalf("read instance removal facts: %v", err)
	}
	return facts
}

func assertRemovedInstanceAbsent(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	response, err := client.ListInstancesWithResponse(ctx, &api.ListInstancesParams{})
	if err != nil {
		t.Fatalf("list instances after removal: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("list instances after removal status/body = %d/%s", response.StatusCode(), response.Body)
	}
	for _, candidate := range response.JSON200.Items {
		if candidate.Id == instanceID {
			t.Fatalf("removed instance %s remains in active instance list", instanceID)
		}
	}
}

func assertRemovedAgentTokenRejected(t *testing.T, ctx context.Context, client *api.ClientWithResponses, instanceID uuid.UUID, agentToken string, reportedAt time.Time) {
	t.Helper()
	response, err := client.ReportAgentMetricsWithResponse(ctx, api.AgentReport{
		InstanceId:   instanceID,
		AgentVersion: "3.0.0",
		Timestamp:    reportedAt,
		Metrics: []api.AgentMetric{{
			Metric: api.AgentMetricMetricHostCpuUsagePercent,
			Value:  91,
		}},
	}, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+agentToken)
		return nil
	})
	if err != nil {
		t.Fatalf("report with removed Agent token: %v", err)
	}
	if response.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("removed Agent token status/body = %d/%s, want 401", response.StatusCode(), response.Body)
	}
}

type removalHistoryExpectation struct {
	instanceID              uuid.UUID
	alertID                 uuid.UUID
	instanceName            string
	actorID                 uuid.UUID
	dispositionHistoryCount int
	alertCount              int
}

func assertRemovalHistory(t *testing.T, ctx context.Context, platform *db.Pool, client *api.ClientWithResponses, expected removalHistoryExpectation) {
	t.Helper()
	history, err := client.ListAlertHistoryWithResponse(ctx, &api.ListAlertHistoryParams{InstanceId: &expected.instanceID})
	if err != nil {
		t.Fatalf("list retained alert history: %v", err)
	}
	if history.StatusCode() != http.StatusOK || history.JSON200 == nil || history.JSON200.Total != expected.alertCount || len(history.JSON200.Items) != expected.alertCount {
		t.Fatalf("retained alert history status/body = %d/%s", history.StatusCode(), history.Body)
	}
	var retainedAlert api.AlertObservation
	for _, candidate := range history.JSON200.Items {
		if candidate.Id == expected.alertID {
			retainedAlert = candidate
			break
		}
	}
	if retainedAlert.Id != expected.alertID || retainedAlert.Status != api.RECOVERED || retainedAlert.RecoveredAt == nil || retainedAlert.InstanceName != expected.instanceName {
		t.Fatalf("retained alert = %+v, want recovered alert %s attributed to %q", retainedAlert, expected.alertID, expected.instanceName)
	}
	retainedDisposition := getDispositionDetail(t, ctx, client, expected.alertID)
	if retainedDisposition.Disposition != api.AlertDispositionACKED || retainedDisposition.DispositionBy == nil || *retainedDisposition.DispositionBy != expected.actorID ||
		len(retainedDisposition.History) != expected.dispositionHistoryCount {
		t.Fatalf("retained disposition = %+v, want ACKED with actor %s and %d events", retainedDisposition, expected.actorID, expected.dispositionHistoryCount)
	}
	var removalActorID uuid.UUID
	if err := platform.QueryRow(ctx, `SELECT actor_id FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'INSTANCE_REMOVED'`, expected.alertID).Scan(&removalActorID); err != nil {
		t.Fatalf("read attributed INSTANCE_REMOVED event: %v", err)
	}
	if removalActorID != expected.actorID {
		t.Fatalf("INSTANCE_REMOVED actor = %s, want %s", removalActorID, expected.actorID)
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
