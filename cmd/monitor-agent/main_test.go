package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/agent"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestConfiguredAgentTokenPrefersDedicatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0600); err != nil {
		t.Fatalf("write Agent token: %v", err)
	}
	t.Setenv("AGENT_TOKEN", "environment-token")
	t.Setenv("AGENT_TOKEN_FILE", path)
	token, err := configuredAgentToken()
	if err != nil {
		t.Fatalf("read configured Agent token: %v", err)
	}
	if token != "file-token" {
		t.Fatalf("configured Agent token = %q, want file token", token)
	}
}

func TestConfiguredAgentTokenFallsBackToEnvironment(t *testing.T) {
	t.Setenv("AGENT_TOKEN", "environment-token")
	t.Setenv("AGENT_TOKEN_FILE", "")

	token, err := configuredAgentToken()
	if err != nil {
		t.Fatalf("read configured Agent token: %v", err)
	}
	if token != "environment-token" {
		t.Fatalf("configured Agent token = %q, want environment token", token)
	}
}

func TestConfiguredAgentTokenReportsFileReadFailure(t *testing.T) {
	t.Setenv("AGENT_TOKEN_FILE", filepath.Join(t.TempDir(), "missing-token"))

	_, err := configuredAgentToken()
	if err == nil || !strings.Contains(err.Error(), "read Agent token file") {
		t.Fatalf("configuredAgentToken() error = %v, want Agent token file read error", err)
	}
}

func TestRunServiceRefusesStartupWhenInitialReportFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"server_version": "1.0.0",
			"server_time":    time.Now().UTC().Add(6 * time.Second),
		}); err != nil {
			t.Errorf("encode Agent report response: %v", err)
		}
	}))
	defer server.Close()

	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create Agent API client: %v", err)
	}
	service := agent.NewService(client, agent.Config{Instance: uuid.New()}, "1.0.0", "/")
	err = runService(context.Background(), service)
	if err == nil || !strings.Contains(err.Error(), "clock differs from the platform by more than 5 seconds") {
		t.Fatalf("runService() error = %v, want initial clock check failure", err)
	}
}
