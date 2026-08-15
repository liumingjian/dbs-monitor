//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	issue60AdminPassword = "acceptance-admin-password"
	issue60S1TestRef     = "test/acceptance/issue60_test.go::TestAcceptance_AC_01_S1"
	issue60S2TestRef     = "test/acceptance/issue60_test.go::TestAcceptance_AC_01_S2"
)

type issue60Runtime struct {
	client      *api.ClientWithResponses
	baseURL     string
	caPath      string
	agentBinary string
	work        string
}

func TestAcceptance_AC_01_S1(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-01-S1")
	}
	started := time.Now()
	defer recordIssue60Result(t, "AC-01-S1", "real Task samples and all three derived metrics were readable", started)()

	runtime := startIssue60Runtime(t, 18448)
	instanceID := createIssue60Instance(t, runtime.client, "AC-01-S1 target", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, instanceID)
	startIssue60Agent(t, runtime, instanceID)

	wantMetrics := []metric.MetricID{
		metric.MetricAvailabilityReachable,
		metric.MetricCollectorLastSuccessTime,
		metric.MetricAgentStatus,
	}
	eventuallyIssue60(t, 45*time.Second, func() (bool, string) {
		states, err := runtime.client.ListCollectionTaskStatesWithResponse(context.Background(), instanceID)
		if err != nil || states.StatusCode() != http.StatusOK || states.JSON200 == nil {
			return false, fmt.Sprintf("collection task states unavailable: status=%d error=%v", states.StatusCode(), err)
		}
		if len(*states.JSON200) != len(metric.Tasks) {
			return false, fmt.Sprintf("collection task count=%d want=%d", len(*states.JSON200), len(metric.Tasks))
		}
		for _, state := range *states.JSON200 {
			if state.LastResult == nil {
				return false, fmt.Sprintf("task %s has not completed", state.TaskId)
			}
		}
		series, detail := readIssue60Metrics(runtime.client, instanceID, wantMetrics, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
		if series == nil {
			return false, detail
		}
		for _, metricID := range wantMetrics {
			item := series[metricID]
			if !issue60HasPoints(item) {
				return false, fmt.Sprintf("derived metric %s has no points: %s", metricID, issue60Outcome(item))
			}
		}
		return true, ""
	})
}

func TestAcceptance_AC_01_S2(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-01-S2")
	}
	started := time.Now()
	defer recordIssue60Result(t, "AC-01-S2", "whole dictionary reconciled and eleven real unavailability conditions were observed", started)()

	runtime := startIssue60Runtime(t, 18449)
	admin := openIssue60Target(t, "monitored", "monitored", "monitored")
	defer admin.Close(context.Background())
	prepareIssue60Targets(t, admin)

	healthyID := createIssue60Instance(t, runtime.client, "AC-01-S2 healthy", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, healthyID)
	startIssue60Agent(t, runtime, healthyID)

	capabilities := waitForIssue60Capabilities(t, runtime.client, healthyID)
	var reconciled map[metric.MetricID]issue60MetricResult
	eventuallyIssue60(t, 45*time.Second, func() (bool, string) {
		found, detail := readIssue60Metrics(runtime.client, healthyID, issue60DictionaryIDs(), time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
		if found == nil {
			return false, detail
		}
		for _, declaration := range metric.Metrics {
			item, exists := found[declaration.ID]
			if !exists {
				return false, fmt.Sprintf("dictionary metric %s is absent from API response", declaration.ID)
			}
			wantReason, blocked := issue60CapabilityReason(declaration.ID, capabilities)
			if blocked {
				if !item.HasReason || item.Unavailability != wantReason || issue60HasPoints(item) {
					return false, fmt.Sprintf("metric %s outcome=%s want=%s", declaration.ID, issue60Outcome(item), wantReason)
				}
				continue
			}
			if !issue60HasPoints(item) || item.HasReason {
				return false, fmt.Sprintf("applicable metric %s has no real sample: %s", declaration.ID, issue60Outcome(item))
			}
		}
		reconciled = found
		return true, ""
	})

	produced := map[api.Unavailability]bool{}
	for _, item := range reconciled {
		if item.HasReason {
			produced[item.Unavailability] = true
		}
	}

	freshID := createIssue60Instance(t, runtime.client, "AC-01-S2 fresh", "monitored", "monitored", issue60TargetPort(t))
	produced[readIssue60MetricCode(t, runtime.client, freshID, metric.MetricCollectorLastSuccessTime, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))] = true

	produced[readIssue60MetricCode(t, runtime.client, healthyID, metric.MetricConnectionTotal, time.Unix(1, 0), time.Unix(2, 0))] = true

	offlineID := createIssue60Instance(t, runtime.client, "AC-01-S2 offline Agent", "monitored", "monitored", issue60TargetPort(t))
	registerIssue60Agent(t, runtime.client, offlineID)
	produced[readIssue60MetricCode(t, runtime.client, offlineID, metric.MetricHostCPUUsagePercent, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))] = true

	unreachableID := createIssue60Instance(t, runtime.client, "AC-01-S2 unreachable", "monitored", "monitored", 1)
	queryStats, err := runtime.client.GetQueryStatisticsSnapshotWithResponse(context.Background(), unreachableID)
	if err != nil || queryStats.StatusCode() != http.StatusOK || queryStats.JSON200 == nil || queryStats.JSON200.Unavailability == nil {
		t.Fatalf("read feature-disabled query statistics: status=%d body=%s error=%v", queryStats.StatusCode(), queryStats.Body, err)
	}
	produced[*queryStats.JSON200.Unavailability] = true
	waitForIssue60MetricCode(t, runtime.client, unreachableID, metric.MetricConnectionTotal, api.DBUNREACHABLE)
	produced[api.DBUNREACHABLE] = true

	permissionID := createIssue60Instance(t, runtime.client, "AC-01-S2 permission", "monitored", "issue60-limited", issue60TargetPort(t))
	waitForIssue60MetricCode(t, runtime.client, permissionID, metric.MetricConnectionTotal, api.PERMISSIONDENIED)
	produced[api.PERMISSIONDENIED] = true

	missingExtensionID := createIssue60Instance(t, runtime.client, "AC-01-S2 no extension", "issue60_noext", "monitored", issue60TargetPort(t))
	waitForIssue60QueryStatisticsCode(t, runtime.client, missingExtensionID, api.EXTENSIONMISSING)
	produced[api.EXTENSIONMISSING] = true

	failingID := createIssue60Instance(t, runtime.client, "AC-01-S2 failed Task", "issue60_failed", "issue60-pgmon", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, failingID)
	waitForIssue60MetricPoints(t, runtime.client, failingID, metric.MetricCollectorLastSuccessTime)
	failingDB := openIssue60Target(t, "issue60_failed", "monitored", "monitored")
	if _, err := failingDB.Exec(context.Background(), "REVOKE ALL ON pg_stat_statements FROM PUBLIC"); err != nil {
		t.Fatalf("revoke query statistics view access: %v", err)
	}
	failingDB.Close(context.Background())
	waitForIssue60QueryStatisticsCode(t, runtime.client, failingID, api.COLLECTIONFAILED)
	produced[api.COLLECTIONFAILED] = true
	waitForIssue60MetricCode(t, runtime.client, failingID, metric.MetricConnectionActive, api.STALE)
	produced[api.STALE] = true

	if _, err := admin.Exec(context.Background(), "SELECT pg_stat_reset()"); err != nil {
		t.Fatalf("reset PostgreSQL counters: %v", err)
	}
	waitForIssue60MetricCode(t, runtime.client, healthyID, metric.MetricTPS, api.COUNTERRESET)
	produced[api.COUNTERRESET] = true

	wantCodes := []api.Unavailability{
		api.NOSAMPLESYET,
		api.NODATAINRANGE,
		api.STALE,
		api.COLLECTIONFAILED,
		api.DBUNREACHABLE,
		api.AGENTOFFLINE,
		api.PERMISSIONDENIED,
		api.EXTENSIONMISSING,
		api.FEATUREDISABLED,
		api.NOTAPPLICABLEROLE,
		api.COUNTERRESET,
	}
	for _, code := range wantCodes {
		if !produced[code] {
			t.Errorf("real unavailability condition %s was not observed", code)
		}
	}
}

func recordIssue60Result(t *testing.T, entryID, passedMessage string, started time.Time) func() {
	return func() {
		status, message := resultPassed, passedMessage
		if t.Failed() {
			status, message = resultFailed, "issue 60 acceptance failed; see go test output"
		}
		acceptanceReport.record(entryID, status, message, time.Since(started))
	}
}

func startIssue60Runtime(t *testing.T, port int) issue60Runtime {
	t.Helper()
	root := repositoryRoot(t)
	work := t.TempDir()
	serverBinary := filepath.Join(work, "dbs-monitor-server")
	agentBinary := filepath.Join(work, "dbs-monitor-agent")
	buildBinary(t, root, serverBinary, "./cmd/monitor-server")
	buildBinary(t, root, agentBinary, "./cmd/monitor-agent")

	certDirectory := filepath.Join(work, "certs")
	keyDirectory := filepath.Join(work, "credentials")
	agentBinaryDirectory := filepath.Join(work, "agent-binaries")
	for _, directory := range []string{certDirectory, keyDirectory, agentBinaryDirectory} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	configPath := writeServerConfig(t, work, os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL"), keyDirectory, agentBinaryDirectory)
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port)
	server := startProcess(t, "issue-60 server", serverBinary, filepath.Join(work, "server.log"), []string{
		"DBS_MONITOR_CONFIG_FILE=" + configPath,
		"INITIAL_ADMIN_PASSWORD=" + issue60AdminPassword,
		fmt.Sprintf("LISTEN_ADDR=127.0.0.1:%d", port),
		"PUBLIC_HOST=127.0.0.1",
		"CERT_DIR=" + certDirectory,
		"PGDATA=/",
	})
	client := waitForAPI(t, server, baseURL, filepath.Join(certDirectory, "ca.crt"))
	login, err := client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
		Username: "admin", Password: issue60AdminPassword,
	})
	if err != nil || login.StatusCode() != http.StatusNoContent {
		t.Fatalf("login through generated client: status=%d body=%s error=%v", login.StatusCode(), login.Body, err)
	}
	return issue60Runtime{
		client: client, baseURL: baseURL, caPath: filepath.Join(certDirectory, "ca.crt"),
		agentBinary: agentBinary, work: work,
	}
}

func createIssue60Instance(t *testing.T, client *api.ClientWithResponses, name, database, username string, port int) uuid.UUID {
	t.Helper()
	created, err := client.CreateInstanceWithResponse(context.Background(), api.InstanceCreateInput{
		Name: name, Host: "127.0.0.1", Port: port, Database: database, Username: username, Password: "monitored",
	})
	if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create %s through generated client: status=%d body=%s error=%v", name, created.StatusCode(), created.Body, err)
	}
	return created.JSON201.Instance.Id
}

func setIssue60TaskIntervals(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	for _, task := range metric.Tasks {
		response, err := client.UpdateCollectionTaskIntervalWithResponse(context.Background(), instanceID, string(task.ID), api.CollectionTaskIntervalInput{IntervalSeconds: 5})
		if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			t.Fatalf("set task %s interval: status=%d body=%s error=%v", task.ID, response.StatusCode(), response.Body, err)
		}
	}
}

func registerIssue60Agent(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) string {
	t.Helper()
	registered, err := client.RegisterAgentWithResponse(context.Background(), instanceID)
	if err != nil || registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
		t.Fatalf("register Agent: status=%d body=%s error=%v", registered.StatusCode(), registered.Body, err)
	}
	return *registered.JSON200.AgentToken
}

func startIssue60Agent(t *testing.T, runtime issue60Runtime, instanceID uuid.UUID) {
	t.Helper()
	tokenPath := filepath.Join(runtime.work, "agent-token-"+instanceID.String())
	if err := os.WriteFile(tokenPath, []byte(registerIssue60Agent(t, runtime.client, instanceID)+"\n"), 0o600); err != nil {
		t.Fatalf("write Agent token: %v", err)
	}
	startProcess(t, "issue-60 Agent", runtime.agentBinary, filepath.Join(runtime.work, "agent-"+instanceID.String()+".log"), []string{
		"SERVER_URL=" + runtime.baseURL,
		"INSTANCE_ID=" + instanceID.String(),
		"AGENT_TOKEN_FILE=" + tokenPath,
		"CA_FILE=" + runtime.caPath,
		"PGDATA=/",
	})
}

func issue60TargetPort(t *testing.T) int {
	t.Helper()
	return envInt(t, "ACCEPTANCE_TARGET_PORT", 55447)
}

func openIssue60Target(t *testing.T, database, username, password string) *pgx.Conn {
	t.Helper()
	address := fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable", username, password, issue60TargetPort(t), database)
	connection, err := pgx.Connect(context.Background(), address)
	if err != nil {
		t.Fatalf("connect to target database %s: %v", database, err)
	}
	return connection
}

func prepareIssue60Targets(t *testing.T, admin *pgx.Conn) {
	t.Helper()
	statements := []string{
		"DROP DATABASE IF EXISTS issue60_noext WITH (FORCE)",
		"DROP DATABASE IF EXISTS issue60_failed WITH (FORCE)",
		"DROP ROLE IF EXISTS \"issue60-limited\"",
		"DROP ROLE IF EXISTS \"issue60-pgmon\"",
		"CREATE ROLE \"issue60-limited\" LOGIN PASSWORD 'monitored'",
		"CREATE ROLE \"issue60-pgmon\" LOGIN PASSWORD 'monitored'",
		"GRANT pg_monitor TO \"issue60-pgmon\"",
		"CREATE DATABASE issue60_noext",
		"CREATE DATABASE issue60_failed",
	}
	for _, statement := range statements {
		if _, err := admin.Exec(context.Background(), statement); err != nil {
			t.Fatalf("prepare issue 60 target with %q: %v", statement, err)
		}
	}
	failingDB := openIssue60Target(t, "issue60_failed", "monitored", "monitored")
	defer failingDB.Close(context.Background())
	if _, err := failingDB.Exec(context.Background(), "CREATE EXTENSION pg_stat_statements"); err != nil {
		t.Fatalf("install pg_stat_statements in failure target: %v", err)
	}
}

type issue60MetricResult struct {
	Series []struct {
		Labels map[string]string `json:"labels"`
		Points [][]*float64      `json:"points"`
	} `json:"series"`
	Unavailability api.Unavailability
	HasReason      bool
}

func issue60DictionaryIDs() []metric.MetricID {
	ids := make([]metric.MetricID, 0, len(metric.Metrics))
	for _, declaration := range metric.Metrics {
		ids = append(ids, declaration.ID)
	}
	return ids
}

func readIssue60Metrics(client *api.ClientWithResponses, instanceID uuid.UUID, ids []metric.MetricID, from, to time.Time) (map[metric.MetricID]issue60MetricResult, string) {
	requested := make([]api.GetMetricSeriesParamsMetric, 0, len(ids))
	for _, id := range ids {
		requested = append(requested, api.GetMetricSeriesParamsMetric(id))
	}
	step := api.Raw
	response, err := client.GetMetricSeriesWithResponse(context.Background(), instanceID, &api.GetMetricSeriesParams{
		Metric: requested, From: from.UTC(), To: to.UTC(), Step: &step,
	})
	if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, fmt.Sprintf("metric series unavailable: status=%d body=%s error=%v", response.StatusCode(), response.Body, err)
	}
	if len(response.JSON200.Metrics) != len(ids) {
		return nil, fmt.Sprintf("metric count=%d want=%d", len(response.JSON200.Metrics), len(ids))
	}
	result := make(map[metric.MetricID]issue60MetricResult, len(ids))
	for _, item := range response.JSON200.Metrics {
		found := issue60MetricResult{Series: item.Series}
		if !item.Unavailability.IsSpecified() {
			return nil, fmt.Sprintf("metric %s omitted required unavailability", item.Metric)
		}
		if !item.Unavailability.IsNull() {
			code, getErr := item.Unavailability.Get()
			if getErr != nil {
				return nil, fmt.Sprintf("metric %s unavailability: %v", item.Metric, getErr)
			}
			found.Unavailability, found.HasReason = code, true
		}
		if issue60HasPoints(found) && found.HasReason {
			return nil, fmt.Sprintf("metric %s has both points and reason %s", item.Metric, found.Unavailability)
		}
		result[metric.MetricID(item.Metric)] = found
	}
	return result, ""
}

func issue60HasPoints(item issue60MetricResult) bool {
	for _, series := range item.Series {
		if len(series.Points) > 0 {
			return true
		}
	}
	return false
}

func issue60Outcome(item issue60MetricResult) string {
	if issue60HasPoints(item) {
		return "points"
	}
	if item.HasReason {
		return string(item.Unavailability)
	}
	return "neither points nor reason"
}

func waitForIssue60Capabilities(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) map[metric.CapabilityID]api.CapabilityStatus {
	t.Helper()
	var result map[metric.CapabilityID]api.CapabilityStatus
	eventuallyIssue60(t, 30*time.Second, func() (bool, string) {
		response, err := client.ListCapabilitySnapshotWithResponse(context.Background(), instanceID)
		if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return false, fmt.Sprintf("capability snapshot unavailable: status=%d error=%v", response.StatusCode(), err)
		}
		states := make(map[metric.CapabilityID]api.CapabilityStatus, len(*response.JSON200))
		for _, capability := range *response.JSON200 {
			if capability.Status == api.UNKNOWN || capability.ObservedAt == nil {
				return false, fmt.Sprintf("capability %s is not observed yet", capability.CapabilityId)
			}
			states[metric.CapabilityID(capability.CapabilityId)] = capability.Status
		}
		if len(states) != len(metric.Capabilities) {
			return false, fmt.Sprintf("capability count=%d want=%d", len(states), len(metric.Capabilities))
		}
		result = states
		return true, ""
	})
	return result
}

func issue60CapabilityReason(metricID metric.MetricID, capabilities map[metric.CapabilityID]api.CapabilityStatus) (api.Unavailability, bool) {
	task, exists := metric.TaskForMetric(metricID)
	if !exists {
		return "", false
	}
	for _, required := range task.Requires {
		switch capabilities[required] {
		case api.PRESENT:
			continue
		case api.MISSING:
			if required == metric.CapabilityRolePGMonitor {
				return api.PERMISSIONDENIED, true
			}
			return api.EXTENSIONMISSING, true
		case api.NOTAPPLICABLE:
			return api.NOTAPPLICABLEROLE, true
		case api.UNKNOWN:
			return api.COLLECTIONFAILED, true
		}
	}
	return "", false
}

func readIssue60MetricCode(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, metricID metric.MetricID, from, to time.Time) api.Unavailability {
	t.Helper()
	series, detail := readIssue60Metrics(client, instanceID, []metric.MetricID{metricID}, from, to)
	if series == nil {
		t.Fatal(detail)
	}
	found := series[metricID]
	if !found.HasReason || issue60HasPoints(found) {
		t.Fatalf("metric %s outcome=%s, want only an unavailability", metricID, issue60Outcome(found))
	}
	return found.Unavailability
}

func waitForIssue60MetricCode(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, metricID metric.MetricID, want api.Unavailability) {
	t.Helper()
	eventuallyIssue60(t, 45*time.Second, func() (bool, string) {
		now := time.Now().UTC()
		series, detail := readIssue60Metrics(client, instanceID, []metric.MetricID{metricID}, now.Add(time.Second), now.Add(2*time.Second))
		if series == nil {
			return false, detail
		}
		found := series[metricID]
		return found.HasReason && found.Unavailability == want, fmt.Sprintf("metric %s outcome=%s want=%s", metricID, issue60Outcome(found), want)
	})
}

func waitForIssue60MetricPoints(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, metricID metric.MetricID) {
	t.Helper()
	eventuallyIssue60(t, 45*time.Second, func() (bool, string) {
		series, detail := readIssue60Metrics(client, instanceID, []metric.MetricID{metricID}, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
		if series == nil {
			return false, detail
		}
		found := series[metricID]
		return issue60HasPoints(found) && !found.HasReason, fmt.Sprintf("metric %s outcome=%s want=points", metricID, issue60Outcome(found))
	})
}

func waitForIssue60QueryStatisticsCode(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, want api.Unavailability) {
	t.Helper()
	eventuallyIssue60(t, 45*time.Second, func() (bool, string) {
		response, err := client.GetQueryStatisticsSnapshotWithResponse(context.Background(), instanceID)
		if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return false, fmt.Sprintf("query statistics unavailable: status=%d error=%v", response.StatusCode(), err)
		}
		if response.JSON200.Unavailability == nil {
			return false, fmt.Sprintf("query statistics has %d items and no reason", len(response.JSON200.Items))
		}
		return *response.JSON200.Unavailability == want, fmt.Sprintf("query statistics reason=%s want=%s", *response.JSON200.Unavailability, want)
	})
}

func eventuallyIssue60(t *testing.T, timeout time.Duration, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	lastDetail := "condition was not evaluated"
	for time.Now().Before(deadline) {
		if ok, detail := check(); ok {
			return
		} else if detail != "" {
			lastDetail = detail
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal(lastDetail)
}
