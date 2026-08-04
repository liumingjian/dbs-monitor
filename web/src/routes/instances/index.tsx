import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Card, Form, Input, InputNumber, Space, Table, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'

export const instancesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances',
  component: InstancesPage,
})

function InstancesPage() {
  const list = $api.useQuery('get', '/api/v1/instances', {}, { refetchInterval: 30_000 })
  const create = $api.useMutation('post', '/api/v1/instances')
  const [agentToken, setAgentToken] = useState('')

  async function createInstance(values: { name: string; host: string; port: number; database: string; username: string; password: string }) {
    create.mutate({ body: values }, { onSuccess: (result) => { setAgentToken(result.agent_token); void list.refetch() } })
  }

  return <Space direction="vertical" size="large" style={{ width: '100%' }}><Typography.Title level={2}>PostgreSQL 实例</Typography.Title>{agentToken && <Alert type="success" message="Agent 令牌仅显示一次" description={agentToken} />}<Card title="接入实例"><Form layout="inline" onFinish={createInstance}><Form.Item name="name" rules={[{ required: true }]}><Input placeholder="名称" /></Form.Item><Form.Item name="host" rules={[{ required: true }]}><Input placeholder="主机" /></Form.Item><Form.Item name="port" initialValue={5432} rules={[{ required: true }]}><InputNumber min={1} max={65535} /></Form.Item><Form.Item name="database" rules={[{ required: true }]}><Input placeholder="数据库" /></Form.Item><Form.Item name="username" rules={[{ required: true }]}><Input placeholder="用户" /></Form.Item><Form.Item name="password" rules={[{ required: true }]}><Input.Password placeholder="密码" autoComplete="new-password" /></Form.Item><Button type="primary" htmlType="submit" loading={create.isPending}>创建</Button></Form></Card><Table loading={list.isPending} rowKey="id" dataSource={list.data ?? []} columns={[{ title: '名称', dataIndex: 'name' }, { title: '地址', render: (_, row) => `${row.host}:${row.port}` }, { title: '告警状态', dataIndex: 'alert_status' }, { title: '操作', render: (_, row) => <Link to="/instances/$id" params={{ id: row.id }} search={defaultTimeRange()}>查看监控</Link> }]} /></Space>
}
