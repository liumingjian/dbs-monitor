import type { components } from '../../api/schema'

export type InstanceEngine = components['schemas']['InstanceEngine']
export type HealthStatusValue = components['schemas']['HealthStatus']
export type InstanceFlag = components['schemas']['InstanceFlag']
export type AlertSeverity = components['schemas']['AlertSeverity']
export type InstanceListSort = components['schemas']['InstanceListSort']

export type InvalidInstanceListSearch = { error: string }

/// 实例列表的地址栏状态。
///
/// 筛选与排序都在这里，因为一个筛好的视图必须能整条发给同事——这是本票的验收标准，
/// 也是总览页下钻链接的落点：总览算出一份这个对象，`<Link to="/instances" search={...}>`
/// 直接落到已经筛好的列表，不拼字符串 URL。
///
/// 服务端参数名与这些字段一一对应（`page` / `page_size` / `q` / `engine` / `status` /
/// `flags` / `severity` / `sort`），所以地址栏、请求 query 与接口契约是同一套词汇。
export type InstanceListSearch = {
  page?: number
  page_size?: number
  q?: string
  engine?: readonly InstanceEngine[]
  status?: readonly HealthStatusValue[]
  flags?: readonly InstanceFlag[]
  severity?: readonly AlertSeverity[]
  sort?: InstanceListSort
}

export const INSTANCE_ENGINES = ['POSTGRESQL'] as const satisfies readonly InstanceEngine[]
export const INSTANCE_HEALTH_STATUSES = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const satisfies readonly HealthStatusValue[]
export const INSTANCE_FLAGS = ['NO_DATA', 'MAINTENANCE', 'RECENTLY_RECOVERED', 'IGNORED', 'CONFIGURATION_MISSING'] as const satisfies readonly InstanceFlag[]
export const INSTANCE_ALERT_SEVERITIES = ['critical', 'warning', 'info'] as const satisfies readonly AlertSeverity[]
export const INSTANCE_LIST_SORTS = ['health', 'name', '-name', 'stalest'] as const satisfies readonly InstanceListSort[]

export const INSTANCE_PAGE_SIZES = [25, 50, 100] as const
const defaultPage = 1
const defaultPageSize = 50
const defaultSort: InstanceListSort = 'health'

/// 缺省视图就是一个空地址：`/instances` 与 `/instances?page=1&page_size=50&sort=health`
/// 是同一个视图，所以取默认值的字段不写进地址栏。链接因此可以只带自己关心的筛选
/// （总览页的下钻链接就是这么发的），而不必每次把整套参数复述一遍。
export function defaultInstanceListSearch(): InstanceListSearch {
  return {}
}

export function currentPage(search: InstanceListSearch): number {
  return search.page === undefined ? defaultPage : search.page
}

export function currentPageSize(search: InstanceListSearch): number {
  return search.page_size === undefined ? defaultPageSize : search.page_size
}

export function currentSort(search: InstanceListSearch): InstanceListSort {
  return search.sort === undefined ? defaultSort : search.sort
}

/// 解析地址栏。任何一个取值不认识就整体报错并给出重置入口，而不是悄悄丢掉它：
/// 悄悄丢等于「同一个地址两个人看到两种结果」，那正是把地址发出去要避免的事。
export function parseInstanceListSearch(search: Record<string, unknown>): InstanceListSearch | InvalidInstanceListSearch {
  const page = parsePositiveInteger(search.page)
  if (search.page !== undefined && page === undefined) return { error: '页码必须是正整数' }

  const pageSize = parsePositiveInteger(search.page_size)
  if (search.page_size !== undefined && pageSize === undefined) return { error: '每页条数必须是正整数' }
  if (pageSize !== undefined && pageSize > 500) return { error: '每页条数最多 500' }

  const q = search.q
  if (q !== undefined && typeof q !== 'string') return { error: '搜索词必须是文本' }

  const engine = parseMembers(search.engine, INSTANCE_ENGINES)
  if (search.engine !== undefined && engine === undefined) return { error: '引擎筛选取值无效' }

  const status = parseMembers(search.status, INSTANCE_HEALTH_STATUSES)
  if (search.status !== undefined && status === undefined) return { error: '健康档位筛选取值无效' }

  const flags = parseMembers(search.flags, INSTANCE_FLAGS)
  if (search.flags !== undefined && flags === undefined) return { error: '标记筛选取值无效' }

  const severity = parseMembers(search.severity, INSTANCE_ALERT_SEVERITIES)
  if (search.severity !== undefined && severity === undefined) return { error: '告警级别筛选取值无效' }

  const sort = search.sort
  if (sort !== undefined && !isMember(sort, INSTANCE_LIST_SORTS)) return { error: '排序取值无效' }

  const result: InstanceListSearch = {}
  if (page !== undefined && page !== defaultPage) result.page = page
  if (pageSize !== undefined && pageSize !== defaultPageSize) result.page_size = pageSize
  if (typeof q === 'string' && q !== '') result.q = q
  if (engine !== undefined && engine.length > 0) result.engine = engine
  if (status !== undefined && status.length > 0) result.status = status
  if (flags !== undefined && flags.length > 0) result.flags = flags
  if (severity !== undefined && severity.length > 0) result.severity = severity
  if (sort !== undefined && sort !== defaultSort) result.sort = sort
  return result
}

/// 交给接口的 query。空清单整项不发——`?status=` 在服务端是「筛了一个空集合」还是
/// 「没筛」说不清楚，不如不发。
export function instanceListQuery(search: InstanceListSearch): {
  page: number
  page_size: number
  q?: string
  engine?: InstanceEngine[]
  status?: HealthStatusValue[]
  flags?: InstanceFlag[]
  severity?: AlertSeverity[]
  sort?: InstanceListSort
} {
  const query: ReturnType<typeof instanceListQuery> = {
    page: currentPage(search),
    page_size: currentPageSize(search),
    sort: currentSort(search),
  }
  if (search.q !== undefined && search.q !== '') query.q = search.q
  if (search.engine !== undefined && search.engine.length > 0) query.engine = [...search.engine]
  if (search.status !== undefined && search.status.length > 0) query.status = [...search.status]
  if (search.flags !== undefined && search.flags.length > 0) query.flags = [...search.flags]
  if (search.severity !== undefined && search.severity.length > 0) query.severity = [...search.severity]
  return query
}

/// 改了筛选就回到第一页：留在第 3 页看一个只剩 4 条的结果集没有意义。
/// 只有翻页本身才保留页码，所以页码的更新走 `page` 这一个字段，别的字段一律重置它。
export function withInstanceFilters(
  current: InstanceListSearch,
  changes: Partial<Omit<InstanceListSearch, 'page'>>,
): InstanceListSearch {
  return pruneEmpty({ ...current, ...changes, page: undefined })
}

export function hasInstanceFilters(search: InstanceListSearch): boolean {
  return search.q !== undefined ||
    search.engine !== undefined ||
    search.status !== undefined ||
    search.flags !== undefined ||
    search.severity !== undefined
}

function pruneEmpty(search: InstanceListSearch): InstanceListSearch {
  const result: InstanceListSearch = {}
  if (search.page !== undefined && search.page !== defaultPage) result.page = search.page
  if (search.page_size !== undefined && search.page_size !== defaultPageSize) result.page_size = search.page_size
  if (search.sort !== undefined && search.sort !== defaultSort) result.sort = search.sort
  if (search.q !== undefined && search.q !== '') result.q = search.q
  if (search.engine !== undefined && search.engine.length > 0) result.engine = search.engine
  if (search.status !== undefined && search.status.length > 0) result.status = search.status
  if (search.flags !== undefined && search.flags.length > 0) result.flags = search.flags
  if (search.severity !== undefined && search.severity.length > 0) result.severity = search.severity
  return result
}

function parsePositiveInteger(value: unknown): number | undefined {
  const candidate = typeof value === 'string' ? Number(value) : value
  if (typeof candidate !== 'number' || !Number.isInteger(candidate) || candidate < 1) return undefined
  return candidate
}

function isMember<Value extends string>(value: unknown, members: readonly Value[]): value is Value {
  return typeof value === 'string' && (members as readonly string[]).includes(value)
}

/// 可重复的 query 参数在地址里可能是一个字符串，也可能是一个数组。两种都收，
/// 出参永远是数组——调用方不该为「只勾了一个」写第二条码路。
function parseMembers<Value extends string>(value: unknown, members: readonly Value[]): readonly Value[] | undefined {
  if (value === undefined) return undefined
  const candidates = Array.isArray(value) ? value : [value]
  const result: Value[] = []
  for (const candidate of candidates) {
    if (!isMember(candidate, members)) return undefined
    result.push(candidate)
  }
  return result
}
