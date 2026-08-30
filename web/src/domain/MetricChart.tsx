import { useMemo } from 'react'
import { LineChart } from '@carbon/charts-react'
import type { LineChartOptions } from '@carbon/charts-react'
import { Alignments, LegendPositions, ScaleTypes } from '@carbon/charts-react'
import { tokenValue, vizPalette } from '../styles/tokens'
import { UnavailabilityBlock, type Unavailability } from './UnavailabilityBlock'
// 图表库自带的预编译 CSS。它不属于 `index.scss` 里那张 Carbon 组件清单（那是
// `@carbon/react` 的子集），也不能走 Sass：`@carbon/charts` 只是 `@carbon/charts-react`
// 的传递依赖，本项目的 `install-strategy=linked` 不把传递依赖摊到顶层，Sass 解析不到
// 它的 `scss/` 入口。这份 CSS 里没有 `@font-face`、没有任何外部 URL（已核对），
// 字族靠 `MetricChart.css` 里的两个自定义属性接回令牌层。
import '@carbon/charts-react/styles.min.css'
// 顺序有意义：本组件的覆盖必须排在库自带样式之后。
import './MetricChart.css'

type Point = (number | null)[]

export type MetricChartSeries = {
  name: string
  unit: string
  points: Point[]
}

export type MetricThreshold = {
  label: string
  unit: string
  value: number
  severity: 'critical' | 'warning' | 'info'
}

type MetricChartProps = {
  label: string
  series: MetricChartSeries[]
  step: string
  unavailability: Unavailability | null
  unavailabilityHref: string
  unavailabilityDetail?: string
  /**
   * 跨图联动光标的分组名。**当前无效果**：图表库没有 `echarts.connect` 的等价物
   * （`BaseChartOptions` 里没有任何跨实例同步选项，`Chart.services` 是未公开的内部对象）。
   * 参数保留是为了不动调用方，联动能力的去留见结题报告。
   */
  connectionGroup?: string
  loading?: boolean
  thresholds?: readonly MetricThreshold[]
}

export function metricUnavailability(metric: { unavailability: Unavailability | null } | undefined): Unavailability | null {
  return metric === undefined ? 'NO_SAMPLES_YET' : metric.unavailability
}

export function chartData(points: Point[]): [number, number | null][] {
  const data: [number, number | null][] = []
  for (const point of points) {
    const timestamp = point[0]
    if (typeof timestamp === 'number') data.push([timestamp * 1000, point[1] ?? null])
  }
  return data
}

const decimal = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })
const percentDecimal = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 })

function compactCount(value: number): string {
  const magnitude = Math.abs(value)
  if (magnitude >= 1_000_000_000) return `${decimal.format(value / 1_000_000_000)}B`
  if (magnitude >= 1_000_000) return `${decimal.format(value / 1_000_000)}M`
  if (magnitude >= 10_000) return `${decimal.format(value / 1000)}K`
  return decimal.format(value)
}

function formatBytes(value: number): string {
  const magnitude = Math.abs(value)
  if (magnitude >= 1024 ** 4) return `${decimal.format(value / 1024 ** 4)} TB`
  if (magnitude >= 1024 ** 3) return `${decimal.format(value / 1024 ** 3)} GB`
  if (magnitude >= 1024 ** 2) return `${decimal.format(value / 1024 ** 2)} MB`
  if (magnitude >= 1024) return `${decimal.format(value / 1024)} KB`
  return `${decimal.format(value)} B`
}

/**
 * `value` is how an operator reads the number ("96.3%"), `axis` is the terser form for
 * a tick label (the unit is already the axis name), and `zeroBaseline` says whether
 * anchoring the axis at zero is honest: it is for a count, but it destroys
 * resolution for byte counts and rates, whose variation sits far above zero.
 * One row per unit, so a new unit is one edit rather than four dispatches on the same string.
 */
type UnitStyle = {
  value: (value: number) => string
  axis: (value: number) => string
  zeroBaseline: boolean
}

const unitStyles: Record<string, UnitStyle> = {
  // percent 的下限由 `percentAxisDomain` 单独决定，不走 zeroBaseline 这条路。
  percent: { value: (value) => `${percentDecimal.format(value)}%`, axis: (value) => `${value}%`, zeroBaseline: true },
  bytes: { value: formatBytes, axis: (value) => formatBytes(value).replace(' ', ''), zeroBaseline: false },
  'bytes/s': { value: (value) => `${formatBytes(value)}/s`, axis: (value) => formatBytes(value).replace(' ', ''), zeroBaseline: false },
  count: { value: compactCount, axis: compactCount, zeroBaseline: true },
  state: { value: compactCount, axis: compactCount, zeroBaseline: true },
  '': { value: compactCount, axis: compactCount, zeroBaseline: false },
  ms: { value: (value) => `${decimal.format(value)} ms`, axis: compactCount, zeroBaseline: false },
  seconds: { value: (value) => `${decimal.format(value)} s`, axis: compactCount, zeroBaseline: false },
}

/** `unit` is free-form API text, not a closed enum, so an unknown one keeps its own name verbatim. */
function unitStyle(unit: string): UnitStyle {
  return unitStyles[unit] ?? {
    value: (value) => `${compactCount(value)} ${unit}`,
    axis: compactCount,
    zeroBaseline: false,
  }
}

/**
 * Renders a metric value the way an operator reads it, not the way the API emits it:
 * "96.32 percent" becomes "96.3%", "522680.35 bytes/s" becomes "510.43 KB/s".
 */
export function formatMetricNumber(value: number, unit: string): string {
  return unitStyle(unit).value(value)
}

/// 严重度对应的语义令牌名。色值不写在 TS 里，见 `web/src/styles/tokens.ts`。
function severityToken(severity: MetricThreshold['severity']): string {
  switch (severity) {
    case 'critical':
      return '--dbs-status-critical'
    case 'warning':
      return '--dbs-status-warning'
    case 'info':
      return '--dbs-status-unknown'
    default:
      return assertNever(severity)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected threshold severity: ${value}`)
}

/**
 * A threshold is only meaningful against its own unit's axis, so it rides the first
 * series carrying that unit. Attaching every threshold to series 0 drew a byte
 * threshold against the percent axis on the dual-unit disk chart.
 */
export function thresholdsForSeries(
  thresholds: readonly MetricThreshold[],
  series: MetricChartSeries[],
  index: number,
): MetricThreshold[] {
  const unit = series[index]?.unit
  if (unit === undefined) return []
  if (series.findIndex((item) => item.unit === unit) !== index) return []
  return thresholds.filter((threshold) => threshold.unit === unit)
}

/**
 * 百分比轴的下限。
 *
 * 这是**新行为**，不是旧实现的搬运：旧实现把百分比轴钉死在 0，缓冲区命中率
 * 于是被画成贴在框顶的一条直线，正是规范抱怨的那个失败。
 *
 * 规则，按这个顺序：
 * 1. `low`（数据与百分比阈值里最小的那个）不超过 10 时下限取 0 —— 数据真的逼近零，
 *    这时钉零是诚实的，放大反而制造出不存在的波动。
 * 2. 否则向下取到 10 的整数倍，但**不超过 90**。上限至少是 100（见 `percentCeiling`），
 *    所以轴永远至少留 10 个百分点的窗口，不会退化成一条被放大到全屏的噪声。
 * 3. 负值（理论上不该出现，但 `unit` 是 API 的自由文本）照样向下取整，不夹到 0。
 */
export function percentAxisFloor(low: number): number {
  const step = Math.floor(low / 10) * 10
  if (low <= 10) return Math.min(0, step)
  return Math.min(90, step)
}

/**
 * 百分比轴的取值区间。
 *
 * 阈值参与下限的计算：一条画不进框里的阈值线等于没画。
 * 没有任何有限取值时退回 `[0, 上限]`，不去猜一个窗口。
 */
export function percentAxisDomain(
  series: readonly MetricChartSeries[],
  thresholds: readonly MetricThreshold[],
): [number, number] {
  const percentThresholds = thresholds.filter((threshold) => threshold.unit === 'percent').map((item) => item.value)
  const ceiling = percentCeiling(thresholds)
  const values: number[] = []
  for (const item of series) {
    if (item.unit !== 'percent') continue
    for (const point of item.points) {
      const value = point[1]
      if (typeof value === 'number' && Number.isFinite(value)) values.push(value)
    }
  }
  const low = Math.min(...values, ...percentThresholds)
  if (!Number.isFinite(low)) return [0, ceiling]
  return [percentAxisFloor(low), ceiling]
}

/** `Math.max(100)` with no thresholds, so an ordinary percent axis still ends at 100. */
function percentCeiling(thresholds: readonly MetricThreshold[]): number {
  return Math.max(100, ...thresholds.filter((threshold) => threshold.unit === 'percent').map((threshold) => threshold.value))
}

type ChartRow = { group: string; date: Date; value: number | null }

/// 图表库吃的是长表（每行一个点），不是 echarts 那种按系列分组的二维数组。
function chartRows(series: MetricChartSeries[]): ChartRow[] {
  return series.flatMap((item) =>
    chartData(item.points).map(([timestamp, value]) => ({ group: item.name, date: new Date(timestamp), value })),
  )
}

/**
 * Every series gets its own unit. A shared formatter printed the disk chart's
 * free-bytes series as a percentage.
 *
 * 图表库按 `(value, label)` 回调，`label` 是系列名；时间那一行传进来的是 `Date`。
 */
function tooltipValueFormatter(series: MetricChartSeries[]): (value: unknown, label?: string) => string {
  const units = new Map(series.map((item) => [item.name, item.unit]))
  return (value, label) => {
    if (value instanceof Date) return value.toLocaleString()
    const unit = label === undefined ? undefined : units.get(label)
    if (typeof value === 'number' && unit !== undefined) return formatMetricNumber(value, unit)
    // A gap must read as a gap; there is no unit under which it becomes a number.
    if (value === null || value === undefined) return '缺数'
    return String(value)
  }
}

export function MetricChart({
  label,
  series,
  step,
  unavailability,
  unavailabilityHref,
  unavailabilityDetail,
  loading = false,
  thresholds,
}: MetricChartProps) {
  if (unavailability) {
    return <UnavailabilityBlock
      code={unavailability}
      href={unavailabilityHref}
      detail={unavailabilityDetail}
    />
  }
  return <ChartFigure
    label={label}
    series={series}
    step={step}
    loading={loading}
    thresholds={thresholds}
  />
}

function ChartFigure({ label, series, step, loading, thresholds }: {
  label: string
  series: MetricChartSeries[]
  step: string
  loading: boolean
  thresholds?: readonly MetricThreshold[]
}) {
  const drawn = useMemo(() => thresholds ?? [], [thresholds])
  const data = useMemo(() => chartRows(series), [series])
  const options = useMemo(() => chartOptions(series, drawn), [series, drawn])

  const tableRows = series.flatMap((item) => chartData(item.points).map(([timestamp, value]) => ({
    key: `${item.name}-${timestamp}`,
    timestamp,
    name: item.name,
    value,
    unit: item.unit,
  })))

  return (
    <figure className="metric-figure" data-testid="metric-chart" data-loading={loading} aria-label={`${label}趋势`}>
      <div className="metric-chart-canvas">
        <LineChart data={data} options={options} />
      </div>
      {drawn.length > 0 && (
        // 图表库的阈值标签只在悬停时浮出来，旧实现是常驻的。少一块常驻的数值就是少一项能力，
        // 所以这里把它补回来 —— 顺带它对屏幕阅读器可读，画在 SVG 里的那个标签从来不是。
        <ul className="metric-thresholds dbs-caption">
          {drawn.map((threshold) => (
            <li key={`${threshold.unit}-${threshold.label}-${threshold.value}`} data-severity={threshold.severity}>
              <span className="metric-thresholds__rule" aria-hidden="true" />
              {threshold.label} {formatMetricNumber(threshold.value, threshold.unit)}
            </li>
          ))}
        </ul>
      )}
      <figcaption>实际粒度：{step}</figcaption>
      <details className="metric-data-table">
        <summary>查看数据表</summary>
        <table>
          <thead>
            <tr><th>时间</th><th>序列</th><th>值</th></tr>
          </thead>
          <tbody>{tableRows.map((row) => (
            <tr key={row.key}>
              <td>{new Date(row.timestamp).toLocaleString()}</td>
              <td>{row.name}</td>
              <td>{row.value == null ? '缺数' : formatMetricNumber(row.value, row.unit)}</td>
            </tr>
          ))}</tbody>
        </table>
      </details>
    </figure>
  )
}

/**
 * 选项推导。只依赖入参与令牌层：没有 ref、没有 DOM 尺寸、没有实例状态。
 *
 * 视觉上刻意关掉的东西：数据点标记（`points.enabled`）、纵向网格线（`grid.x`）、
 * 工具条、缩放条，以及**全部动画** —— 一个每 10–30 秒重新拉数的面板每次刷新都重放
 * 一遍入场，读起来就像数据自己在跳。
 */
function chartOptions(
  series: MetricChartSeries[],
  thresholds: readonly MetricThreshold[],
): LineChartOptions {
  const units = [...new Set(series.map((item) => item.unit))]
  const palette = vizPalette().filter((color) => color !== '')
  const scale: Record<string, string> = {}
  series.forEach((item, index) => {
    const color = palette[index % palette.length]
    if (color !== undefined) scale[item.name] = color
  })

  return {
    // 刻意不给 SVG 无障碍名：可访问名由外层 `<figure aria-label>` 一处给出。
    // 两处都给会让同一张图在无障碍树里出现两个同名节点。
    animations: false,
    resizable: true,
    height: '100%',
    toolbar: { enabled: false },
    points: { enabled: false },
    grid: { x: { enabled: false }, y: { enabled: true } },
    legend: {
      enabled: series.length > 1,
      position: LegendPositions.TOP,
      alignment: Alignments.LEFT,
      clickable: false,
    },
    tooltip: { valueFormatter: tooltipValueFormatter(series) },
    ...(Object.keys(scale).length > 0 ? { color: { scale } } : {}),
    axes: {
      bottom: { mapsTo: 'date', scaleType: ScaleTypes.TIME },
      left: axisOptions(units[0], series, thresholds),
      ...(units.length > 1
        ? {
            right: {
              ...axisOptions(units[1], series, thresholds),
              correspondingDatasets: series.filter((item) => item.unit === units[1]).map((item) => item.name),
            },
          }
        : {}),
    },
  }
}

/// 一个单位一根轴。百分比轴走上面那条下限规则，其余单位沿用旧行为：
/// 计数类钉零，字节数与耗时类按数据实际范围取。
function axisOptions(unit: string | undefined, series: MetricChartSeries[], thresholds: readonly MetricThreshold[]) {
  if (unit === undefined) return { mapsTo: 'value', scaleType: ScaleTypes.LINEAR }
  const style = unitStyle(unit)
  const drawn = thresholds.filter((threshold) => threshold.unit === unit)
  return {
    mapsTo: 'value',
    scaleType: ScaleTypes.LINEAR,
    ...(unit === '' ? {} : { title: unit }),
    ticks: { formatter: (tick: number | Date) => (tick instanceof Date ? tick.toLocaleString() : style.axis(tick)) },
    ...(unit === 'percent'
      ? { domain: percentAxisDomain(series, thresholds), includeZero: false }
      : { includeZero: style.zeroBaseline }),
    thresholds: drawn.map((threshold) => ({
      value: threshold.value,
      label: threshold.label,
      fillColor: tokenValue(severityToken(threshold.severity)),
      valueFormatter: (value: number) => formatMetricNumber(value, unit),
    })),
  }
}
