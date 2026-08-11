import { describe, expect, it } from 'vitest'
import { pollingIntervals } from './polling'

describe('polling intervals', () => {
  it('keeps the frozen page cadence in one declaration', () => {
    expect(pollingIntervals).toMatchObject({
      instances: 30_000,
      overview: 30_000,
      standardMonitoring: 30_000,
      currentAlerts: 15_000,
      sessions: 10_000,
      history: false,
      details: false,
      collectionManagement: 30_000,
    })
  })
})
