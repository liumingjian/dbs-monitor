import { BellOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Empty, Space, Switch, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness } from '../../domain/Freshness'
import { AlertSuppressionTags } from '../../domain/SuppressionTags'
import { rootRoute } from '../root'
import { parseAlertListSearch, type AlertListSearch } from './search'

type AlertObservation = components['schemas']['AlertObservation']
type AlertSeverity = components['schemas']['AlertSeverity']

const alertPageSize = 50

export const alertsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alerts',
  validateSearch: (search): AlertListSearch | { error: string } => parseAlertListSearch(search),
  component: AlertsPage,
})

function AlertsPage() {
  const search = alertsRoute.useSearch()
  const navigate = alertsRoute.useNavigate()
  if ('error' in search) {
    return <Alert
      type="error"
      showIcon
      title={search.error}
      action={<Link to="/alerts" search={{ tab: 'current', include_paused: false }}><Button>使用默认筛选</Button></Link>}
    />
  }
  return <AlertObservationLists
    search={search}
    onSearchChange={(next) => void navigate({ search: next })}
    heading="全局告警"
  />
}

export function AlertObservationLists({ search, onSearchChange, heading }: {
  search: AlertListSearch
  onSearchChange: (search: AlertListSearch) => void
  heading?: string
}) {
  const page = search.page ?? 1
  const offset = (page - 1) * alertPageSize
  const current = $api.useQuery('get', '/api/v1/alerts/current', {
    params: {
      query: {
        instance_id: search.instance_id,
        include_paused: search.include_paused,
        limit: alertPageSize,
        offset,
      },
    },
  }, { refetchInterval: pollingIntervals.currentAlerts })
  const history = $api.useQuery('get', '/api/v1/alerts/history', {
    params: {
      query: {
        instance_id: search.instance_id,
        limit: alertPageSize,
        offset,
      },
    },
  }, { refetchInterval: pollingIntervals.history })

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    {heading && <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>{heading}</Typography.Title>
        <Typography.Text type="secondary">跨实例查看触发、恢复与处置事实</Typography.Text>
      </div>
      <BellOutlined className="page-heading-icon" />
    </Space>}
    <Tabs
      activeKey={search.tab}
      onChange={(tab) => onSearchChange({ ...search, tab: tab as AlertListSearch['tab'], page: 1 })}
      items={[
        {
          key: 'current',
          label: `当前告警 ${current.data?.total ?? ''}`,
          children: <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Space wrap>
              <Switch
                aria-label="包含已暂停冻结告警"
                checked={search.include_paused}
                onChange={(includePaused) => onSearchChange({ ...search, include_paused: includePaused, page: 1 })}
              />
              <Typography.Text>包含已暂停冻结告警</Typography.Text>
              {current.dataUpdatedAt > 0 && <Freshness
                dataUpdatedAt={current.dataUpdatedAt}
                collectionInterval={pollingIntervals.currentAlerts}
              />}
            </Space>
            {current.error && <Alert type="error" showIcon title={apiErrorMessage(current.error, '当前告警加载失败')} />}
            <Table<AlertObservation>
              rowKey="id"
              loading={current.isPending}
              dataSource={current.data?.items ?? []}
              locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有符合筛选的当前告警" /> }}
              pagination={{
                current: page,
                pageSize: alertPageSize,
                total: current.data?.total,
                showSizeChanger: false,
                onChange: (nextPage) => onSearchChange({ ...search, page: nextPage }),
              }}
              scroll={{ x: 1320 }}
              columns={currentColumns}
            />
          </Space>,
        },
        {
          key: 'history',
          label: `告警历史 ${history.data?.total ?? ''}`,
          children: <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {history.error && <Alert type="error" showIcon title={apiErrorMessage(history.error, '告警历史加载失败')} />}
            <Table<AlertObservation>
              rowKey="id"
              loading={history.isPending}
              dataSource={history.data?.items ?? []}
              locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无告警历史" /> }}
              pagination={{
                current: page,
                pageSize: alertPageSize,
                total: history.data?.total,
                showSizeChanger: false,
                onChange: (nextPage) => onSearchChange({ ...search, page: nextPage }),
              }}
              scroll={{ x: 1620 }}
              columns={historyColumns}
            />
          </Space>,
        },
      ]}
    />
  </Space>
}

const currentColumns: TableColumnsType<AlertObservation> = [
  {
    title: '状态与标记',
    width: 260,
    render: (_, alert) => <Space direction="vertical" size={4}>
      <Space><AlertStatus status={alert.status} />{severityTag(alert.severity)}</Space>
      <AlertSuppressionTags
        inMaintenance={alert.in_maintenance}
        disposition={alert.disposition}
        paused={alert.paused}
        pausedAt={alert.paused_at}
      />
    </Space>,
  },
  { title: '实例', width: 170, dataIndex: 'instance_name' },
  { title: '规则', width: 220, dataIndex: 'rule_name' },
  { title: '指标', width: 230, dataIndex: 'metric_id' },
  { title: '触发值', width: 100, render: (_, alert) => optionalNumber(alert.current_value) },
  { title: '阈值', width: 100, render: (_, alert) => optionalNumber(alert.threshold) },
  { title: '首次触发', width: 190, render: (_, alert) => optionalTime(alert.first_triggered_at) },
  { title: '持续时间', width: 120, render: (_, alert) => durationLabel(alert.duration_ms) },
  { title: 'No Data 原因', width: 180, render: (_, alert) => alert.unavailability ?? '—' },
  {
    title: '操作',
    fixed: 'right',
    width: 90,
    render: (_, alert) => <Link
      to="/instances/$id/alerts/$alertId"
      params={{ id: alert.instance_id, alertId: alert.id }}
    >详情</Link>,
  },
]

const historyColumns: TableColumnsType<AlertObservation> = [
  { title: '状态', width: 100, render: (_, alert) => <AlertStatus status={alert.status} /> },
  { title: '实例', width: 160, dataIndex: 'instance_name' },
  { title: '规则', width: 210, dataIndex: 'rule_name' },
  { title: '触发时间', width: 190, render: (_, alert) => optionalTime(alert.first_triggered_at) },
  { title: '恢复时间', width: 190, render: (_, alert) => optionalTime(alert.recovered_at) },
  { title: '持续时间', width: 120, render: (_, alert) => durationLabel(alert.duration_ms) },
  { title: '触发值', width: 90, render: (_, alert) => optionalNumber(alert.current_value) },
  { title: '阈值', width: 90, render: (_, alert) => optionalNumber(alert.threshold) },
  { title: '规则快照 / 版本', width: 150, render: (_, alert) => `版本 ${alert.rule_version}` },
  { title: '通知结果', width: 110, render: () => '—' },
  { title: '处置记录', width: 110, render: (_, alert) => dispositionLabel(alert.disposition) },
  { title: '维护窗口', width: 110, render: (_, alert) => maintenanceWindowLabel(alert.in_maintenance) },
  {
    title: '操作',
    fixed: 'right',
    width: 90,
    render: (_, alert) => <Link
      to="/instances/$id/alerts/$alertId"
      params={{ id: alert.instance_id, alertId: alert.id }}
    >详情</Link>,
  },
]

function severityTag(severity: AlertSeverity) {
  switch (severity) {
    case 'critical': return <Tag color="error">严重</Tag>
    case 'warning': return <Tag color="warning">警告</Tag>
    case 'info': return <Tag color="processing">Info</Tag>
    default: return assertNever(severity)
  }
}

function dispositionLabel(disposition: components['schemas']['AlertDisposition']): string {
  switch (disposition) {
    case 'NONE': return '—'
    case 'ACKED': return '已确认'
    case 'IGNORED': return '已忽略'
    default: return assertNever(disposition)
  }
}

function maintenanceWindowLabel(inMaintenance: boolean | null | undefined): string {
  if (inMaintenance === undefined) return '—'
  return inMaintenance ? '是' : '否'
}

function optionalNumber(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
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
  throw new Error(`unexpected alert observation value: ${value}`)
}
