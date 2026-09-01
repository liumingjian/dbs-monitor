import type { components } from '../../api/schema'
import { healthLabel, healthTone } from '../../domain/HealthStatus'
import type { StatusTone } from '../../primitives/StatusBadge'
import type { HealthStatusValue, InstanceListSearch } from '../../domain/instanceListSearch'
import { INSTANCE_HEALTH_STATUSES, defaultInstanceListSearch, withInstanceFilters } from '../../domain/instanceListSearch'
import type { TopSqlEntry } from '../../domain/topSql'
import { elapsedLabel, statementLabel, topSqlRowKey } from '../../domain/topSql'

export type FleetOverview = components['schemas']['FleetOverview']
export type FleetHealthCounts = components['schemas']['FleetHealthCounts']
export type FleetCollectionHealth = components['schemas']['FleetCollectionHealth']
export type StorageUsageEntry = components['schemas']['StorageUsageEntry']

function assertNever(value: never): never {
  throw new Error(`unexpected overview value: ${String(value)}`)
}

/// 总览页上的一个数字。
///
/// `search` 是它下钻到的那个视图，一份 `InstanceListSearch` 对象而不是一段 URL 文本 ——
/// 地址由路由器按实例列表的契约拼，参数名与取值因此永远和列表说的是同一套话。
export type OverviewCount = {
  /** 稳定的键，也是渲染时的 React key。 */
  key: string
  label: string
  count: number
  tone?: StatusTone
  search: InstanceListSearch
}

/// 机群健康计数：五档，顺序与实例列表的筛选、健康圆点一致（严重在最左）。
///
/// 计数为 0 的档位照样显示。「严重 0」是今天最想看到的那个读数，把它藏起来等于
/// 让读者自己去证明「没有严重的」。
export function healthCountTiles(counts: FleetHealthCounts): OverviewCount[] {
  return INSTANCE_HEALTH_STATUSES.map((status) => ({
    key: status,
    label: healthLabel(status),
    count: healthCount(counts, status),
    tone: healthTone(status),
    search: withInstanceFilters(defaultInstanceListSearch(), { status: [status] }),
  }))
}

function healthCount(counts: FleetHealthCounts, status: HealthStatusValue): number {
  switch (status) {
    case 'CRITICAL':
      return counts.critical
    case 'WARNING':
      return counts.warning
    case 'UNKNOWN':
      return counts.unknown
    case 'HEALTHY':
      return counts.healthy
    case 'PAUSED':
      return counts.paused
    default:
      return assertNever(status)
  }
}

/// 采集自监控：三个数字，各自下钻到实例列表的一个筛选。
///
/// 这一层不会自己报警——规则评估不到数据时不会响，所以「悄悄烂掉」只能靠这三个数字被看见。
/// 暂停单独成一个数字而不并进「不新鲜」：暂停是有人按下的开关，不是故障。
export function collectionCountTiles(collection: FleetCollectionHealth): OverviewCount[] {
  return [
    {
      key: 'stale_data',
      label: '数据不新鲜',
      count: collection.stale_data,
      tone: 'warning',
      search: withInstanceFilters(defaultInstanceListSearch(), { flags: ['STALE_DATA'] }),
    },
    {
      key: 'agent_offline',
      label: 'Agent 离线',
      count: collection.agent_offline,
      tone: 'warning',
      search: withInstanceFilters(defaultInstanceListSearch(), { flags: ['AGENT_OFFLINE'] }),
    },
    {
      key: 'collection_paused',
      label: '采集暂停',
      count: collection.paused,
      tone: 'unknown',
      search: withInstanceFilters(defaultInstanceListSearch(), { status: ['PAUSED'] }),
    },
  ]
}

/// 磁盘使用率的读法。四舍五入到整数百分比：小数位在一屏十行里只是噪声。
export function usagePercentLabel(percent: number): string {
  return `${Math.round(percent)}%`
}

/// 比例条要的 0..1 占比。百分比越界（采集端给出 101%）时由展示件自己夹住，这里不改数。
export function storageRatio(percent: number): number {
  return percent / 100
}

/// 总览第五块的一行。
///
/// 与 SQL 洞察页显示的是同一份事实（同一个接口字段、同一套读法），只是这里只取五条、
/// 每行压成一条指标条：总览回答「现在最费资源的是哪几条」，完整排行在 SQL 洞察页上。
export type TopSqlSummary = {
  /** 稳定的键，也是渲染时的 React key。 */
  key: string
  /** 归一化 SQL 文本；没采到文本时是带 queryid 的说明，不是空串。 */
  statement: string
  /** 已经格式化好的总耗时。 */
  elapsed: string
  /** 「实例 · 调用次数」，一行注解说清这条 SQL 是谁的、跑了多少次。 */
  caption: string
  /** 相对榜首的占比，画那条 4px 的比例条用。榜首恒为 1。 */
  ratio: number
}

/// Top SQL 前 5 的投影。
///
/// 比例条相对**榜首**而不是相对机群总耗时：读者要看的是「第一条比第二条严重多少」，
/// 而机群总耗时无从得知——榜单只有五行，它们加起来不是全部。
/// 榜首耗时为 0（刚重置过统计）时所有条都是空的，这是真实读数，不是缺数。
export function topSqlSummaries(entries: TopSqlEntry[]): TopSqlSummary[] {
  const highest = entries.reduce((most, entry) => Math.max(most, entry.total_exec_time_ms), 0)
  return entries.map((entry) => ({
    key: topSqlRowKey(entry),
    statement: statementLabel(entry),
    elapsed: elapsedLabel(entry.total_exec_time_ms),
    caption: `${entry.instance_name} · ${entry.calls} 次调用`,
    ratio: highest === 0 ? 0 : entry.total_exec_time_ms / highest,
  }))
}
