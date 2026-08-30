import { Button, StructuredListBody, StructuredListCell, StructuredListRow, StructuredListWrapper } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { MetricChart } from '../../domain/MetricChart'
import { unavailabilityHref } from '../../domain/UnavailabilityBlock'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import { rootRoute } from '../root'
import { DispositionSection, TriggerSnapshotSection, triggerSnapshotPresentation } from './alertEvidence'
import {
  eventMonitoringSearch,
  parsePerformanceEventSearch,
  performanceEventChartView,
  serializePerformanceEventSearch,
  type EventMonitoringSearch,
  type PerformanceEventSearch,
} from './performanceEvents'
import {
  PerformanceEventSeverityTag,
  PerformanceEventMaintenanceTag,
  performanceEventDispositionLabel,
  performanceEventDurationLabel,
  performanceEventSeverityPresentation,
  performanceEventTimeLabel,
  performanceEventTypeLabel,
} from './performanceEventPresentation'
import './performanceEventDetail.css'

type PerformanceEvent = components['schemas']['PerformanceEvent']

// 关联指标图那一段的锚点。缺数说明块的「去处」链接指向它，所以它是一个真的 id，
// 不是 Panel 自己生成的那个 —— 生成的 id 每次挂载都不一样，写不进 href。
const metricSectionID = 'performance-event-metric'

export const performanceEventDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/performance-events/$eventId',
  validateSearch: (search): PerformanceEventSearch | { error: string } => parsePerformanceEventSearch(search),
  component: PerformanceEventDetailPage,
})

function PerformanceEventDetailPage() {
  const { id, eventId } = performanceEventDetailRoute.useParams()
  const search = performanceEventDetailRoute.useSearch()
  const event = $api.useQuery('get', '/api/v1/performance-events/{id}', {
    params: { path: { id: eventId } },
  })
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', {
    params: { path: { id } },
  })

  if ('error' in search) {
    return <div className="performance-event-detail">
      <NotificationBar tone="critical" title={search.error} />
    </div>
  }
  // 规范要求骨架占位，不要整页转圈：页面的骨架先把版式立起来，读者知道等的是什么。
  if (event.isPending) {
    return <div className="performance-event-detail">
      <SkeletonBlock lines={2} label="性能事件详情加载中" />
      <SkeletonBlock lines={5} decorative />
      <SkeletonBlock lines={4} decorative />
    </div>
  }
  if (event.error || !event.data) {
    return <div className="performance-event-detail">
      <NotificationBar tone="critical" title={apiErrorMessage(event.error, '性能事件详情不可用')} />
      <Link
        to="/instances/$id/performance-events"
        params={{ id }}
        search={serializePerformanceEventSearch(search)}
      ><Button size="md">返回性能事件</Button></Link>
    </div>
  }

  return <PerformanceEventDetailContent
    event={event.data}
    instanceName={instance.data?.name}
    search={search}
    onDispositionChanged={() => void event.refetch()}
  />
}

function PerformanceEventDetailContent({ event, instanceName, search, onDispositionChanged }: {
  event: PerformanceEvent
  instanceName: string | undefined
  search: PerformanceEventSearch
  onDispositionChanged: () => void
}) {
  const monitoringSearch = eventMonitoringSearch(event)
  const snapshot = triggerSnapshotPresentation(event.trigger_snapshot_result)
  const severity = performanceEventSeverityPresentation(event.severity)

  // Carbon 的 `as` 槽只收组件，不能顺带把路由属性交出去，所以每个去处包成一个已经
  // 知道自己去哪儿的组件，并用 memo 固定身份（先例见 workbench.tsx）。
  const links = useMemo(() => ({
    monitoring: monitoringSearch === undefined
      ? undefined
      : (props: object) => <Link
        {...props}
        to="/instances/$id/monitoring"
        params={{ id: event.instance_id }}
        search={monitoringSearch}
      />,
    alert: (props: object) => <Link
      {...props}
      to="/instances/$id/alerts/$alertId"
      params={{ id: event.instance_id, alertId: event.alert_instance_id }}
    />,
  }), [event.instance_id, event.alert_instance_id, monitoringSearch])

  return <div className="performance-event-detail">
    <Link
      className="cds--link performance-event-detail__back"
      to="/instances/$id/performance-events"
      params={{ id: event.instance_id }}
      search={serializePerformanceEventSearch(search)}
    ><Icon name="arrowLeft" /> 返回性能事件</Link>

    <header className="performance-event-detail__header">
      <div className="performance-event-detail__identity">
        <div className="performance-event-detail__title">
          <h1 className="dbs-page-title">{performanceEventTypeLabel(event.event_type)}</h1>
          <AlertStatus status={event.alert_status} />
          <PerformanceEventSeverityTag severity={event.severity} />
          <PerformanceEventMaintenanceTag inMaintenance={event.in_maintenance} />
        </div>
        <p className="dbs-caption">{instanceName ?? event.instance_id} · {event.metric_id}</p>
      </div>
      <div className="performance-event-detail__actions">
        {links.monitoring && <Button as={links.monitoring} size="md" renderIcon={MonitoringIcon}>查看标准监控</Button>}
        <Button as={links.alert} kind="tertiary" size="md" renderIcon={AlertIcon}>查看告警详情</Button>
      </div>
    </header>

    <Panel title="事件概览">
      <KeyValueList label="事件概览" columns={3} items={[
        { key: 'id', label: '事件 ID', value: event.id },
        { key: 'instance', label: '实例', value: instanceName ?? event.instance_id },
        { key: 'status', label: '状态', value: <AlertStatus status={event.alert_status} /> },
        { key: 'severity', label: '级别', value: severity.label },
        { key: 'disposition', label: '处置状态', value: performanceEventDispositionLabel(event.disposition) },
        { key: 'maintenance', label: '维护窗口', value: event.maintenance_window_id ?? '—' },
        { key: 'derived', label: '首次发生时间', value: performanceEventTimeLabel(event.derived_at) },
        { key: 'updated', label: '最近发生时间', value: performanceEventTimeLabel(event.updated_at) },
        { key: 'recovered', label: '恢复时间', value: performanceEventTimeLabel(event.recovered_at) },
        { key: 'duration', label: '持续时长', value: performanceEventDurationLabel(event.duration_ms) },
        { key: 'metric', label: '触发指标', value: event.metric_id },
        { key: 'value', label: '触发值 / 阈值', value: `${event.trigger_value} / ${event.threshold}` },
        { key: 'snapshot', label: '现场快照', value: snapshot.label },
      ]} />
    </Panel>

    {monitoringSearch && <EventMetricChart event={event} monitoringSearch={monitoringSearch} />}

    <div className="performance-event-detail__guidance">
      <Panel title="原因摘要">
        <p className="dbs-body">{event.cause_summary}</p>
      </Panel>
      <Panel title="建议动作">
        <p className="dbs-body">{event.suggested_action}</p>
      </Panel>
    </div>

    <DispositionSection
      alertInstanceID={event.alert_instance_id}
      recovered={event.alert_status === 'RECOVERED'}
      onChanged={onDispositionChanged}
    />
    <TriggerSnapshotSection alertInstanceID={event.alert_instance_id} eventEvidence />
  </div>
}

function MonitoringIcon() {
  return <Icon name="chartLine" />
}

function AlertIcon() {
  return <Icon name="notification" />
}

/// 键值清单。原来是 AntD 的 `Descriptions`，这里用 Carbon 的结构化列表表达同一件事。
function KeyValueList({ label, items, columns }: {
  label: string
  items: { key: string; label: string; value: ReactNode }[]
  columns: 2 | 3
}) {
  return <StructuredListWrapper
    aria-label={label}
    isCondensed
    className="performance-event-detail__list"
    data-columns={columns}
  >
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

function EventMetricChart({ event, monitoringSearch }: {
  event: PerformanceEvent
  monitoringSearch: EventMonitoringSearch
}) {
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id: event.instance_id },
      query: {
        metric: [monitoringSearch.metric],
        from: monitoringSearch.from,
        to: monitoringSearch.to,
        step: monitoringSearch.step,
      },
    },
  })
  const view = performanceEventChartView(
    monitoringSearch.metric,
    metrics.data?.metrics,
  )
  const currentHref = `#${metricSectionID}`
  const remediationHref = view.unavailability
    ? unavailabilityHref(view.unavailability, {
        current: currentHref,
        collection: `/instances/${encodeURIComponent(event.instance_id)}/collection?metric=${encodeURIComponent(event.metric_id)}`,
      })
    : currentHref

  return <Panel id={metricSectionID} title="关联指标图" className="performance-event-detail__chart">
    <MetricChart
      label={performanceEventTypeLabel(event.event_type)}
      series={view.series}
      step={metrics.data?.step ?? monitoringSearch.step}
      unavailability={view.unavailability}
      unavailabilityHref={remediationHref}
      loading={metrics.isFetching}
    />
  </Panel>
}
