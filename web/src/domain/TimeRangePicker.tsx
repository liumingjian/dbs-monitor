import { ClockCircleOutlined } from '@ant-design/icons'
import { Button, Space } from 'antd'
import { useRef } from 'react'

export type AbsoluteTimeRange = { from: string; to: string }

type TimeRangePickerProps = AbsoluteTimeRange & {
  onChange: (range: AbsoluteTimeRange) => void
}

export function TimeRangePicker({ from, to, onChange }: TimeRangePickerProps) {
  const fromRef = useRef<HTMLInputElement>(null)
  const toRef = useRef<HTMLInputElement>(null)

  function applyRange() {
    if (!fromRef.current?.value || !toRef.current?.value) return
    onChange({
      from: new Date(fromRef.current.value).toISOString(),
      to: new Date(toRef.current.value).toISOString(),
    })
  }

  return <Space wrap>
    <input key={from} ref={fromRef} type="datetime-local" aria-label="开始时间" defaultValue={toLocalInput(from)} />
    <span aria-hidden="true">至</span>
    <input key={to} ref={toRef} type="datetime-local" aria-label="结束时间" defaultValue={toLocalInput(to)} />
    <Button aria-label="应用时间范围" icon={<ClockCircleOutlined />} onClick={applyRange}>应用时间范围</Button>
  </Space>
}

function toLocalInput(value: string): string {
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 19)
}
