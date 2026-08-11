import { DatabaseOutlined, SettingOutlined } from '@ant-design/icons'
import { Button, Space, Tabs, Typography } from 'antd'
import type { SessionSearch } from './sessionSearch'

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
    <a href="/instances">← 返回实例列表</a>
    <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>{instanceName}</Typography.Title>
        <Typography.Text type="secondary">实例工作台</Typography.Text>
      </div>
      <Space>
        <Button icon={<DatabaseOutlined />} href={`/instances/${encodeURIComponent(id)}/collection`}>采集管理</Button>
        <Button icon={<SettingOutlined />} href={`/instances/${encodeURIComponent(id)}/settings`}>接入设置</Button>
      </Space>
    </Space>
    <Tabs activeKey="sessions" items={[
      { key: 'overview', label: '实例总览', disabled: true },
      { key: 'monitoring', label: <a href={monitoringHref(id, search)}>监控与报警</a> },
      { key: 'sessions', label: '会话与阻塞' },
      { key: 'events', label: '性能事件', disabled: true },
      { key: 'alerts', label: '告警', disabled: true },
      { key: 'collection', label: '采集管理', disabled: true },
    ]} />
    <Tabs activeKey={page} items={[
      { key: 'current', label: <a href={sessionPageHref(id, search)}>当前会话</a> },
      { key: 'long-query-samples', label: <a href={longQuerySamplesPageHref(id, search)}>长查询采样记录</a> },
      { key: 'query-statistics', label: <a href={queryStatisticsPageHref(id, search)}>查询统计排行</a> },
    ]} />
  </>
}

export function sessionPageHref(id: string, search: Pick<SessionSearch, 'from' | 'to'> & Partial<SessionSearch>): string {
  return pageHref(id, 'sessions', search)
}

export function longQuerySamplesPageHref(id: string, search: Pick<SessionSearch, 'from' | 'to'> & Partial<SessionSearch>): string {
  return pageHref(id, 'sessions/long-query-samples', search)
}

export function queryStatisticsPageHref(id: string, search: Pick<SessionSearch, 'from' | 'to'> & Partial<SessionSearch>): string {
  return pageHref(id, 'sessions/query-statistics', search)
}

function monitoringHref(id: string, search: SessionSearch): string {
  const params = new URLSearchParams({ from: search.from, to: search.to })
  if (search.metric !== undefined) params.set('metric', search.metric)
  return `/instances/${encodeURIComponent(id)}?${params.toString()}`
}

function pageHref(id: string, path: string, search: Pick<SessionSearch, 'from' | 'to'> & Partial<SessionSearch>): string {
  const params = new URLSearchParams({ from: search.from, to: search.to })
  if (search.metric !== undefined) params.set('metric', search.metric)
  if (search.sampled_at !== undefined) params.set('sampled_at', search.sampled_at)
  if (search.filter !== undefined) params.set('filter', search.filter)
  return `/instances/${encodeURIComponent(id)}/${path}?${params.toString()}`
}
