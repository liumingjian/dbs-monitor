package httpapi

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 机群总览的聚合。
//
// 四块（健康计数 / 采集自监控 / 需要关注的 Top 10 / 容量水位 Top 10）读的是**同一份**
// 实例级投影，所以它们在一个请求里一起算完：分成四个端点会让首屏发四个往返，还会让四块
// 看到四个不同时刻的机群——「严重 3 台」与下面列出的那几台对不上，是比慢更糟的毛病。
//
// 计数与 Top 10 都是纯函数（summarizeFleet / instancesNeedingAttention），因为「哪台该
// 排在前面」算错了不会报错，只会安静地把该看的那台压到第十一位。

// attentionLimit 与 storageLimit 都是十。五百个格子没有信息量：总览要回答的是
// 「我现在该看谁」，剩下的由实例列表按筛选回答。
const (
	attentionLimit = 10
	storageLimit   = 10
)

// storageWatermarkWindow 是「最近一次读数」的有效期。超过这个窗口没有新样本，
// 这台的磁盘水位就是**未知**，不是最后那个值：一个月前的 91% 拿来当今天的水位在骗人。
const storageWatermarkWindow = 6 * time.Hour

// GetFleetOverview 返回总览页四块的聚合结果。
func (handler *Handler) GetFleetOverview(ctx context.Context, _ api.GetFleetOverviewRequestObject) (api.GetFleetOverviewResponseObject, error) {
	instances, err := handler.listInstancesWithHealth(ctx)
	if err != nil {
		return nil, err
	}
	overview := summarizeFleet(instances)
	watermarks, err := handler.storageWatermarks(ctx, instances)
	if err != nil {
		return nil, err
	}
	overview.Storage = watermarks
	return api.GetFleetOverview200JSONResponse(overview), nil
}

// summarizeFleet 把实例级投影收敛成前三块。第四块要读时序，不在这里。
func summarizeFleet(instances []api.Instance) api.FleetOverview {
	overview := api.FleetOverview{
		Total:      len(instances),
		Attention:  instancesNeedingAttention(instances),
		Storage:    []api.StorageWatermarkEntry{},
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
		if first.Name != second.Name {
			return first.Name < second.Name
		}
		return first.Id.String() < second.Id.String()
	})
}

// storageWatermarks 读容量水位：语义位「容量水位」在主机侧解析成 host.disk.usage_percent。
//
// 一台机器上可能挂多个盘，每个挂载点一条序列。实例级取值取**最大**的那一个，不取平均：
// 一个满了的盘就是满了，旁边三个空盘不会让它变得没事。
func (handler *Handler) storageWatermarks(ctx context.Context, instances []api.Instance) ([]api.StorageWatermarkEntry, error) {
	metricID, err := metric.ResolveSlot(metric.SlotStorageUsage, metric.EngineAgnostic)
	if err != nil {
		return nil, err
	}
	since := handler.clock.Now().UTC().Add(-storageWatermarkWindow)
	rows, err := metric.New(handler.platform).LatestSamplePerSeriesForMetric(ctx, metric.LatestSamplePerSeriesForMetricParams{
		MetricID: metricID.String(),
		Since:    pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	names := make(map[uuid.UUID]string, len(instances))
	for _, item := range instances {
		names[item.Id] = item.Name
	}
	worst := make(map[uuid.UUID]api.StorageWatermarkEntry, len(rows))
	for _, row := range rows {
		instanceID := uuid.UUID(row.InstanceID.Bytes)
		name, known := names[instanceID]
		if !known {
			continue
		}
		candidate := api.StorageWatermarkEntry{
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
	entries := make([]api.StorageWatermarkEntry, 0, len(worst))
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
