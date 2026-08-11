import { useEffect, useRef } from 'react'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import type { EChartsType } from 'echarts/core'
import { connect, init, use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { UnavailabilityBlock, type Unavailability } from './UnavailabilityBlock'

use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

type Point = (number | null)[]

export type MetricChartSeries = {
  name: string
  unit: string
  points: Point[]
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

export function MetricChart({
  label,
  series,
  step,
  unavailability,
  unavailabilityHref,
  unavailabilityDetail,
  connectionGroup,
  loading = false,
}: MetricChartProps) {
  if (unavailability) {
    return <UnavailabilityBlock
      code={unavailability}
      href={unavailabilityHref}
      detail={unavailabilityDetail}
    />
  }
  return <ChartCanvas label={label} series={series} step={step} loading={loading} connectionGroup={connectionGroup} />
}

function ChartCanvas({ label, series, step, loading, connectionGroup }: {
  label: string
  series: MetricChartSeries[]
  step: string
  loading: boolean
  connectionGroup?: string
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
    chart.setOption({
      animation: false,
      grid: { left: 54, right: units.length > 1 ? 54 : 20, top: series.length > 1 ? 50 : 24, bottom: 44 },
      legend: { show: series.length > 1, top: 0, type: 'scroll' },
      tooltip: { trigger: 'axis', axisPointer: { type: 'line' }, valueFormatter: (value: unknown) => value == null ? '缺数' : `${value}` },
      xAxis: {
        type: 'time',
        axisLabel: { formatter: '{HH}:{mm}' },
        axisLine: { lineStyle: { color: '#c3c2b7' } },
        splitLine: { show: false },
      },
      yAxis: units.map((unit, index) => ({
        type: 'value',
        name: unit,
        min: 0,
        position: index === 0 ? 'left' : 'right',
        splitLine: { show: index === 0, lineStyle: { color: '#e1e0d9', width: 1 } },
      })),
      series: series.map((item, index) => ({
        name: item.name,
        type: 'line',
        data: chartData(item.points),
        yAxisIndex: units.indexOf(item.unit),
        connectNulls: false,
        showSymbol: false,
        lineStyle: { color: chartColors[index % chartColors.length], width: 2 },
      })),
    }, { notMerge: true })
  }, [connectionGroup, series])

  const tableRows = series.flatMap((item) => chartData(item.points).map(([timestamp, value]) => ({
    key: `${item.name}-${timestamp}`,
    timestamp,
    name: item.name,
    value,
    unit: item.unit,
  })))

  return (
    <figure style={{ margin: 0, opacity: loading ? 0.55 : 1 }} aria-label={`${label}趋势`}>
      <div ref={ref} className="metric-chart-canvas" />
      <figcaption>实际粒度：{step}</figcaption>
      <details>
        <summary>查看数据表</summary>
        <table>
          <thead>
            <tr><th>时间</th><th>序列</th><th>值</th></tr>
          </thead>
          <tbody>{tableRows.map((row) => (
            <tr key={row.key}>
              <td>{new Date(row.timestamp).toISOString()}</td>
              <td>{row.name}</td>
              <td>{row.value == null ? '缺数' : `${row.value} ${row.unit}`}</td>
            </tr>
          ))}</tbody>
        </table>
      </details>
    </figure>
  )
}

const chartColors = ['#1677ff', '#d46b08', '#389e0d', '#c41d7f', '#531dab', '#08979c']
