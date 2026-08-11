package evaluator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

const triggerSnapshotTimeout = 10 * time.Second

const triggerSnapshotSessionsSQL = `SELECT
    pid AS pid,
    usename::text AS username,
    datname::text AS database_name,
    client_addr::text AS client_address,
    state::text AS state,
    query_start AS query_started_at,
    xact_start AS transaction_started_at,
    CASE WHEN query_start IS NULL THEN NULL
         ELSE GREATEST(0, floor(EXTRACT(epoch FROM (clock_timestamp() - query_start)) * 1000))::bigint END AS query_duration_ms,
    CASE WHEN xact_start IS NULL THEN NULL
         ELSE GREATEST(0, floor(EXTRACT(epoch FROM (clock_timestamp() - xact_start)) * 1000))::bigint END AS transaction_duration_ms,
    wait_event_type::text AS wait_event_type,
    wait_event::text AS wait_event,
    pg_blocking_pids(pid)::integer[] AS blocking_pids
FROM pg_stat_activity
WHERE pid <> pg_backend_pid()
  AND backend_type = 'client backend'
  AND application_name IS DISTINCT FROM 'dbs-monitor'
ORDER BY pid`

type triggerSnapshotCapture struct {
	result             string
	failureReason      pgtype.Text
	originalMatchCount int32
	truncated          bool
	sessions           []alerting.TriggerSession
}

func (service *Service) captureTriggerSnapshot(ctx context.Context, target alerting.GetEvaluationTargetRow) triggerSnapshotCapture {
	failed := func(err error) triggerSnapshotCapture {
		return triggerSnapshotCapture{result: "FAILED", failureReason: pgtype.Text{String: err.Error(), Valid: true}}
	}
	captureCtx, cancel := context.WithTimeout(ctx, triggerSnapshotTimeout)
	defer cancel()
	sessions := make([]alerting.TriggerSession, 0)
	err := service.withSnapshotConnection(captureCtx, target, func(conn *monitorpg.TargetConn) error {
		var configured string
		if err := conn.QueryRow(captureCtx, "SELECT set_config('statement_timeout', '10s', false)").Scan(&configured); err != nil {
			return fmt.Errorf("set trigger snapshot timeout: %w", err)
		}
		rows, err := conn.Query(captureCtx, triggerSnapshotSessionsSQL)
		if err != nil {
			return fmt.Errorf("read monitored sessions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var session alerting.TriggerSession
			if err := rows.Scan(
				&session.PID, &session.Username, &session.DatabaseName, &session.ClientAddress,
				&session.State, &session.QueryStartedAt, &session.TransactionStartedAt,
				&session.QueryDurationMS, &session.TransactionDurationMS,
				&session.WaitEventType, &session.WaitEvent, &session.BlockingPIDs,
			); err != nil {
				return fmt.Errorf("scan monitored session: %w", err)
			}
			sessions = append(sessions, session)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read monitored sessions: %w", err)
		}
		return nil
	})
	if err != nil {
		return failed(fmt.Errorf("capture monitored sessions: %w", err))
	}

	scope, _ := alerting.TriggerSnapshotScopeForMetric(target.MetricID)
	sessions = markDirectTriggerSessions(scope, target.MetricID, target.Operator, target.Threshold, sessions)
	selected, originalMatchCount, truncated := alerting.SelectTriggerSessions(scope, sessions, 100)
	return triggerSnapshotCapture{
		result: "SUCCESS", sessions: selected,
		originalMatchCount: int32(originalMatchCount), truncated: truncated,
	}
}

func markDirectTriggerSessions(scope alerting.TriggerSnapshotScope, metricID, operator string, threshold float64, sessions []alerting.TriggerSession) []alerting.TriggerSession {
	if scope == alerting.TriggerSnapshotActiveSessions {
		active := make([]alerting.TriggerSession, 0, len(sessions))
		for _, session := range sessions {
			if session.State.Valid && session.State.String == "active" {
				session.DirectMatch = true
				active = append(active, session)
			}
		}
		sort.SliceStable(active, func(i, j int) bool {
			left, right := active[i].QueryDurationMS.Int64, active[j].QueryDurationMS.Int64
			if left != right {
				return left > right
			}
			return active[i].PID < active[j].PID
		})
		return active
	}
	for index := range sessions {
		session := &sessions[index]
		switch scope {
		case alerting.TriggerSnapshotLockBlocking:
			session.DirectMatch = (session.WaitEventType.Valid && session.WaitEventType.String == "Lock") || len(session.BlockingPIDs) > 0
		case alerting.TriggerSnapshotIdleInTransaction:
			session.DirectMatch = session.State.Valid && strings.HasPrefix(session.State.String, "idle in transaction")
		case alerting.TriggerSnapshotLongTransactions:
			if metricID == "pg.transaction.max_duration_sec" {
				session.DirectMatch = session.TransactionDurationMS.Valid &&
					alerting.Compare(float64(session.TransactionDurationMS.Int64)/1000, operator, threshold)
			} else {
				session.DirectMatch = session.TransactionDurationMS.Valid &&
					session.TransactionDurationMS.Int64 > int64(5*time.Minute/time.Millisecond)
			}
		}
	}
	return sessions
}
