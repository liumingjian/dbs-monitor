import { DatabaseOutlined, InfoCircleOutlined, ProfileOutlined, SettingOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Descriptions, Empty, Modal, Segmented, Select, Space, Spin, Switch, Tabs, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { MetricChart, metricUnavailability, type MetricChartSeries } from '../../domain/MetricChart'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import type { Unavailability } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import {
  buildEnhancedChartView,
  enhancedDisplayBucketSeconds,
  enhancedMetricDescription,
  enhancedMonitoringDefaults,
  enhancedMonitoringGroups,
  enhancedMonitoringMetricIDs,
  enhancedUnavailabilityDetail,
  enhancedWindowOptions,
  parseEnhancedPreferences,
  type EnhancedAggregation,
  type EnhancedColumns,
  type EnhancedPreferences,
} from './enhancedMonitoring'
import { metricOption, metricOptions, type MetricID } from './metricOptions'
import {
  findStandardMonitoringChart,
  standardMonitoringGroups,
  standardMonitoringMetricIDs,
  type StandardMonitoringChart,
} from './standardMonitoring'
import {
  defaultTimeRange,
  defaultEnhancedTimeRange,
  enhancedWindowMinutes,
  parseTimeRange,
  type ChartColumns,
  type MetricStep,
  type MonitoringSearch,
  type MonitoringView,
} from './timeRange'

export const instanceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id',
  validateSearch: (search): MonitoringSearch | { error: string } => parseTimeRange(search),
  component: InstancePage,
})

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]
type CollectionTask = components['schemas']['CollectionTaskState']

const standardMonitoringPollingOptions = { refetchInterval: pollingIntervals.standardMonitoring }
const enhancedMonitoringPollingOptions = { refetchInterval: pollingIntervals.enhancedMonitoring }

function InstancePage() {
  const { id } = instanceRoute.useParams()
  const search = instanceRoute.useSearch()

  if ('error' in search) {
    return <Alert
      type="error"
      showIcon
      title={search.error}
      action={<Link to="/instances/$id" params={{ id }} search={defaultTimeRange()}><Button>使用最近一小时</Button></Link>}
    />
  }

  return search.monitoring === 'enhanced'
    ? <EnhancedMonitoringPage id={id} search={search} />
    : <StandardMonitoringPage id={id} search={search} />
}

function StandardMonitoringPage({ id, search }: { id: string; search: MonitoringSearch }) {
  const navigate = instanceRoute.useNavigate()
  const instance = $api.useQuery(
    'get',
    '/api/v1/instances/{id}',
    { params: { path: { id } } },
    standardMonitoringPollingOptions,
  )

  const step = search.step ?? 'auto'
  const columns = search.columns ?? 2
  const connected = search.connect ?? true
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id },
      query: { metric: standardMonitoringMetricIDs, from: search.from, to: search.to, step },
    },
  }, standardMonitoringPollingOptions)
  const selectedChart = findStandardMonitoringChart(search.metric)

  function updateSearch(update: Partial<MonitoringSearch>) {
    void navigate({ search: { ...search, ...update } })
  }

  function changeMonitoring(view: MonitoringView) {
    void navigate({ search: view === 'enhanced' ? defaultEnhancedTimeRange() : defaultTimeRange() })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchNavigation
      id={id}
      instanceName={instance.data?.name ?? '实例工作台'}
      monitoring="standard"
      onMonitoringChange={changeMonitoring}
    />

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
        {metrics.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metrics.dataUpdatedAt}
          collectionInterval={standardMonitoringPollingOptions.refetchInterval}
        />}
      </Space>
    </section>

    {metrics.isPending ? <Spin size="large" /> : standardMonitoringGroups.map((group) => (
      <section key={group.key} aria-labelledby={`${group.key}-heading`}>
        <Typography.Title id={`${group.key}-heading`} level={3}>{group.title}</Typography.Title>
        <div className="metric-grid" data-columns={columns}>
          {group.charts.map((chart) => {
            const view = buildChartView(chart, metrics.data?.metrics)
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
                  step={metrics.data?.step ?? step}
                  unavailability={view.unavailability}
                  unavailabilityHref={unavailabilityHref(id, primaryMetric, view.unavailability)}
                  connectionGroup={connected ? `standard-monitoring-${id}` : undefined}
                  loading={metrics.isFetching}
                />
              </Card>
            )
          })}
        </div>
      </section>
    ))}

    <MetricDetails
      chart={selectedChart}
      metrics={metrics.data?.metrics}
      onClose={() => updateSearch({ metric: undefined })}
    />
  </Space>
}

function EnhancedMonitoringPage({ id, search }: { id: string; search: MonitoringSearch }) {
  const navigate = instanceRoute.useNavigate()
  const [preferences, setPreferences] = useState<EnhancedPreferences>(() => readEnhancedPreferences(id))
  const windowMinutes = enhancedWindowMinutes(new Date(search.from), new Date(search.to)) ?? enhancedMonitoringDefaults.windowMinutes
  const bucketSeconds = enhancedDisplayBucketSeconds(windowMinutes)
  const instance = $api.useQuery(
    'get',
    '/api/v1/instances/{id}',
    { params: { path: { id } } },
    enhancedMonitoringPollingOptions,
  )
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id },
      query: { metric: preferences.metrics, from: search.from, to: search.to, step: 'raw' },
    },
  }, { ...enhancedMonitoringPollingOptions, enabled: preferences.metrics.length > 0 })
  const tasks = $api.useQuery(
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

  function changeWindow(minutes: 30 | 60 | 180 | 360) {
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

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchNavigation
      id={id}
      instanceName={instance.data?.name ?? '实例工作台'}
      monitoring="enhanced"
      onMonitoringChange={changeMonitoring}
    />

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
          options={metricOptions.filter((option) => option.enhancedCandidate).map((option) => ({ value: option.id, label: option.label }))}
          onChange={(metrics) => updatePreferences({ metrics })}
          maxTagCount="responsive"
          style={{ minWidth: 280 }}
        />
        <span>时间窗口</span>
        <Segmented<30 | 60 | 180 | 360>
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
        {metrics.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metrics.dataUpdatedAt}
          collectionInterval={enhancedMonitoringPollingOptions.refetchInterval}
        />}
      </Space>
    </section>

    {preferences.metrics.length === 0 ? <Empty description="未选择指标" /> : metrics.isPending ? <Spin size="large" /> : enhancedMonitoringGroups.map((group) => {
      const selectedMetrics = group.metrics.filter((metric) => preferences.metrics.includes(metric))
      if (selectedMetrics.length === 0) return null
      return <section key={group.key} aria-labelledby={`enhanced-${group.key}-heading`}>
        <Typography.Title id={`enhanced-${group.key}-heading`} level={3}>{group.title}</Typography.Title>
        <div className="metric-grid" data-columns={preferences.columns}>
          {selectedMetrics.map((metricID) => {
            const option = metricOption(metricID)
            const view = buildEnhancedChartView(metricID, metrics.data?.metrics, preferences.aggregation, bucketSeconds)
            const taskResult = collectionTaskResult(tasks.data, metricID)
            return <Card
              key={metricID}
              className="metric-card enhanced-metric-card"
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
                step={metrics.data?.step ?? enhancedMonitoringDefaults.step}
                unavailability={view.unavailability}
                unavailabilityHref={unavailabilityHref(id, metricID, view.unavailability)}
                unavailabilityDetail={view.unavailability ? enhancedUnavailabilityDetail(view.unavailability, taskResult) : undefined}
                loading={metrics.isFetching}
              />
            </Card>
          })}
        </div>
      </section>
    })}

    <EnhancedMetricDetails
      metric={selectedMetric}
      response={metrics.data?.metrics.find((item) => item.metric === selectedMetric)}
      onClose={() => updateSearch({ metric: undefined })}
    />
  </Space>
}

function WorkbenchNavigation({ id, instanceName, monitoring, onMonitoringChange }: {
  id: string
  instanceName: string
  monitoring: MonitoringView
  onMonitoringChange: (view: MonitoringView) => void
}) {
  return <>
    <Link to="/instances">← 返回实例列表</Link>
    <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>{instanceName}</Typography.Title>
        <Typography.Text type="secondary">实例工作台</Typography.Text>
      </div>
      <Space>
        <Link to="/instances/$id/collection" params={{ id }}><Button icon={<DatabaseOutlined />}>采集管理</Button></Link>
        <Link to="/instances/$id/settings" params={{ id }}><Button icon={<SettingOutlined />}>接入设置</Button></Link>
      </Space>
    </Space>
    <Tabs
      activeKey="monitoring"
      items={[
        { key: 'overview', label: '实例总览', disabled: true },
        { key: 'monitoring', label: '监控与报警' },
        { key: 'sessions', label: '会话与阻塞', disabled: true },
        { key: 'events', label: '性能事件', disabled: true },
        { key: 'alerts', label: '告警', disabled: true },
        { key: 'collection', label: '采集管理', disabled: true },
      ]}
    />
    <Tabs
      activeKey={monitoring}
      onChange={(key) => {
        if (key === 'standard' || key === 'enhanced') onMonitoringChange(key)
      }}
      items={[
        { key: 'standard', label: '标准监控' },
        { key: 'enhanced', label: '增强监控' },
      ]}
    />
  </>
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
