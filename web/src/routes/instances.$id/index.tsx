import {
  AlertOutlined,
  ApiOutlined,
  CalendarOutlined,
  ClusterOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  FundProjectionScreenOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Descriptions, Empty, List, Space, Spin, Typography } from 'antd'
import type { ReactNode } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness } from '../../domain/Freshness'
import { HealthStatus } from '../../domain/HealthStatus'
import { SuppressionTags } from '../../domain/SuppressionTags'
import { unavailabilityCopy } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import { metricOption, type MetricID } from './metricOptions'
import {
  latestMetricFacts,
  overviewDestinations,
  overviewMetricGroups,
  overviewMetricIDs,
  performanceEventsEmptyState,
} from './overview'
import { defaultTimeRange, parseTimeRange, type MonitoringSearch } from './timeRange'
import { WorkbenchHeader } from './workbench'

type Instance = components['schemas']['Instance']
type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]
type PerformanceEvent = components['schemas']['PerformanceEvent']

const overviewPollingOptions = { refetchInterval: pollingIntervals.overview }

export const instanceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id',
  validateSearch: (search): MonitoringSearch | { error: string } => parseTimeRange(search),
  component: InstanceOverviewRoutePage,
})

function InstanceOverviewRoutePage() {
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

  return <InstanceOverviewPage id={id} search={search} />
}

function InstanceOverviewPage({ id, search }: { id: string; search: MonitoringSearch }) {
  const instanceQuery = $api.useQuery(
    'get',
    '/api/v1/instances/{id}',
    { params: { path: { id } } },
    overviewPollingOptions,
  )
  const metricsQuery = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id },
      query: { metric: overviewMetricIDs, from: search.from, to: search.to, step: 'auto' },
    },
  }, overviewPollingOptions)
  const eventsQuery = $api.useQuery('get', '/api/v1/instances/{id}/performance-events', {
    params: {
      path: { id },
      query: { from: search.from, to: search.to, limit: 3, offset: 0, sort: '-updated_at' },
    },
  }, overviewPollingOptions)

  if (instanceQuery.isPending) return <Spin size="large" />
  if (!instanceQuery.data) return <Alert type="error" showIcon title="无法加载实例总览" />

  const instance = instanceQuery.data
  const destinations = overviewDestinations(id, search)
  const metrics = metricsQuery.data?.metrics
  const unresolvedAlertCount = instance.health.counts.critical + instance.health.counts.warning + instance.health.counts.info

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instance.name} activeKey="overview" search={search} />

    <section className="overview-status" aria-labelledby="instance-health-heading">
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Space wrap>
          <HealthStatus status={instance.health.status} pausedAt={instance.collection_pause.updated_at} />
          <Typography.Title id="instance-health-heading" level={3} style={{ margin: 0 }}>
            {attributionLabel(instance)}
          </Typography.Title>
        </Space>
        <Space wrap size={4}>
          <Typography.Text code>C{instance.health.counts.critical}</Typography.Text>
          <Typography.Text code>W{instance.health.counts.warning}</Typography.Text>
          <Typography.Text code>I{instance.health.counts.info}</Typography.Text>
          <SuppressionTags flags={instance.health.flags} />
        </Space>
        <Space wrap>
          <Typography.Text type="secondary">{instance.host}:{instance.port} · {instance.database}</Typography.Text>
          {instance.collection_pause.paused && <Link to="/instances/$id/collection" params={{ id }}>查看采集暂停设置</Link>}
          {instance.health.flags.in_maintenance && <Button type="link" href="/alert-settings/maintenance-windows">查看维护窗口</Button>}
          <Button icon={<CalendarOutlined />} href={destinations.maintenance}>新建维护窗口</Button>
        </Space>
      </Space>
    </section>

    {metricsQuery.dataUpdatedAt > 0 && <Freshness
      dataUpdatedAt={metricsQuery.dataUpdatedAt}
      collectionInterval={overviewPollingOptions.refetchInterval}
    />}

    <div className="overview-grid">
      <OverviewCard module="availability" title="可用性与采集状态" icon={<ApiOutlined />} loading={metricsQuery.isPending}>
        <MetricFacts
          id={id}
          search={search}
          metricIDs={overviewMetricGroups.availability}
          metrics={metrics}
        />
        <Descriptions size="small" column={1} items={[
          { key: 'collected', label: '最近采集时间', children: lastCollectedAtLabel(instance.last_collected_at) },
          { key: 'agent', label: 'Agent 状态', children: agentStatusLabel(instance.agent_status) },
          { key: 'freshness', label: '数据新鲜度', children: dataFreshnessLabel(instance.data_freshness_seconds) },
        ]} />
      </OverviewCard>

      <OverviewCard module="alerts" title="当前告警摘要" icon={<AlertOutlined />}>
        <Descriptions size="small" column={1} items={[
          { key: 'critical', label: '严重告警', children: instance.health.counts.critical },
          { key: 'warning', label: '警告告警', children: instance.health.counts.warning },
          { key: 'info', label: 'Info 告警', children: instance.health.counts.info },
          { key: 'unresolved', label: '未恢复告警', children: unresolvedAlertCount },
          { key: 'attribution', label: '当前归因', children: attributionLabel(instance) },
        ]} />
        <Button type="link" href={destinations.alerts}>查看当前告警</Button>
      </OverviewCard>

      <OverviewCard module="resources" title="核心资源" icon={<DashboardOutlined />} loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.resources} metrics={metrics} />
      </OverviewCard>

      <OverviewCard module="database" title="数据库负载" icon={<DatabaseOutlined />} loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.database} metrics={metrics} />
        <Button type="link" href={destinations.sessions}>查看锁等待会话</Button>
      </OverviewCard>

      <OverviewCard module="replication" title="复制状态" icon={<ClusterOutlined />} loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.replication} metrics={metrics} />
      </OverviewCard>

      <OverviewCard module="events" title="近期性能事件" icon={<FundProjectionScreenOutlined />} loading={eventsQuery.isPending}>
        <PerformanceEvents events={eventsQuery.data?.items} />
      </OverviewCard>

      <OverviewCard module="troubleshooting" title="快速排障入口" icon={<ToolOutlined />}>
        <Space wrap>
          <Button type="primary" icon={<DashboardOutlined />} href={destinations.monitoring}>标准监控</Button>
          <Button icon={<DatabaseOutlined />} href={destinations.sessions}>会话与阻塞</Button>
          <Button icon={<AlertOutlined />} href={destinations.alerts}>当前告警</Button>
          <Button icon={<ApiOutlined />} href={destinations.collection}>采集状态</Button>
        </Space>
      </OverviewCard>
    </div>
  </Space>
}

function OverviewCard({ module, title, icon, loading = false, children }: {
  module: string
  title: string
  icon: ReactNode
  loading?: boolean
  children: ReactNode
}) {
  return <Card
    className="overview-card"
    data-overview-module={module}
    title={<Space>{icon}<span>{title}</span></Space>}
    loading={loading}
  >
    {children}
  </Card>
}

function MetricFacts({ id, search, metricIDs, metrics }: {
  id: string
  search: MonitoringSearch
  metricIDs: readonly MetricID[]
  metrics: ResponseMetric[] | undefined
}) {
  return <Descriptions size="small" column={1} items={metricIDs.map((metricID) => {
    const metric = metrics?.find((item) => item.metric === metricID)
    const snapshot = latestMetricFacts(metric)
    return {
      key: metricID,
      label: <a href={overviewDestinations(id, { ...search, metric: metricID }).monitoring}>{metricOption(metricID).label}</a>,
      children: <MetricFactValue metricID={metricID} snapshot={snapshot} collectionHref={overviewDestinations(id, { ...search, metric: metricID }).collection} />,
    }
  })} />
}

function MetricFactValue({ metricID, snapshot, collectionHref }: {
  metricID: MetricID
  snapshot: ReturnType<typeof latestMetricFacts>
  collectionHref: string
}) {
  if (snapshot.unavailability) {
    return <a href={collectionHref}>{unavailabilityCopy(snapshot.unavailability).title}</a>
  }
  return <Space direction="vertical" size={0}>
    {snapshot.facts.map((fact, index) => <span key={`${fact.sampledAt}-${index}`}>
      {dimensionLabel(fact.labels)}{fact.value === null ? '缺数' : formatMetricValue(metricID, fact.value, snapshot.unit)}
    </span>)}
  </Space>
}

function PerformanceEvents({ events }: { events: PerformanceEvent[] | undefined }) {
  if (!events || events.length === 0) {
    return <Empty
      image={Empty.PRESENTED_IMAGE_SIMPLE}
      description={<Space direction="vertical" size={0}>
        <Typography.Text>{performanceEventsEmptyState.title}</Typography.Text>
        <Typography.Text type="secondary">{performanceEventsEmptyState.description}</Typography.Text>
      </Space>}
    />
  }
  return <List dataSource={events} renderItem={(event) => <List.Item>
    <List.Item.Meta
      title={<Space wrap><AlertStatus status={event.alert_status} /><span>{eventTypeLabel(event.event_type)}</span></Space>}
      description={<>
        {event.cause_summary}<br />
        建议动作：{event.suggested_action}<br />
        <Typography.Text type="secondary">{new Date(event.updated_at).toLocaleString()}</Typography.Text>
      </>}
    />
  </List.Item>} />
}

function attributionLabel(instance: Instance): string {
  const attribution = instance.health.attribution
  if (!attribution) return '无未恢复告警'
  return attribution.current_value === undefined ? attribution.rule_name : `${attribution.rule_name} (${attribution.current_value})`
}

function lastCollectedAtLabel(collectedAt: string | undefined): string {
  return collectedAt ? new Date(collectedAt).toLocaleString() : '尚无成功采集'
}

function dataFreshnessLabel(seconds: number | undefined): string {
  if (seconds === undefined) return '未知'
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  return `${Math.floor(seconds / 3600)} 小时前`
}

function agentStatusLabel(status: components['schemas']['InstanceAgentStatus']): string {
  switch (status) {
    case 'online': return '在线'
    case 'offline': return '离线'
    case 'not_installed': return '未安装'
    case 'permission_denied': return '权限不足'
    case 'error': return '异常'
    default: return assertNever(status)
  }
}

function eventTypeLabel(eventType: components['schemas']['PerformanceEventType']): string {
  switch (eventType) {
    case 'LOCK_BLOCKING': return '锁等待与阻塞'
    case 'LONG_TRANSACTION': return '长事务'
    case 'IDLE_IN_TRANSACTION': return '事务空闲'
    case 'ACTIVE_SESSIONS_HIGH': return '活跃会话过高'
    case 'REPLICATION_LAG': return '复制延迟'
    case 'TEMP_FILES_SURGE': return '临时文件突增'
    default: return assertNever(eventType)
  }
}

function dimensionLabel(labels: Record<string, string>): string {
  const entries = Object.entries(labels)
  return entries.length === 0 ? '' : `${entries.map(([key, value]) => `${key}=${value}`).join(', ')}: `
}

function formatMetricValue(metricID: MetricID, value: number, unit: string): string {
  if (metricID === 'pg.availability.reachable') return value === 1 ? '可连接' : '不可连接'
  if (metricID === 'pg.replication.role') return ['单实例', '主库', '备库'][value] ?? `未知编码 ${value}`
  if (metricID === 'pg.replication.connection_state') {
    return ['已停止', '启动中', '初始化', '追赶中', '流复制中', '备份中', '停止中', '等待中', '重启中'][value] ?? `未知编码 ${value}`
  }
  const formatted = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 }).format(value)
  return unit ? `${formatted} ${unit}` : formatted
}

function assertNever(value: never): never {
  throw new Error(`unexpected overview value: ${value}`)
}
