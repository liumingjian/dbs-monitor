//go:build acceptance

package acceptance

import (
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
