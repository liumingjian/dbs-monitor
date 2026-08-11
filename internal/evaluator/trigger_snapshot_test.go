package evaluator

import (
	"strings"
	"testing"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
)

func TestActiveTriggerSessionsAreDurationOrderedAndCapped(t *testing.T) {
	sessions := make([]alerting.TriggerSession, 60)
	for index := range sessions {
		sessions[index] = alerting.TriggerSession{
			PID:             int32(index + 1),
			State:           pgtype.Text{String: "active", Valid: true},
			QueryDurationMS: pgtype.Int8{Int64: int64(index), Valid: true},
		}
	}
	matched := markDirectTriggerSessions(alerting.TriggerSnapshotActiveSessions, "pg.connection.active", ">=", 0, sessions)
	selected, originalMatchCount, truncated := alerting.SelectTriggerSessions(alerting.TriggerSnapshotActiveSessions, matched, 100)
	if len(selected) != 50 || originalMatchCount != 60 || !truncated {
		t.Fatalf("active selection = %d rows, original %d, truncated %t; want 50, 60, true", len(selected), originalMatchCount, truncated)
	}
	if selected[0].PID != 60 || selected[49].PID != 11 {
		t.Fatalf("active duration order starts/ends with PIDs %d/%d, want 60/11", selected[0].PID, selected[49].PID)
	}
}

func TestLongTransactionTriggerSessionsFollowMetricThreshold(t *testing.T) {
	sessions := []alerting.TriggerSession{
		{PID: 1, TransactionDurationMS: pgtype.Int8{Int64: 300000, Valid: true}},
		{PID: 2, TransactionDurationMS: pgtype.Int8{Int64: 300001, Valid: true}},
	}
	matched := markDirectTriggerSessions(alerting.TriggerSnapshotLongTransactions, "pg.transaction.long_count", ">=", 1, sessions)
	if matched[0].DirectMatch || !matched[1].DirectMatch {
		t.Fatalf("long-count matches = %t/%t, want false/true", matched[0].DirectMatch, matched[1].DirectMatch)
	}
	matched = markDirectTriggerSessions(alerting.TriggerSnapshotLongTransactions, "pg.transaction.max_duration_sec", ">", 300, sessions)
	if matched[0].DirectMatch || !matched[1].DirectMatch {
		t.Fatalf("max-duration matches = %t/%t, want false/true", matched[0].DirectMatch, matched[1].DirectMatch)
	}
}

func TestTriggerSnapshotQueryNeverSelectsSQLText(t *testing.T) {
	for _, token := range strings.FieldsFunc(strings.ToLower(triggerSnapshotSessionsSQL), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '_'
	}) {
		if token == "query" || token == "sql" {
			t.Fatalf("trigger snapshot query contains forbidden standalone token %q", token)
		}
	}
}
