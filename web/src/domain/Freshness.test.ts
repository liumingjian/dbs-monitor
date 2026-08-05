import { describe, expect, it } from 'vitest'
import { isFresh } from './Freshness'

describe('isFresh', () => {
  it('uses dataUpdatedAt rather than fetch state', () => {
    expect(isFresh(10_000, 15_000, 2_000)).toBe(true)
    expect(isFresh(10_000, 15_001, 2_000)).toBe(false)
  })
})
