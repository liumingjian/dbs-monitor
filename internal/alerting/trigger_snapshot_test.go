package alerting

import "testing"

func TestTriggerSnapshotScopeForMetric(t *testing.T) {
	tests := []struct {
		name       string
		metricID   string
		wantScope  TriggerSnapshotScope
		applicable bool
	}{
		{name: "active sessions", metricID: "pg.connection.active", wantScope: TriggerSnapshotActiveSessions, applicable: true},
		{name: "idle in transaction", metricID: "pg.connection.idle_in_transaction", wantScope: TriggerSnapshotIdleInTransaction, applicable: true},
		{name: "long transaction count", metricID: "pg.transaction.long_count", wantScope: TriggerSnapshotLongTransactions, applicable: true},
		{name: "maximum transaction duration", metricID: "pg.transaction.max_duration_sec", wantScope: TriggerSnapshotLongTransactions, applicable: true},
		{name: "lock waits", metricID: "pg.lock.waiting_count", wantScope: TriggerSnapshotLockBlocking, applicable: true},
		{name: "blocked sessions", metricID: "pg.session.blocked_count", wantScope: TriggerSnapshotLockBlocking, applicable: true},
		{name: "replication is frozen out", metricID: "pg.replication.wal_lag_bytes", applicable: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := TriggerSnapshotScopeForMetric(test.metricID)
			if ok != test.applicable || got != test.wantScope {
				t.Fatalf("TriggerSnapshotScopeForMetric(%q) = %q, %t; want %q, %t", test.metricID, got, ok, test.wantScope, test.applicable)
			}
		})
	}
}

func TestSelectTriggerSessionsKeepsCompleteBlockingChain(t *testing.T) {
	sessions := make([]TriggerSession, 0, 102)
	for pid := int32(1); pid <= 99; pid++ {
		sessions = append(sessions, TriggerSession{PID: pid, DirectMatch: true})
	}
	sessions = append(sessions,
		TriggerSession{PID: 100, DirectMatch: true, BlockingPIDs: []int32{101}},
		TriggerSession{PID: 101, BlockingPIDs: []int32{102}},
		TriggerSession{PID: 102},
	)

	selected, originalMatchCount, truncated := SelectTriggerSessions(TriggerSnapshotLockBlocking, sessions, 100)
	if len(selected) != 100 || originalMatchCount != 102 || !truncated {
		t.Fatalf("selection = %d rows, original %d, truncated %t; want 100, 102, true", len(selected), originalMatchCount, truncated)
	}
	selectedPIDs := make(map[int32]bool, len(selected))
	for _, session := range selected {
		selectedPIDs[session.PID] = true
	}
	for _, pid := range []int32{100, 101, 102} {
		if !selectedPIDs[pid] {
			t.Fatalf("complete retained blocking chain is missing PID %d", pid)
		}
	}
}
