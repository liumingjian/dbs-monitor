import { describe, expect, it } from 'vitest'
import { queryStatisticsDrilldown } from './queryStatisticsDrilldown'

describe('SQL 洞察的下钻去处', () => {
  it('drills down onto the query-statistics tab of the owning instance', () => {
    const search = queryStatisticsDrilldown()
    expect(search).toMatchObject({ tab: 'query-statistics' })
    expect('error' in search).toBe(false)
  })
})
