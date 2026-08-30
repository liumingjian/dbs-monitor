import { useEffect, useRef } from 'react'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, MarkLineComponent, TooltipComponent } from 'echarts/components'
import type { EChartsType } from 'echarts/core'
import { connect, init, use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { UnavailabilityBlock, type Unavailability } from './UnavailabilityBlock'

use([LineChart, GridComponent, LegendComponent, MarkLineComponent, TooltipComponent, CanvasRenderer])

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
 * anchoring the axis at zero is honest: it is for a percentage, but it destroys
 * resolution for byte counts and rates, whose variation sits far above zero.
 * One row per unit, so a new unit is one edit rather than four dispatches on the same string.
 */
type UnitStyle = {
  value: (value: number) => string
  axis: (value: number) => string
  zeroBaseline: boolean
}

const unitStyles: Record<string, UnitStyle> = {
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

function severityColor(severity: MetricThreshold['severity']): string {
  switch (severity) {
    case 'critical':
      return '#cf1322'
    case 'warning':
      return '#d46b08'
    case 'info':
      return '#8c8c8c'
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

type TooltipRow = { seriesIndex: number; seriesName?: string; marker?: string; value?: unknown }

/**
 * Every series gets its own unit. A shared formatter printed the disk chart's
 * free-bytes series as a percentage.
 */
function tooltipFormatter(series: MetricChartSeries[]): (params: unknown) => string {
  return (params) => {
    const rows = (Array.isArray(params) ? params : [params]) as TooltipRow[]
    const head = rows[0]
    const heading = head !== undefined && Array.isArray(head.value) && typeof head.value[0] === 'number'
      ? new Date(head.value[0]).toLocaleString()
      : ''
    const lines = rows.map((row) => {
      const raw = Array.isArray(row.value) ? row.value[1] : undefined
      const unit = series[row.seriesIndex]?.unit
      // A gap must read as a gap; there is no unit under which it becomes a number.
      const text = typeof raw === 'number' && unit !== undefined ? formatMetricNumber(raw, unit) : '缺数'
      return `${row.marker ?? ''}${row.seriesName ?? ''}: ${text}`
    })
    return [heading, ...lines].join('<br/>')
  }
}

export function MetricChart({
  label,
  series,
  step,
  unavailability,
  unavailabilityHref,
  unavailabilityDetail,
  connectionGroup,
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
  return <ChartCanvas
    label={label}
    series={series}
    step={step}
    loading={loading}
    connectionGroup={connectionGroup}
    thresholds={thresholds}
  />
}

function ChartCanvas({ label, series, step, loading, connectionGroup, thresholds }: {
  label: string
  series: MetricChartSeries[]
  step: string
  loading: boolean
  connectionGroup?: string
  thresholds?: readonly MetricThreshold[]
}) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const chart = init(ref.current)
    chartRef.current = chart
    const observer = new ResizeObserver(() => chart.resize())
    observer.observe(ref.current)
    return () => {
      observer.disconnect()
      chartRef.current = null
      chart.dispose()
    }
  }, [])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    chart.group = connectionGroup ?? `metric-chart-${chart.id}`
    if (connectionGroup) connect(connectionGroup)
    const units = [...new Set(series.map((item) => item.unit))]
    const drawn = thresholds ?? []
    chart.setOption({
      // A dashboard that repolls every 10-30s must not replay an entrance on every
      // refresh: the motion would read as the data itself jumping.
      animation: false,
      grid: { left: 62, right: units.length > 1 ? 62 : 20, top: series.length > 1 ? 50 : 24, bottom: 44 },
      legend: { show: series.length > 1, top: 0, type: 'scroll' },
      tooltip: { trigger: 'axis', axisPointer: { type: 'line' }, formatter: tooltipFormatter(series) },
      xAxis: {
        type: 'time',
        axisLabel: { formatter: '{HH}:{mm}', hideOverlap: true },
        axisLine: { lineStyle: { color: '#c3c2b7' } },
        splitLine: { show: false },
      },
      yAxis: units.map((unit, index) => {
        const style = unitStyle(unit)
        return {
          type: 'value',
          name: unit,
          min: style.zeroBaseline ? 0 : undefined,
          // 100 is the natural ceiling for a percentage, but a threshold above it
          // would then be drawn outside the grid, which is worse than a taller axis.
          max: unit === 'percent' ? percentCeiling(drawn) : undefined,
          scale: !style.zeroBaseline,
          axisLabel: { formatter: style.axis },
          position: index === 0 ? 'left' : 'right',
          splitLine: { show: index === 0, lineStyle: { color: '#e1e0d9', width: 1 } },
        }
      }),
      series: series.map((item, index) => ({
        name: item.name,
        type: 'line',
        data: chartData(item.points),
        yAxisIndex: units.indexOf(item.unit),
        connectNulls: false,
        showSymbol: false,
        lineStyle: { color: chartColors[index % chartColors.length], width: 2 },
        // The alerting threshold belongs on the chart it is evaluated against.
        ...markLineOption(thresholdsForSeries(drawn, series, index)),
      })),
    }, { notMerge: true })
  }, [connectionGroup, series, thresholds])

  const tableRows = series.flatMap((item) => chartData(item.points).map(([timestamp, value]) => ({
    key: `${item.name}-${timestamp}`,
    timestamp,
    name: item.name,
    value,
    unit: item.unit,
  })))

  return (
    <figure className="metric-figure" data-testid="metric-chart" data-loading={loading} aria-label={`${label}趋势`}>
      <div ref={ref} className="metric-chart-canvas" />
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

/** `Math.max(100)` with no thresholds, so an ordinary percent axis still ends at 100. */
function percentCeiling(thresholds: readonly MetricThreshold[]): number {
  return Math.max(100, ...thresholds.filter((threshold) => threshold.unit === 'percent').map((threshold) => threshold.value))
}

function markLineOption(thresholds: MetricThreshold[]) {
  if (thresholds.length === 0) return {}
  return {
    markLine: {
      silent: true,
      symbol: 'none',
      data: thresholds.map((threshold) => ({
        yAxis: threshold.value,
        lineStyle: { color: severityColor(threshold.severity), type: 'dashed', width: 1 },
        label: {
          formatter: `${threshold.label} ${formatMetricNumber(threshold.value, threshold.unit)}`,
          position: 'insideEndTop',
          color: severityColor(threshold.severity),
          fontSize: 11,
        },
      })),
    },
  }
}

const chartColors = ['#1677ff', '#d46b08', '#389e0d', '#c41d7f', '#531dab', '#08979c']
