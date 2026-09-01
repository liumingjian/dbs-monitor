import { useMemo } from 'react'
import { $api } from '../../api/client'
import type { operations } from '../../api/schema'

export type MetricID = operations['getMetricSeries']['parameters']['query']['metric'][number]

export type MetricOption = {
  id: MetricID
  label: string
}

export const defaultMetric: MetricID = 'pg.connection.total'

const metricIDs = [
  'pg.availability.reachable',
  'pg.probe.latency_ms',
  'collector.last_success_time',
  'agent.status',
  'host.cpu.usage_percent',
  'host.memory.usage_percent',
  'host.disk.usage_percent',
  'host.disk.free_bytes',
  'host.disk.iops',
  'host.disk.throughput_bytes_per_sec',
  'host.network.bytes_per_sec',
  'pg.connection.total',
  'pg.connection.active',
  'pg.connection.idle_in_transaction',
  'pg.connection.max',
  'pg.connection.saturation_percent',
  'pg.tps',
  'pg.xact.commit_per_sec',
  'pg.xact.rollback_per_sec',
  'pg.tuples.read_per_sec',
  'pg.tuples.write_per_sec',
  'pg.temp.files_per_sec',
  'pg.temp.bytes_per_sec',
  'pg.transaction.long_count',
  'pg.transaction.max_duration_sec',
  'pg.lock.waiting_count',
  'pg.session.blocked_count',
  'pg.query.long_running_count',
  'pg.prepared_xacts.count',
  'pg.replication.role',
  'pg.replication.connection_state',
  'pg.replication.replay_lag_ms',
  'pg.replication.wal_lag_bytes',
  'pg.replication_slot.retained_wal_bytes',
  'pg.cache.hit_ratio',
  'pg.cache.block_access_per_sec',
  'pg.database.size_bytes',
  'pg.deadlock.count',
] as const satisfies readonly MetricID[]

export const allMetricIDs: readonly MetricID[] = metricIDs

export function isMetricID(value: unknown): value is MetricID {
  return typeof value === 'string' && (metricIDs as readonly string[]).includes(value)
}

export function isEnhancedCandidate(id: MetricID): boolean {
  switch (id) {
    case 'collector.last_success_time':
    case 'agent.status':
    case 'host.disk.free_bytes':
    case 'pg.prepared_xacts.count':
    case 'pg.replication.role':
    case 'pg.connection.max':
    case 'pg.database.size_bytes':
      // 最大连接数是配置项、数据库体积是慢变量：增强监控的秒级窗口里它们都是一条直线，
      // 选进来只会占一格。饱和度才是连接上限在这里该有的样子。
      return false
    case 'pg.availability.reachable':
    case 'pg.probe.latency_ms':
    case 'host.cpu.usage_percent':
    case 'host.memory.usage_percent':
    case 'host.disk.usage_percent':
    case 'host.disk.iops':
    case 'host.disk.throughput_bytes_per_sec':
    case 'host.network.bytes_per_sec':
    case 'pg.connection.total':
    case 'pg.connection.active':
    case 'pg.connection.idle_in_transaction':
    case 'pg.tps':
    case 'pg.xact.commit_per_sec':
    case 'pg.xact.rollback_per_sec':
    case 'pg.tuples.read_per_sec':
    case 'pg.tuples.write_per_sec':
    case 'pg.temp.files_per_sec':
    case 'pg.temp.bytes_per_sec':
    case 'pg.transaction.long_count':
    case 'pg.transaction.max_duration_sec':
    case 'pg.lock.waiting_count':
    case 'pg.session.blocked_count':
    case 'pg.query.long_running_count':
    case 'pg.replication.connection_state':
    case 'pg.replication.replay_lag_ms':
    case 'pg.replication.wal_lag_bytes':
    case 'pg.replication_slot.retained_wal_bytes':
    case 'pg.connection.saturation_percent':
    case 'pg.cache.hit_ratio':
    case 'pg.cache.block_access_per_sec':
    case 'pg.deadlock.count':
      return true
    default:
      return assertNever(id)
  }
}

export type MetricCatalog = {
  /// 展示名。目录还在路上时给出指标 ID —— 一个真实的、能照着搜的事实，不是编出来的文案。
  label: (id: MetricID) => string
  option: (id: MetricID) => MetricOption
  options: (ids: readonly MetricID[]) => MetricOption[]
}

/// 指标目录由服务端给：展示名与单位都在 `metric_catalog` 里，前端不再自持一份 —— 跨引擎之后
/// 那份副本每加一个指标就要改一次，改漏了不报错，只在界面上露出一个裸指标 ID。
/// 目录是静态数据，取一次就缓存住；多个页面各自调用，TanStack Query 会合成同一次请求。
export function useMetricCatalog(): MetricCatalog {
  const catalogQuery = $api.useQuery('get', '/api/v1/metrics/catalog', {}, catalogQueryOptions)
  const entries = catalogQuery.data?.metrics
  return useMemo(() => {
    const names = new Map<string, string>((entries ?? []).map((entry) => [entry.metric_id, entry.display_name]))
    const label = (id: MetricID) => names.get(id) ?? id
    const option = (id: MetricID): MetricOption => ({ id, label: label(id) })
    return { label, option, options: (ids: readonly MetricID[]) => ids.map(option) }
  }, [entries])
}

const catalogQueryOptions = { staleTime: Infinity, gcTime: Infinity }

function assertNever(value: never): never {
  throw new Error(`unhandled metric ID: ${value}`)
}
