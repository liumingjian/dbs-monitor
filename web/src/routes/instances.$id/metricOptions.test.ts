import { describe, expect, it } from 'vitest'
import { allMetricIDs, isEnhancedCandidate, isMetricID } from './metricOptions'

describe('metric IDs', () => {
  it('exposes every R1 P0 metric exactly once', () => {
    expect(allMetricIDs).toHaveLength(38)
    expect(new Set(allMetricIDs).size).toBe(38)
  })

  it('exposes every pg_stat_activity task metric', () => {
    expect(allMetricIDs).toEqual(expect.arrayContaining([
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

  it('recognises catalogued metric IDs and rejects everything else', () => {
    expect(isMetricID('pg.tps')).toBe(true)
    expect(isMetricID('mysql.qps')).toBe(false)
    expect(isMetricID(undefined)).toBe(false)
  })

  it('keeps control-plane metrics out of the enhanced chart picker', () => {
    expect(isEnhancedCandidate('agent.status')).toBe(false)
    expect(isEnhancedCandidate('pg.tps')).toBe(true)
  })
})
