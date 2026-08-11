import type { MonitoringSearch } from './timeRange'
import { isRFC3339 } from './timeRange'
import { metricOptions } from './metricOptions'

export type PerformanceEventTab = 'firing' | 'recovered' | 'disposed'

export type PerformanceEventSearch = {
  from: string
  to: string
  tab: PerformanceEventTab
  disposition: 'ACKED' | 'IGNORED'
  page: number
}

export type InvalidPerformanceEventSearch = { error: string }

type EventMonitoringContext = {
  metric_id: string
  derived_at: string
  recovered_at?: string
  updated_at: string
}

export function parsePerformanceEventSearch(
  search: Record<string, unknown>,
): PerformanceEventSearch | InvalidPerformanceEventSearch {
  const tab = search.tab ?? 'firing'
  const disposition = search.disposition ?? 'ACKED'
  const page = parsePage(search.page)
  if (!isRFC3339(search.from)
    || !isRFC3339(search.to)
    || new Date(search.to) <= new Date(search.from)
    || (tab !== 'firing' && tab !== 'recovered' && tab !== 'disposed')
    || (disposition !== 'ACKED' && disposition !== 'IGNORED')
    || (search.page !== undefined && page === undefined)) {
    return { error: '性能事件筛选链接无效' }
  }

  return {
    from: new Date(search.from).toISOString(),
    to: new Date(search.to).toISOString(),
    tab,
    disposition,
    page: page ?? 1,
  }
}

export function serializePerformanceEventSearch(
  value: PerformanceEventSearch,
): PerformanceEventSearch {
  return {
    from: value.from,
    to: value.to,
    tab: value.tab,
    disposition: value.disposition,
    page: value.page,
  }
}

export function eventMonitoringSearch(event: EventMonitoringContext): MonitoringSearch | undefined {
  const metric = metricOptions.find((option) => option.id === event.metric_id)
  if (!metric) return undefined

  const start = new Date(event.derived_at)
  const observedEnd = new Date(event.recovered_at ?? event.updated_at)
  const end = observedEnd <= start ? new Date(start.getTime() + 60_000) : observedEnd
  return {
    from: start.toISOString(),
    to: end.toISOString(),
    metric: metric.id,
    step: 'auto',
    columns: 2,
    connect: true,
  }
}

function parsePage(value: unknown): number | undefined {
  const candidate = typeof value === 'string' ? Number(value) : value
  return typeof candidate === 'number' && Number.isInteger(candidate) && candidate > 0 ? candidate : undefined
}
