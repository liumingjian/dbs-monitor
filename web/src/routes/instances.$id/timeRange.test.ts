import { describe, expect, it } from 'vitest'
import { parseTimeRange, serializeTimeRange } from './timeRange'

describe('time range search', () => {
  it('round-trips absolute RFC3339 values', () => {
    const value = {
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
    }
    expect(parseTimeRange(serializeTimeRange(value))).toEqual(value)
  })

  it('returns an explained invalid state when the end is not after the start', () => {
    expect(parseTimeRange({
      from: '2026-08-03T01:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
    })).toEqual({
      error: '结束时间必须晚于开始时间',
    })
  })

  it('returns an explained invalid state for malformed links', () => {
    expect(parseTimeRange({ from: 'last-hour', to: 'bad' })).toEqual({
      error: '时间范围必须是绝对 RFC3339 时间',
    })
  })

  it('keeps a dictionary metric in the URL state', () => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      metric: 'pg.tps',
    })).toEqual({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      metric: 'pg.tps',
    })
  })

  it('rejects a metric outside the generated dictionary enum', () => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      metric: 'not-a-metric',
    })).toEqual({ error: '指标必须来自指标字典' })
  })

  it('round-trips standard monitoring controls through search params', () => {
    const value = {
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      step: '1m' as const,
      columns: 3 as const,
      connect: false,
      metric: 'pg.tps' as const,
    }

    expect(parseTimeRange(serializeTimeRange(value))).toEqual(value)
  })

  it.each([
    [{ step: '10m' }, '粒度必须来自支持的选项'],
    [{ columns: 4 }, '列数必须是 1、2 或 3'],
    [{ connect: 'sometimes' }, '光标联动参数无效'],
  ])('returns an explained invalid state for malformed monitoring search %o', (extra, error) => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      ...extra,
    })).toEqual({ error })
  })
})
