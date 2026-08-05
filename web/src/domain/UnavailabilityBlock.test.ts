import { describe, expect, it } from 'vitest'
import { unavailabilityCopy } from './UnavailabilityBlock'

const codes = [
  'NO_SAMPLES_YET', 'NO_DATA_IN_RANGE', 'STALE', 'COLLECTION_PAUSED',
  'COLLECTION_FAILED', 'DB_UNREACHABLE', 'AGENT_OFFLINE', 'PERMISSION_DENIED',
  'EXTENSION_MISSING', 'FEATURE_DISABLED', 'VERSION_UNSUPPORTED',
  'NOT_APPLICABLE_ROLE', 'COUNTER_RESET',
] as const

describe('unavailability copy', () => {
  it.each(codes)('explains %s with an action', (code) => {
    const copy = unavailabilityCopy(code)
    expect(copy.title.length).toBeGreaterThan(0)
    expect(copy.description.length).toBeGreaterThan(0)
    expect(copy.action.length).toBeGreaterThan(0)
  })
})
