import { useMemo } from 'react'
import { $api } from '../../api/client'
import type { components, operations } from '../../api/schema'

type MetricCatalogEntry = components['schemas']['MetricCatalogEntry']
type SemanticSlotDeclaration = components['schemas']['SemanticSlotDeclaration']
import { instanceEngineLabel, type InstanceEngine } from '../../domain/instanceEngine'

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

/// 一个指标能不能用在跑某种引擎的实例上。不适用时必须带理由：作用域里那台实例是不可选的，
/// 使用者要看得见为什么，而不是发现列表里少了几台。
export type EngineApplicability =
  | { applicable: true }
  | { applicable: false; reason: string }

export type MetricCatalog = {
  /// 展示名。目录还在路上时给出指标 ID —— 一个真实的、能照着搜的事实，不是编出来的文案。
  label: (id: MetricID) => string
  option: (id: MetricID) => MetricOption
  options: (ids: readonly MetricID[]) => MetricOption[]
  /// 与服务端 metric.ResolveForEngine 同一条规则，读的也是同一份目录：引擎无关的指标处处可用；
  /// 填了语义位的指标在任何绑定了这个位的引擎上都可用；既不无关又没有位的是引擎私有指标。
  appliesToEngine: (id: MetricID, engine: InstanceEngine) => EngineApplicability
}

/// 指标目录由服务端给：展示名与单位都在 `metric_catalog` 里，前端不再自持一份 —— 跨引擎之后
/// 那份副本每加一个指标就要改一次，改漏了不报错，只在界面上露出一个裸指标 ID。
/// 目录是静态数据，取一次就缓存住；多个页面各自调用，TanStack Query 会合成同一次请求。
export function useMetricCatalog(): MetricCatalog {
  const catalogQuery = $api.useQuery('get', '/api/v1/metrics/catalog', {}, catalogQueryOptions)
  const entries = catalogQuery.data?.metrics
  const slots = catalogQuery.data?.semantic_slots
  return useMemo(() => metricCatalogFrom(entries ?? [], slots ?? []), [entries, slots])
}

/// 目录的读法本身不依赖 React：取数在 hook 里，问答在这里，测试直接问这一个函数。
export function metricCatalogFrom(rows: MetricCatalogEntry[], slots: SemanticSlotDeclaration[]): MetricCatalog {
  const names = new Map<string, string>(rows.map((entry) => [entry.metric_id, entry.display_name]))
  const byID = new Map(rows.map((entry) => [entry.metric_id, entry]))
  const slotNames = new Map(slots.map((slot) => [slot.slot_id, slot.display_name]))
  const label = (id: MetricID) => names.get(id) ?? id
  const option = (id: MetricID): MetricOption => ({ id, label: label(id) })
  const appliesToEngine = (id: MetricID, engine: InstanceEngine): EngineApplicability => {
    const entry = byID.get(id)
    // 目录还没到手就不拦：可选性的最终裁决在服务端，保存时会再判一次。
    if (entry === undefined || entry.engine === 'AGNOSTIC' || entry.engine === engine) {
      return { applicable: true }
    }
    if (entry.semantic_slot === null) {
      return {
        applicable: false,
        reason: `${label(id)}是 ${instanceEngineLabel(entry.engine)} 的专有指标，${instanceEngineLabel(engine)} 实例上没有对应物`,
      }
    }
    const slot = entry.semantic_slot
    if (rows.some((candidate) => candidate.semantic_slot === slot && candidate.engine === engine)) {
      return { applicable: true }
    }
    return {
      applicable: false,
      reason: `${instanceEngineLabel(engine)} 还没有「${slotNames.get(slot) ?? slot}」这个指标`,
    }
  }
  return { label, option, options: (ids: readonly MetricID[]) => ids.map(option), appliesToEngine }
}

const catalogQueryOptions = { staleTime: Infinity, gcTime: Infinity }

function assertNever(value: never): never {
  throw new Error(`unhandled metric ID: ${value}`)
}
