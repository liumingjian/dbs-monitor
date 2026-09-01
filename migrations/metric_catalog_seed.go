package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// reconcileMetricCatalog 把 internal/metric 的指标字典同步进 metric_catalog / metric_semantic_slot。
//
// 目录是数据不是 DDL：metric_series.metric_id 的外键指着这张表，所以每次迁移之后都要把表刷成
// 字典当前的样子——加指标只改字典，不再动迁移文件。
func reconcileMetricCatalog(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metric catalog transaction: %w", err)
	}
	defer tx.Rollback()

	declaredSlots := make(map[string]bool, len(metric.SemanticSlots))
	for _, slot := range metric.SemanticSlots {
		declaredSlots[slot.ID.String()] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO metric_semantic_slot (slot_id, display_name)
			VALUES ($1, $2)
			ON CONFLICT (slot_id) DO UPDATE SET display_name = excluded.display_name`,
			slot.ID.String(), slot.DisplayName,
		); err != nil {
			return fmt.Errorf("seed semantic slot %q: %w", slot.ID, err)
		}
	}

	declaredMetrics := make(map[string]bool, len(metric.Metrics))
	for _, item := range metric.Metrics {
		declaredMetrics[item.ID.String()] = true
		var slot any
		if item.Slot != "" {
			slot = item.Slot.String()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO metric_catalog (
				metric_id, engine, unit, display_name, semantic_slot, level, aggregation
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (metric_id) DO UPDATE SET
				engine = excluded.engine,
				unit = excluded.unit,
				display_name = excluded.display_name,
				semantic_slot = excluded.semantic_slot,
				level = excluded.level,
				aggregation = excluded.aggregation`,
			item.ID.String(), string(item.Engine), item.Unit, item.DisplayName,
			slot, string(item.Level), string(item.Aggregation),
		); err != nil {
			return fmt.Errorf("seed metric catalog entry %q: %w", item.ID, err)
		}
	}

	// 字典里删掉的指标要从目录里消失，但**只删没有序列引用它的行**：还有历史数据的指标留在目录里，
	// 让外键继续成立，比让一次迁移炸在启动路径上强。语义位同理。
	if err := pruneStaleRows(ctx, tx, declaredMetrics,
		`SELECT catalog.metric_id FROM metric_catalog catalog
			WHERE NOT EXISTS (SELECT 1 FROM metric_series series WHERE series.metric_id = catalog.metric_id)`,
		"DELETE FROM metric_catalog WHERE metric_id = $1",
	); err != nil {
		return fmt.Errorf("prune metric catalog: %w", err)
	}
	if err := pruneStaleRows(ctx, tx, declaredSlots,
		`SELECT slot.slot_id FROM metric_semantic_slot slot
			WHERE NOT EXISTS (SELECT 1 FROM metric_catalog catalog WHERE catalog.semantic_slot = slot.slot_id)`,
		"DELETE FROM metric_semantic_slot WHERE slot_id = $1",
	); err != nil {
		return fmt.Errorf("prune semantic slots: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metric catalog: %w", err)
	}
	return nil
}

func pruneStaleRows(ctx context.Context, tx *sql.Tx, declared map[string]bool, selectSQL, deleteSQL string) error {
	rows, err := tx.QueryContext(ctx, selectSQL)
	if err != nil {
		return err
	}
	stale := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !declared[id] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := tx.ExecContext(ctx, deleteSQL, id); err != nil {
			return err
		}
	}
	return nil
}
