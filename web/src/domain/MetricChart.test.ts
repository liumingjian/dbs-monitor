import { describe, expect, it } from 'vitest'
import { chartData, formatMetricNumber, metricUnavailability, thresholdsForSeries } from './MetricChart'

describe('chartData', () => {
  it('preserves an explicitly missing value as a gap', () => {
    expect(chartData([[1, null], [2, 4]])).toEqual([[1000, null], [2000, 4]])
  })

  it('keeps an explicit null unavailability as available', () => {
    expect(metricUnavailability({ unavailability: null })).toBeNull()
    expect(metricUnavailability(undefined)).toBe('NO_SAMPLES_YET')
  })

  it('does not invent absent time buckets', () => {
    expect(chartData([[1, 3], [3, 5]])).toEqual([[1000, 3], [3000, 5]])
  })

  it('preserves a real zero as a chart point', () => {
    expect(chartData([[1, 0]])).toEqual([[1000, 0]])
  })
})

describe('formatMetricNumber', () => {
  it('reads a percentage as a percentage rather than a raw unit suffix', () => {
    expect(formatMetricNumber(96.324, 'percent')).toBe('96.3%')
  })

  it('scales bytes and byte rates to a human magnitude', () => {
    expect(formatMetricNumber(522680.35, 'bytes/s')).toBe('510.43 KB/s')
    expect(formatMetricNumber(1024 ** 3, 'bytes')).toBe('1 GB')
  })

  it('drops the noise unit on plain counts and compacts large ones', () => {
    expect(formatMetricNumber(14.5, 'count')).toBe('14.5')
    expect(formatMetricNumber(3216.16, 'ops/s')).toBe('3,216.16 ops/s')
    expect(formatMetricNumber(120000, 'count')).toBe('120K')
  })

  it('keeps an unknown unit verbatim instead of inventing copy', () => {
    expect(formatMetricNumber(7, 'iops')).toBe('7 iops')
  })

  it('preserves a real zero', () => {
    expect(formatMetricNumber(0, 'percent')).toBe('0%')
  })
})

describe('thresholdsForSeries', () => {
  const series = [
    { name: '使用率', unit: 'percent', points: [] },
    { name: '剩余空间', unit: 'bytes', points: [] },
    { name: '已用空间', unit: 'bytes', points: [] },
  ]
  const thresholds = [
    { label: '磁盘使用率高', unit: 'percent', value: 90, severity: 'critical' as const },
    { label: '剩余空间低', unit: 'bytes', value: 1024 ** 3, severity: 'warning' as const },
  ]

  it('draws each threshold against the axis of its own unit', () => {
    expect(thresholdsForSeries(thresholds, series, 0).map((item) => item.label)).toEqual(['磁盘使用率高'])
    expect(thresholdsForSeries(thresholds, series, 1).map((item) => item.label)).toEqual(['剩余空间低'])
  })

  it('does not draw the same line twice when two series share a unit', () => {
    expect(thresholdsForSeries(thresholds, series, 2)).toEqual([])
  })
})
