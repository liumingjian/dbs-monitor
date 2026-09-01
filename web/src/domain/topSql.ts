import type { components } from '../api/schema'
import { sqlSummary } from './sqlText'

export type TopSqlEntry = components['schemas']['TopSqlEntry']

/// 「一个 queryid，可能带一段归一化文本」。跨实例榜单（`TopSqlEntry`）与实例内的查询统计
/// 排行（`QueryStatisticsEntry`）都是这个形状 —— 文本本来就是同一张表里按 (实例, queryid)
/// 存的同一份，所以两处的读法只能有一套。用结构类型而不是联合类型：这两个生成类型互不知道
/// 对方存在，联合起来只会把生成物的耦合写进手写代码。
export type StatementIdentity = { queryid: string; query_text?: string }

/// Top SQL 的读法。不认识 React、不取数、不认识路由：SQL 洞察页、机群总览第五块与实例
/// 工作台的查询统计排行共用它 —— 三处显示同一条 SQL 时，文本、耗时与行的身份必须是同一句话。
/// 多个页面共用，所以它住在 domain/ 而不是任何一个页面目录里（web/CLAUDE.md 的目录一节：
/// 页面私有件不上浮，也不许横着去别的页面里拿）。

/// 一行的稳定键：一条 SQL 由「哪台实例 + 哪个 queryid」唯一确定，
/// 这也正是文本去重的键，所以榜上不可能出现两行同键。
export function topSqlRowKey(entry: TopSqlEntry): string {
  return `${entry.instance_id}:${entry.queryid}`
}

/// 列表格子里显示什么。全文进不了 40px 的一行，所以列表放摘要，全文归详情。
///
/// 摘要与全文是同一段文本的两种读法，不是两份数据：详情里那条一定包含这里显示的开头，
/// 读者点开之后不会怀疑自己点错了行。
///
/// 没采到文本时退回 queryid，并把「没采到」说出来：一格空白会让人以为这条 SQL 是空的，
/// 而事实是文本还没采到（扩展刚重置、条目刚被淘汰，或这一轮采集只拿到了指标）。
export function statementSummary(entry: StatementIdentity): string {
  const text = statementText(entry)
  if (text === undefined) return `queryid ${entry.queryid}（未采到 SQL 文本）`
  return sqlSummary(text)
}

/// 全文，没采到就是 undefined。
///
/// 空串与缺席在这里是同一件事，都当作「没采到」：pg_stat_statements 不会给出一条空语句，
/// 一个空串只可能来自采集侧的截断，而把它当成「有文本」会在详情里画出一个空的代码块。
export function statementText(entry: StatementIdentity): string | undefined {
  const text = entry.query_text
  return text === undefined || text === '' ? undefined : text
}

/// 总耗时的读法。毫秒数在机群量级上会长到十位，逐级换单位才读得出量级差；
/// 保留一位小数，多的位数在一屏上只是噪声。
export function elapsedLabel(milliseconds: number): string {
  if (milliseconds < 1000) return `${milliseconds.toFixed(1)} ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} s`
  if (milliseconds < 3_600_000) return `${(milliseconds / 60_000).toFixed(1)} min`
  return `${(milliseconds / 3_600_000).toFixed(1)} h`
}
