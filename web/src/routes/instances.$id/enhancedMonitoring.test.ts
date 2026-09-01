import { describe, expect, it } from 'vitest'
import {
  aggregateEnhancedPoints,
  buildEnhancedChartView,
  enhancedMonitoringDefaults,
  enhancedMonitoringGroups,
  enhancedMonitoringMetricIDs,
  enhancedUnavailabilityDetail,
  enhancedWindowOptions,
  parseEnhancedPreferences,
} from './enhancedMonitoring'

describe('enhanced monitoring', () => {
  it('opens with the frozen four-part baseline and every dictionary candidate', () => {
    expect(enhancedMonitoringDefaults).toEqual({
      windowMinutes: 30,
      step: 'raw',
      aggregation: 'average',
      columns: 2,
    })
    expect(enhancedMonitoringMetricIDs).toHaveLength(31)
    expect(enhancedMonitoringMetricIDs.filter((metric) => metric.startsWith('host.'))).toEqual([
      'host.cpu.usage_percent',
      'host.memory.usage_percent',
      'host.disk.usage_percent',
      'host.disk.iops',
      'host.disk.throughput_bytes_per_sec',
      'host.network.bytes_per_sec',
    ])
    const groupedMetrics = enhancedMonitoringGroups.flatMap((group) => group.metrics)
    expect(groupedMetrics).toHaveLength(enhancedMonitoringMetricIDs.length)
    expect(new Set(groupedMetrics)).toEqual(new Set(enhancedMonitoringMetricIDs))
  })

  it('offers only the four fixed raw-safe windows', () => {
    expect(enhancedWindowOptions).toEqual([
      { minutes: 30, label: '30 分钟' },
      { minutes: 60, label: '1 小时' },
      { minutes: 180, label: '3 小时' },
      { minutes: 360, label: '6 小时' },
    ])
    expect(Math.max(...enhancedWindowOptions.map((option) => option.minutes))).toBe(360)
  })

  it.each([
    ['average', [[1, 3], [11, 8], [28, 0]]],
    ['maximum', [[1, 4], [11, 8], [28, 0]]],
    ['minimum', [[1, 2], [11, 8], [28, 0]]],
  ] as const)('applies %s only to populated display buckets', (aggregation, expected) => {
    expect(aggregateEnhancedPoints([
      [1, 2], [6, 4], [11, 8], [18, null], [28, 0],
    ], aggregation, 10)).toEqual(expected)
  })

  it('keeps an all-null bucket as a gap without inventing absent buckets or zeroes', () => {
    expect(aggregateEnhancedPoints([[1, null], [21, 5]], 'average', 10)).toEqual([[1, null], [21, 5]])
  })

  it.each([
    { stored: {}, description: 'defaults missing fields' },
    { stored: { metrics: ['not-a-metric'], aggregation: 'average', columns: 2 }, description: 'rejects unknown metrics' },
    { stored: { metrics: ['pg.tps'], aggregation: 'median', columns: 2 }, description: 'rejects unknown aggregation' },
    { stored: { metrics: ['pg.tps'], aggregation: 'maximum', columns: 4 }, description: 'rejects unsupported columns' },
  ])('$description', ({ stored }) => {
    expect(parseEnhancedPreferences(stored)).toEqual({
      metrics: enhancedMonitoringMetricIDs,
      aggregation: 'average',
      columns: 2,
    })
  })

  it('accepts a valid local preference record', () => {
    expect(parseEnhancedPreferences({
      metrics: ['pg.tps', 'host.cpu.usage_percent'],
      aggregation: 'minimum',
      columns: 3,
    })).toEqual({
      metrics: ['pg.tps', 'host.cpu.usage_percent'],
      aggregation: 'minimum',
      columns: 3,
    })
  })

  it.each([
    ['COLLECTION_FAILED', 'SKIPPED_BACKPRESSURE', '平台自我保护'],
    ['COLLECTION_FAILED', 'BACKOFF', '平台自我保护'],
    ['COLLECTION_FAILED', 'FAILED', '目标库故障'],
    ['DB_UNREACHABLE', undefined, '目标库故障'],
    ['DB_UNREACHABLE', 'SKIPPED_BACKPRESSURE', '目标库故障'],
  ] as const)('explains %s with %s as %s', (code, result, expected) => {
    expect(enhancedUnavailabilityDetail(code, result)).toContain(expected)
  })

  it('does not mislabel ordinary missing data as a platform or database failure', () => {
    expect(enhancedUnavailabilityDetail('NO_DATA_IN_RANGE', 'SUCCESS')).toBeUndefined()
  })

  it('keeps one unavailable chart from degrading an available neighbor', () => {
    const responses = [
      { metric: 'pg.tps', unit: 'tx/s', unavailability: 'COLLECTION_FAILED', series: [] },
      { metric: 'host.cpu.usage_percent', unit: 'percent', unavailability: null, series: [{ labels: {}, points: [[1, 42], [6, null], [11, 45]] }] },
    ] as const

    const metricLabel = (id: string) => (id === 'host.cpu.usage_percent' ? 'CPU 使用率' : id)
    expect(buildEnhancedChartView('pg.tps', responses, 'average', 5, metricLabel)).toEqual({
      series: [],
      unavailability: 'COLLECTION_FAILED',
    })
    expect(buildEnhancedChartView('host.cpu.usage_percent', responses, 'average', 5, metricLabel)).toEqual({
      series: [{ name: 'CPU 使用率', unit: 'percent', points: [[1, 42], [6, null], [11, 45]] }],
      unavailability: null,
    })
  })
})
