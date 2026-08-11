package alerting

import "fmt"

type PerformanceEventType string

const (
	PerformanceEventLockBlocking       PerformanceEventType = "LOCK_BLOCKING"
	PerformanceEventLongTransaction    PerformanceEventType = "LONG_TRANSACTION"
	PerformanceEventIdleInTransaction  PerformanceEventType = "IDLE_IN_TRANSACTION"
	PerformanceEventActiveSessionsHigh PerformanceEventType = "ACTIVE_SESSIONS_HIGH"
	PerformanceEventReplicationLag     PerformanceEventType = "REPLICATION_LAG"
	PerformanceEventTempFilesSurge     PerformanceEventType = "TEMP_FILES_SURGE"
)

var performanceEventTypes = []PerformanceEventType{
	PerformanceEventLockBlocking,
	PerformanceEventLongTransaction,
	PerformanceEventIdleInTransaction,
	PerformanceEventActiveSessionsHigh,
	PerformanceEventReplicationLag,
	PerformanceEventTempFilesSurge,
}

func PerformanceEventTypeForMetric(metricID string) (PerformanceEventType, bool) {
	switch metricID {
	case "pg.lock.waiting_count", "pg.session.blocked_count":
		return PerformanceEventLockBlocking, true
	case "pg.transaction.long_count", "pg.transaction.max_duration_sec":
		return PerformanceEventLongTransaction, true
	case "pg.connection.idle_in_transaction":
		return PerformanceEventIdleInTransaction, true
	case "pg.connection.active":
		return PerformanceEventActiveSessionsHigh, true
	case "pg.replication.wal_lag_bytes":
		return PerformanceEventReplicationLag, true
	case "pg.temp.files_per_sec", "pg.temp.bytes_per_sec":
		return PerformanceEventTempFilesSurge, true
	default:
		return "", false
	}
}

type KnowledgeContext struct {
	MetricID     string
	Threshold    string
	TriggerValue string
}

type RenderedKnowledge struct {
	CauseSummary    string
	SuggestedAction string
}

type KnowledgeTemplate struct {
	cause  string
	action string
}

func (template KnowledgeTemplate) Render(context KnowledgeContext) RenderedKnowledge {
	prefix := fmt.Sprintf("指标 %s 的触发值为 %s，达到告警条件（阈值 %s）。", context.MetricID, context.TriggerValue, context.Threshold)
	return RenderedKnowledge{
		CauseSummary:    prefix + template.cause,
		SuggestedAction: template.action,
	}
}

var performanceEventKnowledge = map[PerformanceEventType]KnowledgeTemplate{
	PerformanceEventLockBlocking: {
		cause:  "检测到锁等待或阻塞会话积压，通常由长时间持锁事务或并发访问冲突引起。",
		action: "查看告警触发时的阻塞链，定位最上游阻塞会话，并评估提交、回滚或终止相关事务。",
	},
	PerformanceEventLongTransaction: {
		cause:  "检测到事务持续时间或长事务数量异常，可能阻碍 vacuum 并延长锁持有时间。",
		action: "检查告警触发时的事务会话及其调用方，确认事务边界并优先结束非必要的长事务。",
	},
	PerformanceEventIdleInTransaction: {
		cause:  "检测到会话在事务中空闲，连接可能遗留未提交事务并持续持有锁或旧快照。",
		action: "检查告警触发时处于 idle in transaction 的会话，修正应用事务管理并配置合理超时。",
	},
	PerformanceEventActiveSessionsHigh: {
		cause:  "活跃会话数量异常升高，可能由请求突增、慢查询或连接使用模式变化引起。",
		action: "按持续时间检查告警触发时的活跃会话，结合应用流量确认慢查询和并发来源。",
	},
	PerformanceEventReplicationLag: {
		cause:  "WAL 字节延迟升高，说明主库生成 WAL 的速度超过副本接收或回放速度。",
		action: "检查副本连接、网络吞吐、磁盘延迟和回放进程，并确认是否存在长查询阻碍回放。",
	},
	PerformanceEventTempFilesSurge: {
		cause:  "临时文件生成速率突增，通常表示排序、哈希或聚合操作超出可用工作内存。",
		action: "定位同期负载与慢查询，检查执行计划，并在评估并发内存预算后调整查询或 work_mem。",
	},
}

func KnowledgeTemplateForEventType(eventType PerformanceEventType) (KnowledgeTemplate, bool) {
	template, ok := performanceEventKnowledge[eventType]
	return template, ok
}
