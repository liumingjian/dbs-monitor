import { Dropdown as CarbonDropdown } from '@carbon/react'
import type { ComponentProps } from 'react'

function assertNever(value: never): never {
  throw new Error(`unhandled list box message: ${String(value)}`)
}

function listBoxText(messageId: 'close.menu' | 'open.menu'): string {
  switch (messageId) {
    case 'open.menu':
      return '展开选项'
    case 'close.menu':
      return '收起选项'
    default:
      return assertNever(messageId)
  }
}

type CarbonDropdownProps<ItemType> = ComponentProps<typeof CarbonDropdown<ItemType>>

/// 单选下拉。组件库的 `Dropdown` 原样透出，只把展开箭头的英文可访问名换成中文。
///
/// 组件库默认给那枚箭头 `Open menu` / `Close menu`，不给就静默生效，而界面是硬编码
/// 简体中文、没有 i18n 框架 —— 读屏用户会听见英文。和 `MultiSelect` 是同一件事、
/// 同一份文案，所以收在同一层，而不是让每个调用点各写一份 `translateWithId`。
export function Dropdown<ItemType>(props: CarbonDropdownProps<ItemType>) {
  return <CarbonDropdown<ItemType> translateWithId={listBoxText} {...props} />
}
