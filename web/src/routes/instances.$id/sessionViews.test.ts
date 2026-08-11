import { describe, expect, it } from 'vitest'
import { longQueryTableFields } from './longQuerySamples'
import { queryStatisticsTableFields } from './queryStatisticsPage'
import { groupSessionSnapshot, sessionTableFields } from './sessionViews'

const sessions = [
  { pid: 10, state: 'active', transaction_duration_ms: 301_000, wait_event_type: 'Lock', blocking_pids: [20] },
  { pid: 20, state: 'idle', transaction_duration_ms: 10_000, blocking_pids: [] },
]

describe('session snapshot views', () => {
  it('derives all current-state sections from the same snapshot', () => {
    expect(groupSessionSnapshot(sessions)).toMatchObject({
      active: [{ pid: 10 }],
      longTransactions: [{ pid: 10 }],
      lockWaits: [{ pid: 10 }],
      blockingChains: [{ pid: 10 }],
      details: sessions,
    })
  })

  it('never defines a SQL text field', () => {
    for (const fields of [sessionTableFields, longQueryTableFields, queryStatisticsTableFields]) {
      expect(fields).not.toEqual(expect.arrayContaining(['query', 'sql', 'query_text', 'sql_text']))
    }
  })
})
