/// 地址栏里的查询参数怎么写、怎么读。整台路由器共用这一份编解码。
///
/// 路由器默认把值 JSON 编码，于是一个多选筛选写出来是 `?status=%5B%22CRITICAL%22%5D`
/// （`?status=["CRITICAL"]` 的转义形式）。规范要求「筛选条件必须体现在地址栏里」，
/// 目的是把筛好的视图整条发给同事：那串方括号引号在聊天框里会被截断、被自动链接识别
/// 成两段，粘回来还得靠对方的浏览器帮忙还原。而且服务端的同名参数用的是**重复键**
/// （`?status=CRITICAL&status=WARNING`，见 `instanceListQuery`），地址栏与请求写法不一样，
/// 「地址栏、请求 query 与接口契约是同一套词汇」这句话就只剩一半。
///
/// 所以清单一律编码成重复键，其余取值照旧（数字与布尔仍然按 JSON 认，`?page=2`
/// 解析回来就是数字 2，路由各自的 `validateSearch` 不必为此改写）。
/// 解析仍然认识旧的 JSON 数组写法：已经发出去的链接不该因为这次改动失效。
export function stringifySearch(search: Record<string, unknown>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) {
    if (value === undefined) continue
    if (Array.isArray(value)) {
      for (const item of value) params.append(key, encodeValue(item))
      continue
    }
    params.append(key, encodeValue(value))
  }
  const query = params.toString()
  return query === '' ? '' : `?${query}`
}

export function parseSearch(searchString: string): Record<string, unknown> {
  const params = new URLSearchParams(searchString.startsWith('?') ? searchString.slice(1) : searchString)
  const result: Record<string, unknown> = {}
  for (const key of new Set(params.keys())) {
    const values = params.getAll(key).map(decodeValue)
    // 一个键出现多次就是一份清单；只出现一次的键交回单值，
    // 由各自的解析函数决定它是不是清单（`parseInstanceListSearch` 两种都收）。
    result[key] = values.length > 1 ? values : values[0]
  }
  return result
}

function encodeValue(value: unknown): string {
  if (typeof value === 'string') return value
  return JSON.stringify(value) ?? ''
}

const numeric = /^-?\d+(\.\d+)?([eE][-+]?\d+)?$/

function decodeValue(raw: string): unknown {
  if (raw === 'true') return true
  if (raw === 'false') return false
  if (raw === 'null') return null
  if (numeric.test(raw)) return Number(raw)
  if (raw.startsWith('[') || raw.startsWith('{')) {
    try {
      return JSON.parse(raw) as unknown
    } catch {
      return raw
    }
  }
  return raw
}
