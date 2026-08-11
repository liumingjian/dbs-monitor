import { describe, expect, it } from 'vitest'
import type { components } from '../../api/schema'
import {
  OVERVIEW_MODULES,
  latestMetricFacts,
  overviewDestinations,
  performanceEventsEmptyState,
} from './overview'

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]

describe('instance overview', () => {
  it('keeps the frozen seven modules complete and ordered', () => {
    expect(OVERVIEW_MODULES).toEqual([
      'availability',
      'alerts',
      'resources',
      'database',
      'replication',
      'events',
      'troubleshooting',
    ])
  })

  it('uses each series latest fact without turning missing values into zero', () => {
    const metric: ResponseMetric = {
      metric: 'host.disk.iops',
      unit: 'iops',
      unavailability: null,
      series: [
        { labels: { device: 'sda' }, points: [[1, 4], [3, null]] },
        { labels: { device: 'sdb' }, points: [[1, 2], [2, 0]] },
      ],
    }

    expect(latestMetricFacts(metric)).toEqual({
      unavailability: null,
      unit: 'iops',
      facts: [
        { labels: { device: 'sda' }, value: null, sampledAt: 3 },
        { labels: { device: 'sdb' }, value: 0, sampledAt: 2 },
      ],
    })
    expect(latestMetricFacts(undefined).unavailability).toBe('NO_SAMPLES_YET')
  })

  it('inherits the relevant URL context for every troubleshooting destination', () => {
    const destinations = overviewDestinations('instance / primary', {
      from: '2026-08-11T10:00:00.000Z',
      to: '2026-08-11T11:00:00.000Z',
      metric: 'pg.lock.waiting_count',
      step: '1m',
      columns: 3,
      connect: false,
    })

    const monitoring = new URL(destinations.monitoring, 'https://dbs.test')
    expect(monitoring.pathname).toBe('/instances/instance%20%2F%20primary/monitoring')
    expect(Object.fromEntries(monitoring.searchParams)).toMatchObject({
      from: '2026-08-11T10:00:00.000Z',
      to: '2026-08-11T11:00:00.000Z',
      metric: 'pg.lock.waiting_count',
      step: '1m',
      columns: '3',
      connect: 'false',
    })

    const sessions = new URL(destinations.sessions, 'https://dbs.test')
    expect(Object.fromEntries(sessions.searchParams)).toEqual({
      from: '2026-08-11T10:00:00.000Z',
      to: '2026-08-11T11:00:00.000Z',
      filter: 'lock_wait',
    })
    expect(new URL(destinations.alerts, 'https://dbs.test').searchParams.get('from')).toBe('2026-08-11T10:00:00.000Z')
    expect(new URL(destinations.collection, 'https://dbs.test').searchParams.get('metric')).toBe('pg.lock.waiting_count')
    expect(new URL(destinations.maintenance, 'https://dbs.test').searchParams.get('instance_id')).toBe('instance / primary')
  })

  it('declares the performance-event empty state as a value', () => {
    expect(performanceEventsEmptyState).toEqual({
      title: '近期没有性能事件',
      description: '所选时间范围内没有触发中或已恢复的性能事件。',
    })
  })
})
