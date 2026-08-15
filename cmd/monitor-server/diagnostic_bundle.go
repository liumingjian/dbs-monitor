package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

const (
	diagnosticBundleCommand   = "diagnostic-bundle"
	diagnosticBundleMaxBytes  = int64(64 << 20)
	diagnosticArchiveFileMode = 0600
	diagnosticJournalUnit     = "dbs-monitor-server.service"
	diagnosticRedactedValue   = "[REDACTED]"
)

type diagnosticManifest struct {
	GeneratedAt      time.Time `json:"generated_at"`
	JournalWindow    string    `json:"journal_window"`
	JournalTruncated bool      `json:"journal_truncated"`
	MaximumBytes     int64     `json:"maximum_bytes"`
}

type deploymentSummary struct {
	Version   string     `json:"version"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Shape     string     `json:"shape"`
}

type diagnosticConfigSummary struct {
	PartitionSpan                string `json:"partition_span"`
	PartitionMaintenanceInterval string `json:"partition_maintenance_interval"`
	RepeatIntervalMinimum        string `json:"repeat_interval_minimum"`
	SnapshotTruncationLimit      int    `json:"snapshot_truncation_limit"`
	CollectionFreshnessThreshold string `json:"collection_freshness_threshold"`
	MigrationLockWaitTimeout     string `json:"migration_lock_wait_timeout"`
	SessionAbsoluteTTL           string `json:"session_absolute_ttl"`
	SessionIdleTTL               string `json:"session_idle_ttl"`
	AgentBinaryDir               string `json:"agent_binary_dir"`
	PlatformDatabaseHost         string `json:"platform_database_host"`
	PlatformDatabasePort         string `json:"platform_database_port"`
	PlatformDatabaseName         string `json:"platform_database_name"`
	PlatformDatabaseUsername     string `json:"platform_database_username"`
	PlatformDatabasePassword     string `json:"platform_database_password"`
	PlatformDatabaseSearchPath   string `json:"platform_database_search_path"`
	PlatformDatabaseSSLMode      string `json:"platform_database_sslmode"`
}

type diagnosticFile struct {
	name    string
	content []byte
}

type diagnosticBundleOptions struct {
	InputTruncated bool
	Config         *serverConfig
}

func runDiagnosticBundleCommand(ctx context.Context, output string) error {
	config, _, err := loadServerConfig(env("DBS_MONITOR_CONFIG_FILE", defaultServerConfigPath))
	if err != nil {
		return err
	}
	journal, inputTruncated, err := readPlatformJournal(ctx, diagnosticBundleMaxBytes)
	if err != nil {
		return err
	}
	return createDiagnosticBundleWithOptions(
		output, journal, time.Now().UTC(), "linux-systemd", diagnosticBundleMaxBytes,
		diagnosticBundleOptions{InputTruncated: inputTruncated, Config: &config},
	)
}

func platformDatabasePassword(connectionString string) (string, error) {
	parsed, err := url.Parse(connectionString)
	if err != nil {
		return "", fmt.Errorf("parse platform database URL for diagnostic secret scan: %w", err)
	}
	if parsed.User == nil {
		return "", nil
	}
	password, _ := parsed.User.Password()
	return password, nil
}

func readPlatformJournal(ctx context.Context, maximumBytes int64) ([]byte, bool, error) {
	command := exec.CommandContext(
		ctx,
		"journalctl",
		"--unit", diagnosticJournalUnit,
		"--since=-24 hours",
		"--reverse",
		"--no-pager",
		"--output=cat",
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("open journal output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, false, fmt.Errorf("start journalctl: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var newestFirst [][]byte
	var totalBytes int64
	truncated := false
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		line = append(line, '\n')
		if totalBytes+int64(len(line)) > maximumBytes {
			truncated = true
			break
		}
		newestFirst = append(newestFirst, line)
		totalBytes += int64(len(line))
	}
	if truncated {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, false, fmt.Errorf("read journalctl output: %w", scanErr)
	}
	if waitErr != nil && !truncated {
		return nil, false, fmt.Errorf("journalctl failed: %s", strings.TrimSpace(stderr.String()))
	}

	var journal bytes.Buffer
	for index := len(newestFirst) - 1; index >= 0; index-- {
		_, _ = journal.Write(newestFirst[index])
	}
	return journal.Bytes(), truncated, nil
}

func createDiagnosticBundle(output string, journal []byte, generatedAt time.Time, shape string, maximumBytes int64) error {
	return createDiagnosticBundleWithOptions(
		output, journal, generatedAt, shape, maximumBytes, diagnosticBundleOptions{},
	)
}

func createDiagnosticBundleWithOptions(output string, journal []byte, generatedAt time.Time, shape string, maximumBytes int64, options diagnosticBundleOptions) error {
	if maximumBytes <= 0 {
		return errors.New("diagnostic bundle maximum size must be positive")
	}
	var configuredSecrets []string
	if options.Config != nil {
		platformPassword, err := platformDatabasePassword(options.Config.PlatformDatabaseURL)
		if err != nil {
			return err
		}
		configuredSecrets = append(configuredSecrets, platformPassword)
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("diagnostic bundle output already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect diagnostic bundle output: %w", err)
	}

	if err := scanDiagnosticContent("journal.log", journal, configuredSecrets...); err != nil {
		return err
	}
	snapshot, err := latestHealthSummary(journal)
	if err != nil {
		return err
	}
	snapshotFiles, err := diagnosticSnapshotFiles(snapshot, shape, options.Config)
	if err != nil {
		return err
	}
	for _, file := range snapshotFiles {
		if err := scanDiagnosticContent(file.name, file.content, configuredSecrets...); err != nil {
			return err
		}
	}

	lines := bytes.SplitAfter(journal, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	archive, err := renderDiagnosticArchive(snapshotFiles, journal, generatedAt, maximumBytes, options.InputTruncated)
	if err != nil {
		return err
	}
	if int64(len(archive)) > maximumBytes {
		archive, err = largestFittingDiagnosticArchive(snapshotFiles, lines, generatedAt, maximumBytes)
		if err != nil {
			return err
		}
	}
	return publishDiagnosticArchive(output, archive)
}

func latestHealthSummary(journal []byte) (platformhealth.Snapshot, error) {
	var latest platformhealth.Snapshot
	found := false
	for _, line := range bytes.Split(journal, []byte{'\n'}) {
		start := bytes.IndexByte(line, '{')
		if start < 0 {
			continue
		}
		candidate := line[start:]
		var envelope struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(candidate, &envelope) != nil || envelope.Event != "platform_health_summary" {
			continue
		}
		var snapshot platformhealth.Snapshot
		if err := json.Unmarshal(candidate, &snapshot); err != nil {
			continue
		}
		latest = snapshot
		found = true
	}
	if !found {
		return platformhealth.Snapshot{}, errors.New("diagnostic bundle requires a platform_health_summary from the last 24 hours")
	}
	return latest, nil
}

func diagnosticSnapshotFiles(snapshot platformhealth.Snapshot, shape string, config *serverConfig) ([]diagnosticFile, error) {
	files := make([]diagnosticFile, 0, 9)
	health, err := marshalDiagnosticJSON(snapshot)
	if err != nil {
		return nil, err
	}
	files = append(files, diagnosticFile{name: "diagnostics/health.json", content: health})

	endpoints := []struct {
		name   string
		source platformhealth.Source
	}{
		{name: "disk", source: platformhealth.SourceDisk},
		{name: "scheduler", source: platformhealth.SourceCollectionScheduler},
		{name: "partitions", source: platformhealth.SourcePartitionMaintenance},
		{name: "certificate", source: platformhealth.SourceTLSCertificate},
		{name: "keyring", source: platformhealth.SourceCredentialKeyring},
		{name: "platform", source: platformhealth.SourceServerProcess},
	}
	bySource := make(map[platformhealth.Source]platformhealth.SourceSnapshot, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		bySource[source.Source] = source
	}
	for _, endpoint := range endpoints {
		source, exists := bySource[endpoint.source]
		if !exists {
			source = platformhealth.SourceSnapshot{
				Source: endpoint.source,
				Status: platformhealth.StatusUnknown,
				Code:   "FACT_UNAVAILABLE",
			}
		}
		content, err := marshalDiagnosticJSON(source)
		if err != nil {
			return nil, err
		}
		files = append(files, diagnosticFile{name: "diagnostics/" + endpoint.name + ".json", content: content})
	}

	process := bySource[platformhealth.SourceServerProcess]
	version := ""
	if process.Version != nil {
		version = *process.Version
	}
	deployment, err := marshalDiagnosticJSON(deploymentSummary{
		Version:   version,
		StartedAt: process.StartedAt,
		Shape:     shape,
	})
	if err != nil {
		return nil, err
	}
	files = append(files, diagnosticFile{name: "deployment.json", content: deployment})
	if config != nil {
		summary, err := effectiveDiagnosticConfigSummary(*config)
		if err != nil {
			return nil, err
		}
		content, err := marshalDiagnosticJSON(summary)
		if err != nil {
			return nil, err
		}
		files = append(files, diagnosticFile{name: "config.json", content: content})
	}
	return files, nil
}

func effectiveDiagnosticConfigSummary(config serverConfig) (diagnosticConfigSummary, error) {
	parsed, err := url.Parse(config.PlatformDatabaseURL)
	if err != nil {
		return diagnosticConfigSummary{}, fmt.Errorf("parse platform database URL for diagnostic config summary: %w", err)
	}
	username := ""
	if parsed.User != nil {
		username = parsed.User.Username()
	}
	return diagnosticConfigSummary{
		PartitionSpan:                config.PartitionSpan.String(),
		PartitionMaintenanceInterval: config.PartitionMaintenanceInterval.String(),
		RepeatIntervalMinimum:        config.RepeatIntervalMinimum.String(),
		SnapshotTruncationLimit:      config.SnapshotTruncationLimit,
		CollectionFreshnessThreshold: config.CollectionFreshnessThreshold.String(),
		MigrationLockWaitTimeout:     config.MigrationLockWaitTimeout.String(),
		SessionAbsoluteTTL:           config.SessionAbsoluteTTL.String(),
		SessionIdleTTL:               config.SessionIdleTTL.String(),
		AgentBinaryDir:               config.AgentBinaryDir,
		PlatformDatabaseHost:         parsed.Hostname(),
		PlatformDatabasePort:         parsed.Port(),
		PlatformDatabaseName:         strings.TrimPrefix(parsed.Path, "/"),
		PlatformDatabaseUsername:     username,
		PlatformDatabasePassword:     diagnosticRedactedValue,
		PlatformDatabaseSearchPath:   parsed.Query().Get("search_path"),
		PlatformDatabaseSSLMode:      parsed.Query().Get("sslmode"),
	}, nil
}

func marshalDiagnosticJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode diagnostic JSON: %w", err)
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("normalize diagnostic JSON: %w", err)
	}
	redactDiagnosticPasswordFields(normalized)
	encoded, err = json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode redacted diagnostic JSON: %w", err)
	}
	return append(encoded, '\n'), nil
}

func redactDiagnosticPasswordFields(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if isDiagnosticPasswordField(key) {
				value[key] = diagnosticRedactedValue
				continue
			}
			redactDiagnosticPasswordFields(child)
		}
	case []any:
		for _, child := range value {
			redactDiagnosticPasswordFields(child)
		}
	}
}

func renderDiagnosticArchive(snapshotFiles []diagnosticFile, journal []byte, generatedAt time.Time, maximumBytes int64, journalTruncated bool) ([]byte, error) {
	manifest, err := marshalDiagnosticJSON(diagnosticManifest{
		GeneratedAt:      generatedAt.UTC(),
		JournalWindow:    "24h",
		JournalTruncated: journalTruncated,
		MaximumBytes:     maximumBytes,
	})
	if err != nil {
		return nil, err
	}
	files := make([]diagnosticFile, 0, len(snapshotFiles)+2)
	files = append(files, diagnosticFile{name: "manifest.json", content: manifest})
	files = append(files, diagnosticFile{name: "journal.log", content: journal})
	files = append(files, snapshotFiles...)

	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	gzipWriter.Header.ModTime = generatedAt.UTC()
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		header := &tar.Header{
			Name:    file.name,
			Mode:    diagnosticArchiveFileMode,
			Size:    int64(len(file.content)),
			ModTime: generatedAt.UTC(),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write diagnostic archive header: %w", err)
		}
		if _, err := tarWriter.Write(file.content); err != nil {
			return nil, fmt.Errorf("write diagnostic archive content: %w", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close diagnostic tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close diagnostic gzip: %w", err)
	}
	return output.Bytes(), nil
}

func largestFittingDiagnosticArchive(snapshotFiles []diagnosticFile, lines [][]byte, generatedAt time.Time, maximumBytes int64) ([]byte, error) {
	emptyJournalArchive, err := renderDiagnosticArchive(snapshotFiles, nil, generatedAt, maximumBytes, true)
	if err != nil {
		return nil, err
	}
	if int64(len(emptyJournalArchive)) > maximumBytes {
		return nil, fmt.Errorf("diagnostic metadata exceeds maximum bundle size of %d bytes", maximumBytes)
	}
	largestArchive := emptyJournalArchive
	minimumLines, maximumLines := 0, len(lines)
	for minimumLines <= maximumLines {
		keptLineCount := minimumLines + (maximumLines-minimumLines)/2
		journal := bytes.Join(lines[len(lines)-keptLineCount:], nil)
		candidate, err := renderDiagnosticArchive(snapshotFiles, journal, generatedAt, maximumBytes, true)
		if err != nil {
			return nil, err
		}
		if int64(len(candidate)) <= maximumBytes {
			largestArchive = candidate
			minimumLines = keptLineCount + 1
		} else {
			maximumLines = keptLineCount - 1
		}
	}
	return largestArchive, nil
}

func scanDiagnosticContent(name string, content []byte, configuredSecrets ...string) error {
	for _, secret := range configuredSecrets {
		if secret != "" && bytes.Contains(content, []byte(secret)) {
			return fmt.Errorf("diagnostic bundle secret scan rejected %s: configured secret", name)
		}
	}
	contentToScan, err := diagnosticContentWithoutRedactedPasswordFields(name, content)
	if err != nil {
		return err
	}
	lowercaseContent := bytes.ToLower(contentToScan)
	for _, forbidden := range [][]byte{
		[]byte("password"), []byte("ciphertext"), []byte("master_key"), []byte("master key"),
		[]byte("token"), []byte("authorization"), []byte("dsn"), []byte("request_body"), []byte("raw_sql"),
		[]byte("postgres://"), []byte("postgresql://"), []byte("bearer "), []byte("-----begin private key"),
		[]byte("select "), []byte("insert into "), []byte("update "), []byte("delete from "),
		[]byte("create table "), []byte("alter table "), []byte("drop table "),
	} {
		if bytes.Contains(lowercaseContent, forbidden) {
			return fmt.Errorf("diagnostic bundle secret scan rejected %s: forbidden marker %q", name, forbidden)
		}
	}
	return nil
}

func diagnosticContentWithoutRedactedPasswordFields(name string, content []byte) ([]byte, error) {
	if !strings.HasSuffix(name, ".json") {
		return content, nil
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return content, nil
	}
	if err := removeRedactedPasswordFields(value); err != nil {
		return nil, fmt.Errorf("diagnostic bundle secret scan rejected %s: %w", name, err)
	}
	filtered, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("normalize %s for diagnostic secret scan: %w", name, err)
	}
	return filtered, nil
}

func removeRedactedPasswordFields(value any) error {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if isDiagnosticPasswordField(key) {
				if child != diagnosticRedactedValue {
					return fmt.Errorf("password field %q is not redacted", key)
				}
				delete(value, key)
				continue
			}
			if err := removeRedactedPasswordFields(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := removeRedactedPasswordFields(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func isDiagnosticPasswordField(name string) bool {
	return strings.Contains(strings.ToLower(name), "password")
}

func publishDiagnosticArchive(output string, archive []byte) error {
	directory := filepath.Dir(output)
	temporary, err := os.CreateTemp(directory, ".dbs-monitor-diagnostics-*.tmp")
	if err != nil {
		return fmt.Errorf("create diagnostic bundle temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(diagnosticArchiveFileMode); err != nil {
		return fmt.Errorf("secure diagnostic bundle temporary file: %w", err)
	}
	if _, err := io.Copy(temporary, bytes.NewReader(archive)); err != nil {
		return fmt.Errorf("write diagnostic bundle: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close diagnostic bundle: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("publish diagnostic bundle: %w", err)
	}
	return nil
}
