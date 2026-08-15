//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	resultPassed  = "passed"
	resultFailed  = "failed"
	resultPending = "pending"
	resultNA      = "n-a"
)

type acceptanceMatrix struct {
	Version    int           `yaml:"version"`
	Exceptions []any         `yaml:"exceptions"`
	Entries    []matrixEntry `yaml:"entries"`
}

type matrixEntry struct {
	ID       string `yaml:"id"`
	Baseline bool   `yaml:"baseline"`
	Status   string `yaml:"status"`
	Reason   string `yaml:"reason"`
}

type acceptanceResult struct {
	MatrixVersion int           `json:"matrix_version"`
	CandidateSHA  string        `json:"candidate_sha"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Entries       []entryResult `json:"entries"`
	Summary       resultSummary `json:"summary"`
}

type entryResult struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ActualResult string `json:"actual_result"`
	DurationMS   int64  `json:"duration_ms"`
	baseline     bool
}

type resultSummary struct {
	BaselinePassed bool     `json:"baseline_passed"`
	PendingCount   int      `json:"pending_count"`
	PendingIDs     []string `json:"pending_ids"`
}

func readMatrix(path string) (acceptanceMatrix, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return acceptanceMatrix{}, fmt.Errorf("read acceptance matrix: %w", err)
	}
	var matrix acceptanceMatrix
	if err := yaml.Unmarshal(contents, &matrix); err != nil {
		return acceptanceMatrix{}, fmt.Errorf("parse acceptance matrix: %w", err)
	}
	return matrix, nil
}

func loadMatrix(t *testing.T, path string) acceptanceMatrix {
	t.Helper()
	matrix, err := readMatrix(path)
	if err != nil {
		t.Fatalf("load acceptance matrix: %v", err)
	}
	return matrix
}

func executionOrder(entries []matrixEntry) []matrixEntry {
	ordered := make([]matrixEntry, 0, len(entries))
	appendMatching := func(match func(matrixEntry) bool) {
		for _, entry := range entries {
			if match(entry) {
				ordered = append(ordered, entry)
			}
		}
	}
	appendMatching(func(entry matrixEntry) bool { return entry.ID == "AC-08-S1" })
	appendMatching(func(entry matrixEntry) bool {
		return entry.ID != "AC-08-S1" && entry.ID != "AC-08-S7" &&
			!strings.HasPrefix(entry.ID, "REC-") && !strings.HasPrefix(entry.ID, "SEC-")
	})
	appendMatching(func(entry matrixEntry) bool { return strings.HasPrefix(entry.ID, "REC-") })
	appendMatching(func(entry matrixEntry) bool {
		return strings.HasPrefix(entry.ID, "SEC-") && entry.ID != "SEC-3" && entry.ID != "SEC-4" && entry.ID != "SEC-5"
	})
	for _, id := range []string{"SEC-3", "SEC-4", "SEC-5"} {
		appendMatching(func(entry matrixEntry) bool { return entry.ID == id })
	}
	appendMatching(func(entry matrixEntry) bool { return entry.ID == "AC-08-S7" })
	return ordered
}

func newResult(matrix acceptanceMatrix, candidateSHA string) *acceptanceResult {
	result := &acceptanceResult{
		MatrixVersion: matrix.Version,
		CandidateSHA:  candidateSHA,
		GeneratedAt:   time.Now().UTC(),
	}
	for _, entry := range executionOrder(matrix.Entries) {
		status := resultPending
		actual := "not implemented by this tracer ticket"
		if entry.Status == resultNA {
			status = resultNA
			actual = entry.Reason
		}
		result.Entries = append(result.Entries, entryResult{
			ID: entry.ID, Status: status, ActualResult: actual, baseline: entry.Baseline,
		})
	}
	result.rebuildSummary()
	return result
}

func (result *acceptanceResult) record(id, status, actual string, duration time.Duration) {
	for index := range result.Entries {
		if result.Entries[index].ID != id {
			continue
		}
		result.Entries[index].Status = status
		result.Entries[index].ActualResult = actual
		result.Entries[index].DurationMS = duration.Milliseconds()
		result.rebuildSummary()
		return
	}
}

func (result *acceptanceResult) rebuildSummary() {
	result.Summary = resultSummary{BaselinePassed: true}
	for _, entry := range result.Entries {
		if entry.Status == resultPending {
			result.Summary.PendingCount++
			result.Summary.PendingIDs = append(result.Summary.PendingIDs, entry.ID)
		}
		if entry.baseline && entry.Status != resultPassed && entry.Status != resultNA {
			result.Summary.BaselinePassed = false
		}
	}
}

func (result *acceptanceResult) write(path string) error {
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode acceptance result: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create acceptance result directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(contents, '\n'), 0o644); err != nil {
		return fmt.Errorf("write acceptance result: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish acceptance result: %w", err)
	}
	return nil
}
