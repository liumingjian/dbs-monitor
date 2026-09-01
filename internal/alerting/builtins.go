package alerting

import "github.com/liumingjian/dbs-monitor/internal/api"

type BuiltinRule struct {
	ID                        string
	Identifier                string
	Name                      string
	MetricID                  string
	Aggregation               string
	Operator                  string
	Threshold                 float64
	RecoveryOperator          string
	RecoveryThreshold         float64
	WindowSeconds             int
	ConsecutiveCount          int
	RecoveryConsecutiveCount  int
	Severity                  string
	NoDataPolicy              string
	EvaluationIntervalSeconds int
	Enabled                   bool
}

type BuiltinRuleChange struct {
	Delete   bool
	Enabled  *bool
	Severity *api.AlertSeverity
}

func BuiltinRuleRestriction(isBuiltin bool, change BuiltinRuleChange) api.ErrorErrorCode {
	if !isBuiltin {
		return ""
	}
	if change.Delete {
		return api.BUILTINRULEDELETEFORBIDDEN
	}
	if change.Enabled != nil && !*change.Enabled {
		return api.BUILTINRULEDISABLEFORBIDDEN
	}
	if change.Severity != nil && *change.Severity == api.Info {
		return api.BUILTINRULESEVERITYTOOLOW
	}
	return ""
}

var BuiltinCollectionRules = []BuiltinRule{
	{
		ID: "00000000-0000-0000-0000-000000063001", Identifier: "database_unreachable", Name: "数据库不可达",
		MetricID: string(api.MetricIdPgAvailabilityReachable), Aggregation: "latest",
		Operator: "=", Threshold: 0, RecoveryOperator: "=", RecoveryThreshold: 1,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30, Enabled: true,
	},
	{
		ID: "00000000-0000-0000-0000-000000063002", Identifier: "agent_offline", Name: "Agent 离线",
		MetricID: string(api.MetricIdAgentStatus), Aggregation: "latest",
		Operator: "=", Threshold: 0, RecoveryOperator: "=", RecoveryThreshold: 1,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30, Enabled: true,
	},
	{
		ID: "00000000-0000-0000-0000-000000063003", Identifier: "data_stale", Name: "数据过期",
		MetricID: string(api.MetricIdCollectorLastSuccessTime), Aggregation: "latest",
		Operator: ">", Threshold: 600, RecoveryOperator: "<", RecoveryThreshold: 450,
		WindowSeconds: 60, ConsecutiveCount: 2, RecoveryConsecutiveCount: 2,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60, Enabled: true,
	},
}

const (
	DefaultNotificationPolicyID         = "00000000-0000-0000-0000-000000063000"
	DefaultNotificationPolicyIdentifier = "default"
	DefaultNotificationPolicyName       = "默认策略"
)

type RuleTemplate struct {
	Identifier                string
	Version                   int
	Name                      string
	MetricID                  string
	Aggregation               string
	Operator                  string
	Threshold                 float64
	RecoveryOperator          string
	RecoveryThreshold         float64
	WindowSeconds             int
	ConsecutiveCount          int
	RecoveryConsecutiveCount  int
	Severity                  string
	NoDataPolicy              string
	EvaluationIntervalSeconds int
}

var BuiltinRuleTemplates = []RuleTemplate{
	{
		Identifier: "probe_latency_high", Version: 1, Name: "探针延迟过高",
		MetricID: "pg.probe.latency_ms", Aggregation: "avg", Operator: ">=", Threshold: 500,
		RecoveryOperator: "<=", RecoveryThreshold: 300,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "cpu_high", Version: 1, Name: "CPU 高",
		MetricID: "host.cpu.usage_percent", Aggregation: "avg", Operator: ">=", Threshold: 80,
		RecoveryOperator: "<=", RecoveryThreshold: 70,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "memory_high", Version: 1, Name: "内存高",
		MetricID: "host.memory.usage_percent", Aggregation: "avg", Operator: ">=", Threshold: 85,
		RecoveryOperator: "<=", RecoveryThreshold: 75,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "disk_usage_high", Version: 1, Name: "磁盘使用率高",
		MetricID: "host.disk.usage_percent", Aggregation: "latest", Operator: ">=", Threshold: 90,
		RecoveryOperator: "<=", RecoveryThreshold: 85,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "connections_high", Version: 1, Name: "连接数过高",
		MetricID: "pg.connection.total", Aggregation: "max", Operator: ">=", Threshold: 500,
		RecoveryOperator: "<=", RecoveryThreshold: 400,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "active_connections_high", Version: 1, Name: "活跃连接数过高",
		MetricID: "pg.connection.active", Aggregation: "max", Operator: ">=", Threshold: 100,
		RecoveryOperator: "<=", RecoveryThreshold: 80,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "idle_in_transaction_high", Version: 1, Name: "idle in transaction 过多",
		MetricID: "pg.connection.idle_in_transaction", Aggregation: "latest", Operator: ">=", Threshold: 10,
		RecoveryOperator: "<=", RecoveryThreshold: 5,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "long_transaction", Version: 1, Name: "长事务",
		MetricID: "pg.transaction.max_duration_sec", Aggregation: "max", Operator: ">=", Threshold: 300,
		RecoveryOperator: "<=", RecoveryThreshold: 60,
		WindowSeconds: 30, ConsecutiveCount: 2, RecoveryConsecutiveCount: 2,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "lock_waiting", Version: 1, Name: "锁等待",
		MetricID: "pg.lock.waiting_count", Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "blocked_sessions", Version: 1, Name: "阻塞会话",
		MetricID: "pg.session.blocked_count", Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "long_queries_high", Version: 1, Name: "长查询数量过多",
		MetricID: "pg.query.long_running_count", Aggregation: "latest", Operator: ">=", Threshold: 5,
		RecoveryOperator: "<=", RecoveryThreshold: 2,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "replication_wal_lag", Version: 1, Name: "复制延迟（WAL 字节）",
		MetricID: "pg.replication.wal_lag_bytes", Aggregation: "avg", Operator: ">=", Threshold: 104_857_600,
		RecoveryOperator: "<=", RecoveryThreshold: 52_428_800,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "replication_slot_backlog", Version: 1, Name: "Slot 积压",
		MetricID: "pg.replication_slot.retained_wal_bytes", Aggregation: "latest", Operator: ">=", Threshold: 1_073_741_824,
		RecoveryOperator: "<=", RecoveryThreshold: 536_870_912,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "temp_bytes_high", Version: 1, Name: "临时文件写入过高",
		MetricID: "pg.temp.bytes_per_sec", Aggregation: "avg", Operator: ">=", Threshold: 10_485_760,
		RecoveryOperator: "<=", RecoveryThreshold: 5_242_880,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "prepared_xacts", Version: 1, Name: "2PC 数量异常",
		MetricID: "pg.prepared_xacts.count", Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "info", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
}
