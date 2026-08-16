import { Alert, Button, Space, Spin, Table, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { $api } from '../../api/client'
import type { components } from '../../api/schema'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'
import { longQuerySamplesPageHref, SessionWorkbenchHeader } from './sessionLayout'
import { parseSessionSearch, type SessionSearch } from './sessionSearch'

type LongQuerySample = components['schemas']['LongQuerySample']

export const longQuerySamplesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions/long-query-samples',
  validateSearch: (search): SessionSearch | { error: string } => parseSessionSearch(search),
  component: LongQuerySamplesPage,
})

function LongQuerySamplesPage() {
  const { id } = longQuerySamplesRoute.useParams()
  const search = longQuerySamplesRoute.useSearch()
  if ('error' in search) {
    return <Alert type="error" showIcon title={search.error} action={
      <Button href={longQuerySamplesPageHref(id, defaultTimeRange())}>使用最近一小时</Button>
    } />
  }
  return <LongQuerySamples id={id} search={search} />
}

function LongQuerySamples({ id, search }: { id: string; search: SessionSearch }) {
  const navigate = longQuerySamplesRoute.useNavigate()
  const [offset, setOffset] = useState(0)
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const samples = $api.useQuery('get', '/api/v1/instances/{id}/long-query-samples', {
    params: { path: { id }, query: { from: search.from, to: search.to, limit: 50, offset, sort: '-sampled_at' } },
  })

  function updateRange(range: Pick<SessionSearch, 'from' | 'to'>) {
    setOffset(0)
    void navigate({ search: { ...search, ...range } })
  }

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    <SessionWorkbenchHeader id={id} instanceName={instance.data?.name ?? '实例工作台'} search={search} page="long-query-samples" />
    <Space className="snapshot-meta" wrap>
      <TimeRangePicker from={search.from} to={search.to} onChange={updateRange} />
      {search.sampled_at && <Typography.Text type="secondary">下钻采样时间：{formatTime(search.sampled_at)}</Typography.Text>}
    </Space>
    <Alert
      type="info"
      showIcon
      title="标识具有时效性"
      description="PID 可能因查询结束或后端复用而失效，仅作为数据库侧排查线索。"
    />
    {samples.isPending ? <Spin size="large" /> : <Table
      size="small"
      rowKey={(item) => `${item.sampled_at}-${item.pid}`}
      dataSource={samples.data?.items ?? []}
      columns={longQueryColumns}
      scroll={{ x: 1320 }}
      pagination={{
        current: Math.floor(offset / 50) + 1,
        pageSize: 50,
        total: samples.data?.total,
        showSizeChanger: false,
        onChange: (page) => setOffset((page - 1) * 50),
      }}
    />}
  </Space>
}

const longQueryColumns: ColumnsType<LongQuerySample> = [
  { title: 'PID', dataIndex: 'pid', fixed: 'left', width: 90, render: (value: number) => <Typography.Text copyable>{value}</Typography.Text> },
  { title: '采样时间', dataIndex: 'sampled_at', width: 190, render: formatTime },
  { title: '查询开始时间', dataIndex: 'query_started_at', width: 190, render: formatTime },
  { title: '数据库', dataIndex: 'database_name', width: 140, render: copyableValue },
  { title: '数据库用户', dataIndex: 'username', width: 140, render: copyableValue },
  { title: '状态', dataIndex: 'state', width: 130, render: optionalValue },
  { title: '查询持续时间', dataIndex: 'query_duration_ms', width: 150, render: formatDuration },
  { title: '等待事件', width: 190, render: (_, item) => [item.wait_event_type, item.wait_event].filter(Boolean).join(' / ') || '—' },
  { title: '阻塞源 PID', dataIndex: 'blocking_pids', width: 150, render: (values: number[]) => values.length > 0 ? values.join(', ') : '—' },
]

function copyableValue(value: string | undefined) {
  return value === undefined ? '—' : <Typography.Text copyable>{value}</Typography.Text>
}

function optionalValue(value: string | undefined) {
  return value ?? '—'
}

function formatTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(value: number | undefined) {
  return value === undefined ? '—' : `${(value / 1000).toFixed(1)} s`
}
