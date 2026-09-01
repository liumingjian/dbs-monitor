import type { HTMLAttributes } from 'react'
import './TruncatedText.css'

export type TruncatedTextProps = {
  /** 只收字符串：悬停提示要用它的全文，节点没法当提示。 */
  children: string
  /** 悬停提示文案，默认就是全文。全文本身没有可读性时（编码、指纹）才另给。 */
  title?: string
} & Omit<HTMLAttributes<HTMLSpanElement>, 'children' | 'title'>

/// 单行截断 + 省略号 + 悬停提示。
///
/// 提示用原生 `title` 而不是浮层组件：等宽 SQL 这类内容在表格里到处都是，
/// 每格挂一个浮层实例的代价与它带来的好处不成比例；`title` 还能被读屏与键盘用户取到。
export function TruncatedText({ children, title, className, ...rest }: TruncatedTextProps) {
  return (
    <span {...rest} className={['dbs-truncate', className].filter(Boolean).join(' ')} title={title ?? children}>
      {children}
    </span>
  )
}
