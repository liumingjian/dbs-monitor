package httpapi

import (
	"context"

	"github.com/oapi-codegen/nullable"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// GetMetricCatalog 把指标目录交给前端：展示名、单位、引擎、级别、聚合方式、语义位归属。
//
// 前端不再自己维护一份展示名——跨引擎之后那份副本每加一个指标就要改一次，而且改漏了不会报错，
// 只会在界面上露出一个裸指标 ID。
func (handler *Handler) GetMetricCatalog(
	_ context.Context,
	_ api.GetMetricCatalogRequestObject,
) (api.GetMetricCatalogResponseObject, error) {
	metrics := make([]api.MetricCatalogEntry, 0, len(metric.Metrics))
	for _, item := range metric.Metrics {
		entry := api.MetricCatalogEntry{
			MetricId:    item.ID.String(),
			Engine:      api.MetricEngine(item.Engine),
			Unit:        item.Unit,
			DisplayName: item.DisplayName,
			Level:       api.MetricLevel(item.Level),
			Aggregation: api.MetricAggregation(item.Aggregation),
		}
		entry.SemanticSlot = nullable.NewNullNullable[api.SemanticSlot]()
		if item.Slot != "" {
			entry.SemanticSlot = nullable.NewNullableWithValue(api.SemanticSlot(item.Slot))
		}
		metrics = append(metrics, entry)
	}
	slots := make([]api.SemanticSlotDeclaration, 0, len(metric.SemanticSlots))
	for _, declaration := range metric.SemanticSlots {
		slots = append(slots, api.SemanticSlotDeclaration{
			SlotId:      api.SemanticSlot(declaration.ID),
			DisplayName: declaration.DisplayName,
		})
	}
	return api.GetMetricCatalog200JSONResponse{Metrics: metrics, SemanticSlots: slots}, nil
}
