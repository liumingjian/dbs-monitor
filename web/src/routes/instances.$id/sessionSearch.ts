import type { MetricID } from './metricOptions'
import { isRFC3339, parseTimeRange } from './timeRange'

export type SessionFilter = 'active' | 'long_transaction' | 'lock_wait' | 'blocked'

export type SessionSearch = {
  from: string
  to: string
  metric?: MetricID
  sampled_at?: string
  filter?: SessionFilter
  /**
   * 查询统计排行里被打开详情的那条语句的 queryid。详情进地址栏而不是组件状态：
   * 一条打开着 SQL 详情的链接可以直接发给同事，与监控页的「指标详情」是同一个约定。
   *
   * 认不出来就当没打开（不校验成错）：它只决定要不要弹一层详情，为一个坏参数把整页
   * 会话数据也扣下来，读者失去的比得到的多。
   */
  queryid?: string
}

export function parseSessionSearch(search: Record<string, unknown>): SessionSearch | { error: string } {
  const timeRange = parseTimeRange(search)
  if ('error' in timeRange) return timeRange

  if (search.sampled_at !== undefined && !isRFC3339(search.sampled_at)) {
    return { error: '采样时间必须是绝对 RFC3339 时间' }
  }
  if (search.filter !== undefined && !isSessionFilter(search.filter)) {
    return { error: '会话过滤条件无效' }
  }

  const result: SessionSearch = { from: timeRange.from, to: timeRange.to }
  if (timeRange.metric !== undefined) result.metric = timeRange.metric
  if (search.sampled_at !== undefined) result.sampled_at = search.sampled_at
  if (search.filter !== undefined) result.filter = search.filter
  if (typeof search.queryid === 'string' && search.queryid !== '') result.queryid = search.queryid
  return result
}

export function serializeSessionSearch(search: SessionSearch): Record<string, string> {
  const result: Record<string, string> = { from: search.from, to: search.to }
  if (search.metric !== undefined) result.metric = search.metric
  if (search.sampled_at !== undefined) result.sampled_at = search.sampled_at
  if (search.filter !== undefined) result.filter = search.filter
  if (search.queryid !== undefined) result.queryid = search.queryid
  return result
}

function isSessionFilter(value: unknown): value is SessionFilter {
  return value === 'active' || value === 'long_transaction' || value === 'lock_wait' || value === 'blocked'
}
