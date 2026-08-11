import { Alert, Button, Empty, Space, Spin, Table, Tabs, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import { Freshness } from '../../domain/Freshness'
import { UnavailabilityBlock } from '../../domain/UnavailabilityBlock'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'
import { sessionPageHref, SessionWorkbenchHeader } from './sessionLayout'
import { parseSessionSearch, type SessionFilter, type SessionSearch } from './sessionSearch'
import { groupSessionSnapshot, type SessionSnapshotEntry } from './sessionViews'

export const sessionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions',
  validateSearch: (search): SessionSearch | { error: string } => parseSessionSearch(search),
  component: SessionsPage,
})

const pollingOptions = { refetchInterval: pollingIntervals.sessions }

function SessionsPage() {
  const { id } = sessionsRoute.useParams()
  const search = sessionsRoute.useSearch()
  if ('error' in search) {
    return <Alert type="error" showIcon title={search.error} action={
      <Button href={sessionPageHref(id, defaultTimeRange())}>使用最近一小时</Button>
    } />
  }

  return <SessionSnapshotPage id={id} search={search} />
}

function SessionSnapshotPage({ id, search }: { id: string; search: SessionSearch }) {
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } }, pollingOptions)
  const snapshot = $api.useQuery('get', '/api/v1/instances/{id}/sessions', { params: { path: { id } } }, pollingOptions)
  const [activeView, setActiveView] = useState<SessionView>(initialView(search.filter))
  let snapshotContent = <Alert type="error" showIcon title="无法加载会话快照" />
  if (snapshot.isPending) {
    snapshotContent = <Spin size="large" />
  } else if (snapshot.data?.unavailability !== undefined) {
    snapshotContent = <UnavailabilityBlock
      code={snapshot.data.unavailability}
      href={`/instances/${encodeURIComponent(id)}/collection`}
    />
  } else if (snapshot.data !== undefined) {
    snapshotContent = <>
      <SessionSnapshotMeta
        sampledAt={snapshot.data.sampled_at}
        dataUpdatedAt={snapshot.dataUpdatedAt}
        originalCount={snapshot.data.original_count}
        itemCount={snapshot.data.items.length}
      />
      {snapshot.data.truncated && <Alert
        type="warning"
        showIcon
        title="快照已截断"
        description="本次响应达到 500 行服务端上限，阻塞链与会话列表可能不完整。"
      />}
      <SessionViews items={snapshot.data.items} activeView={activeView} onChange={setActiveView} />
    </>
  }

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    <SessionWorkbenchHeader id={id} instanceName={instance.data?.name ?? '实例工作台'} search={search} page="current" />
    {snapshotContent}
  </Space>
}

export function SessionSnapshotMeta({ sampledAt, dataUpdatedAt, originalCount, itemCount }: {
  sampledAt: string | undefined
  dataUpdatedAt: number
  originalCount: number | undefined
  itemCount: number
}) {
  return <Space className="snapshot-meta" wrap>
    <Typography.Text>采集时间：{formatTime(sampledAt)}</Typography.Text>
    {dataUpdatedAt > 0 && <Freshness dataUpdatedAt={dataUpdatedAt} collectionInterval={pollingIntervals.sessions} />}
    <Typography.Text type="secondary">原始会话数：{originalCount ?? itemCount}</Typography.Text>
  </Space>
}

type SessionView = 'active' | 'long-transactions' | 'lock-waits' | 'blocking-chains' | 'details'

function SessionViews({ items, activeView, onChange }: {
  items: SessionSnapshotEntry[]
  activeView: SessionView
  onChange: (view: SessionView) => void
}) {
  const groups = groupSessionSnapshot(items)
  return <Tabs activeKey={activeView} onChange={(key) => {
    if (isSessionView(key)) onChange(key)
  }} items={[
    { key: 'active', label: `活跃会话 ${groups.active.length}`, children: <SessionTable items={groups.active} empty="当前快照无活跃会话" /> },
    { key: 'long-transactions', label: `长事务 ${groups.longTransactions.length}`, children: <SessionTable items={groups.longTransactions} empty="当前快照无长事务" /> },
    { key: 'lock-waits', label: `锁等待 ${groups.lockWaits.length}`, children: <SessionTable items={groups.lockWaits} empty="当前快照无锁等待" /> },
    { key: 'blocking-chains', label: `阻塞链 ${groups.blockingChains.length}`, children: <SessionTable items={groups.blockingChains} empty="当前快照无阻塞链" /> },
    { key: 'details', label: `会话详情 ${groups.details.length}`, children: <SessionTable items={groups.details} empty="当前快照无会话" details /> },
  ]} />
}

function SessionTable({ items, empty, details = false }: { items: SessionSnapshotEntry[]; empty: string; details?: boolean }) {
  return <Table
    size="small"
    rowKey="pid"
    pagination={false}
    dataSource={items}
    columns={details ? sessionDetailColumns : sessionColumns}
    scroll={{ x: details ? 1360 : 900 }}
    locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={empty} /> }}
  />
}

const sessionColumns: ColumnsType<SessionSnapshotEntry> = [
  { title: 'PID', dataIndex: 'pid', fixed: 'left', width: 90, render: (value: number) => <Typography.Text copyable>{value}</Typography.Text> },
  { title: '数据库', dataIndex: 'database_name', width: 140, render: copyableValue },
  { title: '数据库用户', dataIndex: 'username', width: 140, render: copyableValue },
  { title: '状态', dataIndex: 'state', width: 140, render: optionalValue },
  { title: '事务持续时间', dataIndex: 'transaction_duration_ms', width: 150, render: formatDuration },
  { title: '等待事件', width: 190, render: (_, item) => waitEvent(item) },
  { title: '阻塞源 PID', dataIndex: 'blocking_pids', width: 160, render: (values: number[]) => values.length > 0 ? values.map(String).join(', ') : '—' },
]

const sessionDetailColumns: ColumnsType<SessionSnapshotEntry> = [
  ...sessionColumns,
  { title: '客户端地址', dataIndex: 'client_address', width: 150, render: copyableValue },
  { title: '查询开始时间', dataIndex: 'query_started_at', width: 190, render: formatTime },
  { title: '事务开始时间', dataIndex: 'transaction_started_at', width: 190, render: formatTime },
  { title: '查询持续时间', dataIndex: 'query_duration_ms', width: 150, render: formatDuration },
]

function initialView(filter: SessionFilter | undefined): SessionView {
  switch (filter) {
    case 'active': return 'active'
    case 'long_transaction': return 'long-transactions'
    case 'lock_wait': return 'lock-waits'
    case 'blocked': return 'blocking-chains'
    case undefined: return 'active'
    default: return assertNever(filter)
  }
}

function isSessionView(value: string): value is SessionView {
  switch (value) {
    case 'active':
    case 'long-transactions':
    case 'lock-waits':
    case 'blocking-chains':
    case 'details':
      return true
    default:
      return false
  }
}

function copyableValue(value: string | undefined) {
  return value === undefined ? '—' : <Typography.Text copyable>{value}</Typography.Text>
}

function optionalValue(value: string | undefined) {
  return value ?? '—'
}

function waitEvent(item: SessionSnapshotEntry) {
  const value = [item.wait_event_type, item.wait_event].filter(Boolean).join(' / ')
  return value || '—'
}

function formatTime(value: string | undefined) {
  return value === undefined ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(value: number | undefined) {
  if (value === undefined) return '—'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(1)} s`
}

function assertNever(value: never): never {
  throw new Error(`unhandled session filter: ${value}`)
}
