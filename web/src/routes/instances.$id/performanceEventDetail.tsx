import { AlertOutlined, ArrowLeftOutlined, BarChartOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Descriptions, Space, Spin, Typography } from 'antd'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { rootRoute } from '../root'
import { DispositionSection, TriggerSnapshotSection, triggerSnapshotPresentation } from './alertEvidence'
import {
  eventMonitoringSearch,
  parsePerformanceEventSearch,
  serializePerformanceEventSearch,
  type PerformanceEventSearch,
} from './performanceEvents'
import {
  PerformanceEventSeverityTag,
  performanceEventDispositionLabel,
  performanceEventDurationLabel,
  performanceEventSeverityPresentation,
  performanceEventTimeLabel,
  performanceEventTypeLabel,
} from './performanceEventPresentation'

type PerformanceEvent = components['schemas']['PerformanceEvent']

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
    return <Alert type="error" showIcon title={search.error} />
  }
  if (event.isPending) return <Spin size="large" />
  if (event.error || !event.data) {
    return <Alert
      type="error"
      showIcon
      title={apiErrorMessage(event.error, '性能事件详情不可用')}
      action={<Link to="/instances/$id/performance-events" params={{ id }} search={serializePerformanceEventSearch(search)}><Button>返回性能事件</Button></Link>}
    />
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

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Link
      to="/instances/$id/performance-events"
      params={{ id: event.instance_id }}
      search={serializePerformanceEventSearch(search)}
    ><ArrowLeftOutlined /> 返回性能事件</Link>

    <Space className="workbench-heading" wrap>
      <div>
        <Space wrap>
          <Typography.Title level={2} style={{ margin: 0 }}>{performanceEventTypeLabel(event.event_type)}</Typography.Title>
          <AlertStatus status={event.alert_status} />
          <PerformanceEventSeverityTag severity={event.severity} />
        </Space>
        <Typography.Text type="secondary">{instanceName ?? event.instance_id} · {event.metric_id}</Typography.Text>
      </div>
      <Space wrap>
        {monitoringSearch && <Link to="/instances/$id/monitoring" params={{ id: event.instance_id }} search={monitoringSearch}>
          <Button type="primary" icon={<BarChartOutlined />}>查看标准监控</Button>
        </Link>}
        <Link to="/instances/$id/alerts/$alertId" params={{ id: event.instance_id, alertId: event.alert_instance_id }}>
          <Button icon={<AlertOutlined />}>查看告警详情</Button>
        </Link>
      </Space>
    </Space>

    <Descriptions bordered size="small" column={{ xs: 1, sm: 2, lg: 3 }} items={[
      { key: 'id', label: '事件 ID', children: event.id },
      { key: 'instance', label: '实例', children: instanceName ?? event.instance_id },
      { key: 'status', label: '状态', children: <AlertStatus status={event.alert_status} /> },
      { key: 'severity', label: '级别', children: severity.label },
      { key: 'disposition', label: '处置状态', children: performanceEventDispositionLabel(event.disposition) },
      { key: 'derived', label: '首次发生时间', children: performanceEventTimeLabel(event.derived_at) },
      { key: 'updated', label: '最近发生时间', children: performanceEventTimeLabel(event.updated_at) },
      { key: 'recovered', label: '恢复时间', children: performanceEventTimeLabel(event.recovered_at) },
      { key: 'duration', label: '持续时长', children: performanceEventDurationLabel(event.duration_ms) },
      { key: 'metric', label: '触发指标', children: event.metric_id },
      { key: 'value', label: '触发值 / 阈值', children: `${event.trigger_value} / ${event.threshold}` },
      { key: 'snapshot', label: '现场快照', children: snapshot.label },
    ]} />

    <section className="event-guidance" aria-labelledby="event-cause-heading">
      <div>
        <Typography.Title id="event-cause-heading" level={3}>原因摘要</Typography.Title>
        <Typography.Paragraph>{event.cause_summary}</Typography.Paragraph>
      </div>
      <div>
        <Typography.Title level={3}>建议动作</Typography.Title>
        <Typography.Paragraph>{event.suggested_action}</Typography.Paragraph>
      </div>
    </section>

    <DispositionSection
      alertInstanceID={event.alert_instance_id}
      recovered={event.alert_status === 'RECOVERED'}
      onChanged={onDispositionChanged}
    />
    <TriggerSnapshotSection alertInstanceID={event.alert_instance_id} eventEvidence />
  </Space>
}
