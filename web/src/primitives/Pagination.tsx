import { Pagination as CarbonPagination } from '@carbon/react'
import type { ComponentProps } from 'react'

export type PaginationProps = ComponentProps<typeof CarbonPagination>

/// 分页条。**它存在的唯一理由是：组件库的每一句文案默认都是英文。**
///
/// 界面是硬编码简体中文、没有 i18n 框架，所以每个页面都得把 `backwardText` / `forwardText` /
/// `itemsPerPageText` / `itemRangeText` … 一条条写成中文。三个列表页因此抄了同一份九行字面量，
/// 而且都漏掉了同样的三条 —— 漏掉的那三条不会报错，只是悄悄说英文：
///
///   - `pageSelectLabelText`：页码下拉的可访问名，默认 `Page of 3 pages`。它是视觉隐藏的
///     `<label>`，眼睛看不见，读屏用户听得见。
///   - `pageText`：`pageSizeInputDisabled` 时代替下拉显示的那句，默认 `page 2`。
///   - `itemText`：总条数未知时代替 `itemRangeText` 的那句，默认 `1–25 items`。
///
/// 所以默认值放在这一层，页面只给数据（第几页、每页多少、共多少）。
/// 传进来的同名 props 仍然覆盖默认值——需要一句更贴切的文案时照样可以给。
export function Pagination(props: PaginationProps) {
  return (
    <CarbonPagination
      backwardText="上一页"
      forwardText="下一页"
      itemsPerPageText="每页条数"
      itemRangeText={(min, max, total) => `第 ${min}–${max} 条，共 ${total} 条`}
      itemText={(min, max) => `第 ${min}–${max} 条`}
      pageRangeText={(_current, total) => `共 ${total} 页`}
      pageText={(page) => `第 ${page} 页`}
      pageSelectLabelText={(total) => `页码，共 ${total} 页`}
      pageNumberText="页码"
      {...props}
    />
  )
}
