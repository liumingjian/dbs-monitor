import type { MetricID } from './metricOptions'
import { metricOptions } from './metricOptions'

export type InvalidTimeRange = { error: string }
export type MetricStep = 'auto' | '15s' | '1m' | '5m' | 'raw'
export type ChartColumns = 1 | 2 | 3
export type MonitoringView = 'standard' | 'enhanced'

export type MonitoringSearch = {
  from: string
  to: string
  metric?: MetricID
  step?: MetricStep
  columns?: ChartColumns
  connect?: boolean
  monitoring?: MonitoringView
}

export function parseTimeRange(search: Record<string, unknown>): MonitoringSearch | InvalidTimeRange {
  if (!isRFC3339(search.from) || !isRFC3339(search.to)) return { error: '时间范围必须是绝对 RFC3339 时间' }

  const from = new Date(search.from)
  const to = new Date(search.to)
  if (to <= from) return { error: '结束时间必须晚于开始时间' }

  const monitoring = search.monitoring
  if (monitoring !== undefined && monitoring !== 'standard' && monitoring !== 'enhanced') return { error: '监控页签参数无效' }

  const metric = search.metric
  if (metric !== undefined && !isMetricID(metric)) return { error: '指标必须来自指标字典' }

  const step = search.step
  if (step !== undefined && !isMetricStep(step)) return { error: '粒度必须来自支持的选项' }
  if (monitoring === 'enhanced' && step !== 'raw') return { error: '增强监控固定读取原始粒度' }
  if (monitoring === 'enhanced' && enhancedWindowMinutes(from, to) === undefined) {
    return { error: '增强监控时间窗口必须是 30 分钟、1 小时、3 小时或 6 小时' }
  }

  const columns = parseColumns(search.columns)
  if (search.columns !== undefined && columns === undefined) return { error: '列数必须是 1、2 或 3' }

  const connect = parseConnect(search.connect)
  if (search.connect !== undefined && connect === undefined) return { error: '光标联动参数无效' }

  const result: MonitoringSearch = { from: from.toISOString(), to: to.toISOString() }
  if (metric !== undefined) result.metric = metric
  if (step !== undefined) result.step = step
  if (columns !== undefined) result.columns = columns
  if (connect !== undefined) result.connect = connect
  if (monitoring !== undefined) result.monitoring = monitoring
  return result
}

export function serializeTimeRange(value: MonitoringSearch): Record<string, string | number | boolean> {
  const result: Record<string, string | number | boolean> = { from: value.from, to: value.to }
  if (value.metric !== undefined) result.metric = value.metric
  if (value.step !== undefined) result.step = value.step
  if (value.columns !== undefined) result.columns = value.columns
  if (value.connect !== undefined) result.connect = value.connect
  if (value.monitoring !== undefined) result.monitoring = value.monitoring
  return result
}

export function defaultEnhancedTimeRange(now = new Date()): MonitoringSearch {
  return {
    from: new Date(now.getTime() - 30 * 60 * 1000).toISOString(),
    to: now.toISOString(),
    monitoring: 'enhanced',
    step: 'raw',
  }
}

export function enhancedWindowMinutes(from: Date, to: Date): 30 | 60 | 180 | 360 | undefined {
  const minutes = (to.getTime() - from.getTime()) / 60_000
  return minutes === 30 || minutes === 60 || minutes === 180 || minutes === 360 ? minutes : undefined
}

export function defaultTimeRange(now = new Date()): MonitoringSearch {
  return {
    from: new Date(now.getTime() - 60 * 60 * 1000).toISOString(),
    to: now.toISOString(),
    step: 'auto',
    columns: 2,
    connect: true,
  }
}

function isRFC3339(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value) && !Number.isNaN(Date.parse(value))
}

function isMetricID(value: unknown): value is MetricID {
  return typeof value === 'string' && metricOptions.some((option) => option.id === value)
}

function isMetricStep(value: unknown): value is MetricStep {
  return value === 'auto' || value === '15s' || value === '1m' || value === '5m' || value === 'raw'
}

function parseColumns(value: unknown): ChartColumns | undefined {
  const candidate = typeof value === 'string' ? Number(value) : value
  return candidate === 1 || candidate === 2 || candidate === 3 ? candidate : undefined
}

function parseConnect(value: unknown): boolean | undefined {
  if (value === true || value === 'true') return true
  if (value === false || value === 'false') return false
  return undefined
}
