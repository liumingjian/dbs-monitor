import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { createElement } from 'react'
import { UnavailabilityBlock, unavailabilityCopy } from './UnavailabilityBlock'

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

  it.each(codes)('renders a destination link for %s', (code) => {
    const view = render(createElement(UnavailabilityBlock, { code, href: `/next/${code}` }))
    expect(view.getByRole('link', { name: unavailabilityCopy(code).action }).getAttribute('href')).toBe(`/next/${code}`)
    view.unmount()
  })

  it('never presents collection pause as collection failure or database unreachability', () => {
    const copy = unavailabilityCopy('COLLECTION_PAUSED')
    const text = `${copy.title}${copy.description}${copy.action}`
    expect(text).not.toContain('采集失败')
    expect(text).not.toContain('数据库不可达')
  })
})
