import type { components } from '../../api/schema'
import { withSessionTab } from '../instances.$id/sessionTabs'
import { defaultTimeRange } from '../instances.$id/timeRange'

export type TopSqlEntry = components['schemas']['TopSqlEntry']

/// SQL 洞察的投影函数。不认识 React、不取数，页面与总览第五块共用同一份读法 ——
/// 两处显示同一条 SQL 时，文本、耗时与去处必须是同一句话。

/// 一行的稳定键：一条 SQL 由「哪台实例 + 哪个 queryid」唯一确定，
/// 这也正是文本去重的键，所以榜上不可能出现两行同键。
export function topSqlRowKey(entry: TopSqlEntry): string {
  return `${entry.instance_id}:${entry.queryid}`
}

/// 显示什么。有归一化文本就显示文本 —— 这一页存在的理由就是「显示 SQL 而不是 queryid」。
///
/// 没采到文本时退回 queryid，并把「没采到」说出来：一行空白会让人以为这条 SQL 是空的，
/// 而事实是文本还没采到（扩展刚重置、条目刚被淘汰，或这一轮采集只拿到了指标）。
export function statementLabel(entry: TopSqlEntry): string {
  const text = entry.query_text
  if (text === undefined || text === '') return `queryid ${entry.queryid}（未采到 SQL 文本）`
  return text
}

/// 总耗时的读法。毫秒数在机群量级上会长到十位，逐级换单位才读得出量级差；
/// 保留一位小数，多的位数在一屏上只是噪声。
export function elapsedLabel(milliseconds: number): string {
  if (milliseconds < 1000) return `${milliseconds.toFixed(1)} ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} s`
  if (milliseconds < 3_600_000) return `${(milliseconds / 60_000).toFixed(1)} min`
  return `${(milliseconds / 3_600_000).toFixed(1)} h`
}

/// 下钻到该实例的查询统计：会话与阻塞页的「查询统计排行」标签，带一份默认时间范围。
/// 地址由路由器按那一页的契约拼，这里只负责给出一份合法的 search 对象。
export function queryStatisticsDrilldown() {
  return withSessionTab(defaultTimeRange(), 'query-statistics')
}
