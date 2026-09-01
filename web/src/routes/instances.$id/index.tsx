import { Button, StructuredListBody, StructuredListCell, StructuredListRow, StructuredListWrapper } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness } from '../../domain/Freshness'
import { formatMetricNumber } from '../../domain/MetricChart'
import { HealthStatus } from '../../domain/HealthStatus'
import { SuppressionTags } from '../../domain/SuppressionTags'
import { unavailabilityCopy, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import { instanceEngineLabel } from '../../domain/instanceEngine'
import { Icon } from '../../primitives/Icon'
import { MetricBar } from '../../primitives/MetricBar'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import {
  attributionLabel,
  dataFreshnessLabel,
  lastCollectedAtLabel,
} from '../instanceProjection'
import { rootRoute } from '../root'
import { FlashOnChange } from '../../primitives/FlashOnChange'
import { useMetricCatalog, type MetricID } from './metricOptions'
import {
  latestMetricFacts,
  overviewDestinations,
  overviewMetricGroups,
  overviewMetricIDs,
  performanceEventsEmptyState,
  type LatestMetricFacts,
  type OverviewModule,
} from './overview'
import { defaultTimeRange, parseTimeRange, type MonitoringSearch } from './timeRange'
import { WorkbenchHeader } from './workbench'
import './overview.css'

type ResponseMetric = components['schemas']['MetricSeriesResponse']['metrics'][number]
type PerformanceEvent = components['schemas']['PerformanceEvent']

const overviewPollingOptions = { refetchInterval: pollingIntervals.overview }

// 健康状态那一段的标题 id。写死而不是 `useId`：它是页面上唯一的一处，
// 而且要能被段落的 `aria-labelledby` 稳定引用。
const statusHeadingID = 'instance-health-heading'

export const instanceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id',
  validateSearch: (search): MonitoringSearch | { error: string } => parseTimeRange(search),
  component: InstanceOverviewRoutePage,
})

function InstanceOverviewRoutePage() {
  const { id } = instanceRoute.useParams()
  const search = instanceRoute.useSearch()
  const navigate = instanceRoute.useNavigate()

  if ('error' in search) {
    return <div className="overview-page">
      <NotificationBar tone="critical" title={search.error} />
      {/* 复位是一个动作而不是一个地址：当前地址本身就是坏的，没有可复制的链接可言。 */}
      <Button size="md" className="overview-page__reset" onClick={() => void navigate({ search: defaultTimeRange() })}>
        使用最近一小时
      </Button>
    </div>
  }

  return <InstanceOverviewPage id={id} search={search} />
}

/// 实例总览。
///
/// 版式：工作台页头（`h1` + 页签条）→ 健康状态带（这台实例现在是什么状况）→ 数据新鲜度
/// → 七个模块面板。模块的集合与顺序由 `overview.ts` 的 `OVERVIEW_MODULES` 定死，
/// 这里不另立一套。
///
/// 刷新后确实变了的数值闪一次（`primitives/FlashOnChange`）：这一页每 30 秒整块重画，
/// 没有这个提示就分不清「刷新了但没变」与「变了」。
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

  // Carbon 的 `as` 槽只收组件，不能顺带把路由属性交出去，所以每个去处包成一个已经
  // 知道自己去哪儿的组件，并用 memo 固定身份（先例见 workbench.tsx）。
  const links = useMemo(() => {
    const lockWaitSearch = { from: search.from, to: search.to, filter: 'lock_wait' } as const
    const alertsSearch = { tab: 'current', include_paused: false } as const
    return {
      maintenance: (props: object) => <Link {...props} to="/alert-settings/maintenance-windows/new" search={{ instance_id: id }} />,
      monitoring: (props: object) => <Link {...props} to="/instances/$id/monitoring" params={{ id }} search={search} />,
      sessions: (props: object) => <Link {...props} to="/instances/$id/sessions" params={{ id }} search={lockWaitSearch} />,
      alerts: (props: object) => <Link {...props} to="/instances/$id/alerts" params={{ id }} search={alertsSearch} />,
      collection: (props: object) => <Link
        {...props}
        to="/instances/$id/collection"
        params={{ id }}
        search={search.metric ? { metric: search.metric } : {}}
      />,
    }
  }, [id, search])

  // 规范要求骨架占位，不要整页转圈：先把版式立起来，读者知道自己在等什么。
  if (instanceQuery.isPending) {
    return <div className="overview-page">
      <SkeletonBlock lines={2} label="实例总览加载中" />
      <SkeletonBlock lines={4} decorative />
      <SkeletonBlock lines={6} decorative />
    </div>
  }
  if (!instanceQuery.data) {
    return <div className="overview-page">
      <NotificationBar tone="critical" title="无法加载实例总览" />
    </div>
  }

  const instance = instanceQuery.data
  const metrics = metricsQuery.data?.metrics
  const counts = instance.health.counts
  const unresolvedAlertCount = counts.critical + counts.warning + counts.info

  return <div className="overview-page">
    <WorkbenchHeader id={id} instanceName={instance.name} activeKey="overview" search={search} />

    <section className="overview-page__status" data-testid="overview-status" aria-labelledby={statusHeadingID}>
      <div className="overview-page__status-line">
        <HealthStatus status={instance.health.status} pausedAt={instance.collection_pause.updated_at} />
        {/* 当前归因就是「这台实例现在为什么是这个状态」，所以它是这一段的标题。 */}
        <h2 id={statusHeadingID} className="dbs-panel-title overview-page__attribution">
          {attributionLabel(instance)}
        </h2>
      </div>
      <div className="overview-page__status-line">
        <span className="overview-page__counts dbs-numeric">
          <FlashOnChange value={counts.critical}><span data-tone="critical">C{counts.critical}</span></FlashOnChange>
          <FlashOnChange value={counts.warning}><span data-tone="warning">W{counts.warning}</span></FlashOnChange>
          <FlashOnChange value={counts.info}><span data-tone="unknown">I{counts.info}</span></FlashOnChange>
        </span>
        <SuppressionTags flags={instance.health.flags} />
      </div>
      <div className="overview-page__status-line">
        {/* 端点这一行答的是「这台实例是什么、连到哪儿」：引擎 · 端点 ·
            建连接用的库。库名不是监控范围 —— 这条连接下的所有库都归这台实例。 */}
        <span className="dbs-caption overview-page__endpoint">
          {instanceEngineLabel(instance.engine)} · {instance.host}:{instance.port}
          {instance.database !== undefined && ` · 连接库 ${instance.database}`}
        </span>
        {instance.collection_pause.paused && <Link
          className="cds--link"
          to="/instances/$id/collection"
          params={{ id }}
        >查看采集暂停设置</Link>}
        {instance.health.flags.in_maintenance && <Link
          className="cds--link"
          to="/alert-settings/maintenance-windows"
        >查看维护窗口</Link>}
        <Button as={links.maintenance} kind="tertiary" size="md" renderIcon={Icon.glyph.calendar}>新建维护窗口</Button>
      </div>
    </section>

    {metricsQuery.dataUpdatedAt > 0 && <div className="overview-page__freshness">
      <Freshness
        dataUpdatedAt={metricsQuery.dataUpdatedAt}
        collectionInterval={overviewPollingOptions.refetchInterval}
      />
    </div>}

    <div className="overview-page__grid">
      <OverviewPanel module="availability" title="可用性与采集状态" loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.availability} metrics={metrics} />
        <KeyValueList label="采集状态" items={[
          { key: 'collected', label: '最近采集时间', value: lastCollectedAtLabel(instance.last_collected_at) },
          { key: 'agent', label: 'Agent 状态', value: agentStatusLabel(instance.agent_status) },
          { key: 'freshness', label: '数据新鲜度', value: dataFreshnessLabel(instance.data_freshness_seconds) },
        ]} />
      </OverviewPanel>

      <OverviewPanel module="alerts" title="当前告警摘要">
        <KeyValueList label="当前告警摘要" items={[
          { key: 'critical', label: '严重告警', value: counts.critical },
          { key: 'warning', label: '警告告警', value: counts.warning },
          { key: 'info', label: 'Info 告警', value: counts.info },
          { key: 'unresolved', label: '未恢复告警', value: unresolvedAlertCount },
          { key: 'attribution', label: '当前归因', value: attributionLabel(instance) },
        ]} />
        <Link
          className="cds--link overview-page__panel-link"
          to="/instances/$id/alerts"
          params={{ id }}
          search={{ tab: 'current', include_paused: false }}
        >查看当前告警</Link>
      </OverviewPanel>

      <OverviewPanel module="resources" title="核心资源" loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.resources} metrics={metrics} />
      </OverviewPanel>

      <OverviewPanel module="database" title="数据库负载" loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.database} metrics={metrics} />
        <Link
          className="cds--link overview-page__panel-link"
          to="/instances/$id/sessions"
          params={{ id }}
          search={{ from: search.from, to: search.to, filter: 'lock_wait' }}
        >查看锁等待会话</Link>
      </OverviewPanel>

      <OverviewPanel module="replication" title="复制状态" loading={metricsQuery.isPending}>
        <MetricFacts id={id} search={search} metricIDs={overviewMetricGroups.replication} metrics={metrics} />
      </OverviewPanel>

      <OverviewPanel module="events" title="近期性能事件" loading={eventsQuery.isPending}>
        <PerformanceEvents events={eventsQuery.data?.items} />
      </OverviewPanel>

      <OverviewPanel module="troubleshooting" title="快速排障入口">
        <div className="overview-page__actions">
          <Button as={links.monitoring} size="md" renderIcon={Icon.glyph.chartLine}>标准监控</Button>
          <Button as={links.sessions} kind="tertiary" size="md" renderIcon={Icon.glyph.database}>会话与阻塞</Button>
          <Button as={links.alerts} kind="tertiary" size="md" renderIcon={Icon.glyph.notification}>当前告警</Button>
          <Button as={links.collection} kind="tertiary" size="md" renderIcon={Icon.glyph.plug}>采集状态</Button>
        </div>
      </OverviewPanel>
    </div>
  </div>
}

/// 总览模块面板。`data-overview-module` 承载的是领域取值（这是哪一个模块），
/// `data-testid` 只是定位钩子，两者各有各的用处，不要合并。
function OverviewPanel({ module, title, loading = false, children }: {
  module: OverviewModule
  title: string
  loading?: boolean
  children: ReactNode
}) {
  return <Panel
    className="overview-page__panel"
    data-overview-module={module}
    title={<span data-testid="overview-module-title">{title}</span>}
    loading={loading}
  >
    {children}
  </Panel>
}

/// 键值清单。原来是 AntD 的 `Descriptions`，这里用 Carbon 的结构化列表表达同一件事。
/// 值一律过一遍变化高亮：这一页每 30 秒重画，不标出来就看不见哪个数动了。
function KeyValueList({ label, items }: {
  label: string
  items: { key: string; label: string; value: string | number }[]
}) {
  return <StructuredListWrapper aria-label={label} isCondensed className="overview-page__list">
    <StructuredListBody>
      {items.map((item) => (
        <StructuredListRow key={item.key}>
          <StructuredListCell noWrap>{item.label}</StructuredListCell>
          <StructuredListCell><FlashOnChange value={item.value} /></StructuredListCell>
        </StructuredListRow>
      ))}
    </StructuredListBody>
  </StructuredListWrapper>
}

function MetricFacts({ id, search, metricIDs, metrics }: {
  id: string
  search: MonitoringSearch
  metricIDs: readonly MetricID[]
  metrics: ResponseMetric[] | undefined
}) {
  return <div className="overview-page__facts">
    {metricIDs.map((metricID) => <MetricFact
      key={metricID}
      id={id}
      search={search}
      metricID={metricID}
      snapshot={latestMetricFacts(metrics?.find((item) => item.metric === metricID))}
    />)}
  </div>
}

/// 一个指标的当前值。取不到值时这里出现的是一句「为什么没有」加一个去处，不是一个 0 ——
/// 缺数不是 0，规范在图表与总览两处都是这么要求的。
function MetricFact({ id, search, metricID, snapshot }: {
  id: string
  search: MonitoringSearch
  metricID: MetricID
  snapshot: LatestMetricFacts
}) {
  const catalog = useMetricCatalog()
  const name = <Link
    className="cds--link"
    to="/instances/$id/monitoring"
    params={{ id }}
    search={{ ...search, metric: metricID }}
  >{catalog.label(metricID)}</Link>

  if (snapshot.unavailability) {
    const destinations = overviewDestinations(id, { ...search, metric: metricID })
    const copy = unavailabilityCopy(snapshot.unavailability)
    const href = unavailabilityHref(snapshot.unavailability, {
      current: destinations.monitoring,
      collection: destinations.collection,
    })
    // 整条通知条放进格子里太重了（一个面板里能有六个指标），所以是紧凑的三行：
    // 标题、原因、去处。文案与去处仍然取自 `domain/UnavailabilityBlock` 的那一份。
    return <div className="overview-page__missing">
      <span className="dbs-caption">{name}</span>
      <span className="dbs-body">{copy.title}</span>
      <span className="dbs-caption">{copy.description}</span>
      <a className="cds--link" href={href}>{copy.action}</a>
    </div>
  }

  return <>
    {snapshot.facts.map((fact, index) => {
      const value = fact.value === null ? '缺数' : formatMetricValue(metricID, fact.value, snapshot.unit)
      return <MetricBar
        key={`${fact.sampledAt}-${index}`}
        label={name}
        value={<FlashOnChange value={value} />}
        ratio={ratioOf(snapshot.unit, fact.value)}
        caption={dimensionLabel(fact.labels)}
      />
    })}
  </>
}

/// 只有百分比有「满格」这回事；别的单位没有分母，画一条比例条就是编造。
function ratioOf(unit: string, value: number | null): number | undefined {
  return unit === 'percent' && value !== null ? value / 100 : undefined
}

function PerformanceEvents({ events }: { events: PerformanceEvent[] | undefined }) {
  if (!events || events.length === 0) {
    return <div className="overview-page__empty">
      <span className="dbs-body">{performanceEventsEmptyState.title}</span>
      <span className="dbs-caption">{performanceEventsEmptyState.description}</span>
    </div>
  }
  return <ul className="overview-page__events">
    {events.map((event) => <li key={event.id} className="overview-page__event">
      <div className="overview-page__event-head">
        <AlertStatus status={event.alert_status} />
        <span className="dbs-body">{eventTypeLabel(event.event_type)}</span>
        <span className="dbs-caption">{new Date(event.updated_at).toLocaleString()}</span>
      </div>
      <p className="dbs-caption">{event.cause_summary}</p>
      <p className="dbs-caption">建议动作：{event.suggested_action}</p>
    </li>)}
  </ul>
}

function agentStatusLabel(status: components['schemas']['InstanceAgentStatus']): string {
  switch (status) {
    case 'online':
      return '在线'
    case 'offline':
      return '离线'
    case 'not_installed':
      return '未安装'
    case 'permission_denied':
      return '权限不足'
    case 'error':
      return '异常'
    default:
      return assertNever(status)
  }
}

function eventTypeLabel(eventType: components['schemas']['PerformanceEventType']): string {
  switch (eventType) {
    case 'LOCK_BLOCKING':
      return '锁等待与阻塞'
    case 'LONG_TRANSACTION':
      return '长事务'
    case 'IDLE_IN_TRANSACTION':
      return '事务空闲'
    case 'ACTIVE_SESSIONS_HIGH':
      return '活跃会话过高'
    case 'REPLICATION_LAG':
      return '复制延迟'
    case 'TEMP_FILES_SURGE':
      return '临时文件突增'
    default:
      return assertNever(eventType)
  }
}

function dimensionLabel(labels: Record<string, string>): string | undefined {
  const entries = Object.entries(labels)
  return entries.length === 0 ? undefined : entries.map(([key, value]) => `${key}=${value}`).join(', ')
}

function formatMetricValue(metricID: MetricID, value: number, unit: string): string {
  if (metricID === 'pg.availability.reachable') return value === 1 ? '可连接' : '不可连接'
  if (metricID === 'pg.replication.role') return ['单实例', '主库', '备库'][value] ?? `未知编码 ${value}`
  if (metricID === 'pg.replication.connection_state') {
    return ['已停止', '启动中', '初始化', '追赶中', '流复制中', '备份中', '停止中', '等待中', '重启中'][value] ?? `未知编码 ${value}`
  }
  return formatMetricNumber(value, unit)
}

function assertNever(value: never): never {
  throw new Error(`unexpected overview value: ${value}`)
}
