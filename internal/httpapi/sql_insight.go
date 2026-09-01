package httpapi

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

// SQL 洞察：跨实例的 Top SQL。
//
// DBA 的实际动线是「哪条 SQL 在拖垮我的机群」，而查询统计排行至今只按单台实例给，
// 还只显示 queryid——没有人认得出那是什么语句。这个端点把两件事一起补上：跨实例排序，
// 以及归一化后的 SQL 文本。
//
// **文本只有一种来源。** 榜上的 query_text 来自 query_statement_text，那张表只接
// pg_stat_statements 的归一化文本（字面量已是占位符）。pg_stat_activity 的原文带真实
// 字面量，既不采也不存，因此这个端点在结构上就不可能把它吐出来。
//
// 去重是主键 + upsert 的自然结果，不是这里的一段过滤：同一条 SQL 采多少次，
// (instance_id, queryid) 上都只有一行，文本按最新的更新。

// topSQLOverviewLimit 是总览页第五块的行数。五条，不是五十条：
// 总览要回答「现在最费资源的是哪几条」，完整排行在 SQL 洞察页上。
const topSQLOverviewLimit = 5

// ListTopSql 返回跨实例按总耗时排序的 Top SQL。
func (handler *Handler) ListTopSql(ctx context.Context, request api.ListTopSqlRequestObject) (api.ListTopSqlResponseObject, error) {
	limit := 100
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	items, err := handler.topSQL(ctx, limit)
	if err != nil {
		return nil, err
	}
	return api.ListTopSql200JSONResponse{Items: items}, nil
}

// topSQL 是 SQL 洞察页与总览第五块共用的那一份排行，只是取的条数不同。
// 两处共用一个查询，是为了让「总览上的前五条」与「洞察页的前五行」永远是同一份事实。
func (handler *Handler) topSQL(ctx context.Context, limit int) ([]api.TopSqlEntry, error) {
	rows, err := New(handler.platform).ListFleetTopSql(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	items := make([]api.TopSqlEntry, 0, len(rows))
	for _, row := range rows {
		entry := api.TopSqlEntry{
			InstanceId:      uuid.UUID(row.InstanceID.Bytes),
			InstanceName:    row.InstanceName,
			Queryid:         strconv.FormatInt(row.Queryid, 10),
			Calls:           row.Calls,
			TotalExecTimeMs: row.TotalExecTimeMs,
		}
		// 文本缺失时字段整个不出现，而不是一个空串：「还没采到文本」与
		// 「文本是空的」是两件事，前端据此显示 queryid 兜底而不是一行空白。
		if row.QueryText.Valid {
			text := row.QueryText.String
			entry.QueryText = &text
		}
		items = append(items, entry)
	}
	return items, nil
}
