import { describe, expect, it } from 'vitest'
import { apiErrorMessage, apiFieldErrors } from './errors'

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

describe('apiFieldErrors', () => {
  it('converts validation field errors for AntD forms', () => {
    expect(apiFieldErrors({
      error: {
        field_errors: [
          { field: 'recovery_threshold', message: 'must be below threshold' },
          { field: 'name', message: 'must not be blank' },
        ],
      },
    })).toEqual([
      { name: 'recovery_threshold', errors: ['must be below threshold'] },
      { name: 'name', errors: ['must not be blank'] },
    ])
  })

  it('ignores malformed field errors', () => {
    expect(apiFieldErrors({ error: { field_errors: [{ field: 42, message: 'bad' }] } })).toEqual([])
  })
})
