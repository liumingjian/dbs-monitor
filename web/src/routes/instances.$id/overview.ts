import type { components } from '../../api/schema'
import { metricUnavailability } from '../../domain/MetricChart'
import type { Unavailability } from '../../domain/UnavailabilityBlock'
import type { MetricID } from './metricOptions'
import { serializeTimeRange, type MonitoringSearch } from './timeRange'

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]

export const OVERVIEW_MODULES = [
  'availability',
  'alerts',
  'resources',
  'database',
  'replication',
  'events',
  'troubleshooting',
] as const

export type OverviewModule = typeof OVERVIEW_MODULES[number]

export const overviewMetricGroups = {
  availability: ['pg.availability.reachable', 'pg.probe.latency_ms'],
  resources: [
    'host.cpu.usage_percent',
    'host.memory.usage_percent',
    'host.disk.usage_percent',
    'host.disk.iops',
    'host.network.bytes_per_sec',
  ],
  database: [
    'pg.connection.total',
    'pg.connection.active',
    'pg.tps',
    'pg.temp.bytes_per_sec',
    'pg.transaction.long_count',
    'pg.lock.waiting_count',
  ],
  replication: [
    'pg.replication.role',
    'pg.replication.connection_state',
    'pg.replication.wal_lag_bytes',
    'pg.replication_slot.retained_wal_bytes',
  ],
} as const satisfies Record<'availability' | 'resources' | 'database' | 'replication', readonly MetricID[]>

export const overviewMetricIDs: MetricID[] = Object.values(overviewMetricGroups).flat()

export type LatestMetricFact = {
  labels: Record<string, string>
  value: number | null
  sampledAt: number
}

export type LatestMetricFacts = {
  unavailability: Unavailability | null
  unit: string
  facts: LatestMetricFact[]
}

export function latestMetricFacts(metric: ResponseMetric | undefined): LatestMetricFacts {
  const unavailability = metricUnavailability(metric)
  if (!metric || unavailability) {
    return { unavailability, unit: metric?.unit ?? '', facts: [] }
  }

  const facts: LatestMetricFact[] = []
  for (const series of metric.series) {
    let latestPoint: (number | null)[] | undefined
    for (const point of series.points) {
      const sampledAt = point[0]
      if (typeof sampledAt !== 'number') continue
      if (!latestPoint || typeof latestPoint[0] !== 'number' || sampledAt > latestPoint[0]) {
        latestPoint = point
      }
    }
    if (!latestPoint || typeof latestPoint[0] !== 'number') continue

    facts.push({
      labels: series.labels,
      value: typeof latestPoint[1] === 'number' ? latestPoint[1] : null,
      sampledAt: latestPoint[0],
    })
  }

  return {
    unavailability: facts.length === 0 ? 'NO_SAMPLES_YET' : null,
    unit: metric.unit,
    facts,
  }
}

export const performanceEventsEmptyState = {
  title: '近期没有性能事件',
  description: '所选时间范围内没有触发中或已恢复的性能事件。',
} as const

export function overviewDestinations(id: string, search: MonitoringSearch) {
  const instancePath = `/instances/${encodeURIComponent(id)}`
  return {
    monitoring: withSearch(`${instancePath}/monitoring`, serializeTimeRange(search)),
    sessions: withSearch(`${instancePath}/sessions`, { from: search.from, to: search.to, filter: 'lock_wait' }),
    alerts: withSearch(`${instancePath}/alerts`, { from: search.from, to: search.to }),
    collection: withSearch(`${instancePath}/collection`, search.metric ? { metric: search.metric } : {}),
    maintenance: withSearch('/alert-settings/maintenance-windows/new', { instance_id: id }),
  }
}

function withSearch(path: string, search: Record<string, string | number | boolean>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) params.set(key, String(value))
  const query = params.toString()
  return query ? `${path}?${query}` : path
}
