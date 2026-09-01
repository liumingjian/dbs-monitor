import { describe, expect, it } from 'vitest'
import { defaultEnhancedTimeRange, parseTimeRange, serializeTimeRange } from './timeRange'

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
      databases: true,
      metric: 'pg.tps' as const,
    }

    expect(parseTimeRange(serializeTimeRange(value))).toEqual(value)
  })

  it.each([
    [{ step: '10m' }, '粒度必须来自支持的选项'],
    [{ columns: 4 }, '列数必须是 1、2 或 3'],
    [{ connect: 'sometimes' }, '光标联动参数无效'],
    [{ databases: 'maybe' }, '按库展开参数无效'],
  ])('returns an explained invalid state for malformed monitoring search %o', (extra, error) => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
      ...extra,
    })).toEqual({ error })
  })

  /// 按库展开是地址的一部分，缺省不展开：默认口径与列表、总览一致，都是实例级值。
  it('defaults to the instance-level view when the address says nothing about databases', () => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T01:00:00.000Z',
    })).toEqual({ from: '2026-08-03T00:00:00.000Z', to: '2026-08-03T01:00:00.000Z' })
  })

  it('builds and round-trips the enhanced 30-minute raw baseline', () => {
    const value = defaultEnhancedTimeRange(new Date('2026-08-11T11:00:00.000Z'))
    expect(value).toEqual({
      from: '2026-08-11T10:30:00.000Z',
      to: '2026-08-11T11:00:00.000Z',
      monitoring: 'enhanced',
      step: 'raw',
    })
    expect(parseTimeRange(serializeTimeRange(value))).toEqual(value)
  })

  it.each([
    [{ monitoring: 'enhanced', step: 'auto' }, '增强监控固定读取原始粒度'],
    [{ monitoring: 'enhanced', step: 'raw', from: '2026-08-03T00:00:00.000Z', to: '2026-08-03T02:00:00.000Z' }, '增强监控时间窗口必须是 30 分钟、1 小时、3 小时或 6 小时'],
    [{ monitoring: 'other' }, '监控页签参数无效'],
  ])('rejects malformed enhanced search params %o', (extra, error) => {
    expect(parseTimeRange({
      from: '2026-08-03T00:00:00.000Z',
      to: '2026-08-03T00:30:00.000Z',
      ...extra,
    })).toEqual({ error })
  })
})
