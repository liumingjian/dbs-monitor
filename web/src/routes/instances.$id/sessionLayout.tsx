import { DatabaseOutlined, SettingOutlined } from '@ant-design/icons'
import { Button, Space, Tabs, Typography } from 'antd'
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
      { key: 'overview', label: <a href={`/instances/${encodeURIComponent(id)}?${timeRangeParams(search)}`}>实例总览</a> },
      { key: 'monitoring', label: <a href={monitoringHref(id, search)}>监控与报警</a> },
      { key: 'sessions', label: '会话与阻塞' },
      { key: 'events', label: '性能事件', disabled: true },
      { key: 'alerts', label: <a href={`/instances/${encodeURIComponent(id)}/alerts?tab=current&include_paused=false`}>告警</a> },
      { key: 'collection', label: '采集管理', disabled: true },
    ]} />
    <Tabs activeKey={page} items={[
      { key: 'current', label: <a href={sessionPageHref(id, search)}>当前会话</a> },
      { key: 'long-query-samples', label: <a href={longQuerySamplesPageHref(id, search)}>长查询采样记录</a> },
      { key: 'query-statistics', label: <a href={queryStatisticsPageHref(id, search)}>查询统计排行</a> },
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

function monitoringHref(id: string, search: SessionSearch): string {
  return `/instances/${encodeURIComponent(id)}/monitoring?${timeRangeParams(search)}`
}

function timeRangeParams(search: SessionSearch): string {
  const params = new URLSearchParams({ from: search.from, to: search.to })
  if (search.metric !== undefined) params.set('metric', search.metric)
  return params.toString()
}

function pageHref(id: string, path: string, search: SessionSearch): string {
  const params = new URLSearchParams(serializeSessionSearch(search))
  return `/instances/${encodeURIComponent(id)}/${path}?${params.toString()}`
}
