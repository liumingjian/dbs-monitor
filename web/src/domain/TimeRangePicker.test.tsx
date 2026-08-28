import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TimeRangePicker, quickRange } from './TimeRangePicker'

afterEach(cleanup)

describe('TimeRangePicker', () => {
  it('commits absolute RFC3339 values through its callback', () => {
    const onChange = vi.fn()
    render(<TimeRangePicker
      from="2026-08-03T00:00:00.000Z"
      to="2026-08-03T01:00:00.000Z"
      onChange={onChange}
    />)

    fireEvent.change(screen.getByLabelText('开始时间'), { target: { value: '2026-08-03T02:00:00' } })
    fireEvent.change(screen.getByLabelText('结束时间'), { target: { value: '2026-08-03T03:30:00' } })
    fireEvent.click(screen.getByRole('button', { name: '应用时间范围' }))

    expect(onChange).toHaveBeenCalledWith({
      from: '2026-08-03T02:00:00.000Z',
      to: '2026-08-03T03:30:00.000Z',
    })
  })
})

describe('quickRange', () => {
  it('resolves to an absolute range ending now', () => {
    const now = Date.parse('2026-08-03T12:00:00.000Z')
    expect(quickRange(60, now)).toEqual({
      from: '2026-08-03T11:00:00.000Z',
      to: '2026-08-03T12:00:00.000Z',
    })
  })
})

describe('TimeRangePicker quick ranges', () => {
  it('commits a preset without typing absolute timestamps', () => {
    const onChange = vi.fn()
    render(<TimeRangePicker
      from="2026-08-03T00:00:00.000Z"
      to="2026-08-03T01:00:00.000Z"
      onChange={onChange}
    />)

    fireEvent.click(screen.getByRole('button', { name: /最近 6 小时/ }))

    const range = onChange.mock.calls[0][0]
    expect(Date.parse(range.to) - Date.parse(range.from)).toBe(6 * 60 * 60 * 1000)
  })
})
