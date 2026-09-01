package metric

import (
	"errors"
	"fmt"
)

// Engine is the database engine a metric belongs to.
//
// 这是**指标层**需要的最小引擎类型：目录每一行都要说清楚自己属于谁。实例侧的引擎字段是另一张票
// (#216) 的事，合并时两处定义要归一。`EngineAgnostic` 是给 host.* / agent.* / collector.* 这类
// 与引擎无关的指标用的——它们挂在实例上，量的却是主机与采集自身。
type Engine string

const (
	EnginePostgreSQL Engine = "POSTGRESQL"
	EngineAgnostic   Engine = "AGNOSTIC"
)

// Engines 是目录里允许出现的引擎全集。
var Engines = []Engine{EnginePostgreSQL, EngineAgnostic}

func (engine Engine) String() string {
	return string(engine)
}

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
func ResolveSlot(slot SemanticSlot, engine Engine) (MetricID, error) {
	if !SlotDeclared(slot) {
		return "", fmt.Errorf("%w: %q", ErrUnknownSemanticSlot, slot)
	}
	for _, item := range Metrics {
		if item.Slot == slot && item.Engine == engine {
			return item.ID, nil
		}
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
