import { describe, expect, it } from 'vitest'
import { alertMonitoringSearch, parseAlertListSearch, serializeAlertListSearch } from './search'

describe('alert observation search', () => {
  it('defaults current alerts to excluding paused frozen alerts', () => {
    expect(parseAlertListSearch({})).toEqual({ tab: 'current', include_paused: false, page: 1 })
  })

  it('round-trips an explicit paused-alert and instance filter', () => {
    const value = {
      tab: 'history' as const,
      include_paused: true,
      instance_id: '10000000-0000-4000-8000-000000000001',
      page: 3,
    }

    expect(parseAlertListSearch(serializeAlertListSearch(value))).toEqual(value)
  })

  it('returns an explained state for a malformed shared link', () => {
    expect(parseAlertListSearch({ tab: 'unknown', include_paused: 'perhaps', page: 0 })).toEqual({
      error: '告警筛选链接无效',
    })
  })

  it('carries alert context into an absolute standard-monitoring range', () => {
    expect(alertMonitoringSearch({
      metric_id: 'pg.connection.total',
      first_triggered_at: '2026-08-11T10:15:00Z',
      recovered_at: '2026-08-11T10:45:00Z',
      updated_at: '2026-08-11T10:50:00Z',
    })).toEqual({
      from: '2026-08-11T10:15:00.000Z',
      to: '2026-08-11T10:45:00.000Z',
      metric: 'pg.connection.total',
      step: 'auto',
      columns: 2,
      connect: true,
    })
  })

  it('gives a just-triggered alert a valid monitoring interval', () => {
    expect(alertMonitoringSearch({
      metric_id: 'pg.connection.total',
      first_triggered_at: '2026-08-11T10:15:00Z',
      updated_at: '2026-08-11T10:15:00Z',
    })).toMatchObject({
      from: '2026-08-11T10:15:00.000Z',
      to: '2026-08-11T10:16:00.000Z',
    })
  })
})
