import { ClockCircleOutlined } from '@ant-design/icons'
import { Button, Space } from 'antd'
import { useRef } from 'react'

export type AbsoluteTimeRange = { from: string; to: string }

type TimeRangePickerProps = AbsoluteTimeRange & {
  onChange: (range: AbsoluteTimeRange) => void
}

export const QUICK_RANGES = [
  { label: '15 分钟', minutes: 15 },
  { label: '1 小时', minutes: 60 },
  { label: '6 小时', minutes: 360 },
  { label: '24 小时', minutes: 1440 },
] as const

export function quickRange(minutes: number, now: number): AbsoluteTimeRange {
  return {
    from: new Date(now - minutes * 60_000).toISOString(),
    to: new Date(now).toISOString(),
  }
}

export function TimeRangePicker({ from, to, onChange }: TimeRangePickerProps) {
  const fromRef = useRef<HTMLInputElement>(null)
  const toRef = useRef<HTMLInputElement>(null)

  function applyRange() {
    const fromValue = fromRef.current?.value
    const toValue = toRef.current?.value
    if (!fromValue || !toValue) return

    onChange({
      from: new Date(fromValue).toISOString(),
      to: new Date(toValue).toISOString(),
    })
  }

  const spanMinutes = Math.round((new Date(to).getTime() - new Date(from).getTime()) / 60_000)

  return (
    <Space wrap>
      {/* Typing two absolute timestamps was the price of the most common action on the page. */}
      <span className="time-quick-label">最近</span>
      <Space.Compact className="time-quick-ranges">
        {QUICK_RANGES.map((range) => (
          <Button
            key={range.minutes}
            size="small"
            type={spanMinutes === range.minutes ? 'primary' : 'default'}
            aria-label={`最近 ${range.label}`}
            onClick={() => onChange(quickRange(range.minutes, Date.now()))}
          >{range.label}</Button>
        ))}
      </Space.Compact>
      <input key={from} ref={fromRef} type="datetime-local" aria-label="开始时间" defaultValue={toLocalInput(from)} />
      <span aria-hidden="true">至</span>
      <input key={to} ref={toRef} type="datetime-local" aria-label="结束时间" defaultValue={toLocalInput(to)} />
      <Button aria-label="应用时间范围" icon={<ClockCircleOutlined />} onClick={applyRange}>应用时间范围</Button>
    </Space>
  )
}

function toLocalInput(value: string): string {
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 19)
}
