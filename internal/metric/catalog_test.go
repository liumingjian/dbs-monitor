package metric_test

import (
	"errors"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestResolveSlotBindings(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		slot   metric.SemanticSlot
		engine metric.Engine
		want   metric.MetricID
	}{
		{name: "throughput", slot: metric.SlotThroughput, engine: metric.EnginePostgreSQL, want: metric.MetricTPS},
		{name: "connections", slot: metric.SlotConnections, engine: metric.EnginePostgreSQL, want: metric.MetricConnectionTotal},
		{name: "probe latency", slot: metric.SlotProbeLatency, engine: metric.EnginePostgreSQL, want: metric.MetricProbeLatencyMS},
		{name: "rollback rate", slot: metric.SlotRollbackRate, engine: metric.EnginePostgreSQL, want: metric.MetricXactRollbackPerS},
		{name: "replication lag", slot: metric.SlotReplicationLag, engine: metric.EnginePostgreSQL, want: metric.MetricReplicationReplayLagMS},
		{name: "connection saturation", slot: metric.SlotConnectionSaturation, engine: metric.EnginePostgreSQL, want: metric.MetricConnectionSaturationPercent},
		{name: "cache hit ratio", slot: metric.SlotCacheHitRatio, engine: metric.EnginePostgreSQL, want: metric.MetricCacheHitRatio},
		{name: "deadlocks", slot: metric.SlotDeadlocks, engine: metric.EnginePostgreSQL, want: metric.MetricDeadlockCount},
		// 容量水位是规范里唯一一个「库级 + 主机」的位：PostgreSQL 侧是库的体积，
		// 主机侧是数据目录所在文件系统的水位。两条绑定落在两个引擎上，位只有一个。
		{name: "storage usage on PostgreSQL", slot: metric.SlotStorageUsage, engine: metric.EnginePostgreSQL, want: metric.MetricDatabaseSizeBytes},
		{name: "storage usage on the host", slot: metric.SlotStorageUsage, engine: metric.EngineAgnostic, want: metric.MetricHostDiskUsagePercent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := metric.ResolveSlot(testCase.slot, testCase.engine)
			if err != nil {
				t.Fatalf("resolve %q on %q: %v", testCase.slot, testCase.engine, err)
			}
			if got != testCase.want {
				t.Fatalf("resolve %q on %q = %q, want %q", testCase.slot, testCase.engine, got, testCase.want)
			}
		})
	}
}

// 该引擎没有这个位时必须说「不适用」。返回一个空指标 ID 而不报错，会让调用方把它当成一个真指标
// 去查序列，界面上得到的就是「无数据」——一句关于数据的谎话，而不是一句关于引擎的事实。
func TestResolveSlotReportsNotApplicable(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		slot   metric.SemanticSlot
		engine metric.Engine
	}{
		{name: "engine without a binding for a declared slot", slot: metric.SlotThroughput, engine: metric.Engine("MYSQL")},
		{name: "slot bound on one engine only", slot: metric.SlotCacheHitRatio, engine: metric.Engine("MYSQL")},
		{name: "engine-private slot on an engine-agnostic caller", slot: metric.SlotReplicationLag, engine: metric.EngineAgnostic},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := metric.ResolveSlot(testCase.slot, testCase.engine)
			if !errors.Is(err, metric.ErrSlotNotApplicable) {
				t.Fatalf("resolve %q on %q error = %v, want ErrSlotNotApplicable", testCase.slot, testCase.engine, err)
			}
			if got != "" {
				t.Fatalf("resolve %q on %q returned metric %q alongside a not-applicable error", testCase.slot, testCase.engine, got)
			}
		})
	}
}

func TestResolveSlotRejectsUndeclaredSlot(t *testing.T) {
	if _, err := metric.ResolveSlot(metric.SemanticSlot("query_latency"), metric.EnginePostgreSQL); !errors.Is(err, metric.ErrUnknownSemanticSlot) {
		t.Fatalf("resolve undeclared slot error = %v, want ErrUnknownSemanticSlot", err)
	}
}

// 「位 + 引擎 -> 指标 ID」必须是个函数：同一个位在同一个引擎下只能落到一个指标，
// 否则 ResolveSlot 的答案取决于字典里的先后顺序。metric_catalog 上的 UNIQUE 是同一条约束。
func TestSlotBindingsAreUniquePerEngine(t *testing.T) {
	type binding struct {
		slot   metric.SemanticSlot
		engine metric.Engine
	}
	seen := make(map[binding]metric.MetricID)
	for _, item := range metric.Metrics {
		if item.Slot == "" {
			continue
		}
		if !metric.SlotDeclared(item.Slot) {
			t.Errorf("metric %q claims undeclared semantic slot %q", item.ID, item.Slot)
			continue
		}
		key := binding{slot: item.Slot, engine: item.Engine}
		if previous, exists := seen[key]; exists {
			t.Errorf("slot %q on engine %q is bound to both %q and %q", item.Slot, item.Engine, previous, item.ID)
		}
		seen[key] = item.ID
	}
}

func TestEveryCatalogEntryIsDescribed(t *testing.T) {
	for _, item := range metric.Metrics {
		if item.DisplayName == "" {
			t.Errorf("metric %q has no display name", item.ID)
		}
		if item.Unit == "" {
			t.Errorf("metric %q has no unit", item.ID)
		}
		if !contains(engineValues(), string(item.Engine)) {
			t.Errorf("metric %q has unknown engine %q", item.ID, item.Engine)
		}
		if !contains(metricLevelValues(), string(item.Level)) {
			t.Errorf("metric %q has unknown level %q", item.ID, item.Level)
		}
		// 实例级指标没有可聚合的东西；库级指标必须说清楚怎么收敛。metric_catalog 上的
		// metric_catalog_aggregation_matches_level 是同一条约束。
		if item.Level == metric.LevelInstance && item.Aggregation != metric.AggregationNone {
			t.Errorf("instance-level metric %q declares aggregation %q", item.ID, item.Aggregation)
		}
		if item.Level == metric.LevelDatabase && item.Aggregation == metric.AggregationNone {
			t.Errorf("database-level metric %q declares no aggregation", item.ID)
		}
	}
}

// 加权平均与权重指标必须成对：没有权重的加权平均就是算术平均，而算术平均正是
// 这条聚合规则要挡住的东西。metric_catalog 上的 metric_catalog_weighted_average_has_weight
// 是同一条约束。库级指标之外的东西不能当权重——权重要能逐库和被加权的指标配对。
func TestWeightedAverageMetricsDeclareAWeight(t *testing.T) {
	for _, item := range metric.Metrics {
		weight, hasWeight := metric.WeightMetricFor(item.ID)
		if (item.Aggregation == metric.AggregationWeightedAverage) != hasWeight {
			t.Errorf("metric %q has aggregation %q and weight %q", item.ID, item.Aggregation, weight)
			continue
		}
		if !hasWeight {
			continue
		}
		declared, exists := metric.Lookup(weight)
		if !exists {
			t.Errorf("metric %q weights on %q, which is not in the catalogue", item.ID, weight)
			continue
		}
		if declared.Level != metric.LevelDatabase {
			t.Errorf("metric %q weights on %q, which is %s-level", item.ID, weight, declared.Level)
		}
	}
}

func TestNineSemanticSlotsAreDeclared(t *testing.T) {
	if len(metric.SemanticSlots) != 9 {
		t.Fatalf("declared %d semantic slots, want 9", len(metric.SemanticSlots))
	}
	for _, declaration := range metric.SemanticSlots {
		if declaration.DisplayName == "" {
			t.Errorf("semantic slot %q has no display name", declaration.ID)
		}
	}
}

// 九个位到此全部有 PostgreSQL 绑定——语义位是列表、总览与告警模板唯一能引用的东西，
// 缺一个位就等于那三处各自要为 PostgreSQL 开一条特例通道。
func TestEverySemanticSlotBindsOnPostgreSQL(t *testing.T) {
	for _, declaration := range metric.SemanticSlots {
		if _, err := metric.ResolveSlot(declaration.ID, metric.EnginePostgreSQL); err != nil {
			t.Errorf("resolve slot %q on PostgreSQL: %v", declaration.ID, err)
		}
	}
}
