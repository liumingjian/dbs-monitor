import { Alert, Button, Space, Spin, Table, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { createRoute } from '@tanstack/react-router'
import { $api } from '../../api/client'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'
import { queryStatisticsView } from './queryStatistics'
import { queryStatisticsPageHref, SessionWorkbenchHeader } from './sessionLayout'
import { parseSessionSearch, type SessionSearch } from './sessionSearch'

type QueryStatisticsEntry = components['schemas']['QueryStatisticsEntry']

export const queryStatisticsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions/query-statistics',
  validateSearch: (search): SessionSearch | { error: string } => parseSessionSearch(search),
  component: QueryStatisticsPage,
})

const pollingOptions = { refetchInterval: pollingIntervals.sessions }

function QueryStatisticsPage() {
  const { id } = queryStatisticsRoute.useParams()
  const search = queryStatisticsRoute.useSearch()
  if ('error' in search) {
    return <Alert type="error" showIcon title={search.error} action={
      <Button href={queryStatisticsPageHref(id, defaultTimeRange())}>使用最近一小时</Button>
    } />
  }
  return <QueryStatisticsRanking id={id} search={search} />
}

function QueryStatisticsRanking({ id, search }: { id: string; search: SessionSearch }) {
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } }, pollingOptions)
  const statistics = $api.useQuery('get', '/api/v1/instances/{id}/query-stats', { params: { path: { id } } }, pollingOptions)

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    <SessionWorkbenchHeader id={id} instanceName={instance.data?.name ?? '实例工作台'} search={search} page="query-statistics" />
    <Alert
      type="info"
      showIcon
      title="标识具有时效性"
      description="queryid 可能因统计重置、条目淘汰或 PostgreSQL 版本变化而失效，仅作为数据库侧排查线索。"
    />
    {statistics.isPending ? <Spin size="large" /> : statistics.data ? <QueryStatisticsContent
      response={statistics.data}
      dataUpdatedAt={statistics.dataUpdatedAt}
    /> : <Alert type="error" showIcon title="无法加载查询统计" />}
  </Space>
}

function QueryStatisticsContent({ response, dataUpdatedAt }: {
  response: components['schemas']['QueryStatisticsSnapshot']
  dataUpdatedAt: number
}) {
  const view = queryStatisticsView(response)
  if (view.kind === 'unavailable') {
    return <Alert type="warning" showIcon title={view.title} description={view.description} />
  }
  return <>
    <Space className="snapshot-meta" wrap>
      <Typography.Text>统计时间截至：{formatTime(view.sampledAt)}</Typography.Text>
      {dataUpdatedAt > 0 && <Freshness dataUpdatedAt={dataUpdatedAt} collectionInterval={pollingIntervals.sessions} />}
    </Space>
    <Table
      size="small"
      rowKey={(item) => `${item.queryid}-${item.database_oid}-${item.user_oid}`}
      pagination={false}
      dataSource={view.items}
      columns={queryStatisticsColumns}
      scroll={{ x: 760 }}
    />
  </>
}

export const queryStatisticsTableFields = [
  'queryid', 'database_oid', 'user_oid', 'calls', 'total_exec_time_ms',
] as const satisfies readonly (keyof QueryStatisticsEntry)[]

const queryStatisticsColumns: ColumnsType<QueryStatisticsEntry> = [
  { title: 'queryid', dataIndex: 'queryid', fixed: 'left', width: 220, render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
  { title: '数据库 OID', dataIndex: 'database_oid', width: 150, render: (value: number) => <Typography.Text copyable>{value}</Typography.Text> },
  { title: '数据库用户 OID', dataIndex: 'user_oid', width: 170, render: (value: number) => <Typography.Text copyable>{value}</Typography.Text> },
  { title: '调用次数', dataIndex: 'calls', width: 120 },
  { title: '总执行时间', dataIndex: 'total_exec_time_ms', width: 150, render: (value: number) => `${value.toFixed(1)} ms` },
]

function formatTime(value: string | undefined) {
  return value === undefined ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
}
