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

// queryStatisticsEntry 是 pg_stat_statements 的一行。
//
// **NormalizedText 是这个包里唯一一个装 SQL 文本的字段**，而它只可能来自
// pg_stat_statements：那里的字面量已经被扩展换成 $1 占位符。带真实字面量的
// pg_stat_activity.query 既不采（见 metric.TaskStatActivity 的 SQL）也不存
// （见 stat_activity.go 的 statActivitySession 与 long_query_sample 的列）。
type queryStatisticsEntry struct {
	QueryID         int64
	DatabaseOID     uint32
	UserOID         uint32
	Calls           int64
	TotalExecTimeMS float64
	NormalizedText  string
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
			&entry.NormalizedText,
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
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"query_statistics_snapshot_entry"}, []string{
		"instance_id", "sampled_at", "queryid", "database_oid", "user_oid", "calls", "total_exec_time_ms",
	}, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	return persistNormalizedStatementText(ctx, tx, instanceID, sampledAt, snapshot)
}

// persistNormalizedStatementText 把这一轮采到的归一化文本按 (实例, queryid) 去重存一份。
//
// 快照表只存指标，文本单独一张表：每五分钟一次快照都存全文，500 台 × 每台 500 条
// 会迅速失控。同一个 queryid 可能在多个库/用户下各有一行，文本是同一条，去重后只写一次。
func persistNormalizedStatementText(
	ctx context.Context,
	tx pgx.Tx,
	instanceID pgtype.UUID,
	sampledAt time.Time,
	snapshot queryStatisticsSnapshot,
) error {
	texts := make(map[int64]string, len(snapshot.entries))
	for _, entry := range snapshot.entries {
		// 空文本不写：pg_stat_statements 的 query 可能因为外部文件被回收或权限不足而取不到，
		// 那是「这次没采到」，不该把上一次采到的真文本覆盖成空。
		if entry.NormalizedText == "" {
			continue
		}
		texts[entry.QueryID] = entry.NormalizedText
	}
	return PersistNormalizedStatementTexts(ctx, tx, instanceID, sampledAt, texts)
}

// PersistNormalizedStatementTexts 是**归一化 SQL 文本落库的唯一入口**。
//
// 写法是 upsert 而不是 insert：主键 (instance_id, queryid) 加 ON CONFLICT 让
// 「重复采集不产生重复行」由结构保证，同时文本按最新的更新（同一个 queryid 的归一化文本
// 理论上不变，但 PostgreSQL 大版本升级会改归一化规则）。调用方把它放进采集事务里，
// 一次采集要么指标与文本都有，要么都没有，不会留下「有指标没文本」的半截结果。
//
// 导出是为了让 HTTP 层的集成测试走**同一条写入路径**去验证去重的可观察行为，
// 而不是在测试里再抄一遍这段 SQL —— 抄一遍就等于两处各自去重，测的不再是生产代码。
//
// texts 的取值只允许是**归一化**文本（pg_stat_statements 的 query，字面量已是占位符）。
// pg_stat_activity 的原文不采也不存，见 internal/metric 的 statement_text_test.go。
func PersistNormalizedStatementTexts(ctx context.Context, database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, instanceID pgtype.UUID, sampledAt time.Time, texts map[int64]string) error {
	if len(texts) == 0 {
		return nil
	}
	queryIDs := make([]int64, 0, len(texts))
	values := make([]string, 0, len(texts))
	for queryID, text := range texts {
		queryIDs = append(queryIDs, queryID)
		values = append(values, text)
	}
	_, err := database.Exec(ctx, `INSERT INTO query_statement_text (instance_id, queryid, query_text, updated_at)
		SELECT $1, queryid, query_text, $4
		FROM unnest($2::bigint[], $3::text[]) AS batch(queryid, query_text)
		ON CONFLICT (instance_id, queryid) DO UPDATE SET
			query_text = EXCLUDED.query_text,
			updated_at = EXCLUDED.updated_at`, instanceID, queryIDs, values, sampledAt)
	return err
}

func DropExpiredQueryStatisticsSnapshots(ctx context.Context, database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, now time.Time) error {
	_, err := database.Exec(ctx, "DELETE FROM query_statistics_snapshot WHERE sampled_at < $1", now.UTC().Add(-queryStatisticsSnapshotRetention))
	return err
}
