import { parseSessionSearch, serializeSessionSearch, type SessionSearch } from './sessionSearch'

/// 会话合并页的页签词汇与地址解析。
///
/// 会话快照 / 长查询采样记录 / 查询统计排行原本是三个地址、三个页面，各自顶着同一条
/// 「其实是三组链接」的页签条。现在收拢成 `/instances/$id/sessions` 一个地址，
/// 「在哪个标签」是 search param（web/CLAUDE.md：URL 状态 → search params）。
///
/// 取值刻意沿用旧地址的最后一段（`long-query-samples` / `query-statistics`），
/// 这样「旧地址 → 新标签」的映射一眼可读，重定向也不需要再查表。
///
/// 纯模块，不认识 React 也不认识路由器：页面、页签条与两条重定向共用这一份解析。
/// `sessionSearch.ts` 一个字没动 —— 那份调查上下文（时间范围 / 指标 / 采样时刻 / 会话过滤）
/// 在合并前后完全一样，页签只是又多了一个正交的维度。

export type SessionTab = 'current' | 'long-query-samples' | 'query-statistics'

/** 页签顺序即渲染顺序，`selectedIndex` 从这里算。 */
export const sessionTabs = [
  'current',
  'long-query-samples',
  'query-statistics',
] as const satisfies readonly SessionTab[]

/**
 * 合并页的完整地址状态：调查上下文 + 当前标签。
 *
 * 上下文解析失败时仍然带着标签，页面才能把错误说在正确的标签上，
 * 两条重定向也才能原样把坏参数转交过去而不是自己吞掉。
 *
 * **`tab` 是可选的**，而 `parseSessionPageSearch` 永远填得出一个：省略它就是「默认标签」。
 * 这一条是合并的兼容面 —— 实例总览的锁等待下钻、工作台页签条上的「会话与阻塞」都只给
 * 时间范围，不该因为这一页多了个页签维度就得跟着改。
 */
export type SessionPageSearch =
  | (SessionSearch & { tab?: SessionTab })
  | { error: string; tab?: SessionTab }

export function sessionTabLabel(tab: SessionTab): string {
  switch (tab) {
    case 'current':
      return '当前会话'
    case 'long-query-samples':
      return '长查询采样记录'
    case 'query-statistics':
      return '查询统计排行'
    default:
      return assertNever(tab)
  }
}

/// 地址解析。认不出来的 `tab` 退回第一个标签而不是渲染空页 ——
/// 合并之前 `/instances/$id/sessions` 这个前缀下的每个地址都渲染得出东西，之后也得如此。
export function parseSessionPageSearch(search: Record<string, unknown>): SessionPageSearch {
  return withSessionTab(parseSessionSearch(search), isSessionTab(search.tab) ? search.tab : 'current')
}

/// 把调查上下文接上一个标签，得到合并页的地址状态。
/// 解析失败的上下文照样带着标签走 —— 错误也要落在正确的页签上。
export function withSessionTab(search: SessionSearch | { error: string }, tab: SessionTab): SessionPageSearch {
  if ('error' in search) return { error: search.error, tab }
  const result: SessionSearch & { tab: SessionTab } = { from: search.from, to: search.to, tab }
  if (search.metric !== undefined) result.metric = search.metric
  if (search.sampled_at !== undefined) result.sampled_at = search.sampled_at
  if (search.filter !== undefined) result.filter = search.filter
  if (search.queryid !== undefined) result.queryid = search.queryid
  return result
}

/// 拼字符串地址时用的查询参数（`href` 属性、不可用说明块的去处）。
/// `tab` 是唯一的新字段，其余照 `serializeSessionSearch` 的老约定。
export function sessionTabQuery(search: SessionSearch, tab: SessionTab): Record<string, string> {
  return { ...serializeSessionSearch(search), tab }
}

function isSessionTab(value: unknown): value is SessionTab {
  return typeof value === 'string' && (sessionTabs as readonly string[]).includes(value)
}

function assertNever(value: never): never {
  throw new Error(`unhandled session tab: ${String(value)}`)
}
