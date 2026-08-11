import { ArrowLeftOutlined, BarChartOutlined, DatabaseOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Descriptions, Empty, Space, Spin, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { MetricChart, metricUnavailability, type MetricChartSeries } from '../../domain/MetricChart'
import { AlertSuppressionTags } from '../../domain/SuppressionTags'
import { UnavailabilityBlock } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import { alertMonitoringSearch } from '../alerts/search'
import { metricOptions } from './metricOptions'

type AlertDetail = components['schemas']['AlertDetail']
type RuleVersionRecord = components['schemas']['AlertRuleVersionRecord']

export const instanceAlertDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts/$alertId',
  component: InstanceAlertDetailPage,
})

function InstanceAlertDetailPage() {
  const { id, alertId } = instanceAlertDetailRoute.useParams()
  const detail = $api.useQuery('get', '/api/v1/alert-instances/{id}', { params: { path: { id: alertId } } })

  if (detail.isPending) return <Spin size="large" />
  if (detail.error || !detail.data) {
    return <Alert
      type="error"
      showIcon
      title={apiErrorMessage(detail.error, '告警详情不可用')}
      action={<Link to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current', include_paused: false }}><Button>返回告警列表</Button></Link>}
    />
  }
  return <AlertDetailContent detail={detail.data} routeInstanceID={id} />
}

function AlertDetailContent({ detail, routeInstanceID }: { detail: AlertDetail; routeInstanceID: string }) {
  const metric = metricOptions.find((option) => option.id === detail.metric_id)
  const monitoringSearch = metric ? alertMonitoringSearch({
    metric_id: metric.id,
    first_triggered_at: detail.first_triggered_at,
    recovered_at: detail.recovered_at,
    updated_at: detail.updated_at,
  }) : undefined
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', {
    params: { path: { id: detail.instance_id } },
  })

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Link
      to="/instances/$id/alerts"
      params={{ id: routeInstanceID }}
      search={{ tab: detail.status === 'RECOVERED' ? 'history' : 'current', include_paused: false }}
    ><ArrowLeftOutlined /> 返回告警列表</Link>

    <Space className="workbench-heading" wrap>
      <div>
        <Space wrap>
          <Typography.Title level={2} style={{ margin: 0 }}>{detail.rule_name}</Typography.Title>
          <AlertStatus status={detail.status} />
        </Space>
        <Typography.Text type="secondary">{detail.instance_name} · {detail.metric_id}</Typography.Text>
      </div>
      <Space wrap>
        {monitoringSearch && <Link to="/instances/$id" params={{ id: detail.instance_id }} search={monitoringSearch}>
          <Button type="primary" icon={<BarChartOutlined />}>查看标准监控</Button>
        </Link>}
        <Link to="/instances/$id/collection" params={{ id: detail.instance_id }}>
          <Button icon={<DatabaseOutlined />}>查看采集状态</Button>
        </Link>
      </Space>
    </Space>

    <Descriptions
      bordered
      size="small"
      column={{ xs: 1, sm: 2, lg: 3 }}
      items={[
        { key: 'status', label: '状态与标记', children: <><AlertStatus status={detail.status} /> <AlertSuppressionTags inMaintenance={detail.in_maintenance} disposition={detail.disposition} paused={detail.paused} pausedAt={detail.paused_at} /></> },
        { key: 'severity', label: '级别', children: severityLabel(detail.severity) },
        { key: 'value', label: '触发值 / 阈值', children: `${optionalNumber(detail.current_value)} / ${optionalNumber(detail.threshold)}` },
        { key: 'first', label: '首次触发', children: optionalTime(detail.first_triggered_at) },
        { key: 'updated', label: '最近评估', children: optionalTime(detail.updated_at) },
        { key: 'recovered', label: '恢复时间', children: optionalTime(detail.recovered_at) },
      ]}
    />

    <section className="alert-detail-section" aria-labelledby="trigger-metric-heading">
      <Typography.Title id="trigger-metric-heading" level={3}>触发指标</Typography.Title>
      {metric && monitoringSearch
        ? <AlertMetricChart detail={detail} metricID={metric.id} monitoringSearch={monitoringSearch} />
        : <Alert type="error" showIcon title="告警指标不在指标字典中" />}
    </section>

    <section className="alert-detail-section" aria-labelledby="rule-snapshot-heading">
      <Typography.Title id="rule-snapshot-heading" level={3}>规则快照</Typography.Title>
      <div className="rule-snapshot-layout">
        <pre className="rule-snapshot-json">{JSON.stringify(detail.rule_snapshot, null, 2)}</pre>
        <Table<RuleVersionRecord>
          rowKey={(record) => `${record.version}-${record.evaluated_at}`}
          size="small"
          pagination={false}
          dataSource={detail.rule_version_history}
          columns={ruleVersionColumns}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无版本切换记录" /> }}
        />
      </div>
    </section>

    <section className="alert-detail-section" aria-labelledby="no-data-heading">
      <Typography.Title id="no-data-heading" level={3}>No Data 原因</Typography.Title>
      {detail.unavailability
        ? <UnavailabilityBlock code={detail.unavailability} href={`/instances/${encodeURIComponent(detail.instance_id)}/collection?metric=${encodeURIComponent(detail.metric_id)}`} />
        : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无 No Data 原因" />}
    </section>

    <section className="alert-detail-section" aria-labelledby="collection-status-heading">
      <Typography.Title id="collection-status-heading" level={3}>采集状态</Typography.Title>
      <Descriptions size="small" bordered column={{ xs: 1, sm: 3 }} items={[
        { key: 'agent', label: 'Agent', children: instance.data ? agentStatusLabel(instance.data.agent_status) : '—' },
        { key: 'collected', label: '最近成功采集', children: optionalTime(instance.data?.last_collected_at) },
        { key: 'pause', label: '采集暂停', children: instance.data?.collection_pause.paused ? <Tag>已暂停</Tag> : instance.data ? '否' : '—' },
      ]} />
    </section>

    <div className="alert-detail-grid">
      <section className="alert-detail-section" aria-labelledby="notification-heading">
        <Typography.Title id="notification-heading" level={3}>通知结果</Typography.Title>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={detail.notification_results.length === 0 ? '暂无通知结果' : `${detail.notification_results.length} 条通知结果`} />
      </section>
      <section className="alert-detail-section" aria-labelledby="disposition-heading">
        <Typography.Title id="disposition-heading" level={3}>处置记录</Typography.Title>
        {detail.disposition === 'NONE'
          ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无处置记录" />
          : <Typography.Text>{detail.disposition === 'ACKED' ? '已确认' : '已忽略'}</Typography.Text>}
      </section>
      <section className="alert-detail-section" aria-labelledby="trigger-snapshot-heading">
        <Typography.Title id="trigger-snapshot-heading" level={3}>触发现场快照</Typography.Title>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无触发现场快照" />
      </section>
      <section className="alert-detail-section" aria-labelledby="performance-event-heading">
        <Typography.Title id="performance-event-heading" level={3}>关联性能事件</Typography.Title>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联性能事件" />
      </section>
    </div>
  </Space>
}

function AlertMetricChart({ detail, metricID, monitoringSearch }: {
  detail: AlertDetail
  metricID: (typeof metricOptions)[number]['id']
  monitoringSearch: ReturnType<typeof alertMonitoringSearch>
}) {
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id: detail.instance_id },
      query: { metric: [metricID], from: monitoringSearch.from, to: monitoringSearch.to, step: monitoringSearch.step },
    },
  })
  const response = metrics.data?.metrics[0]
  const series: MetricChartSeries[] = response?.unavailability === null
    ? response.series.map((item) => ({ name: detail.metric_id, unit: response.unit, points: item.points }))
    : []

  return <MetricChart
    label={detail.rule_name}
    series={series}
    step={metrics.data?.step ?? 'auto'}
    unavailability={detail.unavailability ?? metricUnavailability(response)}
    unavailabilityHref={`/instances/${encodeURIComponent(detail.instance_id)}/collection?metric=${encodeURIComponent(detail.metric_id)}`}
    loading={metrics.isFetching}
  />
}

const ruleVersionColumns: TableColumnsType<RuleVersionRecord> = [
  { title: '版本', dataIndex: 'version', width: 80 },
  { title: '生效评估时间', render: (_, record) => optionalTime(record.evaluated_at) },
]

function severityLabel(severity: components['schemas']['AlertSeverity']): string {
  switch (severity) {
    case 'critical': return '严重'
    case 'warning': return '警告'
    case 'info': return 'Info'
    default: return assertNever(severity)
  }
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

function optionalNumber(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

function optionalTime(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString()
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert detail value: ${value}`)
}
