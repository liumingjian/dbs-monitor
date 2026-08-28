import { describe, expect, it } from 'vitest'
import { elapsedLabel, freshnessLabel, isFresh } from './Freshness'

describe('isFresh', () => {
  it('uses dataUpdatedAt rather than fetch state', () => {
    expect(isFresh(10_000, 15_000, 2_000)).toBe(true)
    expect(isFresh(10_000, 15_001, 2_000)).toBe(false)
  })
})

describe('freshnessLabel', () => {
  it('ages with the clock instead of freezing at the last render', () => {
    expect(freshnessLabel(10_000, 12_000, 60_000)).toBe('刚刚更新')
    expect(freshnessLabel(10_000, 50_000, 60_000)).toBe('40 秒前更新')
    expect(freshnessLabel(10_000, 130_000, 60_000)).toBe('2 分钟前更新')
  })

  it('says so once the data is past its collection window', () => {
    expect(freshnessLabel(10_000, 200_000, 60_000)).toBe('已过期 · 3 分钟未更新')
  })
})

describe('elapsedLabel', () => {
  it('is the one elapsed vocabulary shared with alert duration', () => {
    expect(elapsedLabel(42)).toBe('42 秒')
    expect(elapsedLabel(90)).toBe('1 分钟')
    expect(elapsedLabel(7200)).toBe('2 小时')
    expect(elapsedLabel(90_000)).toBe('1 天')
  })
})
