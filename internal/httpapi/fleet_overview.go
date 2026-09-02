package httpapi

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 机群总览的聚合。
//
// 五块（健康计数 / 采集自监控 / 需要关注的 Top 10 / 容量水位 Top 10 / Top SQL 前 5）在一个
// 请求里一起算完：分成五个端点会让首屏发五个往返，还会让各块看到不同时刻的机群——
// 「严重 3 台」与下面列出的那几台对不上，是比慢更糟的毛病。
// 前三块读的是同一份实例级投影，后两块各读一次时序/快照，但仍然同一个事务窗口内取完。
//
// 计数与 Top 10 都是纯函数（summarizeFleet / instancesNeedingAttention），因为「哪台该
// 排在前面」算错了不会报错，只会安静地把该看的那台压到第十一位。

// attentionLimit 与 storageLimit 都是十。五百个格子没有信息量：总览要回答的是
// 「我现在该看谁」，剩下的由实例列表按筛选回答。
const (
	attentionLimit = 10
	storageLimit   = 10
)

// storageUsageWindow 是「最近一次读数」的有效期。超过这个窗口没有新样本，这台的磁盘
// 使用率就是**未知**，不是最后那个值：一个月前的 91% 拿来当今天的读数在骗人。
//
// 这里刻意不叫 watermark：CONTEXT.md 把 watermark 留给了采集完整性水位（一个源的每个
// 到期义务都满足到的那个时刻），那是另一件事，两处同名会让术语表失真。
const storageUsageWindow = 6 * time.Hour

// GetFleetOverview 返回总览页四块的聚合结果。
func (handler *Handler) GetFleetOverview(ctx context.Context, _ api.GetFleetOverviewRequestObject) (api.GetFleetOverviewResponseObject, error) {
	instances, err := handler.listInstancesWithHealth(ctx)
	if err != nil {
		return nil, err
	}
	overview := summarizeFleet(instances)
	storage, err := handler.storageUsage(ctx, instances)
	if err != nil {
		return nil, err
	}
	overview.Storage = storage
	topSQL, err := handler.topSQL(ctx, topSQLOverviewLimit)
	if err != nil {
		return nil, err
	}
	overview.TopSql = topSQL
	return api.GetFleetOverview200JSONResponse(overview), nil
}

// summarizeFleet 把实例级投影收敛成前三块。第四、五块要读时序与查询统计快照，不在这里。
func summarizeFleet(instances []api.Instance) api.FleetOverview {
	overview := api.FleetOverview{
		Total:      len(instances),
		Attention:  instancesNeedingAttention(instances),
		Storage:    []api.StorageUsageEntry{},
		TopSql:     []api.TopSqlEntry{},
		Health:     api.FleetHealthCounts{},
		Collection: api.FleetCollectionHealth{},
	}
	for _, item := range instances {
		switch item.Health.Status {
		case api.HealthCritical:
			overview.Health.Critical++
		case api.HealthWarning:
			overview.Health.Warning++
		case api.HealthUnknown:
			overview.Health.Unknown++
		case api.HealthHealthy:
			overview.Health.Healthy++
		case api.HealthPaused:
			overview.Health.Paused++
		}
		// 三个采集自监控的数字与实例列表的三个筛选是同一个判定，一处定义两处使用：
		// 数字点开落到列表时，列表里的行数必须与数字逐个对得上。
		if instanceHasFlag(item, api.InstanceFlagStaleData) {
			overview.Collection.StaleData++
		}
		if instanceHasFlag(item, api.InstanceFlagAgentOffline) {
			overview.Collection.AgentOffline++
		}
		if item.Health.Status == api.HealthPaused {
			overview.Collection.Paused++
		}
	}
	return overview
}

// instancesNeedingAttention 取前十台：先按健康档位，再按告警严重度。
//
// 正常与已暂停的实例一台都不进来——「需要关注」是一份待办清单，把没事的机器凑进来
// 只会让它变成另一张实例列表。清单短于十条时就是短的，不补齐。
func instancesNeedingAttention(instances []api.Instance) []api.Instance {
	needing := make([]api.Instance, 0, len(instances))
	for _, item := range instances {
		if needsAttention(item) {
			needing = append(needing, item)
		}
	}
	sortByAttention(needing)
	if len(needing) > attentionLimit {
		needing = needing[:attentionLimit]
	}
	return needing
}

func needsAttention(item api.Instance) bool {
	switch item.Health.Status {
	case api.HealthCritical, api.HealthWarning, api.HealthUnknown:
		return true
	case api.HealthHealthy, api.HealthPaused:
		return false
	default:
		return false
	}
}

// sortByAttention 的次序：健康档位 → 严重 → 警告 → 提示 → 名称 → id。
// 最后两级与实例列表一样是定序用的：同分的行每次给出同一个顺序，读者才敢相信「前十」。
func sortByAttention(instances []api.Instance) {
	sort.SliceStable(instances, func(left, right int) bool {
		first, second := instances[left], instances[right]
		if healthRank(first.Health.Status) != healthRank(second.Health.Status) {
			return healthRank(first.Health.Status) > healthRank(second.Health.Status)
		}
		if first.Health.Counts.Critical != second.Health.Counts.Critical {
			return first.Health.Counts.Critical > second.Health.Counts.Critical
		}
		if first.Health.Counts.Warning != second.Health.Counts.Warning {
			return first.Health.Counts.Warning > second.Health.Counts.Warning
		}
		if first.Health.Counts.Info != second.Health.Counts.Info {
			return first.Health.Counts.Info > second.Health.Counts.Info
		}
		return instanceStableOrder(first, second)
	})
}

// storageUsage 读容量水位那一块：语义位「容量水位」**逐引擎**解析成具体指标。
//
// 机群里可以混着多个引擎，所以这里按实例的引擎去解析，而不是写死一个引擎去问 ——
// 位的那层指向如果在调用处被一个常量绕过去，它就只是个摆设了。今天九个位里只有这一个
// 由引擎无关的指标（host.disk.usage_percent）填，各引擎解析出来的是同一个指标；
// 真到了某个引擎自带库存水位的那天，这里会自然多查一个指标，一行都不用改。
//
// 一台机器上可能挂多个盘，每个挂载点一条序列。实例级取值取**最大**的那一个，不取平均：
// 一个满了的盘就是满了，旁边三个空盘不会让它变得没事。
func (handler *Handler) storageUsage(ctx context.Context, instances []api.Instance) ([]api.StorageUsageEntry, error) {
	metricIDs := storageMetricsForFleet(instances)
	since := handler.clock.Now().UTC().Add(-storageUsageWindow)
	names := make(map[uuid.UUID]string, len(instances))
	for _, item := range instances {
		names[item.Id] = item.Name
	}
	worst := make(map[uuid.UUID]api.StorageUsageEntry, len(instances))
	for _, metricID := range metricIDs {
		rows, err := metric.New(handler.platform).RecentValuePerSeriesForMetric(ctx, metric.RecentValuePerSeriesForMetricParams{
			MetricID: metricID.String(),
			Since:    pgtype.Timestamptz{Time: since, Valid: true},
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			instanceID := uuid.UUID(row.InstanceID.Bytes)
			name, known := names[instanceID]
			if !known {
				continue
			}
			candidate := api.StorageUsageEntry{
				InstanceId:   instanceID,
				InstanceName: name,
				UsagePercent: row.Value,
				SampledAt:    row.Ts.Time.UTC(),
			}
			current, seen := worst[instanceID]
			if !seen || candidate.UsagePercent > current.UsagePercent {
				worst[instanceID] = candidate
			}
		}
	}
	entries := make([]api.StorageUsageEntry, 0, len(worst))
	for _, entry := range worst {
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].UsagePercent != entries[right].UsagePercent {
			return entries[left].UsagePercent > entries[right].UsagePercent
		}
		if entries[left].InstanceName != entries[right].InstanceName {
			return entries[left].InstanceName < entries[right].InstanceName
		}
		return entries[left].InstanceId.String() < entries[right].InstanceId.String()
	})
	if len(entries) > storageLimit {
		entries = entries[:storageLimit]
	}
	return entries, nil
}

// storageMetricsForFleet 是这一群实例上「容量水位」这个位解析出来的具体指标全集。
//
// 某个引擎没有绑定这个位时，那个引擎的实例就是不适用 —— 跳过它，而不是替它退回一个
// 别的引擎的指标。位解析不出来是一句关于引擎的事实，不是一次可以兜底的失败。
func storageMetricsForFleet(instances []api.Instance) []metric.MetricID {
	metricIDs := make([]metric.MetricID, 0, 1)
	for _, item := range instances {
		resolved, err := metric.ResolveSlot(metric.SlotStorageUsage, metric.Engine(item.Engine))
		if err != nil {
			continue
		}
		if !slices.Contains(metricIDs, resolved) {
			metricIDs = append(metricIDs, resolved)
		}
	}
	return metricIDs
}
