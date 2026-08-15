package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestLoadServerConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		path := writeServerConfig(t, "{}\n", 0o600)
		config, permissionsSecure, err := loadServerConfig(path)
		if err != nil {
			t.Fatalf("loadServerConfig: %v", err)
		}
		if !permissionsSecure {
			t.Fatal("0600 config was reported as insecure")
		}
		if config != defaultServerConfig() {
			t.Fatalf("config = %+v, want defaults %+v", config, defaultServerConfig())
		}
	})

	t.Run("deployment overrides", func(t *testing.T) {
		path := writeServerConfig(t, `partition_span: 1m
partition_maintenance_interval: 2m
repeat_interval_minimum: 30s
snapshot_truncation_limit: 5
collection_freshness_threshold: 20s
migration_lock_wait_timeout: 10s
session_absolute_ttl: 90s
session_idle_ttl: 30s
agent_binary_dir: /srv/dbs-monitor/agents
platform_database_url: postgres://dbs_monitor:secret@platform-db:5432/dbs_monitor?search_path=dbsmon&sslmode=verify-full
master_key_path: /srv/dbs-monitor/credentials
`, 0o600)

		config, _, err := loadServerConfig(path)
		if err != nil {
			t.Fatalf("loadServerConfig: %v", err)
		}
		want := serverConfig{
			PartitionSpan:                time.Minute,
			PartitionMaintenanceInterval: 2 * time.Minute,
			RepeatIntervalMinimum:        30 * time.Second,
			SnapshotTruncationLimit:      5,
			CollectionFreshnessThreshold: 20 * time.Second,
			MigrationLockWaitTimeout:     10 * time.Second,
			SessionAbsoluteTTL:           90 * time.Second,
			SessionIdleTTL:               30 * time.Second,
			AgentBinaryDir:               "/srv/dbs-monitor/agents",
			PlatformDatabaseURL:          "postgres://dbs_monitor:secret@platform-db:5432/dbs_monitor?search_path=dbsmon&sslmode=verify-full",
			MasterKeyPath:                "/srv/dbs-monitor/credentials",
		}
		if config != want {
			t.Fatalf("config = %+v, want %+v", config, want)
		}
	})

	t.Run("environment overrides only the master key path", func(t *testing.T) {
		t.Setenv("DBS_MONITOR_MASTER_KEY_PATH", "/run/secrets/dbs-monitor-credentials")
		path := writeServerConfig(t, "master_key_path: /srv/dbs-monitor/credentials\n", 0o600)

		config, _, err := loadServerConfig(path)
		if err != nil {
			t.Fatalf("loadServerConfig: %v", err)
		}
		if config.MasterKeyPath != "/run/secrets/dbs-monitor-credentials" {
			t.Fatalf("master key path = %q, want environment override", config.MasterKeyPath)
		}
	})

	invalid := []struct {
		name     string
		contents string
		message  string
	}{
		{name: "partition span", contents: "partition_span: 0s\n", message: "partition_span"},
		{name: "partition maintenance interval", contents: "partition_maintenance_interval: -1s\n", message: "partition_maintenance_interval"},
		{name: "repeat interval minimum", contents: "repeat_interval_minimum: never\n", message: "repeat_interval_minimum"},
		{name: "snapshot truncation limit", contents: "snapshot_truncation_limit: 0\n", message: "snapshot_truncation_limit"},
		{name: "collection freshness threshold", contents: "collection_freshness_threshold: 0s\n", message: "collection_freshness_threshold"},
		{name: "migration lock wait timeout", contents: "migration_lock_wait_timeout: 0s\n", message: "migration_lock_wait_timeout"},
		{name: "session absolute ttl", contents: "session_absolute_ttl: 0s\n", message: "session_absolute_ttl"},
		{name: "session idle ttl", contents: "session_idle_ttl: 13h\n", message: "session_idle_ttl"},
		{name: "agent binary directory", contents: "agent_binary_dir: relative/agents\n", message: "agent_binary_dir"},
		{name: "platform database search path", contents: "platform_database_url: postgres://dbs_monitor@platform-db/dbs_monitor?sslmode=verify-full\n", message: "search_path"},
		{name: "platform database ssl mode", contents: "platform_database_url: postgres://dbs_monitor@platform-db/dbs_monitor?search_path=dbsmon&sslmode=require\n", message: "sslmode"},
		{name: "master key path", contents: "master_key_path: relative/credentials\n", message: "master_key_path"},
		{name: "unknown setting", contents: "no_data_periods: 1\n", message: "field no_data_periods not found"},
		{name: "multiple documents", contents: "{}\n---\n{}\n", message: "multiple YAML documents are not allowed"},
	}
	for _, test := range invalid {
		t.Run("rejects "+test.name, func(t *testing.T) {
			path := writeServerConfig(t, test.contents, 0o600)
			_, _, err := loadServerConfig(path)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("loadServerConfig error = %v, want message containing %q", err, test.message)
			}
		})
	}

	t.Run("wide permissions are reported without rejecting config", func(t *testing.T) {
		path := writeServerConfig(t, "{}\n", 0o644)
		_, permissionsSecure, err := loadServerConfig(path)
		if err != nil {
			t.Fatalf("loadServerConfig rejected readable config: %v", err)
		}
		if permissionsSecure {
			t.Fatal("0644 config was reported as secure")
		}
	})
}

func TestReportConfigPermissionsDegradesHealthWithoutStopping(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)

	reportConfigPermissions(health, log.New(&output, "", 0), "/etc/dbs-monitor/config.yaml", false, now)

	process := health.Source(platformhealth.SourceServerProcess)
	if process.Status != platformhealth.StatusDegraded || process.Code != "CONFIG_FILE_PERMISSIONS_INSECURE" {
		t.Fatalf("server process health = %+v, want degraded config permission fact", process)
	}
	if process.Version == nil || *process.Version != "3.0.0" || process.StartedAt == nil || !process.StartedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("server process metadata was not preserved: %+v", process)
	}
	if warning := output.String(); !strings.Contains(warning, "are not 0600") || !strings.Contains(warning, "/etc/dbs-monitor/config.yaml") {
		t.Fatalf("config permission warning = %q", warning)
	}
}

func TestReportConfigPermissionsLeavesSecureConfigHealthy(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)

	reportConfigPermissions(health, log.New(&output, "", 0), "/etc/dbs-monitor/config.yaml", true, now)

	process := health.Source(platformhealth.SourceServerProcess)
	if process.Status != platformhealth.StatusOK || process.Code != "SERVER_PROCESS_RUNNING" {
		t.Fatalf("server process health = %+v, want unchanged healthy process", process)
	}
	if output.Len() != 0 {
		t.Fatalf("secure config permission warning = %q, want none", output.String())
	}
}

func TestServerConfigExamplesLoad(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	for _, name := range []string{"server-minimal.yaml", "server-full.yaml"} {
		t.Run(name, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(root, "config", name))
			if err != nil {
				t.Fatalf("read config example: %v", err)
			}
			config, permissionsSecure, err := loadServerConfig(writeServerConfig(t, string(contents), 0o600))
			if err != nil {
				t.Fatalf("load config example: %v", err)
			}
			if !permissionsSecure {
				t.Fatal("config example does not satisfy the 0600 deployment contract")
			}
			if config.PlatformDatabaseURL == "" || config.AgentBinaryDir == "" || config.MasterKeyPath == "" {
				t.Fatalf("config example does not produce a launchable config: %+v", config)
			}
		})
	}
}

func writeServerConfig(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
