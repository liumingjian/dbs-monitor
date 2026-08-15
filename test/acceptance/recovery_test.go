//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"gopkg.in/yaml.v3"
)

const recoveryAdminPassword = "acceptance-recovery-admin-password"

var recoveryBuild struct {
	sync.Once
	directory string
	server    string
	agent     string
	err       error
}

func TestAcceptance_REC_1(t *testing.T) {
	runRecoveryEntry(t, "REC-1", func(t *testing.T) {
		stack := newRecoveryStack(t, recoveryDatabase(t, platformDatabaseURL(t)), time.Second, 24*time.Hour)
		client := stack.waitForApplication(t)
		instanceID, token := createRecoveryAgent(t, client, "REC-1")
		reportRecoveryMetric(t, client, instanceID, token, 41)
		alertID, firstTriggeredAt := createTriggeredRecoveryAlert(t, client, instanceID, token)
		agentStarted := time.Now().UTC()
		stack.startAgent(t, instanceID, token)
		waitForRecoveryMetricAfter(t, client, instanceID, agentStarted)

		_ = stack.process.Stop()
		time.Sleep(2 * time.Second)
		stack.start(t)
		client = stack.waitForApplication(t)
		waitForRecoveryMetricAfter(t, client, instanceID, time.Now().Add(-time.Second))

		alert := waitForRecoveryAlert(t, client, instanceID)
		if alert.Id != alertID || alert.FirstTriggeredAt == nil || !alert.FirstTriggeredAt.Equal(firstTriggeredAt) {
			t.Fatalf("alert after server restart = id %s first %v, want %s/%s", alert.Id, alert.FirstTriggeredAt, alertID, firstTriggeredAt)
		}
	})
}

func TestAcceptance_REC_2(t *testing.T) {
	runRecoveryEntry(t, "REC-2", func(t *testing.T) {
		stack := newRecoveryStack(t, recoveryDatabase(t, platformDatabaseURL(t)), time.Second, 24*time.Hour)
		client := stack.waitForApplication(t)
		instanceID, token := createRecoveryAgent(t, client, "REC-2")
		createTriggeredRecoveryAlert(t, client, instanceID, token)
		agentStarted := time.Now().UTC()
		agent := stack.startAgent(t, instanceID, token)
		waitForRecoveryMetricAfter(t, client, instanceID, agentStarted)
		before := time.Now().UTC()
		_ = agent.Stop()
		time.Sleep(6 * time.Second)
		after := time.Now().UTC()
		stack.startAgent(t, instanceID, token)
		waitForRecoveryMetricAfter(t, client, instanceID, after)
		if alert := waitForRecoveryAlert(t, client, instanceID); string(alert.Status) == "NO_DATA" {
			t.Fatalf("agent restart window was projected as NO_DATA: %+v", alert)
		}

		points := recoveryMetricPoints(t, client, instanceID, before.Add(-time.Minute), time.Now().Add(time.Second))
		for _, point := range points {
			if len(point) == 2 && point[0] != nil {
				observed := time.Unix(int64(*point[0]), 0)
				if observed.After(before) && observed.Before(after) {
					t.Fatalf("agent restart gap contains an invented point at %s", observed)
				}
			}
		}
	})
}

func TestAcceptance_REC_3(t *testing.T) {
	runRecoveryEntry(t, "REC-3", func(t *testing.T) {
		restoredURL, keyDirectory, instanceID := prepareRecoveryRestore(t, "REC-3")
		if databasePort(t, restoredURL) == databasePort(t, platformDatabaseURL(t)) {
			t.Fatal("restore target shares the source PostgreSQL instance")
		}
		stack := newRecoveryStackWithKey(t, restoredURL, keyDirectory, time.Second, 24*time.Hour)
		client := stack.waitForApplication(t)
		instances, err := client.ListInstancesWithResponse(context.Background())
		if err != nil || instances.StatusCode() != http.StatusOK || instances.JSON200 == nil || len(*instances.JSON200) == 0 {
			t.Fatalf("restored instances = status %d body %s error %v", instances.StatusCode(), instances.Body, err)
		}
		rules, err := client.ListAlertRulesWithResponse(context.Background())
		if err != nil || rules.StatusCode() != http.StatusOK || rules.JSON200 == nil || len(*rules.JSON200) == 0 {
			t.Fatalf("restored rules = status %d body %s error %v", rules.StatusCode(), rules.Body, err)
		}
		waitForRecoveryMetricCount(t, client, instanceID, 2)
		waitForRecoveryMetricID(t, client, instanceID, "pg.connection.total")
		assertRestoredSemantics(t, restoredURL)
	})
}

func TestAcceptance_REC_4(t *testing.T) {
	runRecoveryEntry(t, "REC-4", func(t *testing.T) {
		restoredURL, _, _ := prepareRecoveryRestore(t, "REC-4")

		mismatched := newRecoveryKeyDirectory(t)
		if _, err := instance.OpenCredentialKeyring(mismatched, false); err != nil {
			t.Fatalf("create mismatched keyring: %v", err)
		}
		stack := newRecoveryStackWithKey(t, restoredURL, mismatched, time.Second, 24*time.Hour)
		assertRecoveryKeyringFailed(t, stack.waitForApplication(t))
		if files, _ := filepath.Glob(filepath.Join(mismatched, "master-key-v*")); len(files) != 1 {
			t.Fatalf("mismatched keyring files = %v, want its one original key", files)
		}
		_ = stack.process.Stop()

		missing := filepath.Join(t.TempDir(), "missing-keyring")
		missingStack := newRecoveryStackWithKey(t, restoredURL, missing, time.Second, 24*time.Hour)
		assertRecoveryKeyringFailed(t, missingStack.waitForApplication(t))
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("missing keyring was silently created: %v", err)
		}
	})
}

func TestAcceptance_REC_5(t *testing.T) {
	runRecoveryEntry(t, "REC-5", func(t *testing.T) {
		databaseURL := recoveryDatabase(t, platformDatabaseURL(t))
		first := newRecoveryStack(t, databaseURL, time.Minute, 24*time.Hour)
		time.Sleep(50 * time.Millisecond)
		second := newRecoveryStack(t, databaseURL, time.Minute, 24*time.Hour)
		first.waitForApplication(t)
		waitForProcessExit(t, second.process, 30*time.Second)

		connection := connectRecoveryDatabase(t, databaseURL)
		defer connection.Close(context.Background())
		var duplicateVersions int
		if err := connection.QueryRow(context.Background(), `SELECT count(*) FROM (
			SELECT version_id FROM goose_db_version WHERE is_applied GROUP BY version_id HAVING count(*) > 1
		) duplicated`).Scan(&duplicateVersions); err != nil {
			t.Fatalf("reconcile migration versions: %v", err)
		}
		if duplicateVersions != 0 {
			t.Fatalf("concurrent startup produced %d duplicate migration versions", duplicateVersions)
		}
	})
}

func TestAcceptance_REC_6(t *testing.T) {
	runRecoveryEntry(t, "REC-6", func(t *testing.T) {
		resetRecoveryDatabaseService(t)
		stack := newRecoveryStack(t, recoveryDatabaseServiceURL(t), time.Minute, 24*time.Hour)
		client := stack.waitForHTTP(t)
		assertPlatformUnavailable(t, client)
		composeService(t, "recovery", "up", "-d", "--wait", "acceptance-recovery-platform")
		client = stack.waitForApplication(t)
		instances, err := client.ListInstancesWithResponse(context.Background())
		if err != nil || instances.StatusCode() != http.StatusOK {
			t.Fatalf("business API after database recovery = status %d error %v", instances.StatusCode(), err)
		}
	})
}

func TestAcceptance_REC_7(t *testing.T) {
	runRecoveryEntry(t, "REC-7", func(t *testing.T) {
		databaseURL := recoveryDatabase(t, platformDatabaseURL(t))
		connection := connectRecoveryDatabase(t, databaseURL)
		if _, err := connection.Exec(context.Background(), "CREATE TABLE dbsmon.instance_identity (broken integer)"); err != nil {
			t.Fatalf("create incompatible migration fixture: %v", err)
		}
		connection.Close(context.Background())
		stack := newRecoveryStackWithoutWait(t, databaseURL, newRecoveryKeyDirectory(t), time.Minute, 24*time.Hour)
		waitForProcessExit(t, stack.process, 30*time.Second)
		assertRecoveryLogContains(t, stack.process, "migration")
		assertRecoveryLogContains(t, stack.process, "instance_identity")
		assertPortClosed(t, stack.address)
	})
}

func TestAcceptance_REC_8(t *testing.T) {
	runRecoveryEntry(t, "REC-8", func(t *testing.T) {
		databaseURL := recoveryDatabase(t, platformDatabaseURL(t))
		stack := newRecoveryStack(t, databaseURL, time.Minute, 200*time.Millisecond)
		client := stack.waitForApplication(t)
		connection := connectRecoveryDatabase(t, databaseURL)
		defer connection.Close(context.Background())

		var partitionCount int
		if err := connection.QueryRow(context.Background(), `SELECT count(*) FROM pg_inherits
			JOIN pg_class parent ON parent.oid = inhparent WHERE parent.relname = 'metric_sample'`).Scan(&partitionCount); err != nil {
			t.Fatalf("count prebuilt partitions: %v", err)
		}
		if partitionCount != 8 {
			t.Fatalf("prebuilt partitions = %d, want current + 7", partitionCount)
		}

		oldStart := time.Now().UTC().Truncate(time.Minute).Add(-31 * time.Minute)
		oldName := "metric_sample_" + oldStart.Format("20060102_150405")
		create := fmt.Sprintf("CREATE TABLE %s PARTITION OF metric_sample FOR VALUES FROM ('%s') TO ('%s')", oldName, oldStart.Format(time.RFC3339), oldStart.Add(time.Minute).Format(time.RFC3339))
		if _, err := connection.Exec(context.Background(), create); err != nil {
			t.Fatalf("create expired partition fixture: %v", err)
		}
		waitForRelationAbsent(t, connection, oldName)

		if _, err := connection.Exec(context.Background(), "ALTER TABLE metric_sample RENAME TO metric_sample_unavailable"); err != nil {
			t.Fatalf("inject partition maintenance failure: %v", err)
		}
		defer connection.Exec(context.Background(), "ALTER TABLE metric_sample_unavailable RENAME TO metric_sample")
		waitForPartitionFailure(t, client)
	})
}

func TestAcceptance_REC_9(t *testing.T) {
	runRecoveryEntry(t, "REC-9", func(t *testing.T) {
		stack := newRecoveryStack(t, recoveryDatabase(t, platformDatabaseURL(t)), time.Second, 24*time.Hour)
		client := stack.waitForApplication(t)
		instanceID, token := createRecoveryAgent(t, client, "REC-9")
		time.Sleep(8 * time.Second)
		reportRecoveryMetric(t, client, instanceID, token, 49)
		waitForRecoveryMetricCount(t, client, instanceID, 1)
	})
}

func TestAcceptance_REC_10(t *testing.T) {
	runRecoveryEntry(t, "REC-10", func(t *testing.T) {
		databaseURL := recoveryDatabase(t, platformDatabaseURL(t))
		first := newRecoveryStack(t, databaseURL, time.Minute, 24*time.Hour)
		client := first.waitForApplication(t)
		second := newRecoveryStackWithoutWait(t, databaseURL, newRecoveryKeyDirectory(t), time.Minute, 24*time.Hour)
		waitForProcessExit(t, second.process, 30*time.Second)
		assertRecoveryLogContains(t, second.process, "another monitor-server instance is already running")
		instances, err := client.ListInstancesWithResponse(context.Background())
		if err != nil || instances.StatusCode() != http.StatusOK {
			t.Fatalf("first server after rejected peer = status %d error %v", instances.StatusCode(), err)
		}
	})
}

func TestAcceptance_REC_11(t *testing.T) {
	runRecoveryEntry(t, "REC-11", func(t *testing.T) {
		resetRecoveryDatabaseService(t)
		composeService(t, "platform-pg16", "up", "-d", "--wait", "platform-pg16")
		wrongVersion := newRecoveryStackWithoutWait(t, pg16DatabaseURL(t), newRecoveryKeyDirectory(t), time.Minute, 24*time.Hour)
		waitForProcessExit(t, wrongVersion.process, 30*time.Second)
		assertRecoveryLogContains(t, wrongVersion.process, "PLATFORM_DATABASE_VERSION_UNSUPPORTED")
		assertPlatformTablesAbsent(t, pg16DatabaseURL(t))
		composeService(t, "platform-pg16", "stop", "platform-pg16")

		weakTLSURL := databaseURLWithQuery(t, platformDatabaseURL(t), "sslmode", "require")
		weakTLS := newRecoveryStackWithoutWait(t, weakTLSURL, newRecoveryKeyDirectory(t), time.Minute, 24*time.Hour)
		waitForProcessExit(t, weakTLS.process, 30*time.Second)
		assertRecoveryLogContains(t, weakTLS.process, "sslmode=verify-full")

		dirtyURL := recoveryDatabase(t, platformDatabaseURL(t))
		connection := connectRecoveryDatabase(t, dirtyURL)
		if _, err := connection.Exec(context.Background(), "CREATE TABLE dbsmon.customer_table (id integer)"); err != nil {
			t.Fatalf("create non-platform schema fixture: %v", err)
		}
		connection.Close(context.Background())
		dirty := newRecoveryStackWithoutWait(t, dirtyURL, newRecoveryKeyDirectory(t), time.Minute, 24*time.Hour)
		waitForProcessExit(t, dirty.process, 30*time.Second)
		assertRecoveryLogContains(t, dirty.process, "PLATFORM_DATABASE_SCHEMA_NOT_CLEAN")
		assertPlatformTablesAbsent(t, dirtyURL)
	})
}

func TestAcceptance_REC_12(t *testing.T) {
	runRecoveryEntry(t, "REC-12", func(t *testing.T) {
		stack := newRecoveryStack(t, recoveryDatabase(t, platformDatabaseURL(t)), time.Minute, 24*time.Hour)
		client := stack.waitForApplication(t)
		health, err := client.GetPlatformHealthWithResponse(context.Background())
		if err != nil || health.JSON200 == nil || string(health.JSON200.Status) != "DEGRADED" {
			t.Fatalf("shared-instance health = %+v, %v; want DEGRADED", health.JSON200, err)
		}
		assertRecoveryLogContains(t, stack.process, "PLATFORM_DATABASE_INSTANCE_SHARED")
		assertRecoveryLogContains(t, stack.process, "platform_database_prerequisite_warning")
		instances, err := client.ListInstancesWithResponse(context.Background())
		if err != nil || instances.StatusCode() != http.StatusOK {
			t.Fatalf("business API with warning-only preflight = status %d error %v", instances.StatusCode(), err)
		}
	})
}

func TestAcceptance_REC_13(t *testing.T) {
	runRecoveryEntry(t, "REC-13", func(t *testing.T) {
		resetRecoveryDatabaseService(t)
		stack := newRecoveryStack(t, recoveryDatabaseServiceURL(t), time.Minute, 24*time.Hour)
		client := stack.waitForHTTP(t)
		assertPlatformUnavailable(t, client)

		composeService(t, "platform-pg16", "up", "-d", "--wait", "platform-pg16")
		waitForRecoveryLog(t, stack.process, "PLATFORM_DATABASE_VERSION_UNSUPPORTED")
		time.Sleep(1500 * time.Millisecond)
		if attempts := recoveryLogOccurrences(t, stack.process, "PLATFORM_DATABASE_VERSION_UNSUPPORTED"); attempts > 2 {
			t.Fatalf("recovery preflight busy-looped: %d attempts in the initial backoff window", attempts)
		}
		assertRecoveryLogContains(t, stack.process, "platform_database_prerequisite_failure")
		assertProcessRunning(t, stack.process)
		health, err := client.GetPlatformHealthWithResponse(context.Background())
		if err != nil || health.JSON200 == nil || string(health.JSON200.Status) != "FAILED" {
			t.Fatalf("recovery preflight health = %+v, %v; want FAILED", health.JSON200, err)
		}
		composeService(t, "platform-pg16", "stop", "platform-pg16")
		composeService(t, "recovery", "up", "-d", "--wait", "acceptance-recovery-platform")
		stack.waitForApplication(t)
	})
}

func runRecoveryEntry(t *testing.T, id string, run func(*testing.T)) {
	t.Helper()
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("acceptance recovery environment is required")
	}
	started := time.Now()
	defer func() {
		status, actual := resultPassed, id+" recovery assertions passed against real processes and PostgreSQL"
		if t.Failed() {
			status, actual = resultFailed, id+" recovery assertions failed; see go test output"
		}
		acceptanceReport.record(id, status, actual, time.Since(started))
	}()
	run(t)
}

type recoveryStack struct {
	address       string
	baseURL       string
	configPath    string
	keyDirectory  string
	certDirectory string
	serverBinary  string
	agentBinary   string
	process       *managedProcess
	work          string
}

func newRecoveryStack(t *testing.T, databaseURL string, span, interval time.Duration) *recoveryStack {
	return newRecoveryStackWithKey(t, databaseURL, newRecoveryKeyDirectory(t), span, interval)
}

func newRecoveryStackWithKey(t *testing.T, databaseURL, keyDirectory string, span, interval time.Duration) *recoveryStack {
	stack := newRecoveryStackWithoutWait(t, databaseURL, keyDirectory, span, interval)
	return stack
}

func newRecoveryStackWithoutWait(t *testing.T, databaseURL, keyDirectory string, span, interval time.Duration) *recoveryStack {
	t.Helper()
	serverBinary, agentBinary := recoveryBinaries(t)
	work := t.TempDir()
	certDirectory := filepath.Join(work, "certs")
	agentDirectory := filepath.Join(work, "agent-binaries")
	for _, directory := range []string{certDirectory, agentDirectory} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create recovery directory: %v", err)
		}
	}
	address := availableAddress(t)
	configPath := writeRecoveryConfig(t, work, databaseURL, keyDirectory, agentDirectory, span, interval)
	stack := &recoveryStack{
		address: address, baseURL: "https://" + address, configPath: configPath,
		keyDirectory: keyDirectory, certDirectory: certDirectory,
		serverBinary: serverBinary, agentBinary: agentBinary, work: work,
	}
	stack.start(t)
	return stack
}

func (stack *recoveryStack) start(t *testing.T) {
	t.Helper()
	stack.process = startProcess(t, "recovery server", stack.serverBinary,
		filepath.Join(stack.work, fmt.Sprintf("server-%d.log", time.Now().UnixNano())), []string{
			"DBS_MONITOR_CONFIG_FILE=" + stack.configPath,
			"INITIAL_ADMIN_PASSWORD=" + recoveryAdminPassword,
			"LISTEN_ADDR=" + stack.address,
			"PUBLIC_HOST=127.0.0.1",
			"CERT_DIR=" + stack.certDirectory,
			"PGDATA=/",
		})
}

func (stack *recoveryStack) waitForHTTP(t *testing.T) *api.ClientWithResponses {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-stack.process.done:
			stack.process.done <- err
			contents, _ := os.ReadFile(stack.process.logPath)
			t.Fatalf("recovery server exited before HTTP readiness: %v\n%s", err, contents)
		default:
		}
		client, _, err := generatedClient(stack.baseURL, filepath.Join(stack.certDirectory, "ca.crt"))
		if err == nil {
			response, requestErr := client.GetPlatformHealthWithResponse(context.Background())
			if requestErr == nil && response.HTTPResponse != nil {
				return client
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("recovery server did not expose its health endpoint")
	return nil
}

func (stack *recoveryStack) waitForApplication(t *testing.T) *api.ClientWithResponses {
	t.Helper()
	client := stack.waitForHTTP(t)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
			Username: "admin", Password: recoveryAdminPassword,
		})
		if err == nil && response.StatusCode() == http.StatusNoContent {
			return client
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("recovery server did not expose the application API")
	return nil
}

func (stack *recoveryStack) startAgent(t *testing.T, instanceID fmt.Stringer, token string) *managedProcess {
	t.Helper()
	tokenPath := filepath.Join(stack.work, fmt.Sprintf("agent-token-%d", time.Now().UnixNano()))
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write recovery Agent token: %v", err)
	}
	return startProcess(t, "recovery agent", stack.agentBinary,
		filepath.Join(stack.work, fmt.Sprintf("agent-%d.log", time.Now().UnixNano())), []string{
			"SERVER_URL=" + stack.baseURL,
			"INSTANCE_ID=" + instanceID.String(),
			"AGENT_TOKEN_FILE=" + tokenPath,
			"CA_FILE=" + filepath.Join(stack.certDirectory, "ca.crt"),
			"PGDATA=/",
		})
}

func recoveryBinaries(t *testing.T) (string, string) {
	t.Helper()
	recoveryBuild.Do(func() {
		recoveryBuild.directory, recoveryBuild.err = os.MkdirTemp("", "dbs-monitor-recovery-binaries-")
		if recoveryBuild.err != nil {
			return
		}
		recoveryBuild.server = filepath.Join(recoveryBuild.directory, "dbs-monitor-server")
		recoveryBuild.agent = filepath.Join(recoveryBuild.directory, "dbs-monitor-agent")
		root := repositoryRoot(t)
		for output, pkg := range map[string]string{recoveryBuild.server: "./cmd/monitor-server", recoveryBuild.agent: "./cmd/monitor-agent"} {
			command := exec.Command("go", "build", "-ldflags", "-X main.version=1.0.0 -X main.commitSHA="+candidateSHA(), "-o", output, pkg)
			command.Dir = root
			if contents, err := command.CombinedOutput(); err != nil {
				recoveryBuild.err = fmt.Errorf("build %s: %w\n%s", pkg, err, contents)
				return
			}
		}
	})
	if recoveryBuild.err != nil {
		t.Fatal(recoveryBuild.err)
	}
	return recoveryBuild.server, recoveryBuild.agent
}

func cleanupRecoveryBinaries() {
	if recoveryBuild.directory != "" {
		_ = os.RemoveAll(recoveryBuild.directory)
	}
}

func writeRecoveryConfig(t *testing.T, work, databaseURL, keyDirectory, agentDirectory string, span, interval time.Duration) string {
	t.Helper()
	contents, err := yaml.Marshal(map[string]string{
		"platform_database_url":          databaseURL,
		"master_key_path":                keyDirectory,
		"agent_binary_dir":               agentDirectory,
		"partition_span":                 span.String(),
		"partition_maintenance_interval": interval.String(),
		"migration_lock_wait_timeout":    "10s",
	})
	if err != nil {
		t.Fatalf("encode recovery config: %v", err)
	}
	path := filepath.Join(work, "server.yaml")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write recovery config: %v", err)
	}
	return path
}

func newRecoveryKeyDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create recovery keyring directory: %v", err)
	}
	return directory
}

func createRecoveryAgent(t *testing.T, client *api.ClientWithResponses, entryID string) (uuid.UUID, string) {
	t.Helper()
	created, err := client.CreateInstanceWithResponse(context.Background(), api.InstanceCreateInput{
		Name: entryID + " target", Host: "127.0.0.1", Port: envInt(t, "ACCEPTANCE_TARGET_PORT", 55447),
		Database: "monitored", Username: "monitored", Password: "monitored",
	})
	if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create %s instance = status %d body %s error %v", entryID, created.StatusCode(), created.Body, err)
	}
	instanceID := created.JSON201.Instance.Id
	registered, err := client.RegisterAgentWithResponse(context.Background(), instanceID)
	if err != nil || registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
		t.Fatalf("register %s Agent = status %d body %s error %v", entryID, registered.StatusCode(), registered.Body, err)
	}
	return instanceID, *registered.JSON200.AgentToken
}

func reportRecoveryMetric(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, token string, value float64) {
	t.Helper()
	response, err := client.ReportAgentMetricsWithResponse(context.Background(), api.AgentReport{
		AgentVersion: "1.0.0", InstanceId: instanceID, Timestamp: time.Now().UTC(),
		Metrics: []api.AgentMetric{{Metric: api.AgentMetricMetric("host.cpu.usage_percent"), Value: value}},
	}, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil || response.StatusCode() != http.StatusOK {
		t.Fatalf("report recovery metric = status %d body %s error %v", response.StatusCode(), response.Body, err)
	}
}

func recoveryMetricPoints(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, from, to time.Time) [][]*float64 {
	t.Helper()
	step := api.Raw
	response, err := client.GetMetricSeriesWithResponse(context.Background(), instanceID, &api.GetMetricSeriesParams{
		Metric: []api.GetMetricSeriesParamsMetric{api.GetMetricSeriesParamsMetricHostCpuUsagePercent}, From: from, To: to, Step: &step,
	})
	if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("read recovery metrics = status %d body %s error %v", response.StatusCode(), response.Body, err)
	}
	var points [][]*float64
	for _, metric := range response.JSON200.Metrics {
		for _, series := range metric.Series {
			points = append(points, series.Points...)
		}
	}
	return points
}

func waitForRecoveryMetricCount(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, count int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(recoveryMetricPoints(t, client, instanceID, time.Now().Add(-10*time.Minute), time.Now().Add(time.Second))) >= count {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("recovery metric count did not reach %d", count)
}

func waitForRecoveryMetricAfter(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, after time.Time) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(recoveryMetricPoints(t, client, instanceID, after, time.Now().Add(time.Second))) > 0 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("Agent did not resume metrics after restart")
}

func waitForRecoveryMetricID(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, metricID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		step := api.Raw
		response, err := client.GetMetricSeriesWithResponse(context.Background(), instanceID, &api.GetMetricSeriesParams{
			Metric: []api.GetMetricSeriesParamsMetric{api.GetMetricSeriesParamsMetric(metricID)},
			From:   time.Now().Add(-time.Minute), To: time.Now().Add(time.Second), Step: &step,
		})
		if err == nil && response.JSON200 != nil {
			for _, metric := range response.JSON200.Metrics {
				for _, series := range metric.Series {
					if len(series.Points) > 0 {
						return
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("restored keyring did not permit collection of %s", metricID)
}

func createTriggeredRecoveryAlert(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, token string) (uuid.UUID, time.Time) {
	t.Helper()
	templates, err := client.ListAlertRuleTemplatesWithResponse(context.Background())
	if err != nil || templates.JSON200 == nil {
		t.Fatalf("list alert templates: %v", err)
	}
	for _, template := range *templates.JSON200 {
		if template.MetricId != "host.cpu.usage_percent" {
			continue
		}
		enabled := true
		consecutive := 1
		recoveryCount := 1
		scope := api.AlertRuleScope("INSTANCE")
		instances := []uuid.UUID{instanceID}
		threshold, recoveryThreshold, value := recoveryTriggerValues(template.Operator)
		created, err := client.CreateAlertRuleFromTemplateWithResponse(context.Background(), template.Id, api.AlertRuleTemplateInstantiationInput{
			Enabled: &enabled, ConsecutiveCount: &consecutive, RecoveryConsecutiveCount: &recoveryCount,
			Scope: &scope, InstanceIds: &instances, Threshold: &threshold, RecoveryThreshold: &recoveryThreshold,
		})
		if err != nil || created.StatusCode() != http.StatusCreated {
			t.Fatalf("create recovery alert rule = status %d body %s error %v", created.StatusCode(), created.Body, err)
		}
		reportRecoveryMetric(t, client, instanceID, token, value)
		alert := waitForRecoveryAlert(t, client, instanceID)
		return alert.Id, *alert.FirstTriggeredAt
	}
	t.Fatal("host CPU alert template is required for recovery acceptance")
	return uuid.UUID{}, time.Time{}
}

func recoveryTriggerValues(operator api.AlertOperator) (float64, float64, float64) {
	if strings.HasPrefix(string(operator), ">") {
		return -1, -2, 50
	}
	return 101, 102, 50
}

func waitForRecoveryAlert(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) api.AlertObservation {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{InstanceId: &instanceID})
		if err == nil && response.JSON200 != nil && len(response.JSON200.Items) > 0 && response.JSON200.Items[0].FirstTriggeredAt != nil {
			return response.JSON200.Items[0]
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("recovery alert did not reach FIRING")
	return api.AlertObservation{}
}

func prepareRecoveryRestore(t *testing.T, entryID string) (string, string, uuid.UUID) {
	t.Helper()
	sourceURL := recoveryDatabase(t, platformDatabaseURL(t))
	stack := newRecoveryStack(t, sourceURL, time.Second, 24*time.Hour)
	client := stack.waitForApplication(t)
	instanceID, token := createRecoveryAgent(t, client, entryID)
	reportRecoveryMetric(t, client, instanceID, token, 45)
	createTriggeredRecoveryAlert(t, client, instanceID, token)
	time.Sleep(1100 * time.Millisecond)
	reportRecoveryMetric(t, client, instanceID, token, 46)
	waitForRecoveryMetricCount(t, client, instanceID, 2)
	_ = stack.process.Stop()

	restoreURL := restoreDatabaseURL(t)
	connection := connectRecoveryDatabase(t, restoreURL)
	if _, err := connection.Exec(context.Background(), "DROP SCHEMA IF EXISTS dbsmon CASCADE"); err != nil {
		t.Fatalf("reset independent restore target: %v", err)
	}
	connection.Close(context.Background())
	dumpPath := filepath.Join(t.TempDir(), "platform.dump")
	runPostgresTool(t, "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "--schema=dbsmon", "--file="+dumpPath, postgresToolURL(t, sourceURL))
	runPostgresTool(t, "pg_restore", "--no-owner", "--no-privileges", "--dbname="+postgresToolURL(t, restoreURL), dumpPath)
	return restoreURL, stack.keyDirectory, instanceID
}

func assertRestoredSemantics(t *testing.T, databaseURL string) {
	t.Helper()
	connection := connectRecoveryDatabase(t, databaseURL)
	defer connection.Close(context.Background())
	var partitions, ruleSnapshots, activeAlerts, alertEvents int
	err := connection.QueryRow(context.Background(), `SELECT
		(SELECT count(DISTINCT tableoid) FROM metric_sample),
		(SELECT count(*) FROM alert_rule_version WHERE snapshot IS NOT NULL),
		(SELECT count(*) FROM alert_instance WHERE recovered_at IS NULL),
		(SELECT count(*) FROM alert_event)`).Scan(&partitions, &ruleSnapshots, &activeAlerts, &alertEvents)
	if err != nil {
		t.Fatalf("reconcile restored semantics: %v", err)
	}
	if partitions < 2 || ruleSnapshots == 0 || activeAlerts == 0 || alertEvents == 0 {
		t.Fatalf("restored semantics = partitions %d rules %d active alerts %d events %d", partitions, ruleSnapshots, activeAlerts, alertEvents)
	}
}

func assertRecoveryKeyringFailed(t *testing.T, client *api.ClientWithResponses) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		diagnostics, err := client.GetKeyringDiagnosticsWithResponse(context.Background())
		if err == nil && diagnostics.JSON200 != nil && string(diagnostics.JSON200.Status) == "FAILED" {
			if diagnostics.JSON200.Code == "" || strings.Contains(diagnostics.JSON200.Code, "DATABASE_UNREACHABLE") || strings.Contains(diagnostics.JSON200.Code, "NO_DATA") {
				t.Fatalf("keyring failure code is not explicit: %q", diagnostics.JSON200.Code)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("keyring diagnostics did not reach FAILED")
}

func platformDatabaseURL(t *testing.T) string {
	return requiredRecoveryEnv(t, "ACCEPTANCE_PLATFORM_DATABASE_URL")
}
func restoreDatabaseURL(t *testing.T) string {
	return requiredRecoveryEnv(t, "ACCEPTANCE_RESTORE_DATABASE_URL")
}
func pg16DatabaseURL(t *testing.T) string {
	return requiredRecoveryEnv(t, "ACCEPTANCE_PG16_DATABASE_URL")
}
func recoveryDatabaseServiceURL(t *testing.T) string {
	return requiredRecoveryEnv(t, "ACCEPTANCE_RECOVERY_DATABASE_URL")
}

func requiredRecoveryEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func recoveryDatabase(t *testing.T, sourceURL string) string {
	t.Helper()
	name := fmt.Sprintf("recovery_%d_%d", os.Getpid(), time.Now().UnixNano())
	adminURL := databaseURLWithName(t, sourceURL, "postgres")
	admin := connectRecoveryDatabase(t, adminURL)
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		admin.Close(context.Background())
		t.Fatalf("create recovery database: %v", err)
	}
	admin.Close(context.Background())
	t.Cleanup(func() {
		connection, err := pgx.Connect(context.Background(), adminURL)
		if err == nil {
			_, _ = connection.Exec(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
			_ = connection.Close(context.Background())
		}
	})
	databaseURL := databaseURLWithName(t, sourceURL, name)
	connection := connectRecoveryDatabase(t, databaseURL)
	if _, err := connection.Exec(context.Background(), "CREATE SCHEMA dbsmon AUTHORIZATION dbs_monitor"); err != nil {
		connection.Close(context.Background())
		t.Fatalf("create recovery schema: %v", err)
	}
	connection.Close(context.Background())
	return databaseURL
}

func databaseURLWithName(t *testing.T, value, database string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.Path = "/" + database
	return parsed.String()
}

func databaseURLWithQuery(t *testing.T, value, name, setting string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse database URL query: %v", err)
	}
	query := parsed.Query()
	query.Set(name, setting)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func databasePort(t *testing.T, value string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse database port: %v", err)
	}
	return parsed.Port()
}

func connectRecoveryDatabase(t *testing.T, databaseURL string) *pgx.Conn {
	t.Helper()
	connection, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect recovery database: %v", err)
	}
	return connection
}

func postgresToolURL(t *testing.T, value string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse PostgreSQL tool URL: %v", err)
	}
	query := parsed.Query()
	query.Del("search_path")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func runPostgresTool(t *testing.T, name string, arguments ...string) {
	t.Helper()
	command := exec.Command(name, arguments...)
	if contents, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, contents)
	}
}

func availableAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve recovery server port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release recovery server port: %v", err)
	}
	return address
}

func waitForProcessExit(t *testing.T, process *managedProcess, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-process.done:
		process.done <- err
		return err
	case <-time.After(timeout):
		t.Fatalf("%s did not exit within %s", process.name, timeout)
		return nil
	}
}

func assertProcessRunning(t *testing.T, process *managedProcess) {
	t.Helper()
	select {
	case err := <-process.done:
		process.done <- err
		t.Fatalf("%s exited during recovery: %v", process.name, err)
	default:
	}
}

func assertRecoveryLogContains(t *testing.T, process *managedProcess, expected string) {
	t.Helper()
	contents, err := os.ReadFile(process.logPath)
	if err != nil || !strings.Contains(string(contents), expected) {
		t.Fatalf("%s log does not contain %q: %v\n%s", process.name, expected, err, contents)
	}
}

func recoveryLogOccurrences(t *testing.T, process *managedProcess, expected string) int {
	t.Helper()
	contents, err := os.ReadFile(process.logPath)
	if err != nil {
		t.Fatalf("read %s log: %v", process.name, err)
	}
	return strings.Count(string(contents), expected)
}

func waitForRecoveryLog(t *testing.T, process *managedProcess, expected string) {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		contents, _ := os.ReadFile(process.logPath)
		if strings.Contains(string(contents), expected) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	assertRecoveryLogContains(t, process, expected)
}

func assertPortClosed(t *testing.T, address string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatalf("HTTP port %s remained open after fatal migration failure", address)
	}
}

func waitForRelationAbsent(t *testing.T, connection *pgx.Conn, relation string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var exists bool
		if err := connection.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", relation).Scan(&exists); err == nil && !exists {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expired partition %s was not dropped", relation)
}

func waitForPartitionFailure(t *testing.T, client *api.ClientWithResponses) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		diagnostics, err := client.GetPartitionDiagnosticsWithResponse(context.Background())
		if err == nil && diagnostics.JSON200 != nil && string(diagnostics.JSON200.Status) != "OK" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("partition maintenance failure did not enter the platform health fact source")
}

func assertPlatformUnavailable(t *testing.T, client *api.ClientWithResponses) {
	t.Helper()
	health, err := client.GetPlatformHealthWithResponse(context.Background())
	if err != nil || health.JSON200 == nil || string(health.JSON200.Status) != "FAILED" {
		t.Fatalf("unavailable database health = %+v, %v; want FAILED", health.JSON200, err)
	}
	instances, err := client.ListInstancesWithResponse(context.Background())
	if err != nil || instances.StatusCode() != http.StatusServiceUnavailable ||
		instances.HTTPResponse.Header.Get("X-DBS-Platform-Fault") != "true" || !strings.Contains(string(instances.Body), "INTERNAL") {
		t.Fatalf("business API during database outage = status %d body %s error %v", instances.StatusCode(), instances.Body, err)
	}
}

func assertPlatformTablesAbsent(t *testing.T, databaseURL string) {
	t.Helper()
	connection := connectRecoveryDatabase(t, databaseURL)
	defer connection.Close(context.Background())
	var exists bool
	if err := connection.QueryRow(context.Background(), "SELECT to_regclass('dbsmon.app_user') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("inspect rejected platform database: %v", err)
	}
	if exists {
		t.Fatal("preflight rejection created platform tables")
	}
}

func composeService(t *testing.T, profile string, arguments ...string) {
	t.Helper()
	project := requiredRecoveryEnv(t, "ACCEPTANCE_COMPOSE_PROJECT")
	commandArguments := []string{"compose", "-p", project, "--profile", profile}
	commandArguments = append(commandArguments, arguments...)
	command := exec.Command("docker", commandArguments...)
	command.Dir = repositoryRoot(t)
	if contents, err := command.CombinedOutput(); err != nil {
		t.Fatalf("docker compose %s: %v\n%s", strings.Join(arguments, " "), err, contents)
	}
}

func resetRecoveryDatabaseService(t *testing.T) {
	t.Helper()
	for _, service := range []struct{ profile, name string }{
		{profile: "platform-pg16", name: "platform-pg16"},
		{profile: "recovery", name: "acceptance-recovery-platform"},
	} {
		composeService(t, service.profile, "rm", "-s", "-f", "-v", service.name)
		t.Cleanup(func() { composeService(t, service.profile, "rm", "-s", "-f", "-v", service.name) })
	}
}
