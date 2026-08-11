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
	ID                MetricID
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
	{ID: MetricAvailabilityReachable, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricProbeLatencyMS, Type: MetricTypeGauge, Unit: "ms", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricCollectorLastSuccessTime, Type: MetricTypeState, Unit: "timestamp", Dimensions: []string{"instance", "source_type"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerControlPlane},
	{ID: MetricAgentStatus, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance", "node"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerControlPlane},
	{ID: MetricHostCPUUsagePercent, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostMemoryUsagePercent, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskUsagePercent, Type: MetricTypeGauge, Unit: "percent", Dimensions: []string{"instance", "node", "mount"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskFreeBytes, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "node", "mount"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskIOPS, Type: MetricTypeRate, Unit: "ops/s", Dimensions: []string{"instance", "node", "device"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostDiskThroughputBytesPerS, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance", "node", "device"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerAgent},
	{ID: MetricHostNetworkBytesPerS, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance", "node", "interface"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerAgent},
	{ID: MetricConnectionTotal, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricConnectionActive, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricConnectionIdleInTransaction, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricTPS, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricXactCommitPerS, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricXactRollbackPerS, Type: MetricTypeRate, Unit: "tx/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTuplesReadPerS, Type: MetricTypeRate, Unit: "rows/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTuplesWritePerS, Type: MetricTypeRate, Unit: "rows/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityConditional, Producer: ProducerServerTask},
	{ID: MetricTempFilesPerS, Type: MetricTypeRate, Unit: "files/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricTempBytesPerS, Type: MetricTypeRate, Unit: "bytes/s", Dimensions: []string{"instance"}, Calculation: CalculationCounterDelta, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLongTransactionCount, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricMaxTransactionDurationSec, Type: MetricTypeGauge, Unit: "seconds", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLockWaitingCount, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricBlockedSessionCount, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricLongRunningQueryCount, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricPreparedXactsCount, Type: MetricTypeGauge, Unit: "count", Dimensions: []string{"instance", "database"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationRole, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: false, Alertability: AlertabilityNo, Producer: ProducerServerTask},
	{ID: MetricReplicationConnectionState, Type: MetricTypeState, Unit: "state", Dimensions: []string{"instance", "replica"}, Calculation: CalculationStateMapping, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationReplayLagMS, Type: MetricTypeGauge, Unit: "ms", Dimensions: []string{"instance", "replica"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityNo, Producer: ProducerServerTask},
	{ID: MetricReplicationWALLagBytes, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "replica"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
	{ID: MetricReplicationSlotRetainedWAL, Type: MetricTypeGauge, Unit: "bytes", Dimensions: []string{"instance", "slot"}, Calculation: CalculationRaw, Standard: true, EnhancedCandidate: true, Alertability: AlertabilityYes, Producer: ProducerServerTask},
}

func UnitFor(id MetricID) string {
	for _, item := range Metrics {
		if item.ID == id {
			return item.Unit
		}
	}
	return "count"
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
	TaskQueryStatistics TaskID = "pg.stat_statements"
)

type TaskKind string

const (
	TaskKindProbe        TaskKind = "probe"
	TaskKindSQL          TaskKind = "sql"
	TaskKindAgentDerived TaskKind = "agent-derived"
)

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
		SQL: `SELECT
	       COALESCE(sum(xact_commit), 0)::double precision AS xact_commit,
	       COALESCE(sum(xact_rollback), 0)::double precision AS xact_rollback,
	       COALESCE(sum(tup_returned + tup_fetched), 0)::double precision AS tuples_read,
	       COALESCE(sum(tup_inserted + tup_updated + tup_deleted), 0)::double precision AS tuples_write,
	       COALESCE(sum(temp_files), 0)::double precision AS temp_files,
	       COALESCE(sum(temp_bytes), 0)::double precision AS temp_bytes
FROM pg_stat_database
WHERE datname NOT IN ('template0', 'template1')`,
		Yields: []MetricYield{
			{Metric: MetricTPS, Columns: []string{"xact_commit", "xact_rollback"}},
			{Metric: MetricXactCommitPerS, Columns: []string{"xact_commit"}},
			{Metric: MetricXactRollbackPerS, Columns: []string{"xact_rollback"}},
			{Metric: MetricTuplesReadPerS, Columns: []string{"tuples_read"}},
			{Metric: MetricTuplesWritePerS, Columns: []string{"tuples_write"}},
			{Metric: MetricTempFilesPerS, Columns: []string{"temp_files"}},
			{Metric: MetricTempBytesPerS, Columns: []string{"temp_bytes"}},
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
           count(*) FILTER (WHERE state = 'active' AND query_start IS NOT NULL AND statement_timestamp() - query_start > interval '5 seconds' AND NOT is_monitor)::double precision AS long_running_query_count
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
		Yields: []MetricYield{{Metric: MetricPreparedXactsCount, Columns: []string{"database", "prepared_xacts_count"}, Dimensions: []string{"database"}}},
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
		ID: TaskQueryStatistics, Kind: TaskKindSQL, Interval: 5 * time.Minute,
		Requires: []CapabilityID{CapabilityExtensionPGStatStatements},
		SQL: `SELECT queryid, dbid, userid, calls, total_exec_time
FROM pg_stat_statements`,
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
