package metric

import (
	"errors"
	"fmt"

	"github.com/liumingjian/dbs-monitor/internal/dbengine"
)

// Engine is the database engine a metric belongs to.
//
// 类型本身住在 internal/dbengine：实例也要说「这台跑的是哪个产品」，两处必须是同一个词、
// 同一套字面量。目录侧比实例侧多一个 EngineAgnostic——它是给 host.* / agent.* / collector.*
// 这类与引擎无关的指标用的：它们挂在实例上，量的却是主机与采集自身。实例取不到这个值。
type Engine = dbengine.Engine

const (
	EnginePostgreSQL = dbengine.PostgreSQL
	EngineAgnostic   = dbengine.Agnostic
)

// Engines 是目录里允许出现的引擎全集。
var Engines = dbengine.CatalogEngines()

// SemanticSlot is one engine-neutral position in the metric model; it resolves per engine to a
// concrete metric ID. See docs/adr/0001-semantic-metric-slots.md.
type SemanticSlot string

const (
	SlotThroughput           SemanticSlot = "throughput"
	SlotConnections          SemanticSlot = "connections"
	SlotConnectionSaturation SemanticSlot = "connection_saturation"
	SlotProbeLatency         SemanticSlot = "probe_latency"
	SlotRollbackRate         SemanticSlot = "rollback_rate"
	SlotReplicationLag       SemanticSlot = "replication_lag"
	SlotCacheHitRatio        SemanticSlot = "cache_hit_ratio"
	SlotStorageUsage         SemanticSlot = "storage_usage"
	SlotDeadlocks            SemanticSlot = "deadlocks"
)

func (slot SemanticSlot) String() string {
	return string(slot)
}

type SemanticSlotDeclaration struct {
	ID          SemanticSlot
	DisplayName string
}

// SemanticSlots 是九个语义位的全集。位先立齐，绑定可以缺——缺绑定的位在该引擎下解析成
// ErrSlotNotApplicable，而不是一个空指标 ID。
var SemanticSlots = []SemanticSlotDeclaration{
	{ID: SlotThroughput, DisplayName: "吞吐"},
	{ID: SlotConnections, DisplayName: "连接数"},
	{ID: SlotConnectionSaturation, DisplayName: "连接饱和度"},
	{ID: SlotProbeLatency, DisplayName: "探针延迟"},
	{ID: SlotRollbackRate, DisplayName: "回滚率"},
	{ID: SlotReplicationLag, DisplayName: "复制延迟"},
	{ID: SlotCacheHitRatio, DisplayName: "缓存命中率"},
	{ID: SlotStorageUsage, DisplayName: "容量水位"},
	{ID: SlotDeadlocks, DisplayName: "死锁数"},
}

// MetricLevel says whether a metric describes the whole instance or one database inside it.
type MetricLevel string

const (
	LevelInstance MetricLevel = "INSTANCE"
	LevelDatabase MetricLevel = "DATABASE"
)

// MetricAggregation is how a database-level metric collapses into the instance-level value the
// fleet views show. Instance-level metrics carry AggregationNone: there is nothing to collapse.
type MetricAggregation string

const (
	AggregationNone            MetricAggregation = "NONE"
	AggregationSum             MetricAggregation = "SUM"
	AggregationWeightedAverage MetricAggregation = "WEIGHTED_AVERAGE"
)

var MetricLevels = []MetricLevel{LevelInstance, LevelDatabase}

var MetricAggregations = []MetricAggregation{AggregationNone, AggregationSum, AggregationWeightedAverage}

var (
	// ErrUnknownSemanticSlot 表示请求的语义位根本没有登记。
	ErrUnknownSemanticSlot = errors.New("metric: unknown semantic slot")
	// ErrSlotNotApplicable 表示这个位在这个引擎上不适用——没有绑定，也不该有一个兜底值。
	ErrSlotNotApplicable = errors.New("metric: semantic slot is not applicable to this engine")
)

// ResolveSlot maps a semantic slot plus an engine onto the concrete metric ID that fills it.
//
// 没有绑定时返回 ErrSlotNotApplicable，位本身不存在时返回 ErrUnknownSemanticSlot。调用方**必须**
// 看 error：不适用是一个要显式呈现的结论（「该引擎没有这个指标」），不是空字符串，也不是 0。
//
// 引擎自己的绑定优先；没有时退到**引擎无关**的绑定（容量水位就是这一种：它由
// host.disk.usage_percent 填，量的是主机的盘，与跑哪个数据库产品无关）。这条退路与
// ResolveForEngine 里「引擎无关的指标在哪个引擎上都是它自己」是同一条规则，所以
// 调用方不必先判断「这个位是不是主机侧的」再决定拿什么引擎来问——那种判断一旦写进
// 调用方，位的这层指向就成了摆设。
func ResolveSlot(slot SemanticSlot, engine Engine) (MetricID, error) {
	if !SlotDeclared(slot) {
		return "", fmt.Errorf("%w: %q", ErrUnknownSemanticSlot, slot)
	}
	agnostic := MetricID("")
	for _, item := range Metrics {
		if item.Slot != slot {
			continue
		}
		if item.Engine == engine {
			return item.ID, nil
		}
		if item.Engine == EngineAgnostic {
			agnostic = item.ID
		}
	}
	if agnostic != "" {
		return agnostic, nil
	}
	return "", fmt.Errorf("%w: slot %q on engine %q", ErrSlotNotApplicable, slot, engine)
}

// SlotDeclared reports whether the slot is one of the declared nine.
func SlotDeclared(slot SemanticSlot) bool {
	for _, declaration := range SemanticSlots {
		if declaration.ID == slot {
			return true
		}
	}
	return false
}

// Lookup 取出目录里这一条指标。
func Lookup(id MetricID) (Metric, bool) {
	for _, item := range Metrics {
		if item.ID == id {
			return item, true
		}
	}
	return Metric{}, false
}

// LevelFor 说这个指标量的是整台实例还是其中一个库。目录里没有的指标当作实例级：
// 未知指标不会有库维度的序列，把它当库级只会让读取侧去找一个不存在的聚合方式。
func LevelFor(id MetricID) MetricLevel {
	if item, exists := Lookup(id); exists {
		return item.Level
	}
	return LevelInstance
}

// AggregationFor 是这个库级指标收敛成实例级的方式；实例级指标返回 AggregationNone。
func AggregationFor(id MetricID) MetricAggregation {
	if item, exists := Lookup(id); exists {
		return item.Aggregation
	}
	return AggregationNone
}

// WeightMetricFor 是加权平均的权重指标。只有 WEIGHTED_AVERAGE 的指标有权重。
func WeightMetricFor(id MetricID) (MetricID, bool) {
	item, exists := Lookup(id)
	if !exists || item.Weight == "" {
		return "", false
	}
	return item.Weight, true
}

var (
	// ErrMetricNotInCatalog 表示这个指标 ID 根本不在目录里。
	ErrMetricNotInCatalog = errors.New("metric: metric is not in the catalogue")
	// ErrMetricEngineMismatch 表示这个指标是另一个引擎的私有指标——它没有语义位，
	// 所以在别的引擎上既没有对应物，也不该被悄悄换成一个近似的东西。
	ErrMetricEngineMismatch = errors.New("metric: metric is private to another engine")
)

// EngineFor 是这个指标的归属引擎。目录里没有的指标返回 false——调用方要能区分
// 「不属于任何引擎」（Agnostic）与「根本不认识」。
func EngineFor(id MetricID) (Engine, bool) {
	item, exists := Lookup(id)
	if !exists {
		return "", false
	}
	return item.Engine, true
}

// SlotFor 是这个指标填的语义位；没填位就是空串。
//
// 位与指标在一个引擎下是一一对应的（metric_catalog 上 UNIQUE (semantic_slot, engine)），
// 所以「指标 ID」与「(位, 引擎)」互为反函数：告警规则存下具体指标 ID 就等于存下了位，
// 不必在 alert_rule 上再开一列。ResolveForEngine 走的正是这条反函数。
func SlotFor(id MetricID) SemanticSlot {
	item, exists := Lookup(id)
	if !exists {
		return ""
	}
	return item.Slot
}

// ResolveForEngine 回答「这条按 id 写下的规则，放到一台跑 engine 的实例上，量的是哪个指标」。
//
// 三种归属，三种答案：
//   - 引擎无关的指标（host.* / agent.* / collector.*）在哪个引擎上都是它自己；
//   - 填了语义位的指标解析到该引擎绑定在这个位上的指标——这是模板一份两用的机制；
//   - 既不无关、又没有位的指标是该引擎的私有指标（WAL 保留、复制槽、prepared xacts……），
//     换个引擎就返回 ErrMetricEngineMismatch。
//
// 不适用时返回空 ID **并且**返回 error，和 ResolveSlot 一样：不适用是一个要显式呈现的结论。
func ResolveForEngine(id MetricID, engine Engine) (MetricID, error) {
	item, exists := Lookup(id)
	if !exists {
		return "", fmt.Errorf("%w: %q", ErrMetricNotInCatalog, id)
	}
	if item.Engine == EngineAgnostic {
		return item.ID, nil
	}
	if item.Slot != "" {
		return ResolveSlot(item.Slot, engine)
	}
	if item.Engine == engine {
		return item.ID, nil
	}
	return "", fmt.Errorf("%w: metric %q belongs to engine %q, not %q", ErrMetricEngineMismatch, id, item.Engine, engine)
}

// AppliesToEngine 报告一条建在这个指标上的告警规则能不能指派到跑 engine 的实例上。
func AppliesToEngine(id MetricID, engine Engine) bool {
	_, err := ResolveForEngine(id, engine)
	return err == nil
}
