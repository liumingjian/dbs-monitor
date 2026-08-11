import { InfoCircleOutlined, ProfileOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Descriptions, Modal, Segmented, Select, Space, Spin, Switch, Tabs, Typography } from 'antd'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { MetricChart, metricUnavailability, type MetricChartSeries } from '../../domain/MetricChart'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import type { Unavailability } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import { metricOption, type MetricID } from './metricOptions'
import {
  findStandardMonitoringChart,
  standardMonitoringGroups,
  standardMonitoringMetricIDs,
  type StandardMonitoringChart,
} from './standardMonitoring'
import {
  defaultTimeRange,
  parseTimeRange,
  type ChartColumns,
  type MetricStep,
  type MonitoringSearch,
} from './timeRange'
import { WorkbenchHeader } from './workbench'

export const standardMonitoringRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/monitoring',
  validateSearch: (search): MonitoringSearch | { error: string } => parseTimeRange(search),
  component: StandardMonitoringRoutePage,
})

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]

const standardMonitoringPollingOptions = { refetchInterval: pollingIntervals.standardMonitoring }

function StandardMonitoringRoutePage() {
  const { id } = standardMonitoringRoute.useParams()
  const search = standardMonitoringRoute.useSearch()

  if ('error' in search) {
    return <Alert
      type="error"
      showIcon
      title={search.error}
      action={<Link to="/instances/$id/monitoring" params={{ id }} search={defaultTimeRange()}><Button>使用最近一小时</Button></Link>}
    />
  }

  return <StandardMonitoringPage id={id} search={search} />
}

function StandardMonitoringPage({ id, search }: { id: string; search: MonitoringSearch }) {
  const navigate = standardMonitoringRoute.useNavigate()
  const instanceQuery = $api.useQuery(
    'get',
    '/api/v1/instances/{id}',
    { params: { path: { id } } },
    standardMonitoringPollingOptions,
  )

  const step = search.step ?? 'auto'
  const columns = search.columns ?? 2
  const connected = search.connect ?? true
  const metricsQuery = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id },
      query: { metric: standardMonitoringMetricIDs, from: search.from, to: search.to, step },
    },
  }, standardMonitoringPollingOptions)
  const selectedChart = findStandardMonitoringChart(search.metric)

  function updateSearch(update: Partial<MonitoringSearch>) {
    void navigate({ search: { ...search, ...update } })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="monitoring" search={search} />
    <Tabs activeKey="standard" items={[
      { key: 'standard', label: '标准监控' },
      { key: 'enhanced', label: '增强监控', disabled: true },
    ]} />

    <section id="monitoring-controls" className="monitoring-controls" aria-label="标准监控控制">
      <TimeRangePicker
        from={search.from}
        to={search.to}
        onChange={(range) => updateSearch(range)}
      />
      <Space wrap>
        <label htmlFor="metric-step">数据粒度</label>
        <Select<MetricStep>
          id="metric-step"
          aria-label="数据粒度"
          value={step}
          options={[
            { value: 'auto', label: '自动' },
            { value: '15s', label: '15 秒' },
            { value: '1m', label: '1 分钟' },
            { value: '5m', label: '5 分钟' },
            { value: 'raw', label: '原始粒度' },
          ]}
          onChange={(value) => updateSearch({ step: value })}
          style={{ width: 132 }}
        />
        <span>列数</span>
        <Segmented<ChartColumns>
          aria-label="图表列数"
          value={columns}
          options={[{ label: '1 列', value: 1 }, { label: '2 列', value: 2 }, { label: '3 列', value: 3 }]}
          onChange={(value) => updateSearch({ columns: value })}
        />
        <Switch aria-label="光标联动" checked={connected} onChange={(value) => updateSearch({ connect: value })} />
        <span>光标联动</span>
        {metricsQuery.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metricsQuery.dataUpdatedAt}
          collectionInterval={standardMonitoringPollingOptions.refetchInterval}
        />}
      </Space>
    </section>

    {metricsQuery.isPending ? <Spin size="large" /> : standardMonitoringGroups.map((group) => (
      <section key={group.key} aria-labelledby={`${group.key}-heading`}>
        <Typography.Title id={`${group.key}-heading`} level={3}>{group.title}</Typography.Title>
        <div className="metric-grid" data-columns={columns}>
          {group.charts.map((chart) => {
            const view = buildChartView(chart, metricsQuery.data?.metrics)
            const primaryMetric = chart.metrics[0]
            return (
              <Card
                key={chart.key}
                className="metric-card"
                title={chart.title}
                extra={<Space size="small">
                  {chart.drilldown && <Button
                    type="link"
                    size="small"
                    icon={<ProfileOutlined />}
                    href={longQuerySamplesHref(id, search)}
                  >查看采样记录</Button>}
                  <Button
                    type="text"
                    size="small"
                    icon={<InfoCircleOutlined />}
                    onClick={() => updateSearch({ metric: primaryMetric })}
                  >指标详情</Button>
                </Space>}
              >
                <MetricChart
                  label={chart.title}
                  series={view.series}
                  step={metricsQuery.data?.step ?? step}
                  unavailability={view.unavailability}
                  unavailabilityHref={unavailabilityHref(id, primaryMetric, view.unavailability)}
                  connectionGroup={connected ? `standard-monitoring-${id}` : undefined}
                  loading={metricsQuery.isFetching}
                />
              </Card>
            )
          })}
        </div>
      </section>
    ))}

    <MetricDetails
      chart={selectedChart}
      metrics={metricsQuery.data?.metrics}
      onClose={() => updateSearch({ metric: undefined })}
    />
  </Space>
}

function buildChartView(
  chart: StandardMonitoringChart,
  responseMetrics: ResponseMetric[] | undefined,
): { series: MetricChartSeries[]; unavailability: Unavailability | null } {
  const returned = chart.metrics.map((metricID) => ({
    metricID,
    response: responseMetrics?.find((metric) => metric.metric === metricID),
  }))
  const series = returned.flatMap(({ metricID, response }) => {
    if (!response || response.unavailability !== null) return []
    return response.series.map((item) => ({
      name: seriesName(metricID, item.labels),
      unit: response.unit,
      points: item.points,
    }))
  })
  if (series.length > 0) return { series, unavailability: null }
  return {
    series: [],
    unavailability: metricUnavailability(returned.find(({ response }) => response !== undefined)?.response),
  }
}

function seriesName(metric: MetricID, labels: Record<string, string>): string {
  const dimensions = Object.entries(labels).map(([key, value]) => `${key}=${value}`).join(', ')
  const label = metricOption(metric).label
  return dimensions ? `${label} · ${dimensions}` : label
}

function unavailabilityHref(id: string, metric: MetricID, code: Unavailability | null): string {
  if (code === 'NO_DATA_IN_RANGE' || code === 'NO_SAMPLES_YET' || code === 'COUNTER_RESET') return '#monitoring-controls'
  return `/instances/${encodeURIComponent(id)}/collection?metric=${encodeURIComponent(metric)}`
}

function longQuerySamplesHref(id: string, search: MonitoringSearch): string {
  const params = new URLSearchParams({
    from: search.from,
    to: search.to,
    metric: 'pg.query.long_running_count',
    sampled_at: search.to,
  })
  return `/instances/${encodeURIComponent(id)}/sessions/long-query-samples?${params.toString()}`
}

function MetricDetails({ chart, metrics, onClose }: {
  chart: StandardMonitoringChart | undefined
  metrics: ResponseMetric[] | undefined
  onClose: () => void
}) {
  return <Modal title={chart?.title ?? '指标详情'} open={chart !== undefined} footer={null} onCancel={onClose}>
    {chart && <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Typography.Paragraph>{chart.description}</Typography.Paragraph>
      <Descriptions column={1} size="small" bordered items={chart.metrics.map((metric) => {
        const response = metrics?.find((item) => item.metric === metric)
        return {
          key: metric,
          label: metricOption(metric).label,
          children: <><code>{metric}</code>{response ? ` · ${response.unit}` : ''}</>,
        }
      })} />
    </Space>}
  </Modal>
}
