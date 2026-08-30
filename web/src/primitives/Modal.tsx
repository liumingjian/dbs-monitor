import { Modal as CarbonModal } from '@carbon/react'
import type { ComponentProps } from 'react'

export type ModalProps = ComponentProps<typeof CarbonModal>

/// 模态框。组件库的 `Modal` 原样透出，只把**右上角关闭按钮的可访问名**换成中文。
///
/// 组件库的 `closeButtonLabel` 默认是 `"Close"`，同时落在那个按钮的 `aria-label` 和 `title`
/// 上：读屏用户听见的是英文，鼠标悬停看见的也是英文。界面是硬编码简体中文、没有 i18n 框架，
/// 所以这句话只能显式给；而它在九个路由文件里都要给一遍，谁忘了都不报错 —— 于是给在这里。
///
/// 名字是「关闭对话框」而不是「关闭」：对话框里常常已经有一个写着「关闭」的主按钮
/// （例如一次性口令对话框），两个按钮同名会让「按名字找按钮」这件事变成歧义，
/// 无论对读屏用户还是对测试都是。
///
/// 需要更贴切的名字（「关闭指标详情」这类）就照常传 `closeButtonLabel`，它覆盖默认值。
export function Modal({ closeButtonLabel = '关闭对话框', ...props }: ModalProps) {
  return <CarbonModal closeButtonLabel={closeButtonLabel} {...props} />
}
