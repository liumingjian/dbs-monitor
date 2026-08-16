import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TimeRangePicker } from './TimeRangePicker'

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
