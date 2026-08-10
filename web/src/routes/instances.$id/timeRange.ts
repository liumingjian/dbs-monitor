import type { MetricID } from './metricOptions'
import { metricOptions } from './metricOptions'

export type TimeRange = { from: string; to: string; metric?: MetricID }
export type InvalidTimeRange = { error: string }

export function parseTimeRange(search: Record<string, unknown>): TimeRange | InvalidTimeRange {
  if (isRFC3339(search.from) && isRFC3339(search.to)) {
    const from = new Date(search.from)
    const to = new Date(search.to)
    if (to <= from) return { error: '结束时间必须晚于开始时间' }
    if (search.metric !== undefined && !isMetricID(search.metric)) return { error: '指标必须来自指标字典' }
    return { from: from.toISOString(), to: to.toISOString(), ...(search.metric === undefined ? {} : { metric: search.metric }) }
  }
  return { error: '时间范围必须是绝对 RFC3339 时间' }
}

export function serializeTimeRange(value: TimeRange): Record<string, string> {
  return value.metric === undefined ? { from: value.from, to: value.to } : { from: value.from, to: value.to, metric: value.metric }
}

export function defaultTimeRange(now = new Date()): TimeRange {
  return { from: new Date(now.getTime() - 60 * 60 * 1000).toISOString(), to: now.toISOString() }
}

function isRFC3339(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value) && !Number.isNaN(Date.parse(value))
}

function isMetricID(value: unknown): value is MetricID {
  return typeof value === 'string' && metricOptions.some((option) => option.id === value)
}
