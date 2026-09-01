//go:build acceptance

package acceptance

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"gopkg.in/yaml.v3"
)

const tracerID = onboardingEntryID

var (
	acceptanceReport *acceptanceResult

	// These method expressions make operationId deletion a compile-time acceptance failure.
	_ = (*api.ClientWithResponses).CreateSessionWithResponse
	_ = (*api.ClientWithResponses).CreateUserWithResponse
	_ = (*api.ClientWithResponses).CreateInstanceWithResponse
	_ = (*api.ClientWithResponses).RegisterAgentWithResponse
	_ = (*api.ClientWithResponses).ReportAgentMetricsWithResponse
	_ = (*api.ClientWithResponses).GetMetricSeriesWithResponse
)

func TestMain(m *testing.M) {
	matrix, err := readMatrix("matrix.yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	acceptanceReport = newResult(matrix, candidateSHA())
	resultPath := os.Getenv("ACCEPTANCE_RESULT_PATH")
	if resultPath == "" {
		resultPath = filepath.Join("..", "..", "results", "acceptance-result.json")
	}

	code := m.Run()
	cleanupRecoveryBinaries()
	if err := acceptanceReport.write(resultPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	} else {
		fmt.Printf("acceptance result: %s\n", resultPath)
	}
	os.Exit(acceptanceReport.exitCode(code))
}

func TestAcceptance_AC_08_S1(t *testing.T) {
	platformDatabaseURL := os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL")
	if platformDatabaseURL == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for the live tracer")
	}
	started := time.Now()
	defer func() {
		status, actualResult := resultPassed, "generated-client flow reached a metric reported by the real Agent process"
		if t.Failed() {
			status, actualResult = resultFailed, "live tracer failed; see go test output"
		}
		acceptanceReport.record(tracerID, status, actualResult, time.Since(started))
	}()

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
	configPath := writeServerConfig(t, work, recoveryDatabase(t, platformDatabaseURL), keyDirectory, agentBinaryDirectory)
	server := startProcess(t, "server", serverBinary, filepath.Join(work, "server.log"), []string{
		"DBS_MONITOR_CONFIG_FILE=" + configPath,
		"INITIAL_ADMIN_PASSWORD=acceptance-admin-password",
		"LISTEN_ADDR=127.0.0.1:18443",
		"PUBLIC_HOST=127.0.0.1",
		"CERT_DIR=" + certDirectory,
		"SSL_CERT_FILE=" + os.Getenv("ACCEPTANCE_SMTP_CA_FILE"),
		"PGDATA=/",
	})

	baseURL := "https://127.0.0.1:18443"
	client := waitForAPI(t, server, baseURL, filepath.Join(certDirectory, "ca.crt"))
	login, err := client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
		Username: "admin", Password: "acceptance-admin-password",
	})
	if err != nil {
		t.Fatalf("login through generated client: %v", err)
	}
	if login.StatusCode() != http.StatusNoContent {
		t.Fatalf("login through generated client: status=%d body=%s", login.StatusCode(), login.Body)
	}

	for _, user := range []api.UserCreateInput{
		{Username: "acceptance-platform-admin", Role: api.PLATFORMADMIN},
		{Username: "acceptance-alert-admin", Role: api.ALERTADMIN},
		{Username: "acceptance-viewer", Role: api.READONLY},
	} {
		response, err := client.CreateUserWithResponse(context.Background(), user)
		if err != nil {
			t.Fatalf("create %s through generated client: %v", user.Role, err)
		}
		if response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
			t.Fatalf("create %s through generated client: status=%d body=%s", user.Role, response.StatusCode(), response.Body)
		}
	}

	targetPort := envInt(t, "ACCEPTANCE_TARGET_PORT", 55447)
	created, err := client.CreateInstanceWithResponse(context.Background(), api.InstanceCreateInput{
		Name: "AC-08-S1 target", Host: "127.0.0.1", Port: targetPort,
		Database: "monitored", Username: "monitored", Password: "monitored",
	})
	if err != nil {
		t.Fatalf("create verified instance through generated client: %v", err)
	}
	if created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create verified instance through generated client: status=%d body=%s", created.StatusCode(), created.Body)
	}
	instanceID := created.JSON201.Instance.Id

	registered, err := client.RegisterAgentWithResponse(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("register Agent through generated client: %v", err)
	}
	if registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
		t.Fatalf("register Agent through generated client: status=%d body=%s", registered.StatusCode(), registered.Body)
	}
	tokenPath := filepath.Join(work, "agent-token")
	if err := os.WriteFile(tokenPath, []byte(*registered.JSON200.AgentToken+"\n"), 0o600); err != nil {
		t.Fatalf("write Agent token: %v", err)
	}
	startProcess(t, "agent", agentBinary, filepath.Join(work, "agent.log"), []string{
		"SERVER_URL=" + baseURL,
		"INSTANCE_ID=" + instanceID.String(),
		"AGENT_TOKEN_FILE=" + tokenPath,
		"CA_FILE=" + filepath.Join(certDirectory, "ca.crt"),
		"PGDATA=/",
	})

	waitForAgentMetric(t, client, instanceID)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func buildBinary(t *testing.T, root, output, pkg string) {
	t.Helper()
	command := exec.Command("go", "build", "-ldflags", "-X main.version=1.0.0 -X main.commitSHA="+candidateSHA(), "-o", output, pkg)
	command.Dir = root
	if contents, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, contents)
	}
}

func writeServerConfig(t *testing.T, work, databaseURL, keyDirectory, binaryDirectory string) string {
	t.Helper()
	config := struct {
		AgentBinaryDirectory  string `yaml:"agent_binary_dir"`
		MasterKeyDirectory    string `yaml:"master_key_path"`
		RepeatIntervalMinimum string `yaml:"repeat_interval_minimum"`
		RetryBackoffCap       string `yaml:"notification_retry_backoff_cap"`
		PlatformDatabaseURL   string `yaml:"platform_database_url"`
	}{
		AgentBinaryDirectory:  binaryDirectory,
		MasterKeyDirectory:    keyDirectory,
		RepeatIntervalMinimum: "30s",
		RetryBackoffCap:       "5s",
		PlatformDatabaseURL:   databaseURL,
	}
	contents, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("encode server config: %v", err)
	}
	path := filepath.Join(work, "server.yaml")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write server config: %v", err)
	}
	return path
}

type managedProcess struct {
	name    string
	command *exec.Cmd
	done    chan struct{}
	logPath string
	stop    sync.Once
	exitErr error
}

func startProcess(t *testing.T, name, binary, logPath string, environment []string) *managedProcess {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create %s log: %v", name, err)
	}
	command := exec.Command(binary)
	command.Env = append(os.Environ(), environment...)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start %s: %v", name, err)
	}
	process := &managedProcess{name: name, command: command, done: make(chan struct{}), logPath: logPath}
	go func() {
		process.exitErr = command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		_ = process.Stop()
		_ = logFile.Close()
		if t.Failed() {
			if contents, err := os.ReadFile(logPath); err == nil && len(contents) > 0 {
				t.Logf("%s log:\n%s", name, contents)
			}
		}
	})
	return process
}

func (process *managedProcess) Stop() error {
	process.stop.Do(func() {
		select {
		case <-process.done:
			return
		default:
		}
		_ = process.command.Process.Signal(os.Interrupt)
		select {
		case <-process.done:
		case <-time.After(5 * time.Second):
			_ = process.command.Process.Kill()
			<-process.done
		}
	})
	return process.exitErr
}

func waitForAPI(t *testing.T, server *managedProcess, baseURL, caPath string) *api.ClientWithResponses {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-server.done:
			contents, _ := os.ReadFile(server.logPath)
			t.Fatalf("%s exited before readiness: %v\n%s", server.name, server.exitErr, contents)
		default:
		}
		client, httpClient, err := generatedClient(baseURL, caPath)
		if err == nil {
			response, requestErr := httpClient.Get(baseURL + "/login")
			if requestErr == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return client
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	contents, _ := os.ReadFile(server.logPath)
	t.Fatalf("server did not become ready\n%s", contents)
	return nil
}

func generatedClient(baseURL, caPath string) (*api.ClientWithResponses, *http.Client, error) {
	ca, err := os.ReadFile(caPath)
	if err != nil {
		return nil, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, nil, fmt.Errorf("server CA is not valid PEM")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}
	httpClient := &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    roots,
				MinVersion: tls.VersionTLS13,
			},
		},
		Timeout: 5 * time.Second,
	}
	client, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(httpClient))
	return client, httpClient, err
}

func waitForAgentMetric(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	step := api.Raw
	for time.Now().Before(deadline) {
		now := time.Now().UTC()
		response, err := client.GetMetricSeriesWithResponse(context.Background(), instanceID, &api.GetMetricSeriesParams{
			Metric: []api.MetricId{api.MetricIdHostCpuUsagePercent},
			From:   now.Add(-time.Minute), To: now.Add(time.Second), Step: &step,
		})
		if err == nil && response.StatusCode() == http.StatusOK && response.JSON200 != nil {
			for _, metric := range response.JSON200.Metrics {
				if metric.Metric != "host.cpu.usage_percent" {
					continue
				}
				for _, series := range metric.Series {
					for _, point := range series.Points {
						if len(point) == 2 && point[1] != nil {
							return
						}
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("real Agent report did not become visible through getMetricSeries")
}

func candidateSHA() string {
	if value := os.Getenv("ACCEPTANCE_CANDIDATE_SHA"); value != "" {
		return value
	}
	contents, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(contents))
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		t.Fatalf("%s must be a positive integer", name)
	}
	return parsed
}
