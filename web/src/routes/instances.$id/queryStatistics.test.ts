import { describe, expect, it } from 'vitest'
import { queryStatisticsView } from './queryStatistics'

const codes = [
  'NO_SAMPLES_YET', 'NO_DATA_IN_RANGE', 'STALE', 'COLLECTION_PAUSED',
  'COLLECTION_FAILED', 'DB_UNREACHABLE', 'AGENT_OFFLINE', 'PERMISSION_DENIED',
  'EXTENSION_MISSING', 'FEATURE_DISABLED', 'VERSION_UNSUPPORTED',
  'NOT_APPLICABLE_ROLE', 'COUNTER_RESET',
] as const

describe('query statistics ranking states', () => {
  it.each([
    ['not enabled', { items: [], unavailability: 'EXTENSION_MISSING' as const }, '未启用'],
    ['permission denied', { items: [], unavailability: 'PERMISSION_DENIED' as const }, '权限不足'],
    ['temporarily unavailable', { items: [], unavailability: 'COLLECTION_FAILED' as const }, '暂时不可用'],
    ['statistics reset', { items: [], unavailability: 'COUNTER_RESET' as const }, '统计已重置'],
    ['no records in snapshot', { items: [], unavailability: 'NO_DATA_IN_RANGE' as const }, '区间内无记录'],
  ])('renders %s as an explained state instead of an empty table', (_name, response, title) => {
    expect(queryStatisticsView(response)).toMatchObject({ kind: 'unavailable', title })
  })

  it('renders an available queryid ranking only when rows exist', () => {
    expect(queryStatisticsView({
      sampled_at: '2026-08-11T10:00:00Z',
      items: [{ queryid: '42', database_oid: 5, user_oid: 10, calls: 12, total_exec_time_ms: 320 }],
    })).toMatchObject({ kind: 'available' })
  })

  it.each(codes)('preserves %s for its remediation destination', (code) => {
    expect(queryStatisticsView({ items: [], unavailability: code })).toMatchObject({
      kind: 'unavailable',
      code,
    })
  })
})
