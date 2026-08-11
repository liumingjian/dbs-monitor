package alerting

import (
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
)

type TriggerSnapshotScope string

const (
	TriggerSnapshotLockBlocking      TriggerSnapshotScope = "LOCK_BLOCKING"
	TriggerSnapshotLongTransactions  TriggerSnapshotScope = "LONG_TRANSACTIONS"
	TriggerSnapshotIdleInTransaction TriggerSnapshotScope = "IDLE_IN_TRANSACTION"
	TriggerSnapshotActiveSessions    TriggerSnapshotScope = "ACTIVE_SESSIONS"
)

const maxActiveTriggerSnapshotSessions = 50

func TriggerSnapshotScopeForMetric(metricID string) (TriggerSnapshotScope, bool) {
	switch metricID {
	case "pg.connection.active":
		return TriggerSnapshotActiveSessions, true
	case "pg.connection.idle_in_transaction":
		return TriggerSnapshotIdleInTransaction, true
	case "pg.transaction.long_count", "pg.transaction.max_duration_sec":
		return TriggerSnapshotLongTransactions, true
	case "pg.lock.waiting_count", "pg.session.blocked_count":
		return TriggerSnapshotLockBlocking, true
	default:
		return "", false
	}
}

type TriggerSession struct {
	PID                   int32
	Username              pgtype.Text
	DatabaseName          pgtype.Text
	ClientAddress         pgtype.Text
	State                 pgtype.Text
	QueryStartedAt        pgtype.Timestamptz
	TransactionStartedAt  pgtype.Timestamptz
	QueryDurationMS       pgtype.Int8
	TransactionDurationMS pgtype.Int8
	WaitEventType         pgtype.Text
	WaitEvent             pgtype.Text
	BlockingPIDs          []int32
	DirectMatch           bool
}

func SelectTriggerSessions(scope TriggerSnapshotScope, sessions []TriggerSession, limit int) ([]TriggerSession, int, bool) {
	if limit <= 0 {
		return nil, len(sessions), len(sessions) > 0
	}
	if scope == TriggerSnapshotActiveSessions {
		if limit > maxActiveTriggerSnapshotSessions {
			limit = maxActiveTriggerSnapshotSessions
		}
		selected := sessions
		if len(selected) > limit {
			selected = selected[:limit]
		}
		return selected, len(sessions), len(sessions) > len(selected)
	}

	sessionsByPID := make(map[int32]TriggerSession, len(sessions))
	waiterPIDsByBlocker := make(map[int32][]int32)
	for _, session := range sessions {
		sessionsByPID[session.PID] = session
		for _, blockerPID := range session.BlockingPIDs {
			waiterPIDsByBlocker[blockerPID] = append(waiterPIDsByBlocker[blockerPID], session.PID)
		}
	}

	type sessionGroup struct {
		rootPID int32
		pids    []int32
	}
	groups := make([]sessionGroup, 0)
	relevantPIDs := make(map[int32]bool)
	for _, session := range sessions {
		if !session.DirectMatch {
			continue
		}
		groupPIDs := relatedTriggerSessionPIDs(scope, session.PID, sessionsByPID, waiterPIDsByBlocker)
		for _, pid := range groupPIDs {
			relevantPIDs[pid] = true
		}
		groups = append(groups, sessionGroup{rootPID: session.PID, pids: groupPIDs})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].pids) != len(groups[j].pids) {
			return len(groups[i].pids) > len(groups[j].pids)
		}
		return groups[i].rootPID < groups[j].rootPID
	})

	selectedPIDs := make(map[int32]bool, limit)
	for _, candidate := range groups {
		additionalSessionCount := 0
		for _, pid := range candidate.pids {
			if !selectedPIDs[pid] {
				additionalSessionCount++
			}
		}
		if len(selectedPIDs)+additionalSessionCount > limit {
			continue
		}
		for _, pid := range candidate.pids {
			selectedPIDs[pid] = true
		}
	}

	selected := make([]TriggerSession, 0, len(selectedPIDs))
	for _, session := range sessions {
		if selectedPIDs[session.PID] {
			selected = append(selected, session)
		}
	}
	return selected, len(relevantPIDs), len(selected) < len(relevantPIDs)
}

func relatedTriggerSessionPIDs(
	scope TriggerSnapshotScope,
	rootPID int32,
	sessionsByPID map[int32]TriggerSession,
	waiterPIDsByBlocker map[int32][]int32,
) []int32 {
	relatedPIDs := make(map[int32]bool)
	pendingPIDs := []int32{rootPID}
	for len(pendingPIDs) > 0 {
		pid := pendingPIDs[0]
		pendingPIDs = pendingPIDs[1:]
		if relatedPIDs[pid] {
			continue
		}
		session, exists := sessionsByPID[pid]
		if !exists {
			continue
		}
		relatedPIDs[pid] = true
		pendingPIDs = append(pendingPIDs, session.BlockingPIDs...)
		if scope != TriggerSnapshotLockBlocking {
			pendingPIDs = append(pendingPIDs, waiterPIDsByBlocker[pid]...)
		}
	}

	pids := make([]int32, 0, len(relatedPIDs))
	for pid := range relatedPIDs {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids
}
