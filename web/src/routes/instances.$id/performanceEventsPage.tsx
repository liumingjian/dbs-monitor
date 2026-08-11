import { FundProjectionScreenOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Empty, Segmented, Space, Table, Tabs, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness } from '../../domain/Freshness'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import { rootRoute } from '../root'
import {
  isPerformanceEventTab,
  parsePerformanceEventSearch,
  performanceEventRecoveryFilter,
  serializePerformanceEventSearch,
  type PerformanceEventDisposition,
  type PerformanceEventSearch,
} from './performanceEvents'
import {
  PerformanceEventSeverityTag,
  performanceEventDispositionLabel,
  performanceEventDurationLabel,
  performanceEventTimeLabel,
  performanceEventTypeLabel,
} from './performanceEventPresentation'
import { WorkbenchHeader } from './workbench'

type PerformanceEvent = components['schemas']['PerformanceEvent']

const eventPageSize = 50

export const performanceEventsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/performance-events',
  validateSearch: (search): PerformanceEventSearch | { error: string } => parsePerformanceEventSearch(search),
  component: PerformanceEventsPage,
})

function PerformanceEventsPage() {
  const { id } = performanceEventsRoute.useParams()
  const search = performanceEventsRoute.useSearch()
  const navigate = performanceEventsRoute.useNavigate()

  if ('error' in search) {
    const now = new Date()
    const defaults: PerformanceEventSearch = {
      from: new Date(now.getTime() - 60 * 60_000).toISOString(),
      to: now.toISOString(),
      tab: 'firing',
      disposition: 'ACKED',
      page: 1,
    }
    return <Alert
      type="error"
      showIcon
      title={search.error}
      action={<Link to="/instances/$id/performance-events" params={{ id }} search={serializePerformanceEventSearch(defaults)}><Button>使用默认筛选</Button></Link>}
    />
  }

  return <PerformanceEventLists
    instanceID={id}
    search={search}
    onSearchChange={(next) => void navigate({ search: serializePerformanceEventSearch(next) })}
  />
}

function PerformanceEventLists({ instanceID, search, onSearchChange }: {
  instanceID: string
  search: PerformanceEventSearch
  onSearchChange: (search: PerformanceEventSearch) => void
}) {
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', {
    params: { path: { id: instanceID } },
  })
  const offset = (search.page - 1) * eventPageSize
  const events = $api.useQuery('get', '/api/v1/instances/{id}/performance-events', {
    params: {
      path: { id: instanceID },
      query: {
        from: search.from,
        to: search.to,
        recovered: performanceEventRecoveryFilter(search.tab),
        disposition: search.tab === 'disposed' ? search.disposition : undefined,
        limit: eventPageSize,
        offset,
        sort: '-derived_at',
      },
    },
  }, { refetchInterval: search.tab === 'firing' ? pollingIntervals.firingPerformanceEvents : false })

  function changeTab(tab: string) {
    if (!isPerformanceEventTab(tab)) return
    onSearchChange({ ...search, tab, page: 1 })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader
      id={instanceID}
      instanceName={instance.data?.name}
      activeKey="events"
      search={{ from: search.from, to: search.to }}
    />
    <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>性能事件</Typography.Title>
        <Typography.Text type="secondary">查看告警派生的异常、原因与处置证据</Typography.Text>
      </div>
      <FundProjectionScreenOutlined className="page-heading-icon" />
    </Space>
    <TimeRangePicker
      from={search.from}
      to={search.to}
      onChange={(range) => onSearchChange({ ...search, ...range, page: 1 })}
    />
    <Tabs activeKey={search.tab} onChange={changeTab} items={[
      { key: 'firing', label: search.tab === 'firing' ? `触发中 ${events.data?.total ?? ''}` : '触发中' },
      { key: 'recovered', label: search.tab === 'recovered' ? `已恢复 ${events.data?.total ?? ''}` : '已恢复' },
      { key: 'disposed', label: search.tab === 'disposed' ? `已确认 / 已忽略 ${events.data?.total ?? ''}` : '已确认 / 已忽略' },
    ]} />
    {search.tab === 'disposed' && <Segmented<PerformanceEventDisposition>
      aria-label="处置状态"
      value={search.disposition}
      options={[{ value: 'ACKED', label: '已确认' }, { value: 'IGNORED', label: '已忽略' }]}
      onChange={(disposition) => onSearchChange({
        ...search,
        disposition,
        page: 1,
      })}
    />}
    {search.tab === 'firing' && events.dataUpdatedAt > 0 && <Freshness
      dataUpdatedAt={events.dataUpdatedAt}
      collectionInterval={pollingIntervals.firingPerformanceEvents}
    />}
    {events.error && <Alert type="error" showIcon title={apiErrorMessage(events.error, '性能事件加载失败')} />}
    <Table<PerformanceEvent>
      rowKey="id"
      loading={events.isPending}
      dataSource={events.data?.items ?? []}
      columns={eventColumns(search)}
      scroll={{ x: 1700 }}
      pagination={{
        current: search.page,
        pageSize: eventPageSize,
        total: events.data?.total,
        showSizeChanger: false,
        onChange: (page) => onSearchChange({ ...search, page }),
      }}
      locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={eventEmptyText(search)} /> }}
    />
  </Space>
}

function eventColumns(search: PerformanceEventSearch): TableColumnsType<PerformanceEvent> {
  return [
    {
      title: '状态 / 级别',
      width: 150,
      render: (_, event) => <Space><AlertStatus status={event.alert_status} /><PerformanceEventSeverityTag severity={event.severity} /></Space>,
    },
    { title: '事件类型', width: 170, render: (_, event) => performanceEventTypeLabel(event.event_type) },
    { title: '首次发生', width: 190, render: (_, event) => performanceEventTimeLabel(event.derived_at) },
    { title: '最近发生', width: 190, render: (_, event) => performanceEventTimeLabel(event.updated_at) },
    { title: '持续时间', width: 110, render: (_, event) => performanceEventDurationLabel(event.duration_ms) },
    { title: '触发指标', width: 220, dataIndex: 'metric_id' },
    { title: '触发值 / 阈值', width: 130, render: (_, event) => `${event.trigger_value} / ${event.threshold}` },
    { title: '处置', width: 100, render: (_, event) => performanceEventDispositionLabel(event.disposition) },
    { title: '原因摘要', width: 300, dataIndex: 'cause_summary' },
    { title: '建议动作', width: 300, dataIndex: 'suggested_action' },
    {
      title: '操作',
      fixed: 'right',
      width: 90,
      render: (_, event) => <Link
        to="/instances/$id/performance-events/$eventId"
        params={{ id: event.instance_id, eventId: event.id }}
        search={serializePerformanceEventSearch(search)}
      >详情</Link>,
    },
  ]
}

function eventEmptyText(search: PerformanceEventSearch): string {
  switch (search.tab) {
    case 'firing': return '所选时间范围内没有触发中的性能事件'
    case 'recovered': return '所选时间范围内没有已恢复的性能事件'
    case 'disposed':
      if (search.disposition === 'ACKED') return '所选时间范围内没有已确认的性能事件'
      return '所选时间范围内没有已忽略的性能事件'
    default: return assertNever(search.tab)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected performance event value: ${value}`)
}
