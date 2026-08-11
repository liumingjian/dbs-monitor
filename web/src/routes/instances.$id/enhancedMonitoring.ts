import type { components } from '../../api/schema'
import type { MetricChartSeries } from '../../domain/MetricChart'
import type { Unavailability } from '../../domain/UnavailabilityBlock'
import { metricOptions, type MetricID } from './metricOptions'
import { findStandardMonitoringChart } from './standardMonitoring'

export type EnhancedAggregation = 'average' | 'maximum' | 'minimum'
export type EnhancedColumns = 1 | 2 | 3
export type EnhancedPoint = [number, number | null]
export type EnhancedPreferences = {
  metrics: MetricID[]
  aggregation: EnhancedAggregation
  columns: EnhancedColumns
}

type CollectionTaskResult = components['schemas']['CollectionTaskResult']

export const enhancedMonitoringDefaults = {
  windowMinutes: 30,
  step: 'raw',
  aggregation: 'average',
  columns: 2,
} as const

export const enhancedMonitoringMetricIDs: MetricID[] = metricOptions
  .filter((option) => option.enhancedCandidate)
  .map((option) => option.id)

export const enhancedWindowOptions = [
  { minutes: 30, label: '30 分钟' },
  { minutes: 60, label: '1 小时' },
  { minutes: 180, label: '3 小时' },
  { minutes: 360, label: '6 小时' },
] as const

export const enhancedMonitoringGroups = [
  {
    key: 'resource',
    title: '资源指标',
    metrics: enhancedMonitoringMetricIDs.filter((metric) => metric.startsWith('host.')),
  },
  {
    key: 'database',
    title: '数据库指标',
    metrics: enhancedMonitoringMetricIDs.filter((metric) => metric.startsWith('pg.') && !metric.startsWith('pg.replication')),
  },
  {
    key: 'replication',
    title: '复制指标',
    metrics: enhancedMonitoringMetricIDs.filter((metric) => metric.startsWith('pg.replication')),
  },
] as const

export function enhancedDisplayBucketSeconds(windowMinutes: 30 | 60 | 180 | 360): number {
  switch (windowMinutes) {
    case 30: return 5
    case 60: return 10
    case 180: return 30
    case 360: return 60
    default: return assertNever(windowMinutes)
  }
}

export function enhancedMetricDescription(metricID: MetricID): string {
  const standardDescription = findStandardMonitoringChart(metricID)?.description
  if (standardDescription) return standardDescription
  switch (metricID) {
    case 'pg.availability.reachable': return '通过新建连接、认证并执行轻量探针判断 PostgreSQL 是否可达。'
    case 'pg.probe.latency_ms': return '新建连接、认证并完成轻量探针的端到端耗时。'
    default: throw new Error(`missing metric description: ${metricID}`)
  }
}

export function aggregateEnhancedPoints(
  points: readonly (readonly (number | null)[])[],
  aggregation: EnhancedAggregation,
  bucketSeconds: number,
): EnhancedPoint[] {
  const buckets = new Map<number, { timestamp: number; values: number[] }>()
  for (const point of points) {
    const timestamp = point[0]
    if (typeof timestamp !== 'number') continue
    const value = point[1] ?? null
    const key = Math.floor(timestamp / bucketSeconds)
    const bucket = buckets.get(key) ?? { timestamp, values: [] }
    if (value !== null) bucket.values.push(value)
    buckets.set(key, bucket)
  }

  return [...buckets.values()].map((bucket) => [
    bucket.timestamp,
    aggregateValues(bucket.values, aggregation),
  ])
}

type EnhancedResponseMetric = {
  metric: string
  unit: string
  unavailability: Unavailability | null
  series: readonly {
    labels: Record<string, string>
    points: readonly (readonly (number | null)[])[]
  }[]
}

export function buildEnhancedChartView(
  metricID: MetricID,
  responseMetrics: readonly EnhancedResponseMetric[] | undefined,
  aggregation: EnhancedAggregation,
  bucketSeconds: number,
): { series: MetricChartSeries[]; unavailability: Unavailability | null } {
  const response = responseMetrics?.find((metric) => metric.metric === metricID)
  if (!response) return { series: [], unavailability: 'NO_SAMPLES_YET' }
  if (response.unavailability !== null) return { series: [], unavailability: response.unavailability }
  return {
    series: response.series.map((item) => ({
      name: enhancedSeriesName(metricID, item.labels),
      unit: response.unit,
      points: aggregateEnhancedPoints(item.points, aggregation, bucketSeconds),
    })),
    unavailability: null,
  }
}

function enhancedSeriesName(metricID: MetricID, labels: Record<string, string>): string {
  const dimensions = Object.entries(labels).map(([key, value]) => `${key}=${value}`).join(', ')
  const label = metricOptions.find((option) => option.id === metricID)?.label ?? metricID
  return dimensions ? `${label} · ${dimensions}` : label
}

function aggregateValues(values: number[], aggregation: EnhancedAggregation): number | null {
  if (values.length === 0) return null
  switch (aggregation) {
    case 'average': return values.reduce((sum, value) => sum + value, 0) / values.length
    case 'maximum': return Math.max(...values)
    case 'minimum': return Math.min(...values)
    default: return assertNever(aggregation)
  }
}

export function parseEnhancedPreferences(value: unknown): EnhancedPreferences {
  const defaults: EnhancedPreferences = {
    metrics: enhancedMonitoringMetricIDs,
    aggregation: enhancedMonitoringDefaults.aggregation,
    columns: enhancedMonitoringDefaults.columns,
  }
  if (!isRecord(value) || !Array.isArray(value.metrics)) return defaults
  if (!value.metrics.every(isEnhancedMetricID) || new Set(value.metrics).size !== value.metrics.length) return defaults
  if (!isEnhancedAggregation(value.aggregation) || !isEnhancedColumns(value.columns)) return defaults
  return { metrics: value.metrics, aggregation: value.aggregation, columns: value.columns }
}

export function enhancedUnavailabilityDetail(
  code: Unavailability,
  taskResult: CollectionTaskResult | undefined,
): string | undefined {
  if (code === 'DB_UNREACHABLE') return '目标库故障：平台无法连接目标 PostgreSQL。'
  if (taskResult === 'SKIPPED_BACKPRESSURE') {
    return '平台自我保护：最近一次采集因背压被跳过；该空隙不代表数据库不可达或采集配置错误。'
  }
  if (taskResult === 'BACKOFF') {
    return '平台自我保护：采集任务正处于保护性退避；该空隙不代表数据库不可达或采集配置错误。'
  }
  if (code !== 'COLLECTION_FAILED') return undefined
  switch (taskResult) {
    case 'FAILED': return '目标库故障：最近一次采集失败，请查看任务错误。'
    case 'TIMED_OUT': return '目标库故障：最近一次采集超时，请检查目标库与链路。'
    case 'SUCCESS':
    case undefined:
      return undefined
    default:
      return assertNever(taskResult)
  }
}

function isEnhancedMetricID(value: unknown): value is MetricID {
  return typeof value === 'string' && enhancedMonitoringMetricIDs.some((metric) => metric === value)
}

function isEnhancedAggregation(value: unknown): value is EnhancedAggregation {
  return value === 'average' || value === 'maximum' || value === 'minimum'
}

function isEnhancedColumns(value: unknown): value is EnhancedColumns {
  return value === 1 || value === 2 || value === 3
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function assertNever(value: never): never {
  throw new Error(`unhandled enhanced monitoring value: ${value}`)
}
