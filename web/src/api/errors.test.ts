import { describe, expect, it } from 'vitest'
import { apiErrorMessage } from './errors'

describe('apiErrorMessage', () => {
  it('returns the API error message', () => {
    expect(apiErrorMessage({ error: { message: 'specific failure' } }, 'fallback')).toBe('specific failure')
  })

  it.each([
    new Error('unexpected failure'),
    { error: { message: 42 } },
    null,
  ])('returns the fallback for an unrecognized error shape', (error) => {
    expect(apiErrorMessage(error, 'fallback')).toBe('fallback')
  })
})
