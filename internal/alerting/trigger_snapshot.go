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

var triggerSnapshotScopes = map[string]TriggerSnapshotScope{
	"pg.connection.active":              TriggerSnapshotActiveSessions,
	"pg.connection.idle_in_transaction": TriggerSnapshotIdleInTransaction,
	"pg.transaction.long_count":         TriggerSnapshotLongTransactions,
	"pg.transaction.max_duration_sec":   TriggerSnapshotLongTransactions,
	"pg.lock.waiting_count":             TriggerSnapshotLockBlocking,
	"pg.session.blocked_count":          TriggerSnapshotLockBlocking,
}

func TriggerSnapshotScopeForMetric(metricID string) (TriggerSnapshotScope, bool) {
	scope, ok := triggerSnapshotScopes[metricID]
	return scope, ok
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
		if limit > 50 {
			limit = 50
		}
		selected := sessions
		if len(selected) > limit {
			selected = selected[:limit]
		}
		return selected, len(sessions), len(sessions) > len(selected)
	}

	byPID := make(map[int32]TriggerSession, len(sessions))
	waitersByBlocker := make(map[int32][]int32)
	for _, session := range sessions {
		byPID[session.PID] = session
		for _, blockerPID := range session.BlockingPIDs {
			waitersByBlocker[blockerPID] = append(waitersByBlocker[blockerPID], session.PID)
		}
	}

	type group struct {
		root int32
		pids []int32
	}
	groups := make([]group, 0)
	relevant := make(map[int32]bool)
	for _, session := range sessions {
		if !session.DirectMatch {
			continue
		}
		visited := map[int32]bool{}
		queue := []int32{session.PID}
		for len(queue) > 0 {
			pid := queue[0]
			queue = queue[1:]
			if visited[pid] {
				continue
			}
			current, exists := byPID[pid]
			if !exists {
				continue
			}
			visited[pid] = true
			relevant[pid] = true
			queue = append(queue, current.BlockingPIDs...)
			if scope != TriggerSnapshotLockBlocking {
				queue = append(queue, waitersByBlocker[pid]...)
			}
		}
		pids := make([]int32, 0, len(visited))
		for pid := range visited {
			pids = append(pids, pid)
		}
		sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
		groups = append(groups, group{root: session.PID, pids: pids})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].pids) != len(groups[j].pids) {
			return len(groups[i].pids) > len(groups[j].pids)
		}
		return groups[i].root < groups[j].root
	})

	selectedPIDs := make(map[int32]bool, limit)
	for _, candidate := range groups {
		additional := 0
		for _, pid := range candidate.pids {
			if !selectedPIDs[pid] {
				additional++
			}
		}
		if len(selectedPIDs)+additional > limit {
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
	return selected, len(relevant), len(selected) < len(relevant)
}
