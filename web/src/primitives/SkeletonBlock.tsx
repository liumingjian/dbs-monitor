import type { CSSProperties } from 'react'
import './SkeletonBlock.css'

export type SkeletonBlockProps = {
  /** 骨架行数，默认 3。行宽逐行递减，最后一行 60%，看起来像一段文字而不是一堆条。 */
  lines?: number
  /** 单行高度，任何 CSS 长度。给了 `lines={1}` 加一个大高度就是一块占位面。 */
  height?: string
  /** 整块宽度，任何 CSS 长度，默认 100%。 */
  width?: string
  /** 读屏播报的内容，默认「加载中」。 */
  label?: string
  /**
   * 纯装饰。整片区域已经在别处声明了加载态时（表格骨架行就是这样，
   * 加载态在表格容器的 `aria-busy` 上）用它，免得读屏把每一格都播报一遍。
   */
  decorative?: boolean
  className?: string
}

/// 加载骨架。**规范要求用骨架占位，不要整页转圈** —— 已经加载好的部分要先看得见。
export function SkeletonBlock({
  lines = 3,
  height,
  width,
  label = '加载中',
  decorative = false,
  className,
}: SkeletonBlockProps) {
  const style = { inlineSize: width, '--dbs-skeleton-line-height': height } as CSSProperties
  return (
    <div
      className={['dbs-skeleton', className].filter(Boolean).join(' ')}
      style={style}
      role={decorative ? undefined : 'status'}
      aria-label={decorative ? undefined : label}
      aria-hidden={decorative ? 'true' : undefined}
    >
      {Array.from({ length: lines }, (_, index) => (
        <span
          className="dbs-skeleton__line"
          key={index}
          data-last={index === lines - 1 && lines > 1 ? 'true' : undefined}
        />
      ))}
    </div>
  )
}
