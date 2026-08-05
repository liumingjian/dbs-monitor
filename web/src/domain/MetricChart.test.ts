import { describe, expect, it } from 'vitest'
import { chartData, metricUnavailability } from './MetricChart'

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
})
