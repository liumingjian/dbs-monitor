import type { components } from '../../api/schema'
import { metricUnavailability, type MetricChartSeries } from '../../domain/MetricChart'
import type { MonitoringSearch } from './timeRange'
import { isRFC3339 } from './timeRange'
import { isMetricID, type MetricID } from './metricOptions'

export type PerformanceEventTab = 'firing' | 'recovered' | 'disposed'
export type PerformanceEventDisposition = Extract<components['schemas']['AlertDisposition'], 'ACKED' | 'IGNORED'>

export type PerformanceEventSearch = {
  from: string
  to: string
  tab: PerformanceEventTab
  disposition: PerformanceEventDisposition
  page: number
}

export type InvalidPerformanceEventSearch = { error: string }

type EventMonitoringContext = {
  metric_id: string
  derived_at: string
  recovered_at?: string
  updated_at: string
}

type EventMetricResponse = components['schemas']['MetricSeriesResponse']['metrics'][number]

export type EventMonitoringSearch = MonitoringSearch & {
  metric: MetricID
  step: 'auto'
  columns: 2
  connect: true
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
    || !isPerformanceEventTab(tab)
    || !isPerformanceEventDisposition(disposition)
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

export function eventMonitoringSearch(event: EventMonitoringContext): EventMonitoringSearch | undefined {
  if (!isMetricID(event.metric_id)) return undefined
  const metric = event.metric_id

  const start = new Date(event.derived_at)
  const observedEnd = new Date(event.recovered_at ?? event.updated_at)
  const end = observedEnd <= start ? new Date(start.getTime() + 60_000) : observedEnd
  return {
    from: start.toISOString(),
    to: end.toISOString(),
    metric,
    step: 'auto',
    columns: 2,
    connect: true,
  }
}

export function performanceEventChartView(
  metricID: MetricID,
  responseMetrics: readonly EventMetricResponse[] | undefined,
): {
  series: MetricChartSeries[]
  unavailability: EventMetricResponse['unavailability']
} {
  const metric = responseMetrics?.find((item) => item.metric === metricID)
  if (!metric || metric.unavailability !== null) {
    return { series: [], unavailability: metricUnavailability(metric) }
  }

  return {
    series: metric.series.map((item) => ({ name: metricID, unit: metric.unit, points: item.points })),
    unavailability: null,
  }
}

export function performanceEventRecoveryFilter(tab: PerformanceEventTab): boolean | undefined {
  switch (tab) {
    case 'firing': return false
    case 'recovered': return true
    case 'disposed': return undefined
    default: return assertNever(tab)
  }
}

export function isPerformanceEventTab(value: unknown): value is PerformanceEventTab {
  return value === 'firing' || value === 'recovered' || value === 'disposed'
}

function isPerformanceEventDisposition(value: unknown): value is PerformanceEventDisposition {
  return value === 'ACKED' || value === 'IGNORED'
}

function parsePage(value: unknown): number | undefined {
  const candidate = typeof value === 'string' ? Number(value) : value
  return typeof candidate === 'number' && Number.isInteger(candidate) && candidate > 0 ? candidate : undefined
}

function assertNever(value: never): never {
  throw new Error(`unexpected performance event search value: ${value}`)
}
