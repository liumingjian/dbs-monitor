import { Toggle as CarbonToggle } from '@carbon/react'
import type { ComponentProps } from 'react'
import './Toggle.css'

export type ToggleProps = ComponentProps<typeof CarbonToggle>

/// 开关。组件库的 `Toggle` 原样透出，修掉它两处「看得见的地方点不动」的缺陷，
/// 顺带把两侧的英文默认文案换成中文。
///
/// **缺陷一：可点区域与控件本身不是同一个元素。** 真正的 `<button role="switch">` 被画成
/// 1px 的 visually-hidden 元素，可见的滑块在它后面的 `<label>` 里。角色、键盘、读屏都对，
/// 但指针落在滑块上时命中的是 label —— 任何「按可访问名找到这个控件再点它」的做法
/// （包括真人用鼠标瞄准它、也包括 e2e）都会落在一个 1px 的目标上，或者被判为「被遮挡」。
/// 一个控件应当只有一个命中区，所以这里把 button 铺回控件本身的位置并保持透明。
///
/// **缺陷二：`labelText` 为空时连键盘之外的路都没有。** 组件库把标签渲染成 `<label>` 还是
/// 普通 `<div>`，取决于 `labelText` 真不真：为空时它是 `<div>`，`for=` 不再转发点击。
/// 1.115 起组件库在外层 div 上补了一个 onClick 兜底，但那个兜底建立在「点到的是 label」
/// 之上，仍然不是「点到了这枚 switch」。缺陷一的修法把两种情况一起解决：无论
/// `labelText` 给没给，指针命中的都是那枚 button 自己。
///
/// 角色与 DOM 次序都没动，所以组件库的焦点环（`button:focus + label .cds--toggle__switch::after`
/// 这条兄弟选择器）照常工作。
///
/// 两侧文案（`labelA` / `labelB`）默认是英文的 `Off` / `On`，不给就静默生效，
/// 所以在这里给上中文默认值；照常可以用同名 props 覆盖。
export function Toggle({ className, labelA = '关', labelB = '开', ...props }: ToggleProps) {
  return (
    <CarbonToggle
      className={['dbs-toggle', className].filter(Boolean).join(' ')}
      labelA={labelA}
      labelB={labelB}
      {...props}
    />
  )
}
