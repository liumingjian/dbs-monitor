import { describe, expect, it } from 'vitest'
import { metricOptions } from './metricOptions'

describe('metric options', () => {
  it('exposes every R1 P0 metric exactly once', () => {
    expect(metricOptions).toHaveLength(32)
    expect(new Set(metricOptions.map((option) => option.id)).size).toBe(32)
  })

  it('exposes every pg_stat_activity task metric', () => {
    expect(metricOptions.map((option) => option.id)).toEqual(expect.arrayContaining([
      'pg.connection.total',
      'pg.connection.active',
      'pg.connection.idle_in_transaction',
      'pg.transaction.long_count',
      'pg.transaction.max_duration_sec',
      'pg.lock.waiting_count',
      'pg.session.blocked_count',
      'pg.query.long_running_count',
    ]))
  })
})
