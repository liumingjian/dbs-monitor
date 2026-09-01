import { describe, expect, it } from 'vitest'
import type { TopSqlEntry } from '../../domain/topSql'
import { findStatement, parseSqlInsightSearch } from './sqlInsightSearch'

function entry(overrides: Partial<TopSqlEntry> = {}): TopSqlEntry {
  return {
    instance_id: '11111111-1111-4111-8111-111111111111',
    instance_name: '订单库主库',
    queryid: '-4029384029384',
    calls: 12,
    total_exec_time_ms: 1234,
    ...overrides,
  }
}

describe('parseSqlInsightSearch', () => {
  it('keeps the opened statement in the address', () => {
    expect(parseSqlInsightSearch({ statement: 'abc:1' })).toEqual({ statement: 'abc:1' })
  })

  it('treats an unusable value as "nothing opened" rather than an error', () => {
    expect(parseSqlInsightSearch({})).toEqual({})
    expect(parseSqlInsightSearch({ statement: '' })).toEqual({})
    expect(parseSqlInsightSearch({ statement: ['a'] })).toEqual({})
  })
})

describe('findStatement', () => {
  it('resolves the row the address points at', () => {
    expect(findStatement([entry()], '11111111-1111-4111-8111-111111111111:-4029384029384')?.queryid)
      .toBe('-4029384029384')
  })

  it('returns nothing when the row has dropped off the ranking', () => {
    expect(findStatement([entry()], 'gone:1')).toBeUndefined()
    expect(findStatement([entry()], undefined)).toBeUndefined()
  })
})
