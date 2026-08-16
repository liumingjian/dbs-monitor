//go:build acceptance

package acceptance

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestAcceptanceExecutionOrder(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	ordered := executionOrder(matrix.Entries)
	ids := entryIDs(ordered)

	if len(ids) != 104 {
		t.Fatalf("entry count = %d, want 104", len(ids))
	}
	if len(matrix.Exceptions) != 0 {
		t.Fatalf("exceptions = %v, want none", matrix.Exceptions)
	}
	if ids[0] != "AC-08-S1" {
		t.Fatalf("first entry = %q, want AC-08-S1", ids[0])
	}
	if ids[len(ids)-1] != "AC-08-S7" {
		t.Fatalf("last entry = %q, want AC-08-S7", ids[len(ids)-1])
	}

	firstREC := firstIndexWithPrefix(ids, "REC-")
	firstSEC := firstIndexWithPrefix(ids, "SEC-")
	if firstREC < 0 || firstSEC < 0 || firstREC >= firstSEC {
		t.Fatalf("cross-cutting order has REC at %d and SEC at %d", firstREC, firstSEC)
	}
	for index, id := range ids[1:firstREC] {
		if strings.HasPrefix(id, "REC-") || strings.HasPrefix(id, "SEC-") {
			t.Fatalf("entry %q at %d precedes the REC/SEC groups", id, index+1)
		}
	}

	securityIDs := ids[firstSEC : len(ids)-1]
	wantTail := []string{"SEC-3", "SEC-4", "SEC-5"}
	if !slices.Equal(securityIDs[len(securityIDs)-len(wantTail):], wantTail) {
		t.Fatalf("security group tail = %v, want %v", securityIDs[len(securityIDs)-len(wantTail):], wantTail)
	}
}

func TestRecoveryEntriesAreCoveredAndIndependent(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	recoveryEntries := make(map[string]matrixEntry)
	for _, entry := range matrix.Entries {
		if strings.HasPrefix(entry.ID, "REC-") {
			recoveryEntries[entry.ID] = entry
		}
	}

	if len(recoveryEntries) != 13 {
		t.Fatalf("REC entry count = %d, want 13", len(recoveryEntries))
	}
	for index := 1; index <= 13; index++ {
		id := fmt.Sprintf("REC-%d", index)
		entry, ok := recoveryEntries[id]
		if !ok {
			t.Errorf("%s is missing", id)
			continue
		}
		if entry.Status != "covered" {
			t.Errorf("%s status = %q, want covered", id, entry.Status)
		}
		if entry.TestRef == nil || !strings.Contains(*entry.TestRef, strings.ReplaceAll(id, "-", "_")) {
			t.Errorf("%s test_ref = %v, want an independent ID-bearing reference", id, entry.TestRef)
		}
		if len(entry.RidesOn) != 0 {
			t.Errorf("%s rides_on = %v, want []", id, entry.RidesOn)
		}
	}
}

func TestSecurityEntriesHaveIndependentReferences(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	seenReferences := map[string]string{}
	securityCount := 0
	for _, entry := range matrix.Entries {
		if !strings.HasPrefix(entry.ID, "SEC-") {
			continue
		}
		securityCount++
		if entry.Status != "covered" {
			t.Errorf("%s status = %q, want covered", entry.ID, entry.Status)
		}
		if entry.TestRef == nil || !strings.Contains(*entry.TestRef, entry.ID) {
			t.Errorf("%s test_ref = %v, want an independent reference containing the entry ID", entry.ID, entry.TestRef)
			continue
		}
		if previous, exists := seenReferences[*entry.TestRef]; exists {
			t.Errorf("%s and %s share test_ref %q", previous, entry.ID, *entry.TestRef)
		}
		seenReferences[*entry.TestRef] = entry.ID
	}
	if securityCount != 10 {
		t.Fatalf("security entry count = %d, want 10", securityCount)
	}
}

func TestAcceptanceResultRetainsEveryMatrixEntry(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	report := newResult(matrix, "0123456789012345678901234567890123456789")
	report.record("AC-08-S1", resultPassed, "tracer passed", 1)

	if len(report.Entries) != len(matrix.Entries) {
		t.Fatalf("reported entries = %d, want %d", len(report.Entries), len(matrix.Entries))
	}
	if report.Entries[0].ID != "AC-08-S1" || report.Entries[0].Status != resultPassed {
		t.Fatalf("first result = %+v, want passed AC-08-S1", report.Entries[0])
	}
	if report.Entries[len(report.Entries)-1].ID != "AC-08-S7" {
		t.Fatalf("last result = %+v, want AC-08-S7", report.Entries[len(report.Entries)-1])
	}
	if report.Summary.PendingCount != 98 {
		t.Fatalf("pending count = %d, want 98", report.Summary.PendingCount)
	}
}

func TestAcceptanceResultFailsWhileBaselineEntriesArePending(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	report := newResult(matrix, "0123456789012345678901234567890123456789")

	if report.Summary.BaselinePassed {
		t.Fatal("pending baseline entries produced a passing acceptance result")
	}
	if report.Summary.PendingCount == 0 {
		t.Fatal("pending baseline entries were not included in the acceptance summary")
	}
}

func TestAcceptanceResultExitCode(t *testing.T) {
	for _, test := range []struct {
		name           string
		testExitCode   int
		baselinePassed bool
		want           int
	}{
		{name: "tests and baseline pass", baselinePassed: true, want: 0},
		{name: "tests fail", testExitCode: 1, baselinePassed: true, want: 1},
		{name: "test failure is normalized", testExitCode: 2, baselinePassed: true, want: 1},
		{name: "baseline fails", baselinePassed: false, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := acceptanceResult{
				Summary: resultSummary{BaselinePassed: test.baselinePassed},
			}
			if got := result.exitCode(test.testExitCode); got != test.want {
				t.Errorf("exit code = %d, want %d", got, test.want)
			}
		})
	}
}

func TestAcceptanceResultSerializesVerdictInputs(t *testing.T) {
	matrix := loadMatrix(t, "matrix.yaml")
	report := newResult(matrix, "0123456789012345678901234567890123456789")
	contents, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("encode acceptance result: %v", err)
	}
	var encoded struct {
		Exceptions *json.RawMessage `json:"exceptions"`
		Entries    []struct {
			ID       string  `json:"id"`
			Baseline *bool   `json:"baseline"`
			TestRef  *string `json:"test_ref"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(contents, &encoded); err != nil {
		t.Fatalf("decode acceptance result: %v", err)
	}
	if encoded.Exceptions == nil || string(*encoded.Exceptions) != "[]" {
		t.Fatalf("exceptions = %s, want an explicit empty array", contents)
	}
	if len(encoded.Entries) == 0 || encoded.Entries[0].Baseline == nil || !*encoded.Entries[0].Baseline {
		t.Fatalf("first entry baseline was not serialized: %s", contents)
	}
	for _, entry := range encoded.Entries {
		if entry.ID == "AC-09-F1" {
			if entry.TestRef == nil || *entry.TestRef == "" {
				t.Fatalf("AC-09-F1 test_ref was not serialized: %s", contents)
			}
			return
		}
	}
	t.Fatal("AC-09-F1 was not serialized")
}

func entryIDs(entries []matrixEntry) []string {
	ids := make([]string, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ID
	}
	return ids
}

func firstIndexWithPrefix(ids []string, prefix string) int {
	for index, id := range ids {
		if strings.HasPrefix(id, prefix) {
			return index
		}
	}
	return -1
}
