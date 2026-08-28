import { InfoCircleOutlined, ProfileOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Descriptions, Empty, Modal, Segmented, Select, Space, Spin, Switch, Tabs, Typography } from 'antd'
import { useEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { MetricChart, metricUnavailability, type MetricChartSeries, type MetricThreshold } from '../../domain/MetricChart'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import { unavailabilityHref, type Unavailability } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import {
  buildEnhancedChartView,
  enhancedDisplayBucketSeconds,
  enhancedMetricDescription,
  enhancedMonitoringDefaults,
  enhancedMonitoringGroups,
  enhancedMonitoringMetricIDs,
  enhancedMonitoringMetricOptions,
  enhancedUnavailabilityDetail,
  enhancedWindowOptions,
  parseEnhancedPreferences,
  type EnhancedAggregation,
  type EnhancedColumns,
  type EnhancedPreferences,
} from './enhancedMonitoring'
import { metricOption, type MetricID } from './metricOptions'
import { longQuerySamplesPageHref } from './sessionLayout'
import {
  findStandardMonitoringChart,
  standardMonitoringGroups,
  standardMonitoringMetricIDs,
  type StandardMonitoringChart,
} from './standardMonitoring'
import {
  defaultEnhancedTimeRange,
  defaultTimeRange,
  enhancedWindowMinutes,
  parseTimeRange,
  type ChartColumns,
  type EnhancedWindowMinutes,
  type MetricStep,
  type MonitoringSearch,
  type MonitoringView,
} from './timeRange'
import { WorkbenchHeader } from './workbench'

export const standardMonitoringRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/monitoring',
  validateSearch: (search): MonitoringSearch | { error: string } => parseTimeRange(search),
  component: StandardMonitoringRoutePage,
})

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]
type CollectionTask = components['schemas']['CollectionTaskState']
type AlertRule = components['schemas']['AlertRule']

const standardMonitoringPollingOptions = { refetchInterval: pollingIntervals.standardMonitoring }
const enhancedMonitoringPollingOptions = { refetchInterval: pollingIntervals.enhancedMonitoring }

/**
 * A chart of a metric that has an alerting rule should show where that rule fires;
 * without the line the operator has to hold the threshold in their head.
 */
export function instanceThresholds(
  rules: AlertRule[] | undefined,
  instanceID: string,
  metricIDs: readonly string[],
  metrics: ResponseMetric[] | undefined,
): MetricThreshold[] {
  if (!rules) return []
  return rules
    .filter((rule) => rule.enabled)
    .filter((rule) => rule.scope === 'ALL' || rule.instance_ids.includes(instanceID))
    .filter((rule) => metricIDs.includes(rule.metric_id))
    .flatMap((rule) => {
      // Without the metric there is no unit, and a threshold drawn against a guessed
      // unit lands on the wrong axis. Drop it rather than invent one.
      const unit = metrics?.find((metric) => metric.metric === rule.metric_id)?.unit
      if (unit === undefined) return []
      return [{ label: rule.name, unit, value: rule.threshold, severity: rule.severity }]
    })
}

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

  return search.monitoring === 'enhanced'
    ? <EnhancedMonitoringPage id={id} search={search} />
    : <StandardMonitoringPage id={id} search={search} />
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
  const rulesQuery = $api.useQuery('get', '/api/v1/alert-rules', {}, standardMonitoringPollingOptions)
  const selectedChart = findStandardMonitoringChart(search.metric)

  function updateSearch(update: Partial<MonitoringSearch>) {
    void navigate({ search: { ...search, ...update } })
  }

  function changeMonitoring(view: MonitoringView) {
    void navigate({ search: view === 'enhanced' ? defaultEnhancedTimeRange() : defaultTimeRange() })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="monitoring" search={search} />
    <Tabs activeKey="standard" onChange={(key) => changeMonitoring(key as MonitoringView)} items={[
      { key: 'standard', label: '标准监控' },
      { key: 'enhanced', label: '增强监控' },
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
          {group.charts.map((chart, chartIndex) => {
            const view = buildChartView(chart, metricsQuery.data?.metrics)
            const primaryMetric = chart.metrics[0]
            return (
              <Card
                key={chart.key}
                className="metric-card"
                style={{ '--card-index': chartIndex } as CSSProperties}
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
                  unavailabilityHref={metricUnavailabilityHref(id, primaryMetric, view.unavailability)}
                  connectionGroup={connected ? `standard-monitoring-${id}` : undefined}
                  loading={metricsQuery.isFetching}
                  thresholds={instanceThresholds(rulesQuery.data, id, chart.metrics, metricsQuery.data?.metrics)}
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

function EnhancedMonitoringPage({ id, search }: { id: string; search: MonitoringSearch }) {
  const navigate = standardMonitoringRoute.useNavigate()
  const [preferences, setPreferences] = useState<EnhancedPreferences>(() => readEnhancedPreferences(id))
  const windowMinutes = enhancedWindowMinutes(new Date(search.from), new Date(search.to)) ?? enhancedMonitoringDefaults.windowMinutes
  const bucketSeconds = enhancedDisplayBucketSeconds(windowMinutes)
  const instanceQuery = $api.useQuery(
    'get',
    '/api/v1/instances/{id}',
    { params: { path: { id } } },
    enhancedMonitoringPollingOptions,
  )
  const metricsQuery = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id },
      query: { metric: preferences.metrics, from: search.from, to: search.to, step: 'raw' },
    },
  }, { ...enhancedMonitoringPollingOptions, enabled: preferences.metrics.length > 0 })
  const tasksQuery = $api.useQuery(
    'get',
    '/api/v1/instances/{id}/collection/tasks',
    { params: { path: { id } } },
    enhancedMonitoringPollingOptions,
  )
  const selectedMetric = search.metric && enhancedMonitoringMetricIDs.includes(search.metric) ? search.metric : undefined

  useEffect(() => {
    localStorage.setItem(enhancedPreferencesKey(id), JSON.stringify(preferences))
  }, [id, preferences])

  function updateSearch(update: Partial<MonitoringSearch>) {
    void navigate({ search: { ...search, ...update } })
  }

  function changeMonitoring(view: MonitoringView) {
    void navigate({ search: view === 'enhanced' ? defaultEnhancedTimeRange() : defaultTimeRange() })
  }

  function changeWindow(minutes: EnhancedWindowMinutes) {
    const to = new Date()
    updateSearch({
      from: new Date(to.getTime() - minutes * 60_000).toISOString(),
      to: to.toISOString(),
      step: 'raw',
      monitoring: 'enhanced',
    })
  }

  function updatePreferences(update: Partial<EnhancedPreferences>) {
    setPreferences((current) => ({ ...current, ...update }))
  }

  let monitoringContent: ReactNode
  if (preferences.metrics.length === 0) {
    monitoringContent = <Empty description="未选择指标" />
  } else if (metricsQuery.isPending) {
    monitoringContent = <Spin size="large" />
  } else {
    monitoringContent = enhancedMonitoringGroups.map((group) => {
      const selectedMetrics = group.metrics.filter((metric) => preferences.metrics.includes(metric))
      if (selectedMetrics.length === 0) return null

      return <section key={group.key} aria-labelledby={`enhanced-${group.key}-heading`}>
        <Typography.Title id={`enhanced-${group.key}-heading`} level={3}>{group.title}</Typography.Title>
        <div className="metric-grid" data-columns={preferences.columns}>
          {selectedMetrics.map((metricID) => {
            const option = metricOption(metricID)
            const view = buildEnhancedChartView(metricID, metricsQuery.data?.metrics, preferences.aggregation, bucketSeconds)
            const taskResult = collectionTaskResult(tasksQuery.data, metricID)
            return <Card
              key={metricID}
              className="metric-card enhanced-metric-card"
              style={{ '--card-index': selectedMetrics.indexOf(metricID) } as CSSProperties}
              title={option.label}
              extra={<Space size="small">
                {metricID === 'pg.query.long_running_count' && <Button
                  type="link"
                  size="small"
                  icon={<ProfileOutlined />}
                  href={longQuerySamplesHref(id, search)}
                >查看采样记录</Button>}
                <Button
                  type="text"
                  size="small"
                  icon={<InfoCircleOutlined />}
                  onClick={() => updateSearch({ metric: metricID })}
                >指标详情</Button>
              </Space>}
            >
              <MetricChart
                label={option.label}
                series={view.series}
                step={metricsQuery.data?.step ?? enhancedMonitoringDefaults.step}
                unavailability={view.unavailability}
                unavailabilityHref={metricUnavailabilityHref(id, metricID, view.unavailability)}
                unavailabilityDetail={view.unavailability ? enhancedUnavailabilityDetail(view.unavailability, taskResult) : undefined}
                loading={metricsQuery.isFetching}
              />
            </Card>
          })}
        </div>
      </section>
    })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="monitoring" search={search} />
    <Tabs activeKey="enhanced" onChange={(key) => changeMonitoring(key as MonitoringView)} items={[
      { key: 'standard', label: '标准监控' },
      { key: 'enhanced', label: '增强监控' },
    ]} />

    <Alert
      type="info"
      showIcon
      title="5 秒增强采集常态运行"
      description="增强监控的 5 秒采集为常态运行，磁盘与查询开销与是否打开本页无关；打开本页不会给数据库增加任何额外查询压力。"
    />

    <section id="monitoring-controls" className="monitoring-controls enhanced-monitoring-controls" aria-label="增强监控控制">
      <Space wrap>
        <label htmlFor="enhanced-metrics">指标管理</label>
        <Select<MetricID[], { value: MetricID; label: string }>
          id="enhanced-metrics"
          aria-label="指标管理"
          mode="multiple"
          value={preferences.metrics}
          options={enhancedMonitoringMetricOptions.map((option) => ({ value: option.id, label: option.label }))}
          onChange={(metrics) => updatePreferences({ metrics })}
          maxTagCount="responsive"
          style={{ minWidth: 280 }}
        />
        <span>时间窗口</span>
        <Segmented<EnhancedWindowMinutes>
          aria-label="增强监控时间窗口"
          value={windowMinutes}
          options={enhancedWindowOptions.map((option) => ({ label: option.label, value: option.minutes }))}
          onChange={changeWindow}
        />
      </Space>
      <Space wrap>
        <span>聚合方式</span>
        <Segmented<EnhancedAggregation>
          aria-label="聚合方式"
          value={preferences.aggregation}
          options={[
            { label: '平均', value: 'average' },
            { label: '最大', value: 'maximum' },
            { label: '最小', value: 'minimum' },
          ]}
          onChange={(aggregation) => updatePreferences({ aggregation })}
        />
        <span>布局</span>
        <Segmented<EnhancedColumns>
          aria-label="图表布局"
          value={preferences.columns}
          options={[{ label: '1 列', value: 1 }, { label: '2 列', value: 2 }, { label: '3 列', value: 3 }]}
          onChange={(columns) => updatePreferences({ columns })}
        />
        {metricsQuery.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metricsQuery.dataUpdatedAt}
          collectionInterval={enhancedMonitoringPollingOptions.refetchInterval}
        />}
      </Space>
    </section>

    {monitoringContent}

    <EnhancedMetricDetails
      metric={selectedMetric}
      response={metricsQuery.data?.metrics.find((item) => item.metric === selectedMetric)}
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

function metricUnavailabilityHref(id: string, metric: MetricID, code: Unavailability | null): string {
  const destinations = {
    current: '#monitoring-controls',
    collection: `/instances/${encodeURIComponent(id)}/collection?metric=${encodeURIComponent(metric)}`,
  }
  return code ? unavailabilityHref(code, destinations) : destinations.current
}

function longQuerySamplesHref(id: string, search: MonitoringSearch): string {
  return longQuerySamplesPageHref(id, {
    from: search.from,
    to: search.to,
    metric: 'pg.query.long_running_count',
    sampled_at: search.to,
  })
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

function EnhancedMetricDetails({ metric, response, onClose }: {
  metric: MetricID | undefined
  response: ResponseMetric | undefined
  onClose: () => void
}) {
  return <Modal title={metric ? metricOption(metric).label : '指标详情'} open={metric !== undefined} footer={null} onCancel={onClose}>
    {metric && <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Typography.Paragraph>{enhancedMetricDescription(metric)}</Typography.Paragraph>
      <Descriptions column={1} size="small" bordered items={[
        { key: 'id', label: '指标 ID', children: <code>{metric}</code> },
        { key: 'unit', label: '单位', children: response?.unit ?? '等待样本' },
        { key: 'step', label: '读取粒度', children: '原始点' },
      ]} />
    </Space>}
  </Modal>
}

function collectionTaskResult(tasks: CollectionTask[] | undefined, metricID: MetricID): CollectionTask['last_result'] {
  return tasks?.find((task) => task.metric_ids.includes(metricID))?.last_result
}

function enhancedPreferencesKey(id: string): string {
  return `dbs-monitor.enhanced-monitoring.${id}.v1`
}

function readEnhancedPreferences(id: string): EnhancedPreferences {
  const stored = localStorage.getItem(enhancedPreferencesKey(id))
  if (!stored) return parseEnhancedPreferences(undefined)
  try {
    return parseEnhancedPreferences(JSON.parse(stored))
  } catch {
    return parseEnhancedPreferences(undefined)
  }
}
