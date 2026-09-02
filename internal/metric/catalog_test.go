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
		// 容量水位只有一条绑定，而且是引擎无关的那条：它量的是主机的盘，百分比。
		// 数据库体积（字节）**不填这个位**——同一个位在不同引擎上给出不同单位的话，
		// 谁都没法通用地消费它。所以这个位在任何引擎上问，答案都是主机水位。
		{name: "storage usage on PostgreSQL", slot: metric.SlotStorageUsage, engine: metric.EnginePostgreSQL, want: metric.MetricHostDiskUsagePercent},
		{name: "storage usage on the host", slot: metric.SlotStorageUsage, engine: metric.EngineAgnostic, want: metric.MetricHostDiskUsagePercent},
		{name: "storage usage on a second engine", slot: metric.SlotStorageUsage, engine: engineUnderTest, want: metric.MetricHostDiskUsagePercent},
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

// 一个位在不同引擎上解析出来的指标必须是同一个单位。位是「引擎中立地引用一个数」的
// 唯一手段：单位一旦随引擎变，总览的那一格、告警模板的那个阈值就都在两种量纲之间
// 静默地换来换去——那比没有位更糟，因为界面看起来还是对的。
func TestSlotBindingsAgreeOnUnit(t *testing.T) {
	units := make(map[metric.SemanticSlot]metric.MetricID)
	for _, item := range metric.Metrics {
		if item.Slot == "" {
			continue
		}
		previous, seen := units[item.Slot]
		if !seen {
			units[item.Slot] = item.ID
			continue
		}
		if metric.UnitFor(previous) != metric.UnitFor(item.ID) {
			t.Errorf("slot %q binds %q (%s) and %q (%s): a slot cannot change unit per engine",
				item.Slot, previous, metric.UnitFor(previous), item.ID, metric.UnitFor(item.ID))
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

// 只有 PostgreSQL 一种引擎接进来，跨引擎的行为在界面上看不见，但它在这里看得见：
// 造一个只在测试里存在的第二引擎，把一个指标绑到某个位上，看解析怎么走。
// **这个引擎不进生产代码**——生产代码里的引擎全集只有 internal/dbengine 那一份。
const engineUnderTest = metric.Engine("ENGINE_UNDER_TEST")

// withTestEngineBinding 把一条「第二引擎」的目录行临时挂进目录，返回时恢复原样。
func withTestEngineBinding(t *testing.T, id metric.MetricID, slot metric.SemanticSlot) {
	t.Helper()
	original := metric.Metrics
	restored := make([]metric.Metric, len(original))
	copy(restored, original)
	metric.Metrics = append(restored, metric.Metric{
		ID: id, DisplayName: string(id), Engine: engineUnderTest,
		Level: metric.LevelInstance, Aggregation: metric.AggregationNone, Slot: slot,
	})
	t.Cleanup(func() { metric.Metrics = original })
}

// 引用语义位的规则跨引擎：同一条规则在两种引擎上解析到各自的具体指标。
func TestResolveForEngineFollowsTheSlotAcrossEngines(t *testing.T) {
	withTestEngineBinding(t, "other.connection.total", metric.SlotConnections)

	resolved, err := metric.ResolveForEngine("pg.connection.total", engineUnderTest)
	if err != nil {
		t.Fatalf("resolve pg.connection.total on the second engine: %v", err)
	}
	if resolved != "other.connection.total" {
		t.Errorf("resolved to %q, want the second engine's binding", resolved)
	}

	resolved, err = metric.ResolveForEngine("pg.connection.total", metric.EnginePostgreSQL)
	if err != nil {
		t.Fatalf("resolve pg.connection.total on PostgreSQL: %v", err)
	}
	if resolved != "pg.connection.total" {
		t.Errorf("resolved to %q on its own engine, want itself", resolved)
	}
}

// 引擎私有指标（没有位）换个引擎就不适用，而且必须以 error 的形式说出来。
func TestResolveForEngineRejectsEnginePrivateMetrics(t *testing.T) {
	for _, id := range []metric.MetricID{
		"pg.replication_slot.retained_wal_bytes",
		"pg.prepared_xacts.count",
		"pg.temp.bytes_per_sec",
		"pg.replication.wal_lag_bytes",
	} {
		resolved, err := metric.ResolveForEngine(id, engineUnderTest)
		if !errors.Is(err, metric.ErrMetricEngineMismatch) {
			t.Errorf("resolve %q on another engine: err = %v, want ErrMetricEngineMismatch", id, err)
		}
		if resolved != "" {
			t.Errorf("resolve %q on another engine returned %q alongside the error", id, resolved)
		}
		if metric.AppliesToEngine(id, engineUnderTest) {
			t.Errorf("metric %q claims to apply to another engine", id)
		}
		if !metric.AppliesToEngine(id, metric.EnginePostgreSQL) {
			t.Errorf("metric %q does not apply to its own engine", id)
		}
	}
}

// 位没有在这个引擎上绑定时，答案是「不适用」，不是一个 PostgreSQL 的兜底指标。
func TestResolveForEngineReportsUnboundSlots(t *testing.T) {
	resolved, err := metric.ResolveForEngine("pg.cache.hit_ratio", engineUnderTest)
	if !errors.Is(err, metric.ErrSlotNotApplicable) {
		t.Fatalf("err = %v, want ErrSlotNotApplicable", err)
	}
	if resolved != "" {
		t.Errorf("resolved to %q alongside the error", resolved)
	}
}

// 引擎无关的指标（host.* / agent.* / collector.*）在任何引擎上都是它自己。
// 磁盘使用率填的是容量水位这个位，但它不属于任何数据库产品，所以在 PostgreSQL 实例上
// 也不该被换成 pg.database.size_bytes——一条「磁盘满了」的规则量的始终是磁盘。
func TestResolveForEngineKeepsAgnosticMetricsThemselves(t *testing.T) {
	for _, id := range []metric.MetricID{"host.disk.usage_percent", "host.cpu.usage_percent", "agent.status"} {
		for _, engine := range []metric.Engine{metric.EnginePostgreSQL, engineUnderTest} {
			resolved, err := metric.ResolveForEngine(id, engine)
			if err != nil {
				t.Errorf("resolve %q on %s: %v", id, engine, err)
				continue
			}
			if resolved != id {
				t.Errorf("resolve %q on %s = %q, want itself", id, engine, resolved)
			}
		}
	}
}

func TestResolveForEngineRejectsUncataloguedMetrics(t *testing.T) {
	if _, err := metric.ResolveForEngine("pg.not.a.metric", metric.EnginePostgreSQL); !errors.Is(err, metric.ErrMetricNotInCatalog) {
		t.Fatalf("err = %v, want ErrMetricNotInCatalog", err)
	}
}
