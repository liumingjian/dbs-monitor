import { describe, expect, it } from 'vitest'
import claudeMd from '../../CLAUDE.md?raw'
import { parseDomainRegistry } from './registry'

const registered = parseDomainRegistry(claudeMd)

describe('domain registry', () => {
  it('contains only registered domain components', () => {
    const actual = Object.keys(import.meta.glob('./**/*.tsx'))
      .filter((name) => !name.endsWith('.test.tsx'))
      .map((name) => name.replace(/^\.\//, '').replace(/\.tsx$/, ''))
      .sort()
    expect(actual).toEqual(registered)
  })
})
