package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/acceptancereport"
)

const testCandidateSHA = "0123456789012345678901234567890123456789"

func TestGenerateFinalReport(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeFinalManifest(t, directory)
	outputPath := filepath.Join(directory, "acceptance-report.json")

	if err := generate(manifestPath, outputPath, false); err != nil {
		t.Fatalf("generate report: %v", err)
	}

	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report acceptancereport.Report
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Verdict != acceptancereport.VerdictGo || report.Provisional {
		t.Fatalf("decision = %s provisional=%v reasons=%v, want GO", report.Verdict, report.Provisional, report.ReasonCodes)
	}
	if len(report.Rounds) != 2 || report.Rounds[0].Evidence.Package == nil || report.Rounds[1].Evidence.LinuxNative == nil {
		t.Fatalf("round evidence was not preserved: %+v", report.Rounds)
	}
	if report.Rounds[0].Evidence.Matrix.ArtifactSHA256 == "" {
		t.Error("matrix artifact SHA-256 is empty")
	}
	if report.Rounds[0].Evidence.Matrix.Exceptions == nil || len(report.Rounds[0].Evidence.Matrix.Exceptions) != 0 {
		t.Errorf("matrix exceptions = %v, want an explicit empty array", report.Rounds[0].Evidence.Matrix.Exceptions)
	}
}

func TestGenerateRefusesGitHubActionsValidationArchive(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeFinalManifest(t, directory)
	outputPath := filepath.Join(repositoryRoot(t), "docs", "validation", "issue-138-should-not-exist.json")
	t.Cleanup(func() { _ = os.Remove(outputPath) })

	err := generate(manifestPath, outputPath, true)

	if err == nil {
		t.Fatal("generate succeeded, want GitHub Actions archive refusal")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("validation report was written: %v", statErr)
	}
}

func TestGenerateGitHubActionsReportIsProvisional(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeFinalManifest(t, directory)
	outputPath := filepath.Join(directory, "acceptance-report.json")

	if err := generate(manifestPath, outputPath, true); err != nil {
		t.Fatalf("generate report: %v", err)
	}
	report := readReport(t, outputPath)
	if report.Verdict != acceptancereport.VerdictProvisionalPass || !report.Provisional {
		t.Fatalf("decision = %s provisional=%v reasons=%v, want PROVISIONAL-PASS", report.Verdict, report.Provisional, report.ReasonCodes)
	}
}

func TestGenerateScansSecretsBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeFinalManifest(t, directory)
	writeJSON(t, filepath.Join(directory, "rt_c.json"), map[string]any{
		"candidate_sha": testCandidateSHA,
		"database_url":  "[REDACTED]",
	})
	outputPath := filepath.Join(directory, "acceptance-report.json")

	err := generate(manifestPath, outputPath, false)

	if err == nil {
		t.Fatal("generate succeeded, want secret scan failure")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("report was written after secret scan failure: %v", statErr)
	}
}

func TestScanSecretsAllowsMetadataAndRejectsRawOutput(t *testing.T) {
	tests := []struct {
		name    string
		value   map[string]any
		wantErr bool
	}{
		{
			name: "permission and redaction metadata",
			value: map[string]any{
				"agent_token_file_mode": "0600",
				"password_redacted":     "[REDACTED]",
			},
		},
		{name: "raw stdout", value: map[string]any{"stdout": "command output"}, wantErr: true},
		{name: "credential field", value: map[string]any{"password_ciphertext": "opaque"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			err = scanSecrets(contents)
			if (err != nil) != test.wantErr {
				t.Fatalf("scan error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func writeFinalManifest(t *testing.T, directory string) string {
	t.Helper()
	gate := map[string]any{"candidate_sha": testCandidateSHA, "result": "pass"}
	matrixEntries := []map[string]any{
		{"id": "AC-03-F2", "baseline": true, "status": "n-a"},
		{"id": "AC-04-F2", "baseline": true, "status": "n-a"},
		{"id": "AC-05-S4", "baseline": false, "status": "n-a"},
		{"id": "AC-08-F2", "baseline": true, "status": "n-a"},
		{"id": "AC-09-F2", "baseline": true, "status": "n-a"},
		{"id": "AC-03-F6", "baseline": false, "status": "pending"},
		{"id": "AC-08-S8", "baseline": false, "status": "pending"},
	}
	for index := len(matrixEntries); index < 104; index++ {
		matrixEntries = append(matrixEntries, map[string]any{
			"id": "PASS-" + strconv.Itoa(index), "baseline": true, "status": "passed",
		})
	}
	matrix := map[string]any{
		"candidate_sha": testCandidateSHA,
		"entries":       matrixEntries,
		"exceptions":    []any{},
	}
	pgRange := map[string]any{
		"candidate_sha": testCandidateSHA,
		"entries":       []map[string]any{{"id": "pg13/stat_database", "status": "pass"}},
	}
	rtC := map[string]any{"candidate_sha": testCandidateSHA, "query": map[string]any{"samples": 100, "p95_ms": 42}}
	manual := map[string]any{
		"operator": "release-operator", "at": "2026-08-15T10:30:00Z", "result": "pass",
		"screenshot_index": []string{"screenshots/ac-05-s4.png"}, "residual_risks": []string{},
	}

	paths := map[string]string{}
	for name, value := range map[string]any{
		"package": gate, "check_full": gate, "matrix": matrix, "pg_range": pgRange,
		"rt_c": rtC, "linux_native": gate, "manual": manual,
	} {
		path := filepath.Join(directory, name+".json")
		writeJSON(t, path, value)
		paths[name] = filepath.Base(path)
	}
	round := func(sequence int, startedAt, finishedAt string) map[string]any {
		return map[string]any{
			"sequence": sequence, "candidate_sha": testCandidateSHA, "valid": true,
			"started_at": startedAt, "finished_at": finishedAt,
			"environment": map[string]any{"os": "Kylin V10", "arch": "amd64"},
			"evidence": map[string]string{
				"package": paths["package"], "check_full": paths["check_full"], "matrix": paths["matrix"],
				"pg_range": paths["pg_range"], "rt_c": paths["rt_c"], "linux_native": paths["linux_native"],
			},
			"manual_review": paths["manual"],
		}
	}
	manifest := map[string]any{
		"candidate_sha": testCandidateSHA, "version": "1.0.0", "generated_at": "2026-08-15T12:00:00Z",
		"environment": map[string]any{"os": "Kylin V10", "arch": "amd64", "platform_pg": 17, "target_pg": []int{13, 14, 15, 16, 17}},
		"profile":     "final",
		"rounds": []any{
			round(1, "2026-08-15T08:00:00Z", "2026-08-15T09:00:00Z"),
			round(2, "2026-08-15T10:00:00Z", "2026-08-15T11:00:00Z"),
		},
	}
	path := filepath.Join(directory, "manifest.json")
	writeJSON(t, path, manifest)
	return path
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readReport(t *testing.T, path string) acceptancereport.Report {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report acceptancereport.Report
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return report
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
