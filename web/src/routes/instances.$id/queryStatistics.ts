import type { components } from '../../api/schema'

type QueryStatisticsSnapshot = components['schemas']['QueryStatisticsSnapshot']

type QueryStatisticsView =
  | { kind: 'available'; items: QueryStatisticsSnapshot['items']; sampledAt: string | undefined }
  | { kind: 'unavailable'; title: string; description: string }

export function queryStatisticsView(response: QueryStatisticsSnapshot): QueryStatisticsView {
  if (response.unavailability !== undefined) return unavailableView(response.unavailability)
  if (response.items.length === 0) return unavailableView('NO_DATA_IN_RANGE')
  return { kind: 'available', items: response.items, sampledAt: response.sampled_at }
}

function unavailableView(code: components['schemas']['Unavailability']): Extract<QueryStatisticsView, { kind: 'unavailable' }> {
  switch (code) {
    case 'EXTENSION_MISSING':
    case 'FEATURE_DISABLED':
      return { title: '未启用', description: '目标数据库未提供可用的 pg_stat_statements 能力，请检查扩展与预加载配置。', kind: 'unavailable' }
    case 'PERMISSION_DENIED':
      return { title: '权限不足', description: '监控账号无法读取查询统计，请核对监控角色权限。', kind: 'unavailable' }
    case 'COUNTER_RESET':
      return { title: '统计已重置', description: '统计计数器已重置，等待下一次完整采集后再查看排行。', kind: 'unavailable' }
    case 'NO_DATA_IN_RANGE':
      return { title: '区间内无记录', description: '最近一次查询统计快照没有可排行的记录。', kind: 'unavailable' }
    case 'NO_SAMPLES_YET':
    case 'STALE':
    case 'COLLECTION_PAUSED':
    case 'COLLECTION_FAILED':
    case 'DB_UNREACHABLE':
    case 'AGENT_OFFLINE':
    case 'VERSION_UNSUPPORTED':
    case 'NOT_APPLICABLE_ROLE':
      return { title: '暂时不可用', description: '当前无法取得可靠的查询统计快照，请检查采集状态后重试。', kind: 'unavailable' }
    default:
      return assertNever(code)
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled query statistics state: ${value}`)
}
