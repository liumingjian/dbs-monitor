import type { MetricID } from '../instances.$id/metricOptions'
import type { MonitoringSearch } from '../instances.$id/timeRange'

export type AlertListSearch = {
  tab: 'current' | 'history'
  include_paused: boolean
  instance_id?: string
  page?: number
}

export type InvalidAlertListSearch = { error: string }

type AlertMonitoringContext = {
  metric_id: MetricID
  first_triggered_at?: string
  recovered_at?: string
  updated_at: string
}

export function parseAlertListSearch(search: Record<string, unknown>): AlertListSearch | InvalidAlertListSearch {
  const tab = search.tab ?? 'current'
  const includePaused = parseBoolean(search.include_paused)
  const instanceID = search.instance_id
  const page = parsePage(search.page)

  if ((tab !== 'current' && tab !== 'history')
    || (search.include_paused !== undefined && includePaused === undefined)
    || (instanceID !== undefined && !isUUID(instanceID))
    || (search.page !== undefined && page === undefined)) {
    return { error: '告警筛选链接无效' }
  }

  const result: AlertListSearch = {
    tab,
    include_paused: includePaused ?? false,
    page: page ?? 1,
  }
  if (typeof instanceID === 'string') result.instance_id = instanceID
  return result
}

export function serializeAlertListSearch(value: AlertListSearch): Record<string, string | boolean> {
  const result: Record<string, string | boolean> = {
    tab: value.tab,
    include_paused: value.include_paused,
  }
  if (value.instance_id !== undefined) result.instance_id = value.instance_id
  if (value.page !== undefined) result.page = String(value.page)
  return result
}

export function alertMonitoringSearch(alert: AlertMonitoringContext): MonitoringSearch {
  const observedEnd = new Date(alert.recovered_at ?? alert.updated_at)
  const start = alert.first_triggered_at
    ? new Date(alert.first_triggered_at)
    : new Date(observedEnd.getTime() - 60 * 60 * 1000)
  const end = observedEnd <= start ? new Date(start.getTime() + 60 * 1000) : observedEnd

  return {
    from: start.toISOString(),
    to: end.toISOString(),
    metric: alert.metric_id,
    step: 'auto',
    columns: 2,
    connect: true,
  }
}

function parseBoolean(value: unknown): boolean | undefined {
  if (value === true || value === 'true') return true
  if (value === false || value === 'false') return false
  return undefined
}

function isUUID(value: unknown): value is string {
  return typeof value === 'string'
    && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function parsePage(value: unknown): number | undefined {
  const candidate = typeof value === 'string' ? Number(value) : value
  return typeof candidate === 'number' && Number.isInteger(candidate) && candidate > 0 ? candidate : undefined
}
