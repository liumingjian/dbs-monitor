import type { CSSProperties, HTMLAttributes, ReactNode } from 'react'
import type { StatusTone } from './StatusBadge'
import './MetricBar.css'

export type MetricBarProps = {
  /** 指标名。 */
  label: ReactNode
  /**
   * 已经格式化好的数值。展示层不做格式化，也不替缺数补零 ——
   * 缺数长什么样由调用方决定（本仓库的写法是「缺数」，不是 0）。
   */
  value: ReactNode
  /** 单位，跟在数值后面小一号显示。 */
  unit?: ReactNode
  /**
   * 0 到 1 的占比。给了才画那条 4px 的比例条；不给就是一个纯数字档位。
   * 超出范围的值会被夹到 [0, 1]，因为条的长度没有「负」或「超过满格」的画法。
   */
  ratio?: number
  /** 状态色。只影响 4px 的条与数值颜色，不作大面积填充。不给就是中性灰。 */
  tone?: StatusTone
  /** 数值下方的一行注解（阈值、环比、采集时刻）。 */
  caption?: ReactNode
  /** 大号数值档位，用于页面顶部的 KPI 条。 */
  emphasis?: boolean
} & HTMLAttributes<HTMLDivElement>

/// 指标条：标签 + 等宽数值（+ 可选比例条 + 可选注解）。
///
/// 数值一律等宽表格数字，这样纵向排列的多个指标条能直接比。
export function MetricBar({
  label,
  value,
  unit,
  ratio,
  tone,
  caption,
  emphasis = false,
  className,
  ...rest
}: MetricBarProps) {
  const fillStyle =
    ratio === undefined
      ? undefined
      : ({ inlineSize: `${Math.min(Math.max(ratio, 0), 1) * 100}%` } as CSSProperties)

  return (
    <div
      {...rest}
      className={['dbs-metric-bar', className].filter(Boolean).join(' ')}
      data-tone={tone}
      data-emphasis={emphasis ? 'true' : undefined}
    >
      <span className="dbs-metric-bar__label dbs-caption">{label}</span>
      <span className={emphasis ? 'dbs-metric-bar__value dbs-numeric-display' : 'dbs-metric-bar__value dbs-numeric'}>
        {value}
        {unit !== undefined && <span className="dbs-metric-bar__unit dbs-caption">{unit}</span>}
      </span>
      {fillStyle !== undefined && (
        <span className="dbs-metric-bar__track" aria-hidden="true">
          <span className="dbs-metric-bar__fill" style={fillStyle} />
        </span>
      )}
      {caption !== undefined && <span className="dbs-metric-bar__caption dbs-caption">{caption}</span>}
    </div>
  )
}
