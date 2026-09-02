import type { TopSqlEntry } from '../../domain/topSql'
import { topSqlRowKey } from '../../domain/topSql'

export type SqlInsightSearch = {
  /**
   * 被打开的那一行的身份，取值就是 `topSqlRowKey`（`实例 ID:queryid`）。
   * 详情进地址栏而不是组件状态：一条打开着详情的链接可以直接发给同事，
   * 与监控页的「指标详情」是同一个约定。
   */
  statement?: string
}

/// 认不出来就当没打开，而不是把整页判错。
///
/// 这一页只有一个可选参数，且它只决定「要不要弹一层详情」；为一个坏参数扣下整张榜单，
/// 读者失去的比得到的多。坏参数的后果只是详情不弹 —— 那正是「没打开」的样子。
export function parseSqlInsightSearch(search: Record<string, unknown>): SqlInsightSearch {
  const statement = search.statement
  return typeof statement === 'string' && statement !== '' ? { statement } : {}
}

/// 地址里那一行现在还在不在榜上。
///
/// 榜单是轮询刷新的，一条语句可能在详情打开期间掉出前一百名。找不到就是找不到 ——
/// 不退回榜首，那会让读者以为自己看的还是原来那条。
export function findStatement(entries: readonly TopSqlEntry[], key: string | undefined): TopSqlEntry | undefined {
  if (key === undefined) return undefined
  return entries.find((entry) => topSqlRowKey(entry) === key)
}
