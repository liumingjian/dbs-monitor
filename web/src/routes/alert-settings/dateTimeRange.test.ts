import { describe, expect, it } from 'vitest'
import {
  composeLocalDateTime,
  durationLabel,
  formatDateTimeRange,
  splitLocalDateTime,
} from './dateTimeRange'

describe('maintenance window date + time composition', () => {
  it('round-trips a wall-clock date and time through an ISO instant', () => {
    const parts = { date: '2026-08-11', time: '12:30' }
    const iso = composeLocalDateTime(parts)
    expect(iso).toBeDefined()
    expect(splitLocalDateTime(iso ?? '')).toEqual(parts)
  })

  it('refuses to compose an incomplete or malformed value instead of guessing one', () => {
    expect(composeLocalDateTime({ date: '', time: '12:30' })).toBeUndefined()
    expect(composeLocalDateTime({ date: '2026-08-11', time: '' })).toBeUndefined()
    expect(composeLocalDateTime({ date: '2026-08-11', time: '24:00' })).toBeUndefined()
    expect(composeLocalDateTime({ date: '11/08/2026', time: '12:30' })).toBeUndefined()
  })

  it('spells the composed range out in full so two date boxes and two time boxes are unambiguous', () => {
    expect(formatDateTimeRange({ date: '2026-08-11', time: '12:00' }, { date: '2026-08-11', time: '13:30' }))
      .toBe('2026-08-11 12:00 → 2026-08-11 13:30（共 1 小时 30 分钟）')
    expect(formatDateTimeRange({ date: '2026-08-11', time: '12:00' }, { date: '', time: '' })).toBe('')
  })

  it('names a backwards range rather than printing a negative duration', () => {
    expect(durationLabel(0)).toBe('结束时间不晚于开始时间')
    expect(durationLabel(-60_000)).toBe('结束时间不晚于开始时间')
    expect(durationLabel(45 * 60_000)).toBe('共 45 分钟')
    expect(durationLabel(3 * 60 * 60_000)).toBe('共 3 小时')
    expect(durationLabel(50 * 60 * 60_000)).toBe('共 2 天 2 小时')
  })
})
