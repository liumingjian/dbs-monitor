import type { HTMLAttributes, ReactNode } from 'react'
import type { StatusTone } from './StatusBadge'
import './StatusDot.css'

export type StatusDotProps = {
  tone: StatusTone
  /**
   * 状态文案。**必填**：8px 的色点靠色相区分，critical 与 warning 的亮度只差 1.04:1，
   * 色觉受限或屏幕偏色时无法分辨，所以点旁边永远有字。
   */
  children: ReactNode
} & Omit<HTMLAttributes<HTMLSpanElement>, 'children'>

/// 状态点 + 文字标签。
export function StatusDot({ tone, children, className, ...rest }: StatusDotProps) {
  return (
    <span {...rest} className={['dbs-status-dot', className].filter(Boolean).join(' ')} data-tone={tone}>
      <span className="dbs-status-dot__mark" aria-hidden="true" />
      <span className="dbs-status-dot__label">{children}</span>
    </span>
  )
}
