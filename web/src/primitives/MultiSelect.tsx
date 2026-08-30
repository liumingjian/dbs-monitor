import { MultiSelect as CarbonMultiSelect } from '@carbon/react'
import type { MultiSelectProps } from '@carbon/react'

function assertNever(value: never): never {
  throw new Error(`unhandled list box message: ${String(value)}`)
}

function listBoxText(messageId: 'clear.all' | 'clear.selection' | 'open.menu' | 'close.menu'): string {
  switch (messageId) {
    case 'clear.all':
      return '清空已选项'
    case 'clear.selection':
      return '清除已选项'
    case 'open.menu':
      return '展开选项'
    case 'close.menu':
      return '收起选项'
    default:
      return assertNever(messageId)
  }
}

/// 多选下拉。组件库的 `MultiSelect` 原样透出，只把它自带的英文文案换成中文。
///
/// 界面是硬编码简体中文、没有 i18n 框架，而这个控件有四处默认英文，一处都不报错：
///
///   - 已选计数徽标旁的清除按钮：`title` 与 `aria-label` 都是 `Clear selected items`
///     —— **这一处鼠标悬停就能看见**，不是只有读屏用户才撞得上；
///   - 展开/收起的箭头：`aria-label` 是 `Open menu` / `Close menu`；
///   - `clearSelectionDescription`：`Total items selected: `，视觉隐藏，读屏会念；
///   - `clearSelectionText`：`To clear selection, press Delete or Backspace`，同上。
///
/// 六个页面各写一遍就是六次机会写漏，所以给在这一层。传进来的同名 props 仍然覆盖默认值。
export function MultiSelect<ItemType>(props: MultiSelectProps<ItemType>) {
  return (
    <CarbonMultiSelect<ItemType>
      clearSelectionDescription="已选中："
      clearSelectionText="按 Delete 或 Backspace 清空选择"
      translateWithId={listBoxText}
      {...props}
    />
  )
}
