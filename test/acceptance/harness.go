//go:build acceptance

package acceptance

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	onboardingEntryID          = "AC-08-S1"
	offlineKeyRotationEntryID  = "AC-08-S7"
	expiredCertificateEntryID  = "SEC-3"
	expiringCertificateEntryID = "SEC-4"
	sessionExpiryEntryID       = "SEC-5"
)

type resultStatus string

const (
	resultPassed  resultStatus = "passed"
	resultFailed  resultStatus = "failed"
	resultPending resultStatus = "pending"
	resultNA      resultStatus = "n-a"
)

type acceptanceMatrix struct {
	Version    int           `yaml:"version"`
	Exceptions []any         `yaml:"exceptions"`
	Entries    []matrixEntry `yaml:"entries"`
}

type matrixEntry struct {
	ID       string       `yaml:"id"`
	Baseline bool         `yaml:"baseline"`
	Status   resultStatus `yaml:"status"`
	TestRef  *string      `yaml:"test_ref"`
	Reason   string       `yaml:"reason"`
}

type acceptanceResult struct {
	MatrixVersion int           `json:"matrix_version"`
	CandidateSHA  string        `json:"candidate_sha"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Exceptions    []any         `json:"exceptions"`
	Entries       []entryResult `json:"entries"`
	Summary       resultSummary `json:"summary"`
}

type entryResult struct {
	ID           string       `json:"id"`
	Status       resultStatus `json:"status"`
	ActualResult string       `json:"actual_result"`
	DurationMS   int64        `json:"duration_ms"`
	Baseline     bool         `json:"baseline"`
	TestRef      *string      `json:"test_ref"`
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
	ordered := append(make([]matrixEntry, 0, len(entries)), entries...)
	slices.SortStableFunc(ordered, func(left, right matrixEntry) int {
		return cmp.Compare(executionRank(left.ID), executionRank(right.ID))
	})
	return ordered
}

func executionRank(id string) int {
	const (
		onboardingEntryRank = iota
		sliceEntryRank
		recoveryEntryRank
		securityEntryRank
		expiredCertificateEntryRank
		expiringCertificateEntryRank
		sessionExpiryEntryRank
		offlineKeyRotationEntryRank
	)

	switch {
	case id == onboardingEntryID:
		return onboardingEntryRank
	case id == offlineKeyRotationEntryID:
		return offlineKeyRotationEntryRank
	case strings.HasPrefix(id, "REC-"):
		return recoveryEntryRank
	case id == expiredCertificateEntryID:
		return expiredCertificateEntryRank
	case id == expiringCertificateEntryID:
		return expiringCertificateEntryRank
	case id == sessionExpiryEntryID:
		return sessionExpiryEntryRank
	case strings.HasPrefix(id, "SEC-"):
		return securityEntryRank
	default:
		return sliceEntryRank
	}
}

func newResult(matrix acceptanceMatrix, candidateSHA string) *acceptanceResult {
	result := &acceptanceResult{
		MatrixVersion: matrix.Version,
		CandidateSHA:  candidateSHA,
		GeneratedAt:   time.Now().UTC(),
		Exceptions:    append(make([]any, 0, len(matrix.Exceptions)), matrix.Exceptions...),
	}
	for _, entry := range executionOrder(matrix.Entries) {
		status := resultPending
		actualResult := "not implemented by this tracer ticket"
		if entry.Status == resultNA {
			status = resultNA
			actualResult = entry.Reason
		}
		result.Entries = append(result.Entries, entryResult{
			ID:           entry.ID,
			Status:       status,
			ActualResult: actualResult,
			Baseline:     entry.Baseline,
			TestRef:      entry.TestRef,
		})
	}
	result.rebuildSummary()
	return result
}

func (result *acceptanceResult) record(id string, status resultStatus, actualResult string, duration time.Duration) {
	for index := range result.Entries {
		if result.Entries[index].ID != id {
			continue
		}
		result.Entries[index].Status = status
		result.Entries[index].ActualResult = actualResult
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
		if entry.Baseline && entry.Status != resultPassed && entry.Status != resultNA {
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

func (result *acceptanceResult) exitCode(testCode int) int {
	if testCode != 0 || !result.Summary.BaselinePassed {
		return 1
	}
	return 0
}
