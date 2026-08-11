package httpapi

import (
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
)

func TestAgentInstallerDistributionContract(t *testing.T) {
	if output, err := exec.Command("sh", "-n", filepath.Join("agent-install.sh")).CombinedOutput(); err != nil {
		t.Fatalf("Agent installer syntax: %v: %s", err, output)
	}
	contents := string(agentInstaller)
	for _, required := range []string{
		"$(id -u)", "clock_delta", `-gt 5`, "sha256sum", "dbs-monitor-agent/$agent_arch",
		"useradd --system", "chmod 0600", "AGENT_TOKEN_FILE", "systemctl enable --now",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("Agent installer is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--insecure", " -k ", "systemd.timer", "upgrade"} {
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
	server := httptest.NewUnstartedServer(NewHandlerWithAgentDistribution(nil, nil, nil, "test", distribution).Routes())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	client := server.Client()

	for path, want := range map[string][]byte{
		"/api/agent/install/install.sh":              agentInstaller,
		"/api/agent/install/ca.crt":                  certificatePEM,
		"/api/agent/install/dbs-monitor-agent/amd64": binary,
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
	response, err := client.Get(server.URL + "/api/agent/install/dbs-monitor-agent/s390x")
	if err != nil {
		t.Fatalf("GET unsupported Agent architecture: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unsupported Agent architecture status = %d, want 404", response.StatusCode)
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

	const token = "one-time-agent-token"
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
