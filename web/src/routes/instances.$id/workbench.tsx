import { DatabaseOutlined, SettingOutlined } from '@ant-design/icons'
import { Link } from '@tanstack/react-router'
import { Button, Space, Tabs, Typography } from 'antd'
import type { MonitoringSearch } from './timeRange'

export function WorkbenchHeader({ id, instanceName, activeKey, search }: {
  id: string
  instanceName: string | undefined
  activeKey: 'overview' | 'monitoring'
  search: MonitoringSearch
}) {
  return <>
    <Link to="/instances">← 返回实例列表</Link>
    <Space className="workbench-heading" wrap>
      <div>
        <Typography.Title level={2} style={{ margin: 0 }}>{instanceName ?? '实例工作台'}</Typography.Title>
        <Typography.Text type="secondary">实例工作台</Typography.Text>
      </div>
      <Space>
        <Link to="/instances/$id/collection" params={{ id }}><Button icon={<DatabaseOutlined />}>采集管理</Button></Link>
        <Link to="/instances/$id/settings" params={{ id }}><Button icon={<SettingOutlined />}>接入设置</Button></Link>
      </Space>
    </Space>
    <Tabs activeKey={activeKey} items={[
      {
        key: 'overview',
        label: <Link to="/instances/$id" params={{ id }} search={search}>实例总览</Link>,
      },
      {
        key: 'monitoring',
        label: <Link to="/instances/$id/monitoring" params={{ id }} search={search}>监控与报警</Link>,
      },
      { key: 'sessions', label: '会话与阻塞', disabled: true },
      { key: 'events', label: '性能事件', disabled: true },
      { key: 'alerts', label: '告警', disabled: true },
      { key: 'collection', label: '采集管理', disabled: true },
    ]} />
  </>
}
