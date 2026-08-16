package collect

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

const (
	queryStatisticsSnapshotLimit     = 500
	queryStatisticsSnapshotRetention = 30 * 24 * time.Hour
)

type queryStatisticsEntry struct {
	QueryID         int64
	DatabaseOID     uint32
	UserOID         uint32
	Calls           int64
	TotalExecTimeMS float64
}

type queryStatisticsSnapshot struct {
	entries []queryStatisticsEntry
}

func collectQueryStatistics(ctx context.Context, conn *monitorpg.TargetConn, task metric.Task) (queryStatisticsSnapshot, error) {
	rows, err := conn.Query(ctx, task.SQL)
	if err != nil {
		return queryStatisticsSnapshot{}, err
	}
	defer rows.Close()

	entries := make([]queryStatisticsEntry, 0, queryStatisticsSnapshotLimit)
	for rows.Next() {
		var entry queryStatisticsEntry
		if err := rows.Scan(
			&entry.QueryID,
			&entry.DatabaseOID,
			&entry.UserOID,
			&entry.Calls,
			&entry.TotalExecTimeMS,
		); err != nil {
			return queryStatisticsSnapshot{}, err
		}
		entries = append(entries, entry)
		if len(entries) > queryStatisticsSnapshotLimit {
			return queryStatisticsSnapshot{}, fmt.Errorf("query statistics snapshot exceeds %d rows", queryStatisticsSnapshotLimit)
		}
	}
	if err := rows.Err(); err != nil {
		return queryStatisticsSnapshot{}, err
	}
	return queryStatisticsSnapshot{entries: entries}, nil
}

func persistQueryStatisticsSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	instanceID pgtype.UUID,
	sampledAt time.Time,
	snapshot queryStatisticsSnapshot,
) error {
	if _, err := tx.Exec(ctx, `INSERT INTO query_statistics_snapshot (instance_id, sampled_at)
		VALUES ($1, $2)`, instanceID, sampledAt); err != nil {
		return err
	}
	if len(snapshot.entries) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		rows = append(rows, []any{
			instanceID, sampledAt, entry.QueryID, entry.DatabaseOID, entry.UserOID,
			entry.Calls, entry.TotalExecTimeMS,
		})
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"query_statistics_snapshot_entry"}, []string{
		"instance_id", "sampled_at", "queryid", "database_oid", "user_oid", "calls", "total_exec_time_ms",
	}, pgx.CopyFromRows(rows))
	return err
}

func DropExpiredQueryStatisticsSnapshots(ctx context.Context, database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, now time.Time) error {
	_, err := database.Exec(ctx, "DELETE FROM query_statistics_snapshot WHERE sampled_at < $1", now.UTC().Add(-queryStatisticsSnapshotRetention))
	return err
}
