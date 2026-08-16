import { describe, expect, it } from 'vitest'
import { parseSessionSearch, serializeSessionSearch } from './sessionSearch'

const range = {
  from: '2026-08-11T10:00:00.000Z',
  to: '2026-08-11T11:00:00.000Z',
}

describe('session route search', () => {
  it('round-trips inherited instance investigation context', () => {
    const search = {
      ...range,
      metric: 'pg.query.long_running_count' as const,
      sampled_at: '2026-08-11T10:55:00.000Z',
      filter: 'lock_wait' as const,
    }
    expect(parseSessionSearch(serializeSessionSearch(search))).toEqual(search)
  })

  it.each([
    [{ ...range, sampled_at: 'yesterday' }, '采样时间必须是绝对 RFC3339 时间'],
    [{ ...range, filter: 'pid:42' }, '会话过滤条件无效'],
    [{ from: 'bad', to: range.to }, '时间范围必须是绝对 RFC3339 时间'],
  ])('returns an explained state for malformed links', (search, error) => {
    expect(parseSessionSearch(search)).toEqual({ error })
  })
})
