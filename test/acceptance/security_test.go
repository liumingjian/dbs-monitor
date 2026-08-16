//go:build acceptance

package acceptance

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"gopkg.in/yaml.v3"
)

const securityAdminPassword = "acceptance-admin-password"

type securityRuntime struct {
	baseURL string
	caPath  string
	client  *api.ClientWithResponses
	http    *http.Client
	logPath string
}

type securityServerOptions struct {
	port        int
	absoluteTTL time.Duration
	idleTTL     time.Duration
	fixture     string
	embedWeb    bool
}

func TestAcceptance_SEC_1(t *testing.T) {
	runSecurityEntry(t, "SEC-1", "six exact security headers covered API, static, and Agent handler chains", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18450})
		loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)
		want := securityHeaderGolden(t)
		for _, request := range []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{name: "API", method: http.MethodGet, path: "/api/v1/me"},
			{name: "static root", method: http.MethodGet, path: "/"},
			{name: "static asset fallback", method: http.MethodGet, path: "/assets/sec-1.js"},
			{name: "Agent ingress", method: http.MethodPost, path: "/api/agent/v1/report", body: "{}"},
		} {
			t.Run(request.name, func(t *testing.T) {
				req, err := http.NewRequest(request.method, runtime.baseURL+request.path, strings.NewReader(request.body))
				if err != nil {
					t.Fatalf("create request: %v", err)
				}
				if request.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				response, err := runtime.http.Do(req)
				if err != nil {
					t.Fatalf("request %s: %v", request.path, err)
				}
				response.Body.Close()
				assertSecurityHeaders(t, response.Header, want)
			})
		}
		csp := want["Content-Security-Policy"]
		if regexp.MustCompile(`script-src[^;]*(?:'unsafe-inline'|'unsafe-eval')`).MatchString(csp) {
			t.Fatalf("CSP script-src permits inline or evaluated scripts: %s", csp)
		}
		if want["Strict-Transport-Security"] != "max-age=31536000" {
			t.Fatalf("HSTS = %q, want max-age only", want["Strict-Transport-Security"])
		}
	})
}

func TestAcceptance_SEC_2(t *testing.T) {
	runSecurityEntry(t, "SEC-2", "TLS 1.2 was rejected while TLS 1.3 and the pinned generated CA completed a business request", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18451})
		roots, _ := loadSecurityCertificate(t, runtime.caPath)
		tls12 := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs: roots, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
		}}, Timeout: 5 * time.Second}
		if response, err := tls12.Get(runtime.baseURL + "/"); err == nil {
			response.Body.Close()
			t.Fatal("TLS 1.2 handshake unexpectedly succeeded")
		}

		pinnedClient := pinnedSecurityClient(t, runtime.baseURL, runtime.caPath, time.Time{})
		loginSecurityUser(t, pinnedClient, "admin", securityAdminPassword)
		me, err := pinnedClient.GetCurrentUserWithResponse(context.Background())
		if err != nil || me.StatusCode() != http.StatusOK || me.JSON200 == nil {
			t.Fatalf("TLS 1.3 business request = status %d body %s error %v", me.StatusCode(), me.Body, err)
		}
	})
}

func TestAcceptance_SEC_6(t *testing.T) {
	runSecurityEntry(t, "SEC-6", "logout, disablement, and password changes revoked the intended server-side sessions immediately", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18452})
		loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)

		logoutUser := createSecurityUser(t, runtime.client, "sec-6-logout-user", api.READONLY)
		logoutClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		peerClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, logoutClient, logoutUser.User.Username, logoutUser.InitialPassword)
		loginSecurityUser(t, peerClient, logoutUser.User.Username, logoutUser.InitialPassword)
		assertSecurityAuthenticated(t, logoutClient, http.StatusOK)
		assertSecurityAuthenticated(t, peerClient, http.StatusOK)
		loggedOut, err := logoutClient.DeleteSessionWithResponse(context.Background(), api.DeleteSessionJSONRequestBody{})
		if err != nil || loggedOut.StatusCode() != http.StatusNoContent {
			t.Fatalf("logout = status %d body %s error %v", loggedOut.StatusCode(), loggedOut.Body, err)
		}
		assertSecurityAuthenticated(t, logoutClient, http.StatusUnauthorized)
		assertSecurityAuthenticated(t, peerClient, http.StatusOK)

		passwordUser := createSecurityUser(t, runtime.client, "sec-6-password-user", api.READONLY)
		current := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		other := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, current, passwordUser.User.Username, passwordUser.InitialPassword)
		loginSecurityUser(t, other, passwordUser.User.Username, passwordUser.InitialPassword)
		changed, err := current.ChangeOwnPasswordWithResponse(context.Background(), api.PasswordChangeInput{
			OldPassword: passwordUser.InitialPassword, NewPassword: "sec-6-new-password-value",
		})
		if err != nil || changed.StatusCode() != http.StatusNoContent {
			t.Fatalf("change password = status %d body %s error %v", changed.StatusCode(), changed.Body, err)
		}
		assertSecurityAuthenticated(t, current, http.StatusOK)
		assertSecurityAuthenticated(t, other, http.StatusUnauthorized)

		resetUser := createSecurityUser(t, runtime.client, "sec-6-reset-user", api.READONLY)
		resetSession := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, resetSession, resetUser.User.Username, resetUser.InitialPassword)
		reset, err := runtime.client.ResetUserPasswordWithResponse(context.Background(), resetUser.User.Id)
		if err != nil || reset.StatusCode() != http.StatusOK || reset.JSON200 == nil {
			t.Fatalf("reset password = status %d body %s error %v", reset.StatusCode(), reset.Body, err)
		}
		assertSecurityAuthenticated(t, resetSession, http.StatusUnauthorized)
		resetLogin := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, resetLogin, resetUser.User.Username, reset.JSON200.Password)
		assertSecurityAuthenticated(t, resetLogin, http.StatusOK)

		disabledUser := createSecurityUser(t, runtime.client, "sec-6-disabled-user", api.READONLY)
		disabledClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, disabledClient, disabledUser.User.Username, disabledUser.InitialPassword)
		disabled, err := runtime.client.UpdateUserStatusWithResponse(context.Background(), disabledUser.User.Id, api.UserStatusInput{Enabled: false})
		if err != nil || disabled.StatusCode() != http.StatusOK {
			t.Fatalf("disable user = status %d body %s error %v", disabled.StatusCode(), disabled.Body, err)
		}
		assertSecurityAuthenticated(t, disabledClient, http.StatusUnauthorized)
	})
}

func TestAcceptance_SEC_7(t *testing.T) {
	runSecurityEntry(t, "SEC-7", "strict __Host cookie attributes were present and raw response bodies never echoed the session token", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18453})
		login, err := runtime.client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
			Username: "admin", Password: securityAdminPassword,
		})
		if err != nil || login.StatusCode() != http.StatusNoContent {
			t.Fatalf("login = status %d body %s error %v", login.StatusCode(), login.Body, err)
		}
		cookies := login.HTTPResponse.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("login cookies = %d, want 1", len(cookies))
		}
		cookie := cookies[0]
		if cookie.Name != "__Host-dbs_monitor_session" || cookie.Path != "/" || cookie.Domain != "" ||
			!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("session cookie attributes = %+v", cookie)
		}
		assertTokenAbsentFromRawResponse(t, cookie.Value, login.Body, login.HTTPResponse)

		me, err := runtime.client.GetCurrentUserWithResponse(context.Background())
		if err != nil || me.StatusCode() != http.StatusOK {
			t.Fatalf("current user = status %d body %s error %v", me.StatusCode(), me.Body, err)
		}
		assertTokenAbsentFromRawResponse(t, cookie.Value, me.Body, me.HTTPResponse)
	})
}

func TestAcceptance_SEC_8(t *testing.T) {
	runSecurityEntry(t, "SEC-8", "three attributable event shapes were visible to every role without echoing submitted credentials", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18456})
		loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)

		const failedPassword = "sec-8-failed-login-secret"
		failed := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		failedLogin, err := failed.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
			Username: "sec-8-missing-user", Password: failedPassword,
		})
		if err != nil || failedLogin.StatusCode() != http.StatusUnauthorized {
			t.Fatalf("failed login = status %d body %s error %v", failedLogin.StatusCode(), failedLogin.Body, err)
		}
		if strings.Contains(string(failedLogin.Body), failedPassword) {
			t.Fatal("failed login response echoed the submitted password")
		}

		viewerUser := createSecurityUser(t, runtime.client, "sec-8-viewer-user", api.READONLY)
		alertUser := createSecurityUser(t, runtime.client, "sec-8-alert-user", api.ALERTADMIN)
		disabledUser := createSecurityUser(t, runtime.client, "sec-8-disabled-user", api.READONLY)
		disabled, err := runtime.client.UpdateUserStatusWithResponse(context.Background(), disabledUser.User.Id, api.UserStatusInput{Enabled: false})
		if err != nil || disabled.StatusCode() != http.StatusOK {
			t.Fatalf("disable user = status %d body %s error %v", disabled.StatusCode(), disabled.Body, err)
		}
		me, err := runtime.client.GetCurrentUserWithResponse(context.Background())
		if err != nil || me.JSON200 == nil {
			t.Fatalf("read administrator identity: %v", err)
		}
		rejected, err := runtime.client.UpdateUserStatusWithResponse(context.Background(), me.JSON200.Id, api.UserStatusInput{Enabled: false})
		if err != nil || rejected.StatusCode() != http.StatusBadRequest {
			t.Fatalf("rejected self-disable = status %d body %s error %v", rejected.StatusCode(), rejected.Body, err)
		}

		const credential = "sec-8-instance-credential-secret"
		created, err := runtime.client.CreateInstanceWithResponse(context.Background(), api.InstanceCreateInput{
			Name: "SEC-8 credential target", Host: "localhost", Port: envInt(t, "ACCEPTANCE_TARGET_PORT", 55447),
			Database: "monitored", Username: "monitored", Password: "monitored",
		})
		if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
			t.Fatalf("create credential target = status %d body %s error %v", created.StatusCode(), created.Body, err)
		}
		instanceID := created.JSON201.Instance.Id
		defer func() { _, _ = runtime.client.DeleteInstanceWithResponse(context.Background(), instanceID) }()
		updated, err := runtime.client.UpdateInstanceCredentialWithResponse(context.Background(), instanceID, api.InstanceCredentialInput{
			Username: "sec8_monitor", Password: credential,
		})
		if err != nil || updated.StatusCode() != http.StatusOK {
			t.Fatalf("update credential = status %d body %s error %v", updated.StatusCode(), updated.Body, err)
		}
		if regexp.MustCompile(regexp.QuoteMeta(credential)).Match(updated.Body) {
			t.Fatal("credential update raw response body echoed the submitted credential")
		}

		viewer := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		alertAdmin := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, viewer, viewerUser.User.Username, viewerUser.InitialPassword)
		loginSecurityUser(t, alertAdmin, alertUser.User.Username, alertUser.InitialPassword)
		for role, client := range map[string]*api.ClientWithResponses{
			"READONLY": viewer, "ALERT_ADMIN": alertAdmin, "PLATFORM_ADMIN": runtime.client,
		} {
			events, err := client.ListPlatformEventsWithResponse(context.Background())
			if err != nil || events.StatusCode() != http.StatusOK || events.JSON200 == nil {
				t.Fatalf("%s event feed = status %d body %s error %v", role, events.StatusCode(), events.Body, err)
			}
			assertSecurityEvent(t, *events.JSON200, api.LOGINFAILED, "sec-8-missing-user", nil)
			assertSecurityEvent(t, *events.JSON200, api.USERSTATUSCHANGED, "admin", &disabledUser.User.Id)
			assertSecurityEvent(t, *events.JSON200, api.USERSTATUSCHANGEREJECTED, "admin", &me.JSON200.Id)
			assertSecurityEvent(t, *events.JSON200, api.INSTANCECREDENTIALUPDATED, "admin", &instanceID)
			body := string(events.Body)
			if strings.Contains(body, failedPassword) || strings.Contains(body, credential) {
				t.Fatalf("%s event feed exposed submitted credentials", role)
			}
		}
	})
}

func TestAcceptance_SEC_9(t *testing.T) {
	runSecurityEntry(t, "SEC-9", "Compose ran monitor-server as UID 65532 with a read-only root and 0700/0600 credential storage", func(t *testing.T) {
		root := repositoryRoot(t)
		binary := filepath.Join(root, "results", "acceptance-server")
		if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
			t.Fatalf("create acceptance result directory: %v", err)
		}
		buildStaticSecurityBinary(t, root, binary)
		t.Cleanup(func() { _ = os.Remove(binary) })
		composeSecurityServer(t, "up", "-d", "acceptance-server")
		t.Cleanup(func() { _, _ = composeSecurityOutput("rm", "-s", "-f", "acceptance-server") })

		deadline := time.Now().Add(60 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			if output, err := composeSecurityOutput("exec", "-T", "acceptance-server", "stat", "-c", "%u:%g:%a", "/etc/dbs-monitor/credentials/current"); err == nil {
				if strings.TrimSpace(output) != "65532:65532:600" {
					t.Fatalf("credential current ownership/mode = %q", strings.TrimSpace(output))
				}
				ready = true
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if !ready {
			logs, _ := composeSecurityOutput("logs", "acceptance-server")
			t.Fatalf("Compose server did not initialize credentials:\n%s", logs)
		}
		top, err := composeSecurityOutput("top", "acceptance-server", "-eo", "uid,pid,args")
		if err != nil || !regexp.MustCompile(`(?m)^65532\s+\d+\s+/workspace/results/acceptance-server(?:\s|$)`).MatchString(top) {
			t.Fatalf("Compose server process identity = %q, error %v", top, err)
		}
		if output, err := composeSecurityOutput("exec", "-T", "acceptance-server", "stat", "-c", "%u:%g:%a", "/etc/dbs-monitor/credentials"); err != nil || strings.TrimSpace(output) != "65532:65532:700" {
			t.Fatalf("credential directory ownership/mode = %q, error %v", strings.TrimSpace(output), err)
		}
		if output, err := composeSecurityOutput("exec", "-T", "acceptance-server", "stat", "-c", "%u:%g:%a", "/etc/dbs-monitor/credentials/master-key-v1"); err != nil || strings.TrimSpace(output) != "65532:65532:600" {
			t.Fatalf("master key ownership/mode = %q, error %v", strings.TrimSpace(output), err)
		}
		for _, path := range []string{"/tmp/sec-9-write", "/workspace/sec-9-write"} {
			if _, err := composeSecurityOutput("exec", "-T", "acceptance-server", "touch", path); err == nil {
				t.Fatalf("server user could write %s outside /etc/dbs-monitor", path)
			}
		}
	})
}

func TestAcceptance_SEC_10(t *testing.T) {
	runSecurityEntry(t, "SEC-10", "real browser login-to-chart flow rendered AntD and ECharts without a CSP violation", func(t *testing.T) {
		root := repositoryRoot(t)
		buildWeb := exec.Command("npm", "run", "build")
		buildWeb.Dir = filepath.Join(root, "web")
		if output, err := buildWeb.CombinedOutput(); err != nil {
			t.Fatalf("build embedded web application: %v\n%s", err, output)
		}
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18455, embedWeb: true})
		loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)
		created, err := runtime.client.CreateInstanceWithResponse(context.Background(), api.InstanceCreateInput{
			Name: "SEC-10 browser target", Host: "127.0.0.1", Port: envInt(t, "ACCEPTANCE_TARGET_PORT", 55447),
			Database: "monitored", Username: "monitored", Password: "monitored",
		})
		if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
			t.Fatalf("create browser target = status %d body %s error %v", created.StatusCode(), created.Body, err)
		}
		defer func() {
			_, _ = runtime.client.DeleteInstanceWithResponse(context.Background(), created.JSON201.Instance.Id)
		}()
		registered, err := runtime.client.RegisterAgentWithResponse(context.Background(), created.JSON201.Instance.Id)
		if err != nil || registered.StatusCode() != http.StatusOK || registered.JSON200 == nil || registered.JSON200.AgentToken == nil {
			t.Fatalf("register browser target Agent = status %d body %s error %v", registered.StatusCode(), registered.Body, err)
		}
		reported, err := runtime.client.ReportAgentMetricsWithResponse(context.Background(), api.AgentReport{
			AgentVersion: "1.0.0",
			InstanceId:   created.JSON201.Instance.Id,
			Timestamp:    time.Now().UTC(),
			Metrics: []api.AgentMetric{{
				Metric: api.AgentMetricMetricHostCpuUsagePercent,
				Value:  42,
			}},
		}, func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer "+*registered.JSON200.AgentToken)
			return nil
		})
		if err != nil || reported.StatusCode() != http.StatusOK {
			t.Fatalf("report browser target metric = status %d body %s error %v", reported.StatusCode(), reported.Body, err)
		}

		command := exec.Command("npm", "exec", "playwright", "test", "e2e/security-boundary.spec.ts")
		command.Dir = filepath.Join(root, "web")
		command.Env = append(os.Environ(),
			"E2E_BASE_URL="+runtime.baseURL,
			"SECURITY_E2E_USERNAME=admin",
			"SECURITY_E2E_PASSWORD="+securityAdminPassword,
			"SECURITY_E2E_INSTANCE=SEC-10 browser target",
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("SEC-10 Playwright path: %v\n%s", err, output)
		}
	})
}

func TestAcceptance_SEC_3(t *testing.T) {
	runCertificateSecurityEntry(t, "SEC-3", 18457, "tls-expired", "CERTIFICATE_EXPIRED", 0)
}

func TestAcceptance_SEC_4(t *testing.T) {
	runCertificateSecurityEntry(t, "SEC-4", 18458, "tls-expiring-20d", "CERTIFICATE_EXPIRING", 1)
}

func TestAcceptance_SEC_5(t *testing.T) {
	runSecurityEntry(t, "SEC-5", "configured 90-second absolute and 30-second idle deadlines rejected sessions through normal authentication", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: 18459, absoluteTTL: 90 * time.Second, idleTTL: 30 * time.Second})
		active := runtime.client
		idle := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, active, "admin", securityAdminPassword)
		loginSecurityUser(t, idle, "admin", securityAdminPassword)
		started := time.Now()
		for requestNumber := 1; requestNumber <= 8; requestNumber++ {
			waitUntilSecurity(t, started.Add(time.Duration(requestNumber)*10*time.Second))
			assertSecurityAuthenticated(t, active, http.StatusOK)
			if requestNumber == 3 {
				waitUntilSecurity(t, started.Add(31*time.Second))
				assertSecurityAuthenticated(t, idle, http.StatusUnauthorized)
			}
		}
		waitUntilSecurity(t, started.Add(91*time.Second))
		assertSecurityAuthenticated(t, active, http.StatusUnauthorized)
	})
}

func runSecurityEntry(t *testing.T, id, passedMessage string, exercise func(*testing.T)) {
	t.Helper()
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for " + id)
	}
	started := time.Now()
	defer func() {
		status, message := resultPassed, passedMessage
		if t.Failed() {
			status, message = resultFailed, id+" failed; see go test output"
		}
		acceptanceReport.record(id, status, message, time.Since(started))
	}()
	exercise(t)
}

func startSecurityRuntime(t *testing.T, options securityServerOptions) securityRuntime {
	t.Helper()
	root := repositoryRoot(t)
	work := t.TempDir()
	serverBinary := filepath.Join(work, "dbs-monitor-server")
	buildSecurityBinary(t, root, serverBinary, options.embedWeb)
	certDirectory := filepath.Join(work, "certs")
	keyDirectory := filepath.Join(work, "credentials")
	agentBinaryDirectory := filepath.Join(work, "agent-binaries")
	for _, directory := range []string{certDirectory, keyDirectory, agentBinaryDirectory} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	if options.fixture != "" {
		installSecurityCertificateFixture(t, root, certDirectory, options.fixture)
	}
	configPath := writeSecurityConfig(t, work, keyDirectory, agentBinaryDirectory, options.absoluteTTL, options.idleTTL)
	logPath := filepath.Join(work, "server.log")
	baseURL := "https://127.0.0.1:" + strconv.Itoa(options.port)
	server := startProcess(t, "security server", serverBinary, logPath, []string{
		"DBS_MONITOR_CONFIG_FILE=" + configPath,
		"INITIAL_ADMIN_PASSWORD=" + securityAdminPassword,
		"LISTEN_ADDR=127.0.0.1:" + strconv.Itoa(options.port),
		"PUBLIC_HOST=127.0.0.1",
		"CERT_DIR=" + certDirectory,
		"PGDATA=/",
	})
	caPath := filepath.Join(certDirectory, "ca.crt")
	var certificateTime time.Time
	if options.fixture == "tls-expired" {
		_, certificate := loadSecurityCertificate(t, caPath)
		certificateTime = certificate.NotAfter.Add(-time.Hour)
	}
	httpClient := securityHTTPClient(t, caPath, certificateTime, nil)
	waitForSecurityAPI(t, server, baseURL, httpClient)
	client, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("create generated security client: %v", err)
	}
	return securityRuntime{
		baseURL: baseURL, caPath: caPath, client: client, http: httpClient, logPath: logPath,
	}
}

func buildSecurityBinary(t *testing.T, root, output string, embedWeb bool) {
	t.Helper()
	arguments := []string{"build", "-ldflags", "-X main.version=1.0.0 -X main.commitSHA=" + candidateSHA()}
	if embedWeb {
		arguments = append(arguments, "-tags", "embed_web")
	}
	arguments = append(arguments, "-o", output, "./cmd/monitor-server")
	command := exec.Command("go", arguments...)
	command.Dir = root
	if contents, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build security server: %v\n%s", err, contents)
	}
}

func buildStaticSecurityBinary(t *testing.T, root, output string) {
	t.Helper()
	command := exec.Command("go", "build", "-ldflags", "-X main.version=1.0.0 -X main.commitSHA="+candidateSHA(), "-o", output, "./cmd/monitor-server")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if contents, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build static security server: %v\n%s", err, contents)
	}
}

func writeSecurityConfig(t *testing.T, work, keyDirectory, binaryDirectory string, absoluteTTL, idleTTL time.Duration) string {
	t.Helper()
	if absoluteTTL == 0 {
		absoluteTTL = 12 * time.Hour
	}
	if idleTTL == 0 {
		idleTTL = 2 * time.Hour
	}
	config := struct {
		AgentBinaryDirectory string `yaml:"agent_binary_dir"`
		MasterKeyDirectory   string `yaml:"master_key_path"`
		PlatformDatabaseURL  string `yaml:"platform_database_url"`
		SessionAbsoluteTTL   string `yaml:"session_absolute_ttl"`
		SessionIdleTTL       string `yaml:"session_idle_ttl"`
	}{
		AgentBinaryDirectory: binaryDirectory,
		MasterKeyDirectory:   keyDirectory,
		PlatformDatabaseURL:  os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL"),
		SessionAbsoluteTTL:   absoluteTTL.String(),
		SessionIdleTTL:       idleTTL.String(),
	}
	contents, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("encode security server config: %v", err)
	}
	path := filepath.Join(work, "server.yaml")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write security server config: %v", err)
	}
	return path
}

func newSecurityClient(t *testing.T, baseURL, caPath string) *api.ClientWithResponses {
	t.Helper()
	httpClient := securityHTTPClient(t, caPath, time.Time{}, nil)
	client, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("create generated security client: %v", err)
	}
	return client
}

func pinnedSecurityClient(t *testing.T, baseURL, caPath string, currentTime time.Time) *api.ClientWithResponses {
	t.Helper()
	_, ca := loadSecurityCertificate(t, caPath)
	wantFingerprint := sha256.Sum256(ca.Raw)
	httpClient := securityHTTPClient(t, caPath, currentTime, func(state tls.ConnectionState) error {
		if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
			return fmt.Errorf("TLS peer has no verified chain")
		}
		root := state.VerifiedChains[0][len(state.VerifiedChains[0])-1]
		got := sha256.Sum256(root.Raw)
		if got != wantFingerprint {
			return fmt.Errorf("CA fingerprint = %s, want pinned %s", hex.EncodeToString(got[:]), hex.EncodeToString(wantFingerprint[:]))
		}
		return nil
	})
	client, err := api.NewClientWithResponses(baseURL, api.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("create pinned generated client: %v", err)
	}
	return client
}

func securityHTTPClient(t *testing.T, caPath string, currentTime time.Time, verify func(tls.ConnectionState) error) *http.Client {
	t.Helper()
	roots, _ := loadSecurityCertificate(t, caPath)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, VerifyConnection: verify}
	if !currentTime.IsZero() {
		config.Time = func() time.Time { return currentTime }
	}
	return &http.Client{Jar: jar, Transport: &http.Transport{TLSClientConfig: config}, Timeout: 5 * time.Second}
}

func loadSecurityCertificate(t *testing.T, path string) (*x509.CertPool, *x509.Certificate) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read TLS certificate %s: %v", path, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		t.Fatalf("append TLS certificate %s", path)
	}
	block, _ := pem.Decode(contents)
	if block == nil {
		t.Fatalf("decode TLS certificate %s", path)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse TLS certificate %s: %v", path, err)
	}
	return roots, certificate
}

func waitForSecurityAPI(t *testing.T, server *managedProcess, baseURL string, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-server.done:
			server.done <- err
			contents, _ := os.ReadFile(server.logPath)
			t.Fatalf("security server exited before readiness: %v\n%s", err, contents)
		default:
		}
		response, err := client.Get(baseURL + "/login")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	contents, _ := os.ReadFile(server.logPath)
	t.Fatalf("security server did not become ready\n%s", contents)
}

func loginSecurityUser(t *testing.T, client *api.ClientWithResponses, username, password string) {
	t.Helper()
	response, err := client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
		Username: username, Password: password,
	})
	if err != nil || response.StatusCode() != http.StatusNoContent {
		t.Fatalf("login %s = status %d body %s error %v", username, response.StatusCode(), response.Body, err)
	}
}

func createSecurityUser(t *testing.T, admin *api.ClientWithResponses, username string, role api.Role) api.UserCreated {
	t.Helper()
	response, err := admin.CreateUserWithResponse(context.Background(), api.UserCreateInput{Username: username, Role: role})
	if err != nil || response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		t.Fatalf("create %s = status %d body %s error %v", username, response.StatusCode(), response.Body, err)
	}
	return *response.JSON201
}

func assertSecurityAuthenticated(t *testing.T, client *api.ClientWithResponses, want int) {
	t.Helper()
	response, err := client.GetCurrentUserWithResponse(context.Background())
	if err != nil || response.StatusCode() != want {
		t.Fatalf("current user = status %d body %s error %v, want %d", response.StatusCode(), response.Body, err, want)
	}
}

func securityHeaderGolden(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join(repositoryRoot(t), "cmd", "monitor-server", "testdata", "security_headers.golden")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open security header golden: %v", err)
	}
	defer file.Close()
	headers := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, value, found := strings.Cut(scanner.Text(), ": ")
		if !found {
			t.Fatalf("invalid security header golden line %q", scanner.Text())
		}
		headers[name] = value
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read security header golden: %v", err)
	}
	if len(headers) != 6 {
		t.Fatalf("security header golden count = %d, want 6", len(headers))
	}
	return headers
}

func assertSecurityHeaders(t *testing.T, got http.Header, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if got.Get(name) != value {
			t.Errorf("%s = %q, want %q", name, got.Get(name), value)
		}
	}
}

func assertTokenAbsentFromRawResponse(t *testing.T, token string, body []byte, response *http.Response) {
	t.Helper()
	pattern := regexp.MustCompile(regexp.QuoteMeta(token))
	if pattern.Match(body) {
		t.Fatal("raw response body contains the session token")
	}
	if response.Request != nil && pattern.MatchString(response.Request.URL.String()) {
		t.Fatal("request URL contains the session token")
	}
	for name, values := range response.Header {
		if strings.EqualFold(name, "Set-Cookie") {
			continue
		}
		if pattern.MatchString(strings.Join(values, "\n")) {
			t.Fatalf("response header %s contains the session token", name)
		}
	}
}

func assertSecurityEvent(t *testing.T, events []api.PlatformEvent, kind api.PlatformEventKind, actor string, subjectID *uuid.UUID) {
	t.Helper()
	for _, event := range events {
		if event.Kind != kind || event.Actor != actor {
			continue
		}
		if subjectID == nil && event.SubjectId == nil {
			return
		}
		if subjectID != nil && event.SubjectId != nil && *event.SubjectId == *subjectID {
			return
		}
	}
	t.Errorf("event feed lacks kind %s actor %q subject %v", kind, actor, subjectID)
}

func installSecurityCertificateFixture(t *testing.T, root, directory, fixture string) {
	t.Helper()
	fixtureRoot := filepath.Join(root, "test", "acceptance", "fixtures")
	for destination, extension := range map[string]string{"server.crt": ".crt", "server.key": ".key", "ca.crt": ".crt"} {
		contents, err := os.ReadFile(filepath.Join(fixtureRoot, fixture+extension))
		if err != nil {
			t.Fatalf("read %s fixture: %v", fixture, err)
		}
		mode := os.FileMode(0o644)
		if extension == ".key" {
			mode = 0o600
		}
		if err := os.WriteFile(filepath.Join(directory, destination), contents, mode); err != nil {
			t.Fatalf("install %s fixture: %v", fixture, err)
		}
	}
}

func runCertificateSecurityEntry(t *testing.T, id string, port int, fixture, wantCode string, minimumDays int) {
	runSecurityEntry(t, id, "real "+fixture+" certificate kept the server available with a distinguishable DEGRADED fact", func(t *testing.T) {
		runtime := startSecurityRuntime(t, securityServerOptions{port: port, fixture: fixture})
		loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)
		response, err := runtime.client.GetCertificateDiagnosticsWithResponse(context.Background())
		if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			t.Fatalf("certificate diagnostics = status %d body %s error %v", response.StatusCode(), response.Body, err)
		}
		if response.JSON200.Status != api.PlatformHealthDegraded || response.JSON200.Code != wantCode {
			t.Fatalf("certificate health = %s/%s, want DEGRADED/%s", response.JSON200.Status, response.JSON200.Code, wantCode)
		}
		if response.JSON200.ValidityDaysRemaining == nil || *response.JSON200.ValidityDaysRemaining < minimumDays {
			t.Fatalf("certificate validity days = %v, want at least %d", response.JSON200.ValidityDaysRemaining, minimumDays)
		}
		contents, err := os.ReadFile(runtime.logPath)
		if err != nil {
			t.Fatalf("read certificate platform event: %v", err)
		}
		remaining := *response.JSON200.ValidityDaysRemaining
		eventPattern := regexp.MustCompile(`"event":"platform_health_change".*"source":"TLS_CERTIFICATE".*"code":"` + regexp.QuoteMeta(wantCode) + `".*"validity_days_remaining":` + strconv.Itoa(remaining))
		if !eventPattern.Match(contents) {
			t.Fatalf("certificate platform event does not contain %s with %d remaining days:\n%s", wantCode, remaining, contents)
		}
	})
}

func waitUntilSecurity(t *testing.T, deadline time.Time) {
	t.Helper()
	if wait := time.Until(deadline); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		<-timer.C
	}
}

func composeSecurityServer(t *testing.T, arguments ...string) {
	t.Helper()
	if output, err := composeSecurityOutput(arguments...); err != nil {
		t.Fatalf("docker compose %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func composeSecurityOutput(arguments ...string) (string, error) {
	project := os.Getenv("ACCEPTANCE_COMPOSE_PROJECT")
	if project == "" {
		project = "dbs-monitor-acceptance"
	}
	commandArguments := []string{"compose", "-p", project, "--profile", "acceptance-server"}
	commandArguments = append(commandArguments, arguments...)
	command := exec.Command("docker", commandArguments...)
	command.Dir, _ = filepath.Abs(filepath.Join("..", ".."))
	output, err := command.CombinedOutput()
	return string(output), err
}
