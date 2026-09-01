import type { HTMLAttributes, ReactNode } from 'react'
import './StatusBadge.css'

/**
 * 状态语汇。**只有四档，且只表示状态**：蓝色不在其中 —— 蓝色表示「可交互」，
 * 用蓝色表示状态是本规范明确禁止的。
 *
 * 这一族取值是**视觉档位**，不是业务枚举：`critical` 不等于「严重告警」，
 * 由调用方把自己的枚举映射到这里。展示层不认识任何业务概念。
 */
export type StatusTone = 'critical' | 'warning' | 'normal' | 'unknown'

export type StatusBadgeProps = {
  tone: StatusTone
  /** 文案必填：颜色永远不是唯一信号。 */
  children: ReactNode
} & Omit<HTMLAttributes<HTMLSpanElement>, 'children'>

/// 状态徽章：浅色底 + 深色同色系文字 + 直角。
///
/// 底色是「小面积浅色」而非状态色本身的大面积填充 —— 状态色只出现在文字上，
/// 面积大小由文本长度决定，永远不会成为一整块色。
export function StatusBadge({ tone, children, className, ...rest }: StatusBadgeProps) {
  return (
    <span
      {...rest}
      className={['dbs-status-badge', 'dbs-caption', className].filter(Boolean).join(' ')}
      data-tone={tone}
    >
      {children}
    </span>
  )
}
