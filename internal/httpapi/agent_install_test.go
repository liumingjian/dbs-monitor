package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestAgentInstallerDistributionContract(t *testing.T) {
	if output, err := exec.Command("sh", "-n", filepath.Join("agent-install.sh")).CombinedOutput(); err != nil {
		t.Fatalf("Agent installer syntax: %v: %s", err, output)
	}
	contents := string(agentInstaller)
	for _, required := range []string{
		"$(id -u)", "sha256sum", "/api/v1/agent/download?arch=linux/$agent_arch", `--config "$curl_config"`,
		"useradd --system", "chmod 0600", "AGENT_TOKEN_FILE", "systemctl enable --now",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("Agent installer is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--insecure", " -k ", " --header ", " -H ", "systemd.timer", "upgrade", "clock_delta", "date_header"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("Agent installer contains forbidden behavior %q", forbidden)
		}
	}
	if strings.Contains(contents, "agent_token=$3") {
		t.Fatal("Agent installer accepts the token as a command-line argument")
	}

	probe := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := probe.TLS.Certificates[0]
	probe.Close()
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, certificatePEM, 0644); err != nil {
		t.Fatalf("write CA fixture: %v", err)
	}
	binary := []byte("agent-binary")
	for _, architecture := range []string{"amd64", "arm64"} {
		if err := os.WriteFile(filepath.Join(directory, "dbs-monitor-agent-linux-"+architecture), binary, 0755); err != nil {
			t.Fatalf("write Agent fixture: %v", err)
		}
	}
	distribution, err := LoadAgentDistribution(caPath, directory)
	if err != nil {
		t.Fatalf("load Agent distribution: %v", err)
	}
	if err := distribution.HealthError(); err != nil {
		t.Fatalf("Agent distribution health: %v", err)
	}
	const token = "one-time-agent-token"
	apiHandler := NewHandlerWithAgentDistribution(nil, nil, nil, "test", distribution).Routes()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/agent/download" {
			apiHandler.ServeHTTP(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+token {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if request.URL.Query().Get("arch") != "linux/amd64" && request.URL.Query().Get("arch") != "linux/arm64" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = writer.Write(binary)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	client := server.Client()

	for path, want := range map[string][]byte{
		"/api/agent/install/install.sh": agentInstaller,
		"/api/agent/install/ca.crt":     certificatePEM,
	} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || string(body) != string(want) {
			t.Fatalf("GET %s = status %d body %q error %v", path, response.StatusCode, body, readErr)
		}
	}
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/agent/download?arch=linux/amd64", nil)
	if err != nil {
		t.Fatalf("create Agent download request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET Agent binary: %v", err)
	}
	downloadedBinary, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || string(downloadedBinary) != string(binary) {
		t.Fatalf("Agent binary download = status %d body %q error %v", response.StatusCode, downloadedBinary, readErr)
	}

	installRoot := filepath.Join(directory, "opt", "dbs-monitor-agent")
	configRoot := filepath.Join(directory, "etc", "dbs-monitor-agent")
	serviceRoot := filepath.Join(directory, "etc", "systemd", "system")
	if err := os.MkdirAll(serviceRoot, 0755); err != nil {
		t.Fatalf("create service fixture directory: %v", err)
	}
	testInstaller := strings.ReplaceAll(contents, "/opt/dbs-monitor-agent", installRoot)
	testInstaller = strings.ReplaceAll(testInstaller, "/etc/dbs-monitor-agent", configRoot)
	testInstaller = strings.ReplaceAll(testInstaller, "/etc/systemd/system", serviceRoot)
	installerPath := filepath.Join(directory, "install.sh")
	if err := os.WriteFile(installerPath, []byte(testInstaller), 0755); err != nil {
		t.Fatalf("write installer fixture: %v", err)
	}
	stubRoot := filepath.Join(directory, "bin")
	if err := os.Mkdir(stubRoot, 0755); err != nil {
		t.Fatalf("create command fixture directory: %v", err)
	}
	for name, script := range map[string]string{
		"chown":     "#!/bin/sh\nexit 0\n",
		"id":        "#!/bin/sh\nif [ \"${1:-}\" = -u ]; then echo 0; exit 0; fi\nexit 1\n",
		"systemctl": "#!/bin/sh\nexit 0\n",
		"useradd":   "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(stubRoot, name), []byte(script), 0755); err != nil {
			t.Fatalf("write %s fixture: %v", name, err)
		}
	}

	command := exec.Command("sh", installerPath, server.URL, "00000000-0000-0000-0000-000000000001", distribution.CAFingerprint, caPath)
	command.Stdin = strings.NewReader(token + "\n")
	command.Env = append(os.Environ(), "PATH="+stubRoot+":"+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run Agent installer: %v: %s", err, output)
	}
	installedToken, err := os.ReadFile(filepath.Join(configRoot, "token"))
	if err != nil || string(installedToken) != token+"\n" {
		t.Fatalf("installed token = %q, error %v", installedToken, err)
	}
	tokenInfo, err := os.Stat(filepath.Join(configRoot, "token"))
	if err != nil {
		t.Fatalf("stat installed token: %v", err)
	}
	if tokenInfo.Mode().Perm() != 0600 {
		t.Fatalf("installed token mode = %v, want 0600", tokenInfo.Mode().Perm())
	}
	installedBinary, err := os.ReadFile(filepath.Join(installRoot, "bin", "dbs-monitor-agent"))
	if err != nil || string(installedBinary) != string(binary) {
		t.Fatalf("installed Agent = %q, error %v", installedBinary, err)
	}
	service, err := os.ReadFile(filepath.Join(serviceRoot, "dbs-monitor-agent.service"))
	if err != nil || !strings.Contains(string(service), "Environment=AGENT_TOKEN_FILE="+configRoot+"/token") {
		t.Fatalf("installed service does not reference token file: %v: %s", err, service)
	}
}

func TestAgentBinaryDownloadRequiresAgentToken(t *testing.T) {
	directory := t.TempDir()
	distribution := AgentDistribution{BinaryDirectory: directory}
	server := httptest.NewServer(NewHandlerWithAgentDistribution(nil, nil, nil, "test", distribution).Routes())
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/agent/download?arch=linux/amd64")
	if err != nil {
		t.Fatalf("GET Agent binary: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Agent download status = %d, want 401", response.StatusCode)
	}
}

func TestAgentBinaryDownloadRejectsUnsupportedArchitecture(t *testing.T) {
	handler := NewHandlerWithAgentDistribution(nil, nil, nil, "test", AgentDistribution{BinaryDirectory: t.TempDir()})
	response, err := handler.DownloadAgentBinary(context.Background(), api.DownloadAgentBinaryRequestObject{
		Params: api.DownloadAgentBinaryParams{Arch: api.DownloadAgentBinaryParamsArch("linux/s390x")},
	})
	if err != nil {
		t.Fatalf("download unsupported Agent architecture: %v", err)
	}
	if _, ok := response.(api.DownloadAgentBinary400JSONResponse); !ok {
		t.Fatalf("unsupported Agent architecture response = %T, want 400", response)
	}
}

func TestAgentDistributionHealthReportsMissingBinaryDirectory(t *testing.T) {
	probe := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := probe.TLS.Certificates[0]
	probe.Close()
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, certificatePEM, 0644); err != nil {
		t.Fatalf("write CA fixture: %v", err)
	}

	distribution, err := LoadAgentDistribution(caPath, filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("load Agent distribution with valid CA: %v", err)
	}
	if healthErr := distribution.HealthError(); healthErr == nil || !strings.Contains(healthErr.Error(), "AGENT_BINARY_DIR") {
		t.Fatalf("Agent distribution health error = %v, want missing AGENT_BINARY_DIR", healthErr)
	}
}
