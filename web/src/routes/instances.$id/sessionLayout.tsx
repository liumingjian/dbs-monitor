import { DatabaseOutlined, SettingOutlined } from '@ant-design/icons'
import { Link } from '@tanstack/react-router'
import { Button, Space, Tabs, Typography } from 'antd'
import type { MetricID } from './metricOptions'
import { serializeSessionSearch, type SessionSearch } from './sessionSearch'

type SessionPage = 'current' | 'long-query-samples' | 'query-statistics'

export function SessionWorkbenchHeader({
  id,
  instanceName,
  search,
  page,
}: {
  id: string
  instanceName: string
  search: SessionSearch
  page: SessionPage
}) {
  return <>
    <Link to="/instances">← 返回实例列表</Link>
    <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>{instanceName}</Typography.Title>
        <Typography.Text type="secondary">实例工作台</Typography.Text>
      </div>
      <Space>
        <Link to="/instances/$id/collection" params={{ id }}><Button icon={<DatabaseOutlined />}>采集管理</Button></Link>
        <Link to="/instances/$id/settings" params={{ id }}><Button icon={<SettingOutlined />}>接入设置</Button></Link>
      </Space>
    </Space>
    <Tabs activeKey="sessions" items={[
      { key: 'overview', label: <Link to="/instances/$id" params={{ id }} search={timeRangeSearch(search)}>实例总览</Link> },
      { key: 'monitoring', label: <Link to="/instances/$id/monitoring" params={{ id }} search={timeRangeSearch(search)}>监控与报警</Link> },
      { key: 'sessions', label: '会话与阻塞' },
      { key: 'events', label: '性能事件', disabled: true },
      { key: 'alerts', label: <Link to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current', include_paused: false }}>告警</Link> },
    ]} />
    <Tabs activeKey={page} items={[
      { key: 'current', label: <Link to="/instances/$id/sessions" params={{ id }} search={search}>当前会话</Link> },
      { key: 'long-query-samples', label: <Link to="/instances/$id/sessions/long-query-samples" params={{ id }} search={search}>长查询采样记录</Link> },
      { key: 'query-statistics', label: <Link to="/instances/$id/sessions/query-statistics" params={{ id }} search={search}>查询统计排行</Link> },
    ]} />
  </>
}

export function sessionPageHref(id: string, search: SessionSearch): string {
  return pageHref(id, 'sessions', search)
}

export function longQuerySamplesPageHref(id: string, search: SessionSearch): string {
  return pageHref(id, 'sessions/long-query-samples', search)
}

export function queryStatisticsPageHref(id: string, search: SessionSearch): string {
  return pageHref(id, 'sessions/query-statistics', search)
}

function timeRangeSearch(search: SessionSearch): { from: string; to: string; metric?: MetricID } {
  return search.metric === undefined
    ? { from: search.from, to: search.to }
    : { from: search.from, to: search.to, metric: search.metric }
}

function pageHref(id: string, path: string, search: SessionSearch): string {
  const params = new URLSearchParams(serializeSessionSearch(search))
  return `/instances/${encodeURIComponent(id)}/${path}?${params.toString()}`
}
