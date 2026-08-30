import { NumberInput as CarbonNumberInput } from '@carbon/react'
import type { NumberInputProps } from '@carbon/react'
import { forwardRef } from 'react'

function assertNever(value: never): never {
  throw new Error(`unhandled number input message: ${String(value)}`)
}

function stepperText(messageId: 'increment.number' | 'decrement.number'): string {
  switch (messageId) {
    case 'increment.number':
      return '增大'
    case 'decrement.number':
      return '减小'
    default:
      return assertNever(messageId)
  }
}

/// 数字输入框。组件库的 `NumberInput` 原样透出，只把加减两个按钮的可访问名换成中文
/// （默认是 `Increment number` / `Decrement number`）。按钮上只有一个图标，这个名字**就是**
/// 读屏用户听到的全部内容，而界面是硬编码简体中文、没有 i18n 框架。
///
/// 转发 ref：表单里的数字字段一律走 `Controller`（取值在 `onChange` 的第二个参数里，
/// `register` 会把按钮元素当成值提交），而 `Controller` 的聚焦要一个真实的 ref。
export const NumberInput = forwardRef<HTMLInputElement, NumberInputProps>(
  function NumberInput(props, ref) {
    return <CarbonNumberInput ref={ref} translateWithId={stepperText} {...props} />
  },
)
