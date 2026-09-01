import type { components } from '../../api/schema'
import { healthLabel, healthTone } from '../../domain/HealthStatus'
import type { StatusTone } from '../../primitives/StatusBadge'
import type { HealthStatusValue, InstanceListSearch } from '../instances/instanceListSearch'
import { INSTANCE_HEALTH_STATUSES, defaultInstanceListSearch, withInstanceFilters } from '../instances/instanceListSearch'

export type FleetOverview = components['schemas']['FleetOverview']
export type FleetHealthCounts = components['schemas']['FleetHealthCounts']
export type FleetCollectionHealth = components['schemas']['FleetCollectionHealth']
export type StorageWatermarkEntry = components['schemas']['StorageWatermarkEntry']

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

/// 磁盘水位的读法。四舍五入到整数百分比：小数位在一屏十行里只是噪声。
export function usagePercentLabel(percent: number): string {
  return `${Math.round(percent)}%`
}

/// 水位档位。只有「快满了」与「满了」两档上色，其余中性 —— 十行全上色等于没有颜色。
/// 与实例列表的连接饱和度用同一组阈值，两处读起来才是同一种紧张程度。
export function storageTone(percent: number): StatusTone | undefined {
  if (percent >= 90) return 'critical'
  if (percent >= 75) return 'warning'
  return undefined
}

/// 比例条要的 0..1 占比。百分比越界（采集端给出 101%）时由展示件自己夹住，这里不改数。
export function storageRatio(percent: number): number {
  return percent / 100
}
