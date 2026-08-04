export type TimeRange = { from: string; to: string }
export type InvalidTimeRange = { error: string }

export function parseTimeRange(search: Record<string, unknown>): TimeRange | InvalidTimeRange {
  if (isRFC3339(search.from) && isRFC3339(search.to)) {
    const from = new Date(search.from)
    const to = new Date(search.to)
    if (to <= from) return { error: '结束时间必须晚于开始时间' }
    return { from: from.toISOString(), to: to.toISOString() }
  }
  return { error: '时间范围必须是绝对 RFC3339 时间' }
}

export function serializeTimeRange(value: TimeRange): Record<string, string> {
  return { from: value.from, to: value.to }
}

export function defaultTimeRange(now = new Date()): TimeRange {
  return { from: new Date(now.getTime() - 60 * 60 * 1000).toISOString(), to: now.toISOString() }
}

function isRFC3339(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value) && !Number.isNaN(Date.parse(value))
}
