import { describe, expect, it, vi } from 'vitest'
import { apiErrorMessage, apiFieldErrors, applyApiFieldErrors } from './errors'

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
  it('reads the validation field errors off the wire shape', () => {
    expect(apiFieldErrors({
      error: {
        field_errors: [
          { field: 'recovery_threshold', message: 'must be below threshold' },
          { field: 'name', message: 'must not be blank' },
        ],
      },
    })).toEqual([
      { field: 'recovery_threshold', message: 'must be below threshold' },
      { field: 'name', message: 'must not be blank' },
    ])
  })

  it('ignores malformed field errors', () => {
    expect(apiFieldErrors({ error: { field_errors: [{ field: 42, message: 'bad' }] } })).toEqual([])
  })
})

type RuleValues = { name: string; recovery_threshold: number }

function failure(fieldErrors: { field: string; message: string }[]): unknown {
  return { error: { message: 'validation failed', field_errors: fieldErrors } }
}

describe('applyApiFieldErrors', () => {
  it('sets one message per field and focuses the first one', () => {
    const setError = vi.fn()

    const applied = applyApiFieldErrors<RuleValues>(
      failure([
        { field: 'recovery_threshold', message: 'must be below threshold' },
        { field: 'name', message: 'must not be blank' },
      ]),
      ['name', 'recovery_threshold'],
      setError,
    )

    expect(applied).toEqual(['recovery_threshold', 'name'])
    expect(setError.mock.calls).toEqual([
      ['recovery_threshold', { type: 'server', message: 'must be below threshold' }, { shouldFocus: true }],
      ['name', { type: 'server', message: 'must not be blank' }, { shouldFocus: false }],
    ])
  })

  it('drops fields the form does not know about', () => {
    const setError = vi.fn()

    const applied = applyApiFieldErrors<RuleValues>(
      failure([{ field: 'tenant_id', message: 'is unknown here' }, { field: 'name', message: 'must not be blank' }]),
      ['name', 'recovery_threshold'],
      setError,
    )

    expect(applied).toEqual(['name'])
    expect(setError).toHaveBeenCalledTimes(1)
    expect(setError).toHaveBeenCalledWith('name', { type: 'server', message: 'must not be blank' }, { shouldFocus: true })
  })

  it('keeps the first message when the server repeats a field', () => {
    const setError = vi.fn()

    const applied = applyApiFieldErrors<RuleValues>(
      failure([{ field: 'name', message: 'first' }, { field: 'name', message: 'second' }]),
      ['name'],
      setError,
    )

    expect(applied).toEqual(['name'])
    expect(setError).toHaveBeenCalledWith('name', { type: 'server', message: 'first' }, { shouldFocus: true })
  })

  it('reports no field errors for an unrecognized failure', () => {
    const setError = vi.fn()

    expect(applyApiFieldErrors<RuleValues>(new Error('boom'), ['name'], setError)).toEqual([])
    expect(setError).not.toHaveBeenCalled()
  })
})
