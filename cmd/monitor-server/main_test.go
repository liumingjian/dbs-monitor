package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		if got, want := err.Error(), "usage: dbs-monitor-server [--version|rotate-master-key|diagnostic-bundle [output]]"; got != want {
			t.Fatalf("runCommand(%q) error = %q, want %q", arguments, got, want)
		}
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

	if err := createDiagnosticBundle(output, []byte(journal), now, "linux-systemd", diagnosticBundleMaxBytes); err != nil {
		t.Fatalf("create diagnostic bundle: %v", err)
	}

	files := readDiagnosticArchive(t, output)
	for _, name := range []string{
		"manifest.json", "journal.log", "diagnostics/health.json", "diagnostics/disk.json",
		"diagnostics/scheduler.json", "diagnostics/partitions.json", "diagnostics/certificate.json",
		"diagnostics/keyring.json", "diagnostics/platform.json", "deployment.json",
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
			},
			want: platformhealth.DiskThresholds{Warning: 75.5, Critical: 85, Emergency: 92.5, Hysteresis: 2},
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
			for _, name := range []string{"DISK_WARNING_PERCENT", "DISK_CRITICAL_PERCENT", "DISK_EMERGENCY_PERCENT"} {
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
