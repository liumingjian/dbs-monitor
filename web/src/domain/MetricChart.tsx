import { useEffect, useRef } from 'react'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import type { EChartsType } from 'echarts/core'
import { init, use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import type { components } from '../api/schema'
import { UnavailabilityBlock, type Unavailability } from './UnavailabilityBlock'

use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

type Point = (number | null)[]

type MetricChartProps = {
  label: string
  unit: string
  points: Point[]
  step: string
  unavailability: Unavailability | null
  loading?: boolean
}

export function metricUnavailability(metric: { unavailability: Unavailability | null } | undefined): Unavailability | null {
  return metric === undefined ? 'NO_SAMPLES_YET' : metric.unavailability
}

export function chartData(points: Point[]): [number, number | null][] {
  return points.flatMap((point) => typeof point[0] === 'number' ? [[point[0] * 1000, point[1] ?? null] as [number, number | null]] : [])
}

export function MetricChart({ label, unit, points, step, unavailability, loading = false }: MetricChartProps) {
  if (unavailability) return <UnavailabilityBlock code={unavailability} />
  return <ChartCanvas label={label} unit={unit} points={chartData(points)} step={step} loading={loading} />
}

function ChartCanvas({ label, unit, points, step, loading }: { label: string; unit: string; points: [number, number | null][]; step: string; loading: boolean }) {
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
    chart.setOption({
      animation: false,
      grid: { left: 52, right: 20, top: 24, bottom: 54 },
      tooltip: { trigger: 'axis', axisPointer: { type: 'line' }, valueFormatter: (value: unknown) => value == null ? '缺数' : `${value}` },
      xAxis: { type: 'time', axisLine: { lineStyle: { color: '#c3c2b7' } }, splitLine: { show: false } },
      yAxis: { type: 'value', name: unit, min: 0, splitLine: { lineStyle: { color: '#e1e0d9', width: 1 } } },
      series: [{ name: label, type: 'line', data: points, connectNulls: false, showSymbol: false, lineStyle: { color: '#2a78d6', width: 2 } }],
    })
  }, [label, unit, points])

  return (
    <figure style={{ margin: 0, opacity: loading ? 0.55 : 1 }} aria-label={`${label}趋势`}>
      <div ref={ref} style={{ height: 340, width: '100%' }} />
      <figcaption>实际粒度：{step}</figcaption>
      <details>
        <summary>查看数据表</summary>
        <table><thead><tr><th>时间</th><th>{label}</th></tr></thead><tbody>{points.map(([timestamp, value]) => <tr key={timestamp}><td>{new Date(timestamp).toISOString()}</td><td>{value == null ? '缺数' : value}</td></tr>)}</tbody></table>
      </details>
    </figure>
  )
}

export type MetricResponse = components['schemas']['MetricSeriesResponse']
