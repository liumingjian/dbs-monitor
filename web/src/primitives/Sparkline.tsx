import type { HTMLAttributes } from 'react'
import type { StatusTone } from './StatusBadge'
import './Sparkline.css'

export type SparklineProps = {
  /**
   * 按时间先后排好的取值。`null` 是缺数：折线在那里断开，**不补零**，
   * 也不跨过它把两端连起来 —— 一条连过缺口的线会让读者以为那段时间有数据。
   */
  values: readonly (number | null)[]
  /**
   * 无障碍名，必填。行内缩略图没有可读文字，不给名字它对屏幕阅读器就是一团噪声；
   * 调用方知道这是哪一行的什么指标（例如「TPS 近 1 小时趋势」），这里不猜。
   */
  label: string
  /** 高度 px。默认 20，配 DESIGN.md 的 40px 标准行高；32px 密集行不画缩略图。 */
  height?: number
  /** 线色档位。不给就是中性的数据色 viz-1；状态色只在调用方确实要表达状态时给。 */
  tone?: StatusTone
} & Omit<HTMLAttributes<HTMLSpanElement>, 'children'>

/// 行内趋势缩略图。
///
/// 为什么是手写 SVG 而不是图表库：图表库没有缩略图组件，官方替代做法是把折线图的
/// 坐标轴、图例、网格逐个关掉来假装，对一个几十像素宽的行内图元过重 —— 每一行都要
/// 挂一个带 ResizeObserver 的图表实例。
///
/// 宽度由容器给，不是写死的：`viewBox` 固定 100 单位宽、`preserveAspectRatio="none"`
/// 让它横向随容器缩放，`vector-effect="non-scaling-stroke"` 保证缩放后描边仍是 1.5px。
/// 1280px 下容器变窄，图元跟着窄下去，不会顶开表格也不会横向滚动。
export function Sparkline({ values, label, height = 20, tone, className, ...rest }: SparklineProps) {
  const segments = pathSegments(values)

  return (
    <span
      {...rest}
      className={['dbs-sparkline', className].filter(Boolean).join(' ')}
      data-tone={tone}
      style={{ blockSize: `${height}px`, ...rest.style }}
      role="img"
      aria-label={label}
    >
      {segments.length === 0 ? (
        // 一个点连不成线。画一条居中的短横，读者看到的是「没有走势可看」，不是空白。
        <svg className="dbs-sparkline__svg" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
          <line className="dbs-sparkline__empty" x1="0" y1="50" x2="100" y2="50" vectorEffect="non-scaling-stroke" />
        </svg>
      ) : (
        <svg className="dbs-sparkline__svg" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
          {segments.map((segment) => (
            <polyline
              key={segment[0]}
              className="dbs-sparkline__line"
              points={segment[1]}
              vectorEffect="non-scaling-stroke"
            />
          ))}
        </svg>
      )}
    </span>
  )
}

/**
 * 把取值序列切成若干段折线，缺口处断开。
 *
 * 纵向映射到 0–100 的 viewBox：最大值贴上沿、最小值贴下沿。整段是一条水平线
 * （最大值等于最小值）时画在中间，否则除以 0。返回 `[起点下标, points 属性]`。
 */
function pathSegments(values: readonly (number | null)[]): [number, string][] {
  const finite = values.filter((value): value is number => typeof value === 'number' && Number.isFinite(value))
  if (finite.length < 2) return []

  const min = Math.min(...finite)
  const max = Math.max(...finite)
  const span = max - min
  const lastIndex = Math.max(values.length - 1, 1)

  const segments: [number, string][] = []
  let current: string[] = []
  let start = 0
  values.forEach((value, index) => {
    if (typeof value !== 'number' || !Number.isFinite(value)) {
      if (current.length >= 2) segments.push([start, current.join(' ')])
      current = []
      return
    }
    if (current.length === 0) start = index
    const x = (index / lastIndex) * 100
    const y = span === 0 ? 50 : 100 - ((value - min) / span) * 100
    current.push(`${round(x)},${round(y)}`)
  })
  if (current.length >= 2) segments.push([start, current.join(' ')])
  return segments
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}
