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
		{name: "slot nothing binds yet", slot: metric.SlotCacheHitRatio, engine: metric.EnginePostgreSQL},
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
