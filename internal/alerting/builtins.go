package alerting

import (
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/dbengine"
)

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

// RuleTemplate 是一份内置告警规则模板。
//
// **引擎归属**：模板引用一个指标，那个指标属于哪个引擎，模板就属于哪个引擎（Engine），
// 填的是哪个语义位，模板就带哪个位（Slot）。两者都是目录里那一行的转写，不是另立的事实——
// builtins_test.go 逐条比对，写歪了就红。归属决定可见性：
//
//   - Slot 非空 → 一份两用，任何把这个位绑上了指标的引擎都看得见、都能建规则；
//   - Engine 是 AGNOSTIC（host.* / agent.* / collector.*）→ 与数据库产品无关，处处可见；
//   - 其余（Engine 具体、Slot 为空）→ 引擎私有指标（WAL 保留、复制槽、prepared xacts、
//     临时文件……），只在该引擎的实例上可见。
//
// 判定统一走 metric.AppliesToEngine / ResolveForEngine，模板、规则作用域、评估三处同一个函数。
//
// Slot 的类型是字符串而不是 metric.SemanticSlot：internal/alerting 与 internal/metric 同层，
// 依赖只能向下（internal/arch_test.go），所以字面量写在这里、由 builtins_test.go 比对。
type RuleTemplate struct {
	Identifier                string
	Version                   int
	Name                      string
	MetricID                  string
	Engine                    dbengine.Engine
	Slot                      string
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
		MetricID: "pg.probe.latency_ms", Engine: dbengine.PostgreSQL, Slot: "probe_latency", Aggregation: "avg", Operator: ">=", Threshold: 500,
		RecoveryOperator: "<=", RecoveryThreshold: 300,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "cpu_high", Version: 1, Name: "CPU 高",
		MetricID: "host.cpu.usage_percent", Engine: dbengine.Agnostic, Aggregation: "avg", Operator: ">=", Threshold: 80,
		RecoveryOperator: "<=", RecoveryThreshold: 70,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "memory_high", Version: 1, Name: "内存高",
		MetricID: "host.memory.usage_percent", Engine: dbengine.Agnostic, Aggregation: "avg", Operator: ">=", Threshold: 85,
		RecoveryOperator: "<=", RecoveryThreshold: 75,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "disk_usage_high", Version: 1, Name: "磁盘使用率高",
		MetricID: "host.disk.usage_percent", Engine: dbengine.Agnostic, Aggregation: "latest", Operator: ">=", Threshold: 90,
		RecoveryOperator: "<=", RecoveryThreshold: 85,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "connections_high", Version: 1, Name: "连接数过高",
		MetricID: "pg.connection.total", Engine: dbengine.PostgreSQL, Slot: "connections", Aggregation: "max", Operator: ">=", Threshold: 500,
		RecoveryOperator: "<=", RecoveryThreshold: 400,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "active_connections_high", Version: 1, Name: "活跃连接数过高",
		MetricID: "pg.connection.active", Engine: dbengine.PostgreSQL, Aggregation: "max", Operator: ">=", Threshold: 100,
		RecoveryOperator: "<=", RecoveryThreshold: 80,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "idle_in_transaction_high", Version: 1, Name: "idle in transaction 过多",
		MetricID: "pg.connection.idle_in_transaction", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">=", Threshold: 10,
		RecoveryOperator: "<=", RecoveryThreshold: 5,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "long_transaction", Version: 1, Name: "长事务",
		MetricID: "pg.transaction.max_duration_sec", Engine: dbengine.PostgreSQL, Aggregation: "max", Operator: ">=", Threshold: 300,
		RecoveryOperator: "<=", RecoveryThreshold: 60,
		WindowSeconds: 30, ConsecutiveCount: 2, RecoveryConsecutiveCount: 2,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "lock_waiting", Version: 1, Name: "锁等待",
		MetricID: "pg.lock.waiting_count", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "blocked_sessions", Version: 1, Name: "阻塞会话",
		MetricID: "pg.session.blocked_count", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "critical", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "long_queries_high", Version: 1, Name: "长查询数量过多",
		MetricID: "pg.query.long_running_count", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">=", Threshold: 5,
		RecoveryOperator: "<=", RecoveryThreshold: 2,
		WindowSeconds: 30, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 30,
	},
	{
		Identifier: "replication_wal_lag", Version: 1, Name: "复制延迟（WAL 字节）",
		MetricID: "pg.replication.wal_lag_bytes", Engine: dbengine.PostgreSQL, Aggregation: "avg", Operator: ">=", Threshold: 104_857_600,
		RecoveryOperator: "<=", RecoveryThreshold: 52_428_800,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "replication_slot_backlog", Version: 1, Name: "Slot 积压",
		MetricID: "pg.replication_slot.retained_wal_bytes", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">=", Threshold: 1_073_741_824,
		RecoveryOperator: "<=", RecoveryThreshold: 536_870_912,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "temp_bytes_high", Version: 1, Name: "临时文件写入过高",
		MetricID: "pg.temp.bytes_per_sec", Engine: dbengine.PostgreSQL, Aggregation: "avg", Operator: ">=", Threshold: 10_485_760,
		RecoveryOperator: "<=", RecoveryThreshold: 5_242_880,
		WindowSeconds: 60, ConsecutiveCount: 5, RecoveryConsecutiveCount: 5,
		Severity: "warning", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
	{
		Identifier: "prepared_xacts", Version: 1, Name: "2PC 数量异常",
		MetricID: "pg.prepared_xacts.count", Engine: dbengine.PostgreSQL, Aggregation: "latest", Operator: ">", Threshold: 0,
		RecoveryOperator: "=", RecoveryThreshold: 0,
		WindowSeconds: 60, ConsecutiveCount: 3, RecoveryConsecutiveCount: 3,
		Severity: "info", NoDataPolicy: "mark_no_data", EvaluationIntervalSeconds: 60,
	},
}
