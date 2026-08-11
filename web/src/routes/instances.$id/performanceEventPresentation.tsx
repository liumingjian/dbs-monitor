import { Tag } from 'antd'
import type { components } from '../../api/schema'

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
  const presentation = performanceEventSeverityPresentation(severity)
  return <Tag color={presentation.color}>{presentation.label}</Tag>
}

export function performanceEventSeverityPresentation(severity: AlertSeverity): SeverityPresentation {
  switch (severity) {
    case 'critical': return { label: '严重', color: 'error' }
    case 'warning': return { label: '警告', color: 'warning' }
    case 'info': return { label: 'Info', color: 'processing' }
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
