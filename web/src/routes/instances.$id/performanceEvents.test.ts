import { describe, expect, it } from 'vitest'
import {
  eventMonitoringSearch,
  parsePerformanceEventSearch,
  performanceEventRecoveryFilter,
  serializePerformanceEventSearch,
  type PerformanceEventSearch,
} from './performanceEvents'

describe('performance event search', () => {
  it('round-trips the active module, absolute range, and page', () => {
    const value = {
      from: '2026-08-11T10:00:00.000Z',
      to: '2026-08-11T11:00:00.000Z',
      tab: 'disposed',
      disposition: 'IGNORED',
      page: 3,
    } satisfies PerformanceEventSearch

    expect(parsePerformanceEventSearch(serializePerformanceEventSearch(value))).toEqual(value)
  })

  it('returns an explained state for a malformed shared link', () => {
    expect(parsePerformanceEventSearch({
      from: 'last-hour',
      to: 'now',
      tab: 'unknown',
      disposition: 'NONE',
      page: 0,
    })).toEqual({ error: '性能事件筛选链接无效' })
  })

  it('carries the event interval and metric into standard monitoring', () => {
    expect(eventMonitoringSearch({
      metric_id: 'pg.lock.waiting_count',
      derived_at: '2026-08-11T10:15:00Z',
      recovered_at: '2026-08-11T10:45:00Z',
      updated_at: '2026-08-11T10:50:00Z',
    })).toEqual({
      from: '2026-08-11T10:15:00.000Z',
      to: '2026-08-11T10:45:00.000Z',
      metric: 'pg.lock.waiting_count',
      step: 'auto',
      columns: 2,
      connect: true,
    })
  })

  it.each([
    ['firing', false],
    ['recovered', true],
    ['disposed', undefined],
  ] as const)('maps the %s tab to its recovery filter', (tab, recovered) => {
    expect(performanceEventRecoveryFilter(tab)).toBe(recovered)
  })
})
