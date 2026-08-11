package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
