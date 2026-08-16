import { describe, expect, it } from 'vitest'
import { collectionPausePresentation, pausedInstanceCount } from './CollectionPausedTag'

describe('collection pause marker', () => {
  const pausedAt = new Date('2026-08-01T00:00:00Z')

  it.each([
    { elapsed: 6 * 24 * 60 * 60 * 1000, label: '已暂停 6 天', warning: false },
    { elapsed: 7 * 24 * 60 * 60 * 1000, label: '已暂停 7 天', warning: false },
    { elapsed: 7 * 24 * 60 * 60 * 1000 + 1, label: '已暂停 7 天', warning: true },
  ])('projects duration and warning at $elapsed ms', ({ elapsed, label, warning }) => {
    expect(collectionPausePresentation(pausedAt, new Date(pausedAt.getTime() + elapsed))).toEqual({ label, warning })
  })

  it('counts only actively paused instances', () => {
    expect(pausedInstanceCount([
      { collection_pause: { paused: true } },
      { collection_pause: { paused: false } },
      { collection_pause: { paused: true } },
    ])).toBe(2)
  })
})
