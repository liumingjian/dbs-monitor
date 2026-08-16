import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SessionSnapshotMeta } from './sessions'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('session snapshot freshness', () => {
  it('uses dataUpdatedAt for the freshness prompt while showing collection time separately', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-11T12:00:30.001Z'))

    render(<SessionSnapshotMeta
      sampledAt="2026-08-11T12:00:29.000Z"
      dataUpdatedAt={new Date('2026-08-11T12:00:00.000Z').getTime()}
      originalCount={12}
      itemCount={10}
    />)

    expect(screen.getByLabelText('数据已过期')).toBeTruthy()
    expect(screen.getByText(/采集时间/)).toBeTruthy()
    expect(screen.getByText('原始会话数：12')).toBeTruthy()
  })
})
