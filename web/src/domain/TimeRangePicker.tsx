import { Button, TextInput } from '@carbon/react'
import { useRef } from 'react'
import { Icon } from '../primitives/Icon'
import './TimeRangePicker.css'

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

/// 时间范围选择器。
///
/// 两个输入框是**浏览器原生的 `datetime-local`**，不是浮层日期选择器：原生控件自带全套
/// 键盘与输入法行为，换一个浮层就要把它们重做一遍，而这里没有一件事是原生做不到的。
/// 它们的可访问名（开始时间 / 结束时间）是端到端用例的定位口，不要改。
///
/// 蓝色只表示可交互，所以选中的快捷档用的是主按钮那支蓝 —— 那说的是「你按下的是这个」，
/// 不是状态；未选中的档走次级按钮。选中态同时由 `aria-pressed` 表达，不只靠颜色。
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
    <div className="dbs-time-range">
      {/* Typing two absolute timestamps was the price of the most common action on the page. */}
      <span className="dbs-time-range__quick-label dbs-caption">最近</span>
      <div className="dbs-time-range__presets">
        {QUICK_RANGES.map((range) => (
          <Button
            key={range.minutes}
            size="md"
            kind={spanMinutes === range.minutes ? 'primary' : 'ghost'}
            aria-pressed={spanMinutes === range.minutes}
            aria-label={`最近 ${range.label}`}
            onClick={() => onChange(quickRange(range.minutes, Date.now()))}
          >{range.label}</Button>
        ))}
      </div>
      <div className="dbs-time-range__field">
        <TextInput
          key={from}
          ref={fromRef}
          id="time-range-from"
          size="md"
          type="datetime-local"
          labelText="开始时间"
          defaultValue={toLocalInput(from)}
        />
      </div>
      <div className="dbs-time-range__field">
        <TextInput
          key={to}
          ref={toRef}
          id="time-range-to"
          size="md"
          type="datetime-local"
          labelText="结束时间"
          defaultValue={toLocalInput(to)}
        />
      </div>
      <Button size="md" kind="tertiary" aria-label="应用时间范围" renderIcon={Icon.glyph.time} onClick={applyRange}>
        应用时间范围
      </Button>
    </div>
  )
}

function toLocalInput(value: string): string {
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 19)
}
