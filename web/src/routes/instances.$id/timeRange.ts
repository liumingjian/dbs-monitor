import type { MetricID } from './metricOptions'
import { metricOptions } from './metricOptions'

export type TimeRange = { from: string; to: string; metric?: MetricID }
export type InvalidTimeRange = { error: string }
export type MetricStep = 'auto' | '15s' | '1m' | '5m' | 'raw'
export type ChartColumns = 1 | 2 | 3

export type MonitoringSearch = TimeRange & {
  step?: MetricStep
  columns?: ChartColumns
  connect?: boolean
}

export function parseTimeRange(search: Record<string, unknown>): MonitoringSearch | InvalidTimeRange {
  if (isRFC3339(search.from) && isRFC3339(search.to)) {
    const from = new Date(search.from)
    const to = new Date(search.to)
    if (to <= from) return { error: '结束时间必须晚于开始时间' }
    if (search.metric !== undefined && !isMetricID(search.metric)) return { error: '指标必须来自指标字典' }
    if (search.step !== undefined && !isMetricStep(search.step)) return { error: '粒度必须来自支持的选项' }
    const columns = parseColumns(search.columns)
    if (search.columns !== undefined && columns === undefined) return { error: '列数必须是 1、2 或 3' }
    const connect = parseConnect(search.connect)
    if (search.connect !== undefined && connect === undefined) return { error: '光标联动参数无效' }
    return {
      from: from.toISOString(),
      to: to.toISOString(),
      ...(search.metric === undefined ? {} : { metric: search.metric }),
      ...(search.step === undefined ? {} : { step: search.step }),
      ...(columns === undefined ? {} : { columns }),
      ...(connect === undefined ? {} : { connect }),
    }
  }
  return { error: '时间范围必须是绝对 RFC3339 时间' }
}

export function serializeTimeRange(value: MonitoringSearch): Record<string, string | number | boolean> {
  return {
    from: value.from,
    to: value.to,
    ...(value.metric === undefined ? {} : { metric: value.metric }),
    ...(value.step === undefined ? {} : { step: value.step }),
    ...(value.columns === undefined ? {} : { columns: value.columns }),
    ...(value.connect === undefined ? {} : { connect: value.connect }),
  }
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
