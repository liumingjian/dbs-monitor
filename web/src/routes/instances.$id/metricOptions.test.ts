import { describe, expect, it } from 'vitest'
import { metricOptions } from './metricOptions'

describe('metric options', () => {
  it('exposes every R1 P0 metric exactly once', () => {
    expect(metricOptions).toHaveLength(32)
    expect(new Set(metricOptions.map((option) => option.id)).size).toBe(32)
  })
})
