import { DatabaseOutlined, SettingOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Select, Space, Typography } from 'antd'
import { useRef } from 'react'
import { $api } from '../../api/client'
import { Freshness } from '../../domain/Freshness'
import { MetricChart, metricUnavailability } from '../../domain/MetricChart'
import { rootRoute } from '../root'
import { defaultMetric, metricOption, metricOptions, type MetricID } from './metricOptions'
import { defaultTimeRange, parseTimeRange, type TimeRange } from './timeRange'

export const instanceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id',
  validateSearch: (search): TimeRange | { error: string } => parseTimeRange(search),
  component: InstancePage,
})

const instancePagePolling = { refetchInterval: 30_000 }

function InstancePage() {
  const { id } = instanceRoute.useParams()
  const search = instanceRoute.useSearch()
  const navigate = instanceRoute.useNavigate()
  const fromRef = useRef<HTMLInputElement>(null)
  const toRef = useRef<HTMLInputElement>(null)
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } }, instancePagePolling)

  if ('error' in search) return <Alert type="error" showIcon message={search.error} action={<Link to="/instances/$id" params={{ id }} search={defaultTimeRange()}><Button>使用最近一小时</Button></Link>} />

  const selectedMetric = search.metric ?? defaultMetric
  const selectedOption = metricOption(selectedMetric)
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: { path: { id }, query: { metric: [selectedMetric], from: search.from, to: search.to, step: 'auto' } },
  }, instancePagePolling)
  const metric = metrics.data?.metrics[0]
  const points = metric?.series.flatMap((series) => series.points) ?? []

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Link to="/instances">← 返回实例列表</Link>
    <Space style={{ width: '100%', justifyContent: 'space-between' }}>
      <Typography.Title level={2} style={{ margin: 0 }}>{instance.data?.name ?? '实例详情'}</Typography.Title>
      <Space>
        <Link to="/instances/$id/collection" params={{ id }}><Button icon={<DatabaseOutlined />}>采集管理</Button></Link>
        <Link to="/instances/$id/settings" params={{ id }}><Button icon={<SettingOutlined />}>接入设置</Button></Link>
      </Space>
    </Space>
    <Space wrap>
      <Select<MetricID>
        aria-label="指标"
        value={selectedMetric}
        options={metricOptions.map((option) => ({ value: option.id, label: `${option.label} (${option.id})` }))}
        onChange={(value) => void navigate({ search: { ...search, metric: value } })}
        style={{ minWidth: 280 }}
      />
      <input ref={fromRef} type="datetime-local" aria-label="开始时间" defaultValue={toLocalInput(search.from)} />
      <input ref={toRef} type="datetime-local" aria-label="结束时间" defaultValue={toLocalInput(search.to)} />
      <Button onClick={() => { if (fromRef.current?.value && toRef.current?.value) void navigate({ search: { ...search, from: new Date(fromRef.current.value).toISOString(), to: new Date(toRef.current.value).toISOString() } }) }}>应用时间范围</Button>
      <Link to="/instances/$id" params={{ id }} search={defaultTimeRange()}><Button>最近一小时</Button></Link>
      <Freshness dataUpdatedAt={metrics.dataUpdatedAt} collectionInterval={instancePagePolling.refetchInterval} />
    </Space>
    <Card title={selectedOption.label}>
      <MetricChart label={selectedOption.label} unit={metric?.unit ?? 'count'} points={points} step={metrics.data?.step ?? 'auto'} unavailability={metricUnavailability(metric)} loading={metrics.isFetching} />
    </Card>
  </Space>
}

function toLocalInput(value: string): string {
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 19)
}
