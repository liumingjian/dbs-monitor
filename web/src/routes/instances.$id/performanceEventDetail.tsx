import { AlertOutlined, ArrowLeftOutlined, BarChartOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Descriptions, Space, Spin, Tag, Typography } from 'antd'
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

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Link
      to="/instances/$id/performance-events"
      params={{ id: event.instance_id }}
      search={serializePerformanceEventSearch(search)}
    ><ArrowLeftOutlined /> 返回性能事件</Link>

    <Space className="workbench-heading" wrap>
      <div>
        <Space wrap>
          <Typography.Title level={2} style={{ margin: 0 }}>{eventTypeLabel(event.event_type)}</Typography.Title>
          <AlertStatus status={event.alert_status} />
          {severityTag(event.severity)}
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
      { key: 'severity', label: '级别', children: severityLabel(event.severity) },
      { key: 'disposition', label: '处置状态', children: dispositionLabel(event.disposition) },
      { key: 'derived', label: '首次发生时间', children: optionalTime(event.derived_at) },
      { key: 'updated', label: '最近发生时间', children: optionalTime(event.updated_at) },
      { key: 'recovered', label: '恢复时间', children: optionalTime(event.recovered_at) },
      { key: 'duration', label: '持续时长', children: durationLabel(event.duration_ms) },
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

function eventTypeLabel(eventType: components['schemas']['PerformanceEventType']): string {
  switch (eventType) {
    case 'LOCK_BLOCKING': return '锁等待 / 阻塞'
    case 'LONG_TRANSACTION': return '长事务'
    case 'IDLE_IN_TRANSACTION': return 'idle in transaction'
    case 'ACTIVE_SESSIONS_HIGH': return '活跃会话过高'
    case 'REPLICATION_LAG': return '复制延迟'
    case 'TEMP_FILES_SURGE': return '临时文件突增'
    default: return assertNever(eventType)
  }
}

function severityTag(severity: components['schemas']['AlertSeverity']) {
  switch (severity) {
    case 'critical': return <Tag color="error">严重</Tag>
    case 'warning': return <Tag color="warning">警告</Tag>
    case 'info': return <Tag color="processing">Info</Tag>
    default: return assertNever(severity)
  }
}

function severityLabel(severity: components['schemas']['AlertSeverity']): string {
  switch (severity) {
    case 'critical': return '严重'
    case 'warning': return '警告'
    case 'info': return 'Info'
    default: return assertNever(severity)
  }
}

function dispositionLabel(disposition: components['schemas']['AlertDisposition']): string {
  switch (disposition) {
    case 'NONE': return '未处置'
    case 'ACKED': return '已确认'
    case 'IGNORED': return '已忽略'
    default: return assertNever(disposition)
  }
}

function optionalTime(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString()
}

function durationLabel(milliseconds: number): string {
  const minutes = Math.floor(milliseconds / 60_000)
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时`
  return `${Math.floor(hours / 24)} 天`
}

function assertNever(value: never): never {
  throw new Error(`unexpected performance event detail value: ${value}`)
}
