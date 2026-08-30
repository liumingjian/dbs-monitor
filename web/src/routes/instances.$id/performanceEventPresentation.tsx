import type { components } from '../../api/schema'
import { StatusBadge, type StatusTone } from '../../primitives/StatusBadge'

type AlertDisposition = components['schemas']['AlertDisposition']
type AlertSeverity = components['schemas']['AlertSeverity']
type PerformanceEventType = components['schemas']['PerformanceEventType']
type SeverityPresentation = {
  label: string
  color: 'error' | 'warning' | 'processing'
}

export function performanceEventTypeLabel(eventType: PerformanceEventType): string {
  switch (eventType) {
    case 'LOCK_BLOCKING': return '锁等待 / 阻塞'
    case 'LONG_TRANSACTION': return '长事务'
    case 'IDLE_IN_TRANSACTION': return 'idle in transaction'
    case 'ACTIVE_SESSIONS_HIGH': return '活跃会话过高'
    case 'REPLICATION_LAG': return '复制延迟'
    case 'TEMP_FILES_SURGE': return '临时文件突增'
    default: return assertNever(eventType)
  }
}

export function PerformanceEventSeverityTag({ severity }: { severity: AlertSeverity }) {
  return <StatusBadge tone={performanceEventSeverityTone(severity)}>
    {performanceEventSeverityPresentation(severity).label}
  </StatusBadge>
}

/// 「维护中」是归因，不是严重度：维护窗口里触发的事件不该和真正的告警抢注意力，
/// 所以走中性档。迁移前它用的是组件库的 processing 蓝，而蓝色只表示可交互。
export function PerformanceEventMaintenanceTag({ inMaintenance }: { inMaintenance: boolean }) {
  return inMaintenance ? <StatusBadge tone="unknown">维护中</StatusBadge> : null
}

export function performanceEventSeverityPresentation(severity: AlertSeverity): SeverityPresentation {
  switch (severity) {
    case 'critical': return { label: '严重', color: 'error' }
    case 'warning': return { label: '警告', color: 'warning' }
    case 'info': return { label: 'Info', color: 'processing' }
    default: return assertNever(severity)
  }
}

/// 严重度 → 展示档位。刻意与上面的 `color` 分开写，而不是把 `color` 翻译一道：
/// `color` 是行为基线用例锁住的旧取值（其中 `processing` 是一支蓝），
/// 档位则是展示层那四档状态语汇 —— `info` 落在中性档，因为状态从不用蓝色表示。
function performanceEventSeverityTone(severity: AlertSeverity): StatusTone {
  switch (severity) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return 'unknown'
    default: return assertNever(severity)
  }
}

export function performanceEventDispositionLabel(disposition: AlertDisposition): string {
  switch (disposition) {
    case 'NONE': return '未处置'
    case 'ACKED': return '已确认'
    case 'IGNORED': return '已忽略'
    default: return assertNever(disposition)
  }
}

export function performanceEventTimeLabel(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString()
}

export function performanceEventDurationLabel(milliseconds: number): string {
  const minutes = Math.floor(milliseconds / 60_000)
  if (minutes < 60) return `${minutes} 分钟`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时`

  return `${Math.floor(hours / 24)} 天`
}

function assertNever(value: never): never {
  throw new Error(`unexpected performance event presentation value: ${value}`)
}
