import { DashboardOutlined, PlusOutlined, SettingOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Form, Input, InputNumber, Modal, Space, Table, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'

type InstanceCreateInput = components['schemas']['InstanceCreateInput']

export const instancesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances',
  component: InstancesPage,
})

function InstancesPage() {
  const list = $api.useQuery('get', '/api/v1/instances', {}, { refetchInterval: 30_000 })
  const currentUser = $api.useQuery('get', '/api/v1/me')
  const create = $api.useMutation('post', '/api/v1/instances')
  const [createOpen, setCreateOpen] = useState(false)
  const [error, setError] = useState('')
  const [form] = Form.useForm<InstanceCreateInput>()
  const canCreate = currentUser.data?.role === 'PLATFORM_ADMIN'

  function createInstance(values: InstanceCreateInput) {
    setError('')
    create.mutate({ body: values }, {
      onSuccess: () => {
        setCreateOpen(false)
        form.resetFields()
        void list.refetch()
      },
      onError: (failure) => setError(apiErrorMessage(failure, '创建实例失败，请检查连接信息')),
    })
  }

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={2} style={{ margin: 0 }}>PostgreSQL 实例</Typography.Title>
        <Tooltip title={canCreate ? undefined : '需要平台管理员角色'}>
          <span>
            <Button type="primary" icon={<PlusOutlined />} disabled={!canCreate} onClick={() => setCreateOpen(true)}>
              新建实例
            </Button>
          </span>
        </Tooltip>
      </Space>
      {error && <Alert type="error" title={error} closable onClose={() => setError('')} />}
      <Table
        loading={list.isPending}
        rowKey="id"
        dataSource={list.data ?? []}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '地址', render: (_, row) => `${row.host}:${row.port}` },
          { title: '告警状态', dataIndex: 'alert_status' },
          {
            title: '操作',
            render: (_, row) => (
              <Space wrap>
                <Link to="/instances/$id" params={{ id: row.id }} search={defaultTimeRange()}>
                  <DashboardOutlined /> 监控
                </Link>
                <Link to="/instances/$id/settings" params={{ id: row.id }}>
                  <SettingOutlined /> 接入设置
                </Link>
              </Space>
            ),
          },
        ]}
      />
      <Modal title="新建实例" open={createOpen} footer={null} destroyOnHidden onCancel={() => setCreateOpen(false)}>
        <Form<InstanceCreateInput> form={form} layout="vertical" onFinish={createInstance}>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="host" label="主机" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="port" label="端口" initialValue={5432} rules={[{ required: true }]}><InputNumber min={1} max={65535} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="database" label="数据库" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true, whitespace: true }]}><Input autoComplete="off" /></Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}><Input type="password" autoComplete="new-password" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={create.isPending}>连接测试并创建</Button>
        </Form>
      </Modal>
    </Space>
  )
}
