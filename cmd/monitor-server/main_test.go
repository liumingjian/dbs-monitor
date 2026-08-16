package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformevent"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestRunCommandRejectsUnsupportedArguments(t *testing.T) {
	for _, arguments := range [][]string{
		{"unsupported"},
		{rotateMasterKeyCommand, "unexpected"},
	} {
		err := runCommand(context.Background(), arguments, io.Discard)
		if err == nil {
			t.Fatalf("runCommand(%q) succeeded", arguments)
		}
		if got := err.Error(); got != commandUsage {
			t.Fatalf("runCommand(%q) error = %q, want %q", arguments, got, commandUsage)
		}
	}
}

func TestRunRejectsUnreadableConfigBeforeStartup(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-config.yaml")
	t.Setenv("DBS_MONITOR_CONFIG_FILE", missing)

	err := runCommand(context.Background(), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "open server config") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("run error = %v, want unreadable config failure", err)
	}
}

func TestRunCommandReportsCandidateIdentity(t *testing.T) {
	previousVersion, previousCommitSHA := version, commitSHA
	version = "0.0.0-dev+0123456789abcdef0123456789abcdef01234567"
	commitSHA = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() {
		version, commitSHA = previousVersion, previousCommitSHA
	})

	var output bytes.Buffer
	if err := runCommand(context.Background(), []string{"--version"}, &output); err != nil {
		t.Fatalf("runCommand(--version): %v", err)
	}
	const want = "dbs-monitor-server 0.0.0-dev+0123456789abcdef0123456789abcdef01234567 (0123456789abcdef0123456789abcdef01234567)\n"
	if output.String() != want {
		t.Fatalf("version output = %q, want %q", output.String(), want)
	}
}

func TestCreateDiagnosticBundleContainsEndpointSnapshots(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	journal := strings.Join([]string{
		`ordinary service log`,
		`2026/08/11 11:59:00 {"event":"platform_health_summary","status":"DEGRADED","sources":[` +
			`{"source":"SERVER_PROCESS","status":"OK","code":"SERVER_PROCESS_RUNNING","version":"3.0.0","started_at":"2026-08-11T10:00:00Z"},` +
			`{"source":"COLLECTION_SCHEDULER","status":"DEGRADED","code":"SCHEDULER_BACKPRESSURE","pending":2},` +
			`{"source":"PARTITION_MAINTENANCE","status":"OK","code":"PARTITIONS_READY","prebuild_days_remaining":7},` +
			`{"source":"TLS_CERTIFICATE","status":"OK","code":"CERTIFICATE_VALID","expires_at":"2027-08-11T00:00:00Z"},` +
			`{"source":"DISK","status":"OK","code":"DISK_NORMAL","disk_level":"NORMAL","disk_usage_percent":42},` +
			`{"source":"CREDENTIAL_KEYRING","status":"OK","code":"CREDENTIAL_KEYRING_READY"}` +
			`],"assembled_at":"2026-08-11T11:59:00Z"}`,
	}, "\n") + "\n"
	output := filepath.Join(t.TempDir(), "diagnostics.tar.gz")
	config := defaultServerConfig()
	config.PlatformDatabaseURL = "postgres://dbs_monitor:platform-db-password-issue-75@platform-db:5432/dbs_monitor?search_path=dbsmon&sslmode=verify-full"

	if err := createDiagnosticBundleWithOptions(
		output, []byte(journal), now, "linux-systemd", diagnosticBundleMaxBytes,
		diagnosticBundleOptions{Config: &config},
	); err != nil {
		t.Fatalf("create diagnostic bundle: %v", err)
	}

	files := readDiagnosticArchive(t, output)
	for _, name := range []string{
		"manifest.json", "journal.log", "diagnostics/health.json", "diagnostics/disk.json",
		"diagnostics/scheduler.json", "diagnostics/partitions.json", "diagnostics/certificate.json",
		"diagnostics/keyring.json", "diagnostics/platform.json", "deployment.json", "config.json",
	} {
		if _, exists := files[name]; !exists {
			t.Errorf("diagnostic archive is missing %s", name)
		}
	}
	if !bytes.Contains(files["diagnostics/scheduler.json"], []byte(`"pending": 2`)) {
		t.Fatalf("scheduler snapshot = %s, want current scheduler facts", files["diagnostics/scheduler.json"])
	}
	if !bytes.Contains(files["deployment.json"], []byte(`"version": "3.0.0"`)) ||
		!bytes.Contains(files["deployment.json"], []byte(`"shape": "linux-systemd"`)) {
		t.Fatalf("deployment summary = %s", files["deployment.json"])
	}
	configSummary := files["config.json"]
	if !bytes.Contains(configSummary, []byte(`"platform_database_password": "[REDACTED]"`)) ||
		!bytes.Contains(configSummary, []byte(`"partition_span": "24h0m0s"`)) {
		t.Fatalf("effective config summary = %s", configSummary)
	}
	for _, forbidden := range []string{
		"platform-db-password-issue-75", "platform_database_url", "# DBS Monitor server deployment configuration",
	} {
		if bytes.Contains(configSummary, []byte(forbidden)) {
			t.Errorf("effective config summary contains forbidden raw config content %q: %s", forbidden, configSummary)
		}
	}
}

func TestCreateDiagnosticBundleTruncatesOldestJournalAndDeclaresIt(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	var journal strings.Builder
	for index := 0; index < 100; index++ {
		journal.WriteString(fmt.Sprintf("oldest-%03d-%x\n", index, sha256.Sum256([]byte(fmt.Sprintf("journal-record-%d", index)))))
	}
	journal.WriteString(`{"event":"platform_health_summary","status":"OK","sources":[],"assembled_at":"2026-08-11T12:00:00Z"}` + "\n")
	output := filepath.Join(t.TempDir(), "diagnostics.tar.gz")
	const maximumBytes = 1400

	if err := createDiagnosticBundle(output, []byte(journal.String()), now, "linux-systemd", maximumBytes); err != nil {
		t.Fatalf("create truncated diagnostic bundle: %v", err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat diagnostic bundle: %v", err)
	}
	if info.Size() > maximumBytes {
		t.Fatalf("diagnostic bundle size = %d, want <= %d", info.Size(), maximumBytes)
	}
	files := readDiagnosticArchive(t, output)
	var manifest struct {
		JournalTruncated bool `json:"journal_truncated"`
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if !manifest.JournalTruncated {
		t.Fatal("manifest does not declare journal truncation")
	}
	if bytes.Contains(files["journal.log"], []byte("oldest-000-")) ||
		!bytes.Contains(files["journal.log"], []byte(`platform_health_summary`)) {
		t.Fatalf("journal did not drop oldest records and retain newest summary: %q", files["journal.log"])
	}
}

func TestCreateDiagnosticBundleRejectsSecretsWithoutPublishingArchive(t *testing.T) {
	output := filepath.Join(t.TempDir(), "diagnostics.tar.gz")
	journal := []byte("Authorization: Bearer should-never-leave-host\n")

	err := createDiagnosticBundle(output, journal, time.Now(), "linux-systemd", diagnosticBundleMaxBytes)
	if err == nil || !strings.Contains(err.Error(), "secret scan") {
		t.Fatalf("create diagnostic bundle error = %v, want secret scan rejection", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("rejected diagnostic bundle was published: %v", statErr)
	}
}

func TestCreateDiagnosticBundleRejectsConfiguredPlatformDatabasePassword(t *testing.T) {
	output := filepath.Join(t.TempDir(), "diagnostics.tar.gz")
	const platformPassword = "platform-db-password-issue-75"
	journal := []byte("connection failed for value " + platformPassword + "\n")
	config := defaultServerConfig()
	config.PlatformDatabaseURL = "postgres://dbs_monitor:" + platformPassword + "@platform-db:5432/dbs_monitor?search_path=dbsmon&sslmode=verify-full"

	err := createDiagnosticBundleWithOptions(
		output, journal, time.Now(), "linux-systemd", diagnosticBundleMaxBytes,
		diagnosticBundleOptions{Config: &config},
	)
	if err == nil || !strings.Contains(err.Error(), "configured secret") {
		t.Fatalf("create diagnostic bundle error = %v, want configured platform password rejection", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("rejected diagnostic bundle was published: %v", statErr)
	}
}

func TestMarshalDiagnosticJSONRedactsPasswordFields(t *testing.T) {
	content, err := marshalDiagnosticJSON(map[string]any{
		"platform_database_password": "platform-db-password-issue-75",
		"nested":                     map[string]any{"password": "another-secret"},
		"large_counter":              int64(9007199254740993),
		"safe":                       "visible",
	})
	if err != nil {
		t.Fatalf("marshal diagnostic JSON: %v", err)
	}
	if bytes.Contains(content, []byte("platform-db-password-issue-75")) || bytes.Contains(content, []byte("another-secret")) {
		t.Fatalf("diagnostic JSON contains a password value: %s", content)
	}
	if got := bytes.Count(content, []byte(`"[REDACTED]"`)); got != 2 {
		t.Fatalf("redacted password values = %d, want 2: %s", got, content)
	}
	if !bytes.Contains(content, []byte(`"large_counter": 9007199254740993`)) {
		t.Fatalf("diagnostic JSON rounded an int64 counter: %s", content)
	}
	if err := scanDiagnosticContent("diagnostics/test.json", content); err != nil {
		t.Fatalf("secret scan rejected fixed redactions: %v", err)
	}
}

func TestDiagnosticBundleStoreListsAndDeletesOneArchiveWithEvent(t *testing.T) {
	directory := t.TempDir()
	oldName := "dbs-monitor-diagnostics-20260815T100000Z.tar.gz"
	newName := "dbs-monitor-diagnostics-20260815T110000Z.tar.gz"
	for name, content := range map[string]string{
		oldName:            "old bundle",
		newName:            "new bundle",
		"unmanaged.tar.gz": "not managed by the diagnostic bundle store",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), diagnosticArchiveFileMode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	oldTime := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(directory, oldName), oldTime, oldTime); err != nil {
		t.Fatalf("set old bundle time: %v", err)
	}
	if err := os.Chtimes(filepath.Join(directory, newName), newTime, newTime); err != nil {
		t.Fatalf("set new bundle time: %v", err)
	}

	bundles, err := listDiagnosticBundles(directory)
	if err != nil {
		t.Fatalf("list diagnostic bundles: %v", err)
	}
	if len(bundles) != 2 || bundles[0].Name != oldName || bundles[1].Name != newName {
		t.Fatalf("listed bundles = %+v, want managed archives oldest first", bundles)
	}

	var event diagnosticBundleDeletionEvent
	record := func(candidate diagnosticBundleDeletionEvent) error {
		event = candidate
		return nil
	}
	deletedAt := newTime.Add(time.Hour)
	if err := deleteDiagnosticBundle(directory, oldName, deletedAt, record); err != nil {
		t.Fatalf("delete diagnostic bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, oldName)); !os.IsNotExist(err) {
		t.Fatalf("deleted bundle still exists: %v", err)
	}
	if event.Event != "diagnostic_bundle_deleted" || event.BundleName != oldName ||
		event.DeletedAt != deletedAt || event.Bytes != int64(len("old bundle")) {
		t.Fatalf("deletion event = %+v", event)
	}
	if _, err := os.Stat(filepath.Join(directory, newName)); err != nil {
		t.Fatalf("delete removed another bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "unmanaged.tar.gz")); err != nil {
		t.Fatalf("delete removed unmanaged archive: %v", err)
	}
}

func TestDiagnosticBundleStopsBeforeWritingAtLocalDiskEmergency(t *testing.T) {
	now := time.Now().UTC()
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), nil)
	health.Update(now, platformhealth.DiskSource(
		1,
		platformhealth.DiskNormal,
		platformhealth.DiskThresholds{Warning: 0.25, Critical: 0.5, Emergency: 0.75, Hysteresis: 0.1},
	))
	if source := health.Source(platformhealth.SourceDisk); source.Status != platformhealth.StatusFailed {
		t.Fatalf("local disk source = %+v, want FAILED after lowering emergency threshold", source)
	}
	output := filepath.Join(t.TempDir(), "dbs-monitor-diagnostics-20260816T120000Z.tar.gz")
	err := createDiagnosticBundleWithOptions(
		output, nil, now, "linux-systemd", diagnosticBundleMaxBytes,
		diagnosticBundleOptions{LocalWriteAllowed: func() bool { return !health.RejectLocalLargeWrites() }},
	)
	if !errors.Is(err, errLocalLargeWriteRejected) {
		t.Fatalf("diagnostic bundle error = %v, want local large write rejection", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnostic bundle was written at local disk emergency: %v", err)
	}
}

func TestLocalArtifactReclamationKeepsConfiguredLimitsAndRecordsEvents(t *testing.T) {
	bundleDirectory := t.TempDir()
	base := time.Date(2026, time.August, 16, 10, 0, 0, 0, time.UTC)
	for index, name := range []string{
		"dbs-monitor-diagnostics-20260816T100000Z.tar.gz",
		"dbs-monitor-diagnostics-20260816T110000Z.tar.gz",
		"dbs-monitor-diagnostics-20260816T120000Z.tar.gz",
	} {
		path := filepath.Join(bundleDirectory, name)
		if err := os.WriteFile(path, []byte(name), diagnosticArchiveFileMode); err != nil {
			t.Fatalf("write diagnostic bundle: %v", err)
		}
		modified := base.Add(time.Duration(index) * time.Hour)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatalf("set diagnostic bundle time: %v", err)
		}
	}

	snapshotPath := filepath.Join(t.TempDir(), "notification-channels.snapshot")
	snapshotStore := notify.NewChannelSnapshotStore(snapshotPath)
	if err := snapshotStore.Write(notify.ChannelSnapshot{FormatVersion: notify.ChannelSnapshotFormatVersion}); err != nil {
		t.Fatalf("write notification snapshot: %v", err)
	}
	var events []localArtifactReclamationEvent
	if err := reclaimLocalArtifacts(bundleDirectory, 1, snapshotStore, 1, base.Add(3*time.Hour), func(event localArtifactReclamationEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("reclaim local artifacts: %v", err)
	}

	bundles, err := listDiagnosticBundles(bundleDirectory)
	if err != nil {
		t.Fatalf("list retained diagnostic bundles: %v", err)
	}
	if len(bundles) != 1 || bundles[0].Name != "dbs-monitor-diagnostics-20260816T120000Z.tar.gz" {
		t.Fatalf("retained diagnostic bundles = %+v, want newest only", bundles)
	}
	if len(events) != 2 || events[0].Kind != platformevent.DiagnosticBundleReclaimed || events[1].Kind != platformevent.DiagnosticBundleReclaimed {
		t.Fatalf("reclamation events = %+v, want two diagnostic bundle events", events)
	}
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("bounded notification snapshot was reclaimed: %v", err)
	}
}

func TestDeleteDiagnosticBundleRestoresArchiveWhenEventRecordingFails(t *testing.T) {
	directory := t.TempDir()
	name := "dbs-monitor-diagnostics-20260815T120000Z.tar.gz"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("bundle"), diagnosticArchiveFileMode); err != nil {
		t.Fatalf("write diagnostic bundle: %v", err)
	}
	recordError := errors.New("platform event unavailable")
	err := deleteDiagnosticBundle(directory, name, time.Now(), func(diagnosticBundleDeletionEvent) error {
		return recordError
	})
	if !errors.Is(err, recordError) {
		t.Fatalf("delete diagnostic bundle error = %v, want event error", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("bundle was not restored after event failure: %v", err)
	}
	if err := deleteDiagnosticBundle(directory, "../"+name, time.Now(), func(diagnosticBundleDeletionEvent) error {
		return nil
	}); err == nil {
		t.Fatal("delete diagnostic bundle accepted path traversal")
	}
}

func TestDiagnosticSecretScanRejectsForbiddenContent(t *testing.T) {
	for _, content := range []string{
		`{"password":"value"}`,
		`{"password_ciphertext":"value"}`,
		`{"master_key":"value"}`,
		`{"agent_token":"value"}`,
		`Authorization: Bearer value`,
		`postgres://user:value@localhost/database`,
		`{"request_body":"value"}`,
		`{"raw_sql":"SELECT 1"}`,
		`SELECT current_database()`,
		`-----BEGIN PRIVATE KEY-----`,
	} {
		if err := scanDiagnosticContent("test", []byte(content)); err == nil {
			t.Errorf("secret scan accepted %q", content)
		}
	}
}

func readDiagnosticArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open diagnostic archive: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open diagnostic gzip: %v", err)
	}
	defer gzipReader.Close()
	files := map[string][]byte{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatalf("read diagnostic tar: %v", err)
		}
		content, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		files[header.Name] = content
	}
}

func TestDiskThresholdsFromEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]string
		want      platformhealth.DiskThresholds
		wantError bool
	}{
		{name: "defaults", want: platformhealth.DefaultDiskThresholds()},
		{
			name: "deployment overrides",
			values: map[string]string{
				"DISK_WARNING_PERCENT":   "75.5",
				"DISK_CRITICAL_PERCENT":  "85",
				"DISK_EMERGENCY_PERCENT": "92.5",
				"DISK_HYSTERESIS_POINTS": "1.5",
			},
			want: platformhealth.DiskThresholds{Warning: 75.5, Critical: 85, Emergency: 92.5, Hysteresis: 1.5},
		},
		{name: "non numeric", values: map[string]string{"DISK_WARNING_PERCENT": "high"}, wantError: true},
		{name: "non finite", values: map[string]string{"DISK_WARNING_PERCENT": "NaN"}, wantError: true},
		{
			name:   "unordered thresholds",
			values: map[string]string{"DISK_WARNING_PERCENT": "91", "DISK_CRITICAL_PERCENT": "90"}, wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"DISK_WARNING_PERCENT", "DISK_CRITICAL_PERCENT", "DISK_EMERGENCY_PERCENT", "DISK_HYSTERESIS_POINTS"} {
				t.Setenv(name, "")
			}
			for name, value := range test.values {
				t.Setenv(name, value)
			}
			got, err := diskThresholdsFromEnvironment()
			if (err != nil) != test.wantError {
				t.Fatalf("diskThresholdsFromEnvironment() error = %v, wantError %t", err, test.wantError)
			}
			if !test.wantError && got != test.want {
				t.Fatalf("disk thresholds = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPlatformDatabaseCapacityThresholdsFromEnvironment(t *testing.T) {
	for name, value := range map[string]string{
		"PLATFORM_DATABASE_CAPACITY_WARNING_PERCENT":   "70",
		"PLATFORM_DATABASE_CAPACITY_CRITICAL_PERCENT":  "85",
		"PLATFORM_DATABASE_CAPACITY_EMERGENCY_PERCENT": "93",
		"PLATFORM_DATABASE_CAPACITY_HYSTERESIS_POINTS": "1",
	} {
		t.Setenv(name, value)
	}
	got, err := platformDatabaseCapacityThresholdsFromEnvironment()
	if err != nil {
		t.Fatalf("platformDatabaseCapacityThresholdsFromEnvironment: %v", err)
	}
	want := platformhealth.DiskThresholds{Warning: 70, Critical: 85, Emergency: 93, Hysteresis: 1}
	if got != want {
		t.Fatalf("platform database capacity thresholds = %+v, want %+v", got, want)
	}
}

func TestEvaluationConfigFromEnvironment(t *testing.T) {
	const setting = "ALERT_TRIGGER_SNAPSHOT_SESSION_LIMIT"
	tests := []struct {
		name      string
		value     string
		wantLimit int
		wantError bool
	}{
		{name: "default", wantLimit: 100},
		{name: "configured for acceptance", value: "5", wantLimit: 5},
		{name: "not an integer", value: "five", wantError: true},
		{name: "not positive", value: "0", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(setting, test.value)
			config, err := evaluationConfigFromEnvironment()
			if (err != nil) != test.wantError {
				t.Fatalf("evaluationConfigFromEnvironment() error = %v, want error %t", err, test.wantError)
			}
			if err == nil && config.TriggerSnapshotSessionLimit != test.wantLimit {
				t.Fatalf("trigger snapshot session limit = %d, want %d", config.TriggerSnapshotSessionLimit, test.wantLimit)
			}
		})
	}
}
