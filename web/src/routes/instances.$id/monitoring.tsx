import {
  Button,
  ContentSwitcher,
  StructuredListBody,
  StructuredListCell,
  StructuredListRow,
  StructuredListWrapper,
  Switch,
  Tab,
  TabList,
  Tabs,
} from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { MetricChart, metricUnavailability, type MetricChartSeries, type MetricThreshold } from '../../domain/MetricChart'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import { unavailabilityHref, type Unavailability } from '../../domain/UnavailabilityBlock'
import { Dropdown } from '../../primitives/Dropdown'
import { Icon } from '../../primitives/Icon'
import { Modal } from '../../primitives/Modal'
import { MultiSelect } from '../../primitives/MultiSelect'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { Toggle } from '../../primitives/Toggle'
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
  type EnhancedPreferences,
} from './enhancedMonitoring'
import { metricOption, type MetricID, type MetricOption } from './metricOptions'
import { longQuerySamplesPageHref } from './sessionLayout'
import {
  findStandardMonitoringChart,
  standardMonitoringGroups,
  standardMonitoringMetricIDs,
  type StandardMonitoringChart,
} from './standardMonitoring'
import {
  defaultTimeRange,
  enhancedWindowMinutes,
  parseTimeRange,
  type ChartColumns,
  type EnhancedWindowMinutes,
  type MetricStep,
  type MonitoringSearch,
} from './timeRange'
import { WorkbenchHeader } from './workbench'
import './monitoring.css'

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

type StepOption = { id: MetricStep; label: string }

// 组件库的 `items` 收的是可变数组，所以这一张不写 `as const`；取值仍然被 `MetricStep` 钉住。
const stepOptions: StepOption[] = [
  { id: 'auto', label: '自动' },
  { id: '15s', label: '15 秒' },
  { id: '1m', label: '1 分钟' },
  { id: '5m', label: '5 分钟' },
  { id: 'raw', label: '原始粒度' },
]

const columnOptions = [1, 2, 3] as const satisfies readonly ChartColumns[]

const aggregationOptions = [
  { value: 'average', label: '平均' },
  { value: 'maximum', label: '最大' },
  { value: 'minimum', label: '最小' },
] as const satisfies readonly { value: EnhancedAggregation; label: string }[]

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
  const navigate = standardMonitoringRoute.useNavigate()

  if ('error' in search) {
    return <div className="monitoring-page">
      <NotificationBar tone="critical" title={search.error} />
      {/* 复位是一个动作而不是一个地址：当前地址本身就是坏的，没有可复制的链接可言。 */}
      <Button size="md" className="monitoring-page__reset" onClick={() => void navigate({ search: defaultTimeRange() })}>
        使用最近一小时
      </Button>
    </div>
  }

  return search.monitoring === 'enhanced'
    ? <EnhancedMonitoringPage id={id} search={search} />
    : <StandardMonitoringPage id={id} search={search} />
}

/// 标准监控与增强监控之间的切换。
///
/// 两档都是**地址**（`monitoring` 是 search param），所以页签是真锚点：`<Tab as={链接组件}>`，
/// 中键新开与复制链接都还在，`role="tab"` / `aria-selected` 由 Carbon 照常给。
/// `activation="manual"` 是必需的 —— 自动激活会让方向键在不导航的情况下改选中态，
/// 页签就和地址对不上了。判定与理由见 `web/CLAUDE.md` 的先例一节。
///
/// 两个去处都从当前时间范围推出来，而不是「点下去那一刻的最近一小时」：换一档视图
/// 不该把读者好不容易定位到的时间窗口丢掉。增强监控只收 30 / 60 / 180 / 360 分钟的窗口
/// （见 `parseTimeRange`），所以取以当前结束时刻收尾的 30 分钟。
function MonitoringViewTabs({ id, search }: { id: string; search: MonitoringSearch }) {
  // `as` 槽只收组件，不能顺带把路由属性交出去；memo 固定身份，否则每次渲染都会重挂锚点、
  // 把键盘焦点甩掉（先例见 workbench.tsx）。
  const links = useMemo(() => {
    const standard: MonitoringSearch = {
      from: search.from,
      to: search.to,
      step: 'auto',
      columns: search.columns ?? 2,
      connect: search.connect ?? true,
    }
    const enhanced: MonitoringSearch = {
      from: new Date(new Date(search.to).getTime() - 30 * 60_000).toISOString(),
      to: search.to,
      monitoring: 'enhanced',
      step: 'raw',
    }
    return {
      standard: (props: object) => <Link {...props} to="/instances/$id/monitoring" params={{ id }} search={standard} />,
      enhanced: (props: object) => <Link {...props} to="/instances/$id/monitoring" params={{ id }} search={enhanced} />,
    }
  }, [id, search])

  return <Tabs selectedIndex={search.monitoring === 'enhanced' ? 1 : 0}>
    <TabList aria-label="监控视图" activation="manual">
      <Tab as={links.standard}>标准监控</Tab>
      <Tab as={links.enhanced}>增强监控</Tab>
    </TabList>
  </Tabs>
}

/// 标准监控：22 张图分三组，粒度 / 列数 / 光标联动 / 时间范围都在地址里，
/// 所以一张截图的链接发给同事，看到的是同一屏。
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

  return <div className="monitoring-page">
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="monitoring" search={search} />
    <MonitoringViewTabs id={id} search={search} />

    <section id="monitoring-controls" className="monitoring-page__controls" aria-label="标准监控控制">
      <TimeRangePicker
        from={search.from}
        to={search.to}
        onChange={(range) => updateSearch(range)}
      />
      <div className="monitoring-page__control-row">
        <Dropdown<StepOption>
          id="metric-step"
          className="monitoring-page__step"
          size="md"
          titleText="数据粒度"
          label="自动"
          items={stepOptions}
          itemToString={(item) => item?.label ?? ''}
          selectedItem={stepOptions.find((option) => option.id === step) ?? stepOptions[0]}
          onChange={({ selectedItem }) => {
            if (selectedItem) updateSearch({ step: selectedItem.id })
          }}
        />
        <ColumnSwitcher
          name="列数"
          label="图表列数"
          columns={columns}
          onChange={(value) => updateSearch({ columns: value })}
        />
        {/*
          * 左右两侧的开关文案清空：状态由 `aria-checked` 与外观表达，这里不需要第三份。
          * 「可见的滑块点不动」那个坑归 `primitives/Toggle` 管，页面不用再绕。
          */}
        <div className="monitoring-page__toggle">
          <Toggle
            id="monitoring-connect"
            size="sm"
            labelText="光标联动"
            labelA=""
            labelB=""
            toggled={connected}
            onToggle={(value) => updateSearch({ connect: value })}
          />
        </div>
        {metricsQuery.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metricsQuery.dataUpdatedAt}
          collectionInterval={standardMonitoringPollingOptions.refetchInterval}
        />}
      </div>
    </section>

    {standardMonitoringGroups.map((group) => (
      <section key={group.key} className="monitoring-page__group" aria-labelledby={`${group.key}-heading`}>
        <h2 id={`${group.key}-heading`} className="dbs-panel-title">{group.title}</h2>
        <div className="monitoring-page__grid" data-testid="metric-grid" data-columns={columns}>
          {group.charts.map((chart) => {
            const view = buildChartView(chart, metricsQuery.data?.metrics)
            const primaryMetric = chart.metrics[0]
            return (
              <Panel
                key={chart.key}
                className="monitoring-page__card"
                data-testid="metric-card"
                headingLevel={3}
                title={chart.title}
                // 规范要求骨架占位而不是整页转圈：卡片框与标题先立住，读者知道等的是哪张图。
                loading={metricsQuery.isPending}
                actions={<>
                  {chart.drilldown && <Button
                    kind="ghost"
                    size="sm"
                    renderIcon={SamplesIcon}
                    href={longQuerySamplesHref(id, search)}
                  >查看采样记录</Button>}
                  <Button
                    kind="ghost"
                    size="sm"
                    renderIcon={DetailsIcon}
                    onClick={() => updateSearch({ metric: primaryMetric })}
                  >指标详情</Button>
                </>}
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
              </Panel>
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
  </div>
}

/// 增强监控：5 秒原始点，指标集合、聚合方式与布局是本机偏好（localStorage），
/// 时间窗口仍然是地址的一部分。
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
    monitoringContent = <div className="monitoring-page__empty">
      <span className="dbs-body">未选择指标</span>
      <span className="dbs-caption">在「指标管理」里挑几个指标，这里就会画出对应的原始粒度曲线。</span>
    </div>
  } else {
    monitoringContent = enhancedMonitoringGroups.map((group) => {
      const selectedMetrics = group.metrics.filter((metric) => preferences.metrics.includes(metric))
      if (selectedMetrics.length === 0) return null

      return <section key={group.key} className="monitoring-page__group" aria-labelledby={`enhanced-${group.key}-heading`}>
        <h2 id={`enhanced-${group.key}-heading`} className="dbs-panel-title">{group.title}</h2>
        <div className="monitoring-page__grid" data-testid="metric-grid" data-columns={preferences.columns}>
          {selectedMetrics.map((metricID) => {
            const option = metricOption(metricID)
            const view = buildEnhancedChartView(metricID, metricsQuery.data?.metrics, preferences.aggregation, bucketSeconds)
            const taskResult = collectionTaskResult(tasksQuery.data, metricID)
            return <Panel
              key={metricID}
              className="monitoring-page__card"
              data-testid="enhanced-metric-card"
              headingLevel={3}
              title={option.label}
              loading={metricsQuery.isPending}
              actions={<>
                {metricID === 'pg.query.long_running_count' && <Button
                  kind="ghost"
                  size="sm"
                  renderIcon={SamplesIcon}
                  href={longQuerySamplesHref(id, search)}
                >查看采样记录</Button>}
                <Button
                  kind="ghost"
                  size="sm"
                  renderIcon={DetailsIcon}
                  onClick={() => updateSearch({ metric: metricID })}
                >指标详情</Button>
              </>}
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
            </Panel>
          })}
        </div>
      </section>
    })
  }

  return <div className="monitoring-page">
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="monitoring" search={search} />
    <MonitoringViewTabs id={id} search={search} />

    <NotificationBar tone="info" title="5 秒增强采集常态运行">
      增强监控的 5 秒采集为常态运行，磁盘与查询开销与是否打开本页无关；打开本页不会给数据库增加任何额外查询压力。
    </NotificationBar>

    <section id="monitoring-controls" className="monitoring-page__controls" aria-label="增强监控控制">
      <div className="monitoring-page__control-row">
        <MultiSelect<MetricOption>
          id="enhanced-metrics"
          className="monitoring-page__metrics"
          size="md"
          titleText="指标管理"
          label={`已选 ${preferences.metrics.length} 个指标`}
          items={enhancedMonitoringMetricOptions}
          itemToString={(item) => item?.label ?? ''}
          selectedItems={enhancedMonitoringMetricOptions.filter((option) => preferences.metrics.includes(option.id))}
          onChange={({ selectedItems }) => {
            // 回来的顺序跟着点选顺序走。按指标字典的固定顺序重排，请求里的 `metric`
            // 参数才稳定，查询键也就不会因为点选先后而抖动。
            const chosen = new Set((selectedItems ?? []).map((item) => item.id))
            updatePreferences({ metrics: enhancedMonitoringMetricIDs.filter((metric) => chosen.has(metric)) })
          }}
        />
        <div className="monitoring-page__field">
          <span className="cds--label">时间窗口</span>
          <ContentSwitcher
            aria-label="增强监控时间窗口"
            size="md"
            selectedIndex={enhancedWindowOptions.findIndex((option) => option.minutes === windowMinutes)}
            onChange={({ index }) => {
              const next = index === undefined ? undefined : enhancedWindowOptions[index]
              if (next !== undefined) changeWindow(next.minutes)
            }}
          >
            {enhancedWindowOptions.map((option) => <Switch key={option.minutes} name={String(option.minutes)} text={option.label} />)}
          </ContentSwitcher>
        </div>
      </div>
      <div className="monitoring-page__control-row">
        <div className="monitoring-page__field">
          <span className="cds--label">聚合方式</span>
          <ContentSwitcher
            aria-label="聚合方式"
            size="md"
            selectedIndex={aggregationOptions.findIndex((option) => option.value === preferences.aggregation)}
            onChange={({ index }) => {
              const next = index === undefined ? undefined : aggregationOptions[index]
              if (next !== undefined) updatePreferences({ aggregation: next.value })
            }}
          >
            {aggregationOptions.map((option) => <Switch key={option.value} name={option.value} text={option.label} />)}
          </ContentSwitcher>
        </div>
        <ColumnSwitcher
          name="布局"
          label="图表布局"
          columns={preferences.columns}
          onChange={(value) => updatePreferences({ columns: value })}
        />
        {metricsQuery.dataUpdatedAt > 0 && <Freshness
          dataUpdatedAt={metricsQuery.dataUpdatedAt}
          collectionInterval={enhancedMonitoringPollingOptions.refetchInterval}
        />}
      </div>
    </section>

    {monitoringContent}

    <EnhancedMetricDetails
      metric={selectedMetric}
      response={metricsQuery.data?.metrics.find((item) => item.metric === selectedMetric)}
      onClose={() => updateSearch({ metric: undefined })}
    />
  </div>
}

/// 列数切换。分段单选而不是下拉：三档都在眼前，选中态由 `aria-selected` 表达，
/// 颜色不是唯一信号。看得见的那句短标签（列数 / 布局）是可访问名的子串，两边不打架。
function ColumnSwitcher({ name, label, columns, onChange }: {
  name: string
  label: string
  columns: ChartColumns
  onChange: (columns: ChartColumns) => void
}) {
  return <div className="monitoring-page__field">
    <span className="cds--label">{name}</span>
    <ContentSwitcher
      aria-label={label}
      size="md"
      selectedIndex={columnOptions.indexOf(columns)}
      onChange={({ index }) => {
        // 组件库把选中下标标成可选；拿不到下标就是没换档，什么都不做，别兜底成第一档。
        const next = index === undefined ? undefined : columnOptions[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {columnOptions.map((value) => <Switch key={value} name={String(value)} text={`${value} 列`} />)}
    </ContentSwitcher>
  </div>
}

function SamplesIcon() {
  return <Icon name="listBulleted" />
}

function DetailsIcon() {
  return <Icon name="information" />
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

/// 键值清单。原来是 AntD 的 `Descriptions`，这里用 Carbon 的结构化列表表达同一件事。
function MetricKeyValues({ label, items }: {
  label: string
  items: { key: string; label: string; value: ReactNode }[]
}) {
  return <StructuredListWrapper aria-label={label} isCondensed className="monitoring-page__list">
    <StructuredListBody>
      {items.map((item) => (
        <StructuredListRow key={item.key}>
          <StructuredListCell noWrap>{item.label}</StructuredListCell>
          <StructuredListCell>{item.value}</StructuredListCell>
        </StructuredListRow>
      ))}
    </StructuredListBody>
  </StructuredListWrapper>
}

function MetricDetails({ chart, metrics, onClose }: {
  chart: StandardMonitoringChart | undefined
  metrics: ResponseMetric[] | undefined
  onClose: () => void
}) {
  return <Modal
    passiveModal
    open={chart !== undefined}
    modalHeading={chart?.title ?? '指标详情'}
    closeButtonLabel="关闭指标详情"
    onRequestClose={onClose}
  >
    {chart && <div className="monitoring-page__details">
      <p className="dbs-body">{chart.description}</p>
      <MetricKeyValues label={`${chart.title} 的指标`} items={chart.metrics.map((metric) => {
        const response = metrics?.find((item) => item.metric === metric)
        return {
          key: metric,
          label: metricOption(metric).label,
          value: <><code>{metric}</code>{response ? ` · ${response.unit}` : ''}</>,
        }
      })} />
    </div>}
  </Modal>
}

function EnhancedMetricDetails({ metric, response, onClose }: {
  metric: MetricID | undefined
  response: ResponseMetric | undefined
  onClose: () => void
}) {
  return <Modal
    passiveModal
    open={metric !== undefined}
    modalHeading={metric ? metricOption(metric).label : '指标详情'}
    closeButtonLabel="关闭指标详情"
    onRequestClose={onClose}
  >
    {metric && <div className="monitoring-page__details">
      <p className="dbs-body">{enhancedMetricDescription(metric)}</p>
      <MetricKeyValues label="指标详情" items={[
        { key: 'id', label: '指标 ID', value: <code>{metric}</code> },
        { key: 'unit', label: '单位', value: response?.unit ?? '等待样本' },
        { key: 'step', label: '读取粒度', value: '原始点' },
      ]} />
    </div>}
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
