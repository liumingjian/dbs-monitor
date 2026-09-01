import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { createElement } from 'react'
import { UnavailabilityBlock, unavailabilityCopy, unavailabilityHref } from './UnavailabilityBlock'

const destinationCases = [
  ['NO_SAMPLES_YET', '/current'],
  ['NO_DATA_IN_RANGE', '/current'],
  ['STALE', '/collection'],
  ['COLLECTION_PAUSED', '/collection'],
  ['COLLECTION_FAILED', '/collection'],
  ['DB_UNREACHABLE', '/collection'],
  ['AGENT_OFFLINE', '/collection'],
  ['PERMISSION_DENIED', '/collection'],
  ['EXTENSION_MISSING', '/collection'],
  ['FEATURE_DISABLED', '/collection'],
  ['VERSION_UNSUPPORTED', '/collection'],
  ['NOT_APPLICABLE_ROLE', '/collection'],
  ['NOT_APPLICABLE_ENGINE', '/collection'],
  ['COUNTER_RESET', '/current'],
] as const
const codes = destinationCases.map(([code]) => code)

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

  it.each(destinationCases)('selects the canonical destination for %s', (code, expected) => {
    expect(unavailabilityHref(code, {
      current: '/current',
      collection: '/collection',
    })).toBe(expected)
  })

  it('never presents collection pause as collection failure or database unreachability', () => {
    const copy = unavailabilityCopy('COLLECTION_PAUSED')
    const text = `${copy.title}${copy.description}${copy.action}`
    expect(text).not.toContain('采集失败')
    expect(text).not.toContain('数据库不可达')
  })

  it('renders page-specific context after the canonical reason copy', () => {
    const view = render(createElement(UnavailabilityBlock, {
      code: 'COLLECTION_FAILED',
      href: '/collection',
      detail: '平台自我保护：最近一次采集因背压被跳过。',
    }))
    expect(view.getByText('最近一次采集未能完成。')).toBeTruthy()
    expect(view.getByText('平台自我保护：最近一次采集因背压被跳过。')).toBeTruthy()
  })
})
