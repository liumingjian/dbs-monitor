import { describe, expect, it } from 'vitest'
import {
  navItemLabel,
  navToggleLabel,
  readNavCollapsed,
  unreadBadgeText,
  writeNavCollapsed,
  type StorageAccess,
} from './navCollapse'

function fakeStorage(initial: Record<string, string> = {}): Storage {
  const entries = new Map(Object.entries(initial))
  return {
    get length() { return entries.size },
    clear: () => entries.clear(),
    getItem: (key: string) => entries.get(key) ?? null,
    key: (index: number) => [...entries.keys()][index] ?? null,
    removeItem: (key: string) => { entries.delete(key) },
    setItem: (key: string, value: string) => { entries.set(key, value) },
  }
}

const unavailable: StorageAccess = () => { throw new DOMException('storage is not available', 'SecurityError') }

describe('nav collapse state', () => {
  it('remembers the collapsed state across page loads', () => {
    const storage = fakeStorage()
    const access = () => storage

    expect(readNavCollapsed(access)).toBe(false)

    writeNavCollapsed(access, true)
    expect(readNavCollapsed(access)).toBe(true)

    writeNavCollapsed(access, false)
    expect(readNavCollapsed(access)).toBe(false)
  })

  it('treats a value it never wrote as expanded', () => {
    expect(readNavCollapsed(() => fakeStorage({ 'dbs-monitor.nav-collapsed': 'yes' }))).toBe(false)
  })

  it('degrades to not remembering when reaching storage throws, without throwing', () => {
    expect(() => writeNavCollapsed(unavailable, true)).not.toThrow()
    expect(readNavCollapsed(unavailable)).toBe(false)
  })

  it('degrades to not remembering when writing throws, without throwing', () => {
    const storage = fakeStorage()
    const full: StorageAccess = () => ({
      ...storage,
      setItem: () => { throw new DOMException('quota exceeded', 'QuotaExceededError') },
    })

    expect(() => writeNavCollapsed(full, true)).not.toThrow()
    expect(readNavCollapsed(() => storage)).toBe(false)
  })

  it('degrades to not remembering when there is no storage at all', () => {
    expect(() => writeNavCollapsed(() => null, true)).not.toThrow()
    expect(readNavCollapsed(() => null)).toBe(false)
    expect(readNavCollapsed(() => undefined)).toBe(false)
  })

  it('labels the toggle with where it goes, not with where it is', () => {
    expect(navToggleLabel(false)).toBe('收起导航')
    expect(navToggleLabel(true)).toBe('展开导航')
  })
})

describe('unread alert badge', () => {
  it('shows nothing at zero and caps at two digits', () => {
    expect(unreadBadgeText(undefined)).toBe('')
    expect(unreadBadgeText(0)).toBe('')
    expect(unreadBadgeText(-3)).toBe('')
    expect(unreadBadgeText(Number.NaN)).toBe('')
    expect(unreadBadgeText(7)).toBe('7')
    expect(unreadBadgeText(99)).toBe('99')
    expect(unreadBadgeText(100)).toBe('99+')
  })

  it('spells the count into the accessible name, because the rail has no label', () => {
    expect(navItemLabel('全局告警', undefined)).toBe('全局告警')
    expect(navItemLabel('全局告警', 0)).toBe('全局告警')
    expect(navItemLabel('全局告警', 3)).toBe('全局告警，3 条未处置告警')
    expect(navItemLabel('全局告警', 250)).toBe('全局告警，99+ 条未处置告警')
  })
})
