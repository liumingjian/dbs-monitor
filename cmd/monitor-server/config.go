package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultServerConfigPath = "/etc/dbs-monitor/config.yaml"

type serverConfig struct {
	PartitionSpan                time.Duration
	PartitionMaintenanceInterval time.Duration
	AlertHistoryRetention        time.Duration
	RepeatIntervalMinimum        time.Duration
	SnapshotTruncationLimit      int
	CollectionFreshnessThreshold time.Duration
	MigrationLockWaitTimeout     time.Duration
	SessionAbsoluteTTL           time.Duration
	SessionIdleTTL               time.Duration
	AgentBinaryDir               string
	PlatformDatabaseURL          string
	MasterKeyPath                string
}

type rawServerConfig struct {
	PartitionSpan                *string `yaml:"partition_span"`
	PartitionMaintenanceInterval *string `yaml:"partition_maintenance_interval"`
	AlertHistoryRetention        *string `yaml:"alert_history_retention"`
	RepeatIntervalMinimum        *string `yaml:"repeat_interval_minimum"`
	SnapshotTruncationLimit      *int    `yaml:"snapshot_truncation_limit"`
	CollectionFreshnessThreshold *string `yaml:"collection_freshness_threshold"`
	MigrationLockWaitTimeout     *string `yaml:"migration_lock_wait_timeout"`
	SessionAbsoluteTTL           *string `yaml:"session_absolute_ttl"`
	SessionIdleTTL               *string `yaml:"session_idle_ttl"`
	AgentBinaryDir               *string `yaml:"agent_binary_dir"`
	PlatformDatabaseURL          *string `yaml:"platform_database_url"`
	MasterKeyPath                *string `yaml:"master_key_path"`
}

func defaultServerConfig() serverConfig {
	return serverConfig{
		PartitionSpan:                24 * time.Hour,
		PartitionMaintenanceInterval: time.Hour,
		AlertHistoryRetention:        90 * 24 * time.Hour,
		RepeatIntervalMinimum:        15 * time.Minute,
		SnapshotTruncationLimit:      100,
		CollectionFreshnessThreshold: 10 * time.Minute,
		MigrationLockWaitTimeout:     time.Minute,
		SessionAbsoluteTTL:           12 * time.Hour,
		SessionIdleTTL:               2 * time.Hour,
		AgentBinaryDir:               "/opt/dbs-monitor/bin",
		PlatformDatabaseURL:          "postgres://dbs_monitor@localhost:5432/dbs_monitor?search_path=dbsmon&sslmode=verify-full",
		MasterKeyPath:                "/etc/dbs-monitor/credentials",
	}
}

func loadServerConfig(path string) (serverConfig, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return serverConfig{}, false, fmt.Errorf("open server config %s: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return serverConfig{}, false, fmt.Errorf("stat server config %s: %w", path, err)
	}
	permissionsSecure := info.Mode().Perm() == 0o600

	var raw rawServerConfig
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return serverConfig{}, permissionsSecure, fmt.Errorf("parse server config %s: %w", path, err)
	}
	var extraDocument any
	err = decoder.Decode(&extraDocument)
	if err == nil {
		err = errors.New("multiple YAML documents are not allowed")
	}
	if !errors.Is(err, io.EOF) {
		return serverConfig{}, permissionsSecure, fmt.Errorf("parse server config %s: %w", path, err)
	}

	config := defaultServerConfig()
	durationOverrides := []struct {
		name   string
		value  *string
		target *time.Duration
	}{
		{name: "partition_span", value: raw.PartitionSpan, target: &config.PartitionSpan},
		{name: "partition_maintenance_interval", value: raw.PartitionMaintenanceInterval, target: &config.PartitionMaintenanceInterval},
		{name: "alert_history_retention", value: raw.AlertHistoryRetention, target: &config.AlertHistoryRetention},
		{name: "repeat_interval_minimum", value: raw.RepeatIntervalMinimum, target: &config.RepeatIntervalMinimum},
		{name: "collection_freshness_threshold", value: raw.CollectionFreshnessThreshold, target: &config.CollectionFreshnessThreshold},
		{name: "migration_lock_wait_timeout", value: raw.MigrationLockWaitTimeout, target: &config.MigrationLockWaitTimeout},
		{name: "session_absolute_ttl", value: raw.SessionAbsoluteTTL, target: &config.SessionAbsoluteTTL},
		{name: "session_idle_ttl", value: raw.SessionIdleTTL, target: &config.SessionIdleTTL},
	}
	for _, setting := range durationOverrides {
		if setting.value == nil {
			continue
		}
		parsed, err := time.ParseDuration(*setting.value)
		if err != nil || parsed <= 0 {
			return serverConfig{}, permissionsSecure, fmt.Errorf("%s must be a positive duration", setting.name)
		}
		*setting.target = parsed
	}
	if raw.SnapshotTruncationLimit != nil {
		config.SnapshotTruncationLimit = *raw.SnapshotTruncationLimit
	}
	if raw.AgentBinaryDir != nil {
		config.AgentBinaryDir = *raw.AgentBinaryDir
	}
	if raw.PlatformDatabaseURL != nil {
		config.PlatformDatabaseURL = *raw.PlatformDatabaseURL
	}
	if raw.MasterKeyPath != nil {
		config.MasterKeyPath = *raw.MasterKeyPath
	}
	if masterKeyPath := os.Getenv("DBS_MONITOR_MASTER_KEY_PATH"); masterKeyPath != "" {
		config.MasterKeyPath = masterKeyPath
	}
	if err := config.validate(); err != nil {
		return serverConfig{}, permissionsSecure, err
	}
	return config, permissionsSecure, nil
}

func (config serverConfig) validate() error {
	if config.PartitionSpan < time.Second {
		return errors.New("partition_span must be at least one second")
	}
	if config.SnapshotTruncationLimit <= 0 {
		return errors.New("snapshot_truncation_limit must be positive")
	}
	if config.RepeatIntervalMinimum > 24*time.Hour {
		return errors.New("repeat_interval_minimum must not exceed 24h")
	}
	if config.SessionIdleTTL > config.SessionAbsoluteTTL {
		return errors.New("session_idle_ttl must not exceed session_absolute_ttl")
	}
	if !filepath.IsAbs(config.AgentBinaryDir) {
		return errors.New("agent_binary_dir must be an absolute path")
	}
	if !filepath.IsAbs(config.MasterKeyPath) {
		return errors.New("master_key_path must be an absolute path")
	}
	if err := validatePlatformDatabaseURL(config.PlatformDatabaseURL); err != nil {
		return err
	}
	return nil
}

func validatePlatformDatabaseURL(connectionString string) error {
	parsed, err := url.ParseRequestURI(connectionString)
	if err != nil {
		return errors.New("platform_database_url must be a PostgreSQL TCP URL")
	}
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" {
		return errors.New("platform_database_url must be a PostgreSQL TCP URL")
	}
	query := parsed.Query()
	if values := query["search_path"]; len(values) != 1 || values[0] != "dbsmon" {
		return errors.New("platform_database_url must set search_path=dbsmon")
	}
	if values := query["sslmode"]; len(values) != 1 || values[0] != "verify-full" {
		return errors.New("platform_database_url must set sslmode=verify-full")
	}
	return nil
}
