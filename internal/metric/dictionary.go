package metric

import (
	"fmt"
	"time"
)

type MetricID string

func (id MetricID) String() string {
	return string(id)
}

const (
	MetricAvailabilityReachable       MetricID = "pg.availability.reachable"
	MetricProbeLatencyMS              MetricID = "pg.probe.latency_ms"
	MetricCollectorLastSuccessTime    MetricID = "collector.last_success_time"
	MetricAgentStatus                 MetricID = "agent.status"
	MetricHostCPUUsagePercent         MetricID = "host.cpu.usage_percent"
	MetricHostMemoryUsagePercent      MetricID = "host.memory.usage_percent"
	MetricHostDiskUsagePercent        MetricID = "host.disk.usage_percent"
	MetricHostDiskFreeBytes           MetricID = "host.disk.free_bytes"
	MetricHostDiskIOPS                MetricID = "host.disk.iops"
	MetricHostDiskThroughputBytesPerS MetricID = "host.disk.throughput_bytes_per_sec"
	MetricHostNetworkBytesPerS        MetricID = "host.network.bytes_per_sec"
	MetricConnectionTotal             MetricID = "pg.connection.total"
	MetricConnectionActive            MetricID = "pg.connection.active"
	MetricConnectionIdleInTransaction MetricID = "pg.connection.idle_in_transaction"
	MetricConnectionMax               MetricID = "pg.connection.max"
	MetricConnectionSaturationPercent MetricID = "pg.connection.saturation_percent"
	MetricTPS                         MetricID = "pg.tps"
	MetricXactCommitPerS              MetricID = "pg.xact.commit_per_sec"
	MetricXactRollbackPerS            MetricID = "pg.xact.rollback_per_sec"
	MetricTuplesReadPerS              MetricID = "pg.tuples.read_per_sec"
	MetricTuplesWritePerS             MetricID = "pg.tuples.write_per_sec"
	MetricTempFilesPerS               MetricID = "pg.temp.files_per_sec"
	MetricTempBytesPerS               MetricID = "pg.temp.bytes_per_sec"
	MetricLongTransactionCount        MetricID = "pg.transaction.long_count"
	MetricMaxTransactionDurationSec   MetricID = "pg.transaction.max_duration_sec"
	MetricLockWaitingCount            MetricID = "pg.lock.waiting_count"
	MetricBlockedSessionCount         MetricID = "pg.session.blocked_count"
	MetricLongRunningQueryCount       MetricID = "pg.query.long_running_count"
	MetricPreparedXactsCount          MetricID = "pg.prepared_xacts.count"
	MetricReplicationRole             MetricID = "pg.replication.role"
	MetricReplicationConnectionState  MetricID = "pg.replication.connection_state"
	MetricReplicationReplayLagMS      MetricID = "pg.replication.replay_lag_ms"
	MetricReplicationWALLagBytes      MetricID = "pg.replication.wal_lag_bytes"
	MetricReplicationSlotRetainedWAL  MetricID = "pg.replication_slot.retained_wal_bytes"
	MetricCacheHitRatio               MetricID = "pg.cache.hit_ratio"
	MetricCacheBlockAccessPerS        MetricID = "pg.cache.block_access_per_sec"
	MetricDatabaseSizeBytes           MetricID = "pg.database.size_bytes"
	MetricDeadlockCount               MetricID = "pg.deadlock.count"
)

type MetricType string

const (
	MetricTypeGauge MetricType = "gauge"
	MetricTypeRate  MetricType = "rate"
	MetricTypeState MetricType = "state"
)

type MetricCalculation string

const (
	CalculationRaw          MetricCalculation = "raw"
	CalculationCounterDelta MetricCalculation = "counter_delta"
	CalculationStateMapping MetricCalculation = "state_mapping"
)

type MetricProducer string

const (
	ProducerServerTask   MetricProducer = "server_task"
	ProducerAgent        MetricProducer = "agent"
	ProducerControlPlane MetricProducer = "control_plane"
)

type Alertability string

const (
	AlertabilityYes         Alertability = "yes"
	AlertabilityNo          Alertability = "no"
	AlertabilityConditional Alertability = "conditional"
)

type Metric struct {
	ID          MetricID
	DisplayName string
	// Engine 与 Slot / Level / Aggregation 一起构成 metric_catalog 的一行；
	// 迁移之后由 migrations.reconcileMetricCatalog 落进表里。
	Engine      Engine
	Level       MetricLevel
	Aggregation MetricAggregation
	// Weight 是加权平均的权重来源：另一个库级指标，逐库与本指标同名同库配对。
	// 只有 AggregationWeightedAverage 有权重，而且必须有——见 catalog_test.go 的成对约束。
	Weight            MetricID
	Slot              SemanticSlot
	Type              MetricType
	Unit              string
	Dimensions        []string
	Calculation       MetricCalculation
	Standard          bool
	EnhancedCandidate bool
	Alertability      Alertability
	Producer          MetricProducer
}

var Metrics = []Metric{
	{ID: MetricAvailabilityReachable, DisplayName: "实例连通性", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricProbeLatencyMS, DisplayName: "主动探针延迟", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Slot: SlotProbeLatency, Type: MetricTypeGauge, Unit: "ms", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricCollectorLastSuccessTime, DisplayName: "最近成功采集时间", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeState, Unit: "timestamp", Dimensions: []string{"instance", "source_type"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerControlPlane},
	{ID: MetricAgentStatus, DisplayName: "Agent 状态", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance", "node"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerControlPlane},
	{ID: MetricHostCPUUsagePercent, DisplayName: "CPU 使用率", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostMemoryUsagePercent, DisplayName: "内存使用率", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskUsagePercent, DisplayName: "磁盘使用率", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Slot: SlotStorageUsage, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node", "mount"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskFreeBytes, DisplayName: "磁盘剩余空间", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "node", "mount"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskIOPS, DisplayName: "磁盘 IOPS", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeRate, Unit: "ops/s", Dimensions: []string{"instance", "node", "device"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskThroughputBytesPerS, DisplayName: "磁盘吞吐", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance", "node", "device"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostNetworkBytesPerS, DisplayName: "网络流量", Engine: EngineAgnostic, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance", "node", "interface"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerAgent},
	{ID: MetricConnectionTotal, DisplayName: "总连接数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Slot: SlotConnections, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricConnectionActive, DisplayName: "活跃连接数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricConnectionIdleInTransaction, DisplayName: "idle in transaction 连接数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricConnectionMax, DisplayName: "最大连接数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityNo, Producer: ProducerServerTask},
	{ID: MetricConnectionSaturationPercent, DisplayName: "连接饱和度", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Slot: SlotConnectionSaturation, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricTPS, DisplayName: "TPS", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Slot: SlotThroughput, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricXactCommitPerS, DisplayName: "提交速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricXactRollbackPerS, DisplayName: "回滚速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Slot: SlotRollbackRate, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTuplesReadPerS, DisplayName: "读行速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "rows/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTuplesWritePerS, DisplayName: "写行速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "rows/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTempFilesPerS, DisplayName: "临时文件数量速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "files/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricTempBytesPerS, DisplayName: "临时文件写入速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLongTransactionCount, DisplayName: "长事务数量", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricMaxTransactionDurationSec, DisplayName: "最长事务时长", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "seconds", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLockWaitingCount, DisplayName: "锁等待数量", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricBlockedSessionCount, DisplayName: "被阻塞会话数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLongRunningQueryCount, DisplayName: "长查询数量", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricPreparedXactsCount, DisplayName: "2PC 数量", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance", "database"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationRole, DisplayName: "实例角色", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityNo, Producer: ProducerServerTask},
	{ID: MetricReplicationConnectionState, DisplayName: "复制连接状态", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance", "replica"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationReplayLagMS, DisplayName: "复制回放延迟", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Slot: SlotReplicationLag, Type: MetricTypeGauge, Unit: "ms", Dimensions: []string{"instance", "replica"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityNo, Producer: ProducerServerTask},
	{ID: MetricReplicationWALLagBytes, DisplayName: "WAL 延迟字节数", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "replica"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationSlotRetainedWAL, DisplayName: "Replication slot WAL 积压", Engine: EnginePostgreSQL, Level: LevelInstance, Aggregation: AggregationNone, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "slot"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	// 缓存命中率是目录里第一个加权平均：实例级值按「这个比率覆盖了多少真实工作量」加权，
	// 权重就是同一对计数器的增量和（pg.cache.block_access_per_sec）。算术平均会让一个
	// 200GB 主库崩到 60% 被同实例下二十个空库的 100% 稀释成 98%。
	{ID: MetricCacheHitRatio, DisplayName: "缓存命中率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationWeightedAverage, Weight: MetricCacheBlockAccessPerS, Slot: SlotCacheHitRatio, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricCacheBlockAccessPerS, DisplayName: "缓存块访问速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeRate, Unit: "blocks/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	// 数据库体积**不填语义位**。容量水位这个位量的是「盘要满了吗」，取值是百分比，
	// 由引擎无关的 host.disk.usage_percent 填；体积是字节数。同一个位在一个引擎上解析出
	// 百分比、在另一个引擎上解析出字节数，位就没法被通用地消费了——总览的水位榜、
	// 告警模板的阈值都会在两种单位之间静默地换来换去。规范表里「容量水位」一行同时列了
	// 这两个指标，落地时只把主机水位绑上位；体积仍在目录里，实例工作台按具体指标 ID 取它
	// （工作台本来就允许下到具体 ID），「哪个库在吃磁盘」这条动线一个字都没少。
	{ID: MetricDatabaseSizeBytes, DisplayName: "数据库体积", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "database"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricDeadlockCount, DisplayName: "死锁速率", Engine: EnginePostgreSQL, Level: LevelDatabase, Aggregation: AggregationSum, Slot: SlotDeadlocks, Type: MetricTypeRate, Unit: "count/s", Dimensions: []string{"instance", "database"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
}

func UnitFor(id MetricID) string {
	for _, item := range Metrics {
		if item.ID == id {
			return item.Unit
		}
	}
	return "count"
}

func ProducerFor(id MetricID) MetricProducer {
	for _, item := range Metrics {
		if item.ID == id {
			return item.Producer
		}
	}
	return ""
}

type TaskID string

const (
	TaskProbe           TaskID = "pg.probe"
	TaskStatDatabase    TaskID = "pg.stat_database"
	TaskStatActivity    TaskID = "pg.stat_activity"
	TaskReplication     TaskID = "pg.replication"
	TaskReplicationSlot TaskID = "pg.replication_slot"
	TaskPreparedXacts   TaskID = "pg.prepared_xacts"
	TaskRole            TaskID = "pg.role"
	TaskSettings        TaskID = "pg.settings"
	TaskDatabaseSize    TaskID = "pg.database_size"
	TaskQueryStatistics TaskID = "pg.stat_statements"
)

type TaskKind string

const (
	TaskKindProbe        TaskKind = "probe"
	TaskKindSQL          TaskKind = "sql"
	TaskKindAgentDerived TaskKind = "agent-derived"
)

// DimensionDatabase 是唯一一个不落进 labels 的维度：它落进 metric_series.database_name。
// 采集侧认这个名字（见 internal/collect/task_rows.go），所以一个新的库级指标只要在它的
// Yields 上写 Dimensions: []string{DimensionDatabase}，就自动按库落序列。
const DimensionDatabase = "database"

type MetricYield struct {
	Metric     MetricID
	Columns    []string
	Dimensions []string
}

type Task struct {
	ID       TaskID
	Kind     TaskKind
	SQL      string
	Requires []CapabilityID
	Interval time.Duration
	Yields   []MetricYield
}

const MinimumTaskInterval = 5 * time.Second

var Tasks = []Task{
	{
		ID: TaskProbe, Kind: TaskKindProbe, SQL: "SELECT 1", Interval: 5 * time.Second,
		Yields: []MetricYield{{Metric: MetricAvailabilityReachable, Columns: []string{"reachable"}}, {Metric: MetricProbeLatencyMS, Columns: []string{"latency_ms"}}},
	},
	{
		ID: TaskStatDatabase, Kind: TaskKindSQL, Interval: 5 * time.Second,
		Requires: []CapabilityID{CapabilityRolePGMonitor},
		// pg_stat_database 一库一行。这里 GROUP BY datname 之后一库落一条序列——原来这条查询
		// 不带 GROUP BY，一次就把全实例加总了，谁在产生这些事务无从查起。实例级的总数改由读取侧
		// 按目录里的聚合方式收敛（这一族都是 SUM），和加总前的每库明细并存。
		SQL: `SELECT datname::text AS database,
	       COALESCE(sum(xact_commit), 0)::double precision AS xact_commit,
	       COALESCE(sum(xact_rollback), 0)::double precision AS xact_rollback,
	       COALESCE(sum(tup_returned + tup_fetched), 0)::double precision AS tuples_read,
	       COALESCE(sum(tup_inserted + tup_updated + tup_deleted), 0)::double precision AS tuples_write,
	       COALESCE(sum(temp_files), 0)::double precision AS temp_files,
	       COALESCE(sum(temp_bytes), 0)::double precision AS temp_bytes,
	       COALESCE(sum(blks_hit), 0)::double precision AS blks_hit,
	       COALESCE(sum(blks_read), 0)::double precision AS blks_read,
	       COALESCE(sum(deadlocks), 0)::double precision AS deadlocks
FROM pg_stat_database
WHERE datname IS NOT NULL AND datname NOT IN ('template0', 'template1')
GROUP BY datname
ORDER BY datname`,
		Yields: []MetricYield{
			{Metric: MetricTPS, Columns: []string{DimensionDatabase, "xact_commit", "xact_rollback"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricXactCommitPerS, Columns: []string{DimensionDatabase, "xact_commit"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricXactRollbackPerS, Columns: []string{DimensionDatabase, "xact_rollback"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricTuplesReadPerS, Columns: []string{DimensionDatabase, "tuples_read"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricTuplesWritePerS, Columns: []string{DimensionDatabase, "tuples_write"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricTempFilesPerS, Columns: []string{DimensionDatabase, "temp_files"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricTempBytesPerS, Columns: []string{DimensionDatabase, "temp_bytes"}, Dimensions: []string{DimensionDatabase}},
			// 命中率与它的权重取自同一对计数器的增量：比率是 blks_hit / (blks_hit + blks_read)，
			// 权重是两者之和的速率。都求差而不取累积值——累积比率在一台跑了三个月的库上
			// 几乎不动，主库命中率崩掉的那半小时在图上看不见。
			{Metric: MetricCacheHitRatio, Columns: []string{DimensionDatabase, "blks_hit", "blks_read"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricCacheBlockAccessPerS, Columns: []string{DimensionDatabase, "blks_hit", "blks_read"}, Dimensions: []string{DimensionDatabase}},
			{Metric: MetricDeadlockCount, Columns: []string{DimensionDatabase, "deadlocks"}, Dimensions: []string{DimensionDatabase}},
		},
	},
	{
		ID: TaskStatActivity, Kind: TaskKindSQL, Interval: 5 * time.Second,
		Requires: []CapabilityID{CapabilityRolePGMonitor},
		SQL: `WITH activity AS MATERIALIZED (
    SELECT pid,
           usename::text AS username,
           datname::text AS database_name,
           client_addr::text AS client_address,
           state::text AS state,
           query_start,
           xact_start AS transaction_started_at,
           GREATEST((EXTRACT(epoch FROM statement_timestamp() - query_start) * 1000)::bigint, 0) AS query_duration_ms,
           GREATEST((EXTRACT(epoch FROM statement_timestamp() - xact_start) * 1000)::bigint, 0) AS transaction_duration_ms,
           wait_event_type::text AS wait_event_type,
           wait_event::text AS wait_event,
           pg_blocking_pids(pid) AS blocking_pids,
           application_name IS NOT DISTINCT FROM 'dbs-monitor' AS is_monitor
    FROM pg_stat_activity
), aggregate AS (
    SELECT count(*)::double precision AS connection_total,
           count(*) FILTER (WHERE state = 'active' AND NOT is_monitor)::double precision AS connection_active,
           count(*) FILTER (WHERE state = 'idle in transaction' AND NOT is_monitor)::double precision AS connection_idle_in_transaction,
           count(*) FILTER (WHERE transaction_started_at IS NOT NULL AND statement_timestamp() - transaction_started_at > interval '5 minutes' AND NOT is_monitor)::double precision AS long_transaction_count,
           COALESCE(max(transaction_duration_ms) FILTER (WHERE transaction_started_at IS NOT NULL AND NOT is_monitor), 0)::double precision / 1000 AS max_transaction_duration_sec,
           count(*) FILTER (WHERE wait_event_type = 'Lock' AND NOT is_monitor)::double precision AS lock_waiting_count,
           count(*) FILTER (WHERE cardinality(blocking_pids) > 0 AND NOT is_monitor)::double precision AS blocked_session_count,
           count(*) FILTER (WHERE state = 'active' AND query_start IS NOT NULL AND statement_timestamp() - query_start > interval '5 seconds' AND NOT is_monitor)::double precision AS long_running_query_count,
           -- 饱和度在这里算，因为分子（连接数）本来就是这条查询数出来的：分母 max_connections
           -- 是一次 GUC 内存查表，不读表也不取锁。max_connections 本身作为配置项另有一条低频
           -- 序列（pg.settings / pg.connection.max），供界面显示分母；把分母缓存到跨任务的内存里
           -- 只会换来一份会过期的状态和重启后的一段空窗。
           count(*)::double precision * 100 / current_setting('max_connections')::double precision AS connection_saturation_percent
    FROM activity
), sessions AS (
    SELECT jsonb_build_object(
               'pid', pid, 'username', username, 'database_name', database_name,
               'client_address', client_address, 'state', state,
               'query_started_at', query_start,
               'transaction_started_at', transaction_started_at,
               'query_duration_ms', query_duration_ms,
               'transaction_duration_ms', transaction_duration_ms,
               'wait_event_type', wait_event_type, 'wait_event', wait_event,
               'blocking_pids', blocking_pids
           ) AS value
    FROM activity
    WHERE NOT is_monitor
    ORDER BY pid
    LIMIT 500
), long_queries AS (
    SELECT jsonb_build_object(
               'pid', pid, 'username', username, 'database_name', database_name,
               'client_address', client_address, 'state', state,
               'query_started_at', query_start,
               'transaction_started_at', transaction_started_at,
               'query_duration_ms', query_duration_ms,
               'transaction_duration_ms', transaction_duration_ms,
               'wait_event_type', wait_event_type, 'wait_event', wait_event,
               'blocking_pids', blocking_pids
           ) AS value
    FROM activity
    WHERE state = 'active'
      AND query_start IS NOT NULL
      AND statement_timestamp() - query_start > interval '5 seconds'
      AND NOT is_monitor
    ORDER BY query_start, pid
    LIMIT 100
)
SELECT aggregate.*,
       statement_timestamp() AS snapshot_at,
       COALESCE((SELECT jsonb_agg(value) FROM sessions), '[]'::jsonb) AS sessions,
       (SELECT count(*) FROM activity WHERE NOT is_monitor)::bigint AS session_count,
       (SELECT count(*) > 500 FROM activity WHERE NOT is_monitor) AS sessions_truncated,
       COALESCE((SELECT jsonb_agg(value) FROM long_queries), '[]'::jsonb) AS long_query_samples,
       (SELECT count(*) FROM activity WHERE state = 'active' AND query_start IS NOT NULL
            AND statement_timestamp() - query_start > interval '5 seconds' AND NOT is_monitor)::bigint AS long_query_sample_count,
       (SELECT count(*) > 100 FROM activity WHERE state = 'active' AND query_start IS NOT NULL
            AND statement_timestamp() - query_start > interval '5 seconds' AND NOT is_monitor) AS long_query_samples_truncated
FROM aggregate`,
		Yields: []MetricYield{
			{Metric: MetricConnectionTotal, Columns: []string{"connection_total"}},
			{Metric: MetricConnectionActive, Columns: []string{"connection_active"}},
			{Metric: MetricConnectionIdleInTransaction, Columns: []string{"connection_idle_in_transaction"}},
			{Metric: MetricLongTransactionCount, Columns: []string{"long_transaction_count"}},
			{Metric: MetricMaxTransactionDurationSec, Columns: []string{"max_transaction_duration_sec"}},
			{Metric: MetricLockWaitingCount, Columns: []string{"lock_waiting_count"}},
			{Metric: MetricBlockedSessionCount, Columns: []string{"blocked_session_count"}},
			{Metric: MetricLongRunningQueryCount, Columns: []string{"long_running_query_count"}},
			{Metric: MetricConnectionSaturationPercent, Columns: []string{"connection_saturation_percent"}},
		},
	},
	{
		ID: TaskReplication, Kind: TaskKindSQL, Interval: 5 * time.Second,
		Requires: []CapabilityID{CapabilityRolePGMonitor, CapabilityTopologyHasReplication},
		SQL: `SELECT
       application_name::text AS replica,
       state::text AS connection_state,
       (EXTRACT(epoch FROM replay_lag) * 1000)::double precision AS replay_lag_ms,
       pg_wal_lsn_diff(sent_lsn, replay_lsn)::double precision AS wal_lag_bytes
FROM pg_stat_replication
UNION ALL
SELECT
       COALESCE(sender_host::text, 'standby') AS replica,
       status::text AS connection_state,
       NULL::double precision AS replay_lag_ms,
       pg_wal_lsn_diff(COALESCE(latest_end_lsn, flushed_lsn), pg_last_wal_replay_lsn())::double precision AS wal_lag_bytes
FROM pg_stat_wal_receiver`,
		Yields: []MetricYield{
			{Metric: MetricReplicationConnectionState, Columns: []string{"replica", "connection_state"}, Dimensions: []string{"replica"}},
			{Metric: MetricReplicationReplayLagMS, Columns: []string{"replica", "replay_lag_ms"}, Dimensions: []string{"replica"}},
			{Metric: MetricReplicationWALLagBytes, Columns: []string{"replica", "wal_lag_bytes"}, Dimensions: []string{"replica"}},
		},
	},
	{
		ID: TaskReplicationSlot, Kind: TaskKindSQL, Interval: 5 * time.Second,
		Requires: []CapabilityID{CapabilityRolePGMonitor, CapabilityTopologyHasSlot},
		SQL: `SELECT slot_name::text AS slot, pg_wal_lsn_diff(pg_current_wal_lsn(), COALESCE(confirmed_flush_lsn, restart_lsn))::double precision AS retained_wal_bytes
FROM pg_replication_slots
WHERE COALESCE(confirmed_flush_lsn, restart_lsn) IS NOT NULL`,
		Yields: []MetricYield{{Metric: MetricReplicationSlotRetainedWAL, Columns: []string{"slot", "retained_wal_bytes"}, Dimensions: []string{"slot"}}},
	},
	{
		ID: TaskPreparedXacts, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		SQL: `SELECT database.datname::text AS database,
       count(prepared.gid)::double precision AS prepared_xacts_count
FROM pg_database AS database
LEFT JOIN pg_prepared_xacts AS prepared ON prepared.database = database.datname
WHERE database.datallowconn
GROUP BY database.datname`,
		Yields: []MetricYield{{Metric: MetricPreparedXactsCount, Columns: []string{"database", "prepared_xacts_count"}, Dimensions: []string{DimensionDatabase}}},
	},
	{
		ID: TaskRole, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		SQL: `SELECT CASE
    WHEN pg_is_in_recovery() THEN 'replica'
    WHEN EXISTS (SELECT 1 FROM pg_stat_replication) THEN 'primary'
    ELSE 'standalone'
END::text AS role`,
		Yields: []MetricYield{{Metric: MetricReplicationRole, Columns: []string{"role"}}},
	},
	{
		// max_connections 是配置项，不是会动的数：低频采一次就够，界面拿它当饱和度的分母显示。
		ID: TaskSettings, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		SQL: `SELECT current_setting('max_connections')::double precision AS max_connections`,
		Yields: []MetricYield{{Metric: MetricConnectionMax, Columns: []string{"max_connections"}}},
	},
	{
		// pg_database_size() 在库很多时的开销没有量过（规范「已知的成本」一节），所以体积走低频；
		// 增长率不单独采，由读取侧对这条序列求差得出。仍然是同一条连接：pg_database 是集群级视图。
		ID: TaskDatabaseSize, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		Requires: []CapabilityID{CapabilityRolePGMonitor},
		SQL: `SELECT datname::text AS database,
       pg_database_size(oid)::double precision AS size_bytes
FROM pg_database
WHERE datallowconn AND datname NOT IN ('template0', 'template1')
ORDER BY datname`,
		Yields: []MetricYield{{Metric: MetricDatabaseSizeBytes, Columns: []string{DimensionDatabase, "size_bytes"}, Dimensions: []string{DimensionDatabase}}},
	},
	{
		ID: TaskQueryStatistics, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		Requires: []CapabilityID{CapabilityExtensionPGStatStatements},
		// query 一列取的是 pg_stat_statements 的归一化文本（字面量已是 $1 占位符，
		// 这是该扩展的设计保证），它是**唯一**允许落库的 SQL 文本来源。
		// pg_stat_activity 的原文带真实字面量，TaskStatActivity 的 SQL 里因此没有 query 这一列，
		// 也不许有——见 dictionary_sql_text_test.go 的守卫用例。
		// left(..., 4096) 给单条文本封顶：track_activity_query_size 可以调到很大，
		// 一条几十 KB 的语句对排查没有额外价值，却会把去重表撑起来。
		// COALESCE 到空串是因为 query 可以是 NULL（文本被扩展的外部文件回收，或权限不足）；
		// 空串在采集侧被跳过，不会写成一行「空文本」盖掉上一次采到的真文本。
		SQL: `SELECT queryid,
       dbid AS database_oid,
       userid AS user_oid,
       sum(calls)::bigint AS calls,
       sum(total_exec_time)::double precision AS total_exec_time_ms,
       COALESCE(left(max(query), 4096), '')::text AS query_text
FROM pg_stat_statements
GROUP BY queryid, dbid, userid
ORDER BY total_exec_time_ms DESC, queryid, dbid, userid
LIMIT 500`,
		Yields: nil,
	},
}

func TaskForMetric(metricID MetricID) (Task, bool) {
	for _, task := range Tasks {
		for _, yield := range task.Yields {
			if yield.Metric == metricID {
				return task, true
			}
		}
	}
	return Task{}, false
}

type CapabilityID string

const (
	CapabilityRolePGMonitor             CapabilityID = "role.pg_monitor"
	CapabilityExtensionPGStatStatements CapabilityID = "ext.pg_stat_statements"
	CapabilityTopologyHasReplication    CapabilityID = "topo.has_replication"
	CapabilityTopologyHasSlot           CapabilityID = "topo.has_slot"
)

type CapabilityClass string

const (
	CapabilityClassFixable    CapabilityClass = "fixable"
	CapabilityClassStructural CapabilityClass = "structural"
)

type Capability struct {
	ID       CapabilityID
	Class    CapabilityClass
	Probe    string
	FixHint  string
	NAReason string
}

var Capabilities = []Capability{
	{ID: CapabilityRolePGMonitor, Class: CapabilityClassFixable, Probe: "SELECT pg_has_role(current_user, 'pg_monitor', 'member')", FixHint: "将监控账号加入 pg_monitor 角色。"},
	{ID: CapabilityExtensionPGStatStatements, Class: CapabilityClassFixable, Probe: "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')", FixHint: "预加载并安装 pg_stat_statements 扩展。"},
	{ID: CapabilityTopologyHasReplication, Class: CapabilityClassStructural, Probe: "SELECT pg_is_in_recovery() OR EXISTS (SELECT 1 FROM pg_stat_replication)", NAReason: "本实例为主库且没有备库，复制指标不适用。"},
	{ID: CapabilityTopologyHasSlot, Class: CapabilityClassStructural, Probe: "SELECT EXISTS (SELECT 1 FROM pg_replication_slots)", NAReason: "本实例没有 replication slot。"},
}

func ValidateTaskInterval(id TaskID, interval time.Duration) error {
	if !hasTask(id) {
		return fmt.Errorf("unknown collection task %q", id)
	}
	if interval < MinimumTaskInterval {
		return fmt.Errorf("collection task %q interval must be at least %s", id, MinimumTaskInterval)
	}
	return nil
}

func hasTask(id TaskID) bool {
	for _, task := range Tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}
