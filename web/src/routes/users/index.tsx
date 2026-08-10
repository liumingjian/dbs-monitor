import { CopyOutlined, PlusOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import { Alert, Button, Form, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import type { components } from '../../api/schema'
import { $api } from '../../api/client'
import { rootRoute } from '../root'

type Role = components['schemas']['Role']
type User = components['schemas']['User']
type IssuedPassword = { title: string; password: string } | null

const roleLabels: Record<Role, string> = {
  READONLY: '只读运维',
  ALERT_ADMIN: '告警管理员',
  PLATFORM_ADMIN: '平台管理员',
}

const roleOptions = (Object.entries(roleLabels) as [Role, string][]).map(([value, label]) => ({ value, label }))

export const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/users',
  component: UsersPage,
})

function UsersPage() {
  const list = $api.useQuery('get', '/api/v1/users')
  const me = $api.useQuery('get', '/api/v1/me')
  const create = $api.useMutation('post', '/api/v1/users')
  const updateStatus = $api.useMutation('put', '/api/v1/users/{id}/status')
  const updateRole = $api.useMutation('put', '/api/v1/users/{id}/role')
  const resetPassword = $api.useMutation('post', '/api/v1/users/{id}/password')
  const [createOpen, setCreateOpen] = useState(false)
  const [roleTarget, setRoleTarget] = useState<User | null>(null)
  const [issued, setIssued] = useState<IssuedPassword>(null)
  const [actionError, setActionError] = useState('')
  const canManage = me.data?.role === 'PLATFORM_ADMIN'

  function refresh() {
    setActionError('')
    void list.refetch()
  }

  function reportError(error: unknown) {
    setActionError(apiErrorMessage(error))
  }

  function createUser(values: { username: string; role: Role }) {
    create.mutate({ body: values }, {
      onSuccess: (result) => {
        setCreateOpen(false)
        setIssued({ title: `${result.user.username} 的初始口令`, password: result.initial_password })
        refresh()
      },
      onError: reportError,
    })
  }

  function changeStatus(user: User) {
    updateStatus.mutate({ params: { path: { id: user.id } }, body: { enabled: !user.enabled } }, {
      onSuccess: refresh,
      onError: reportError,
    })
  }

  function changeRole(values: { role: Role }) {
    if (!roleTarget) return
    updateRole.mutate({ params: { path: { id: roleTarget.id } }, body: values }, {
      onSuccess: () => { setRoleTarget(null); refresh() },
      onError: reportError,
    })
  }

  function reset(user: User) {
    resetPassword.mutate({ params: { path: { id: user.id } } }, {
      onSuccess: (result) => setIssued({ title: `${user.username} 的重置口令`, password: result.password }),
      onError: reportError,
    })
  }

  const requiredRole = canManage ? undefined : '需要平台管理员角色'

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Space style={{ width: '100%', justifyContent: 'space-between' }}>
      <Typography.Title level={2} style={{ margin: 0 }}>用户管理</Typography.Title>
      <Tooltip title={requiredRole}><span><Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={() => setCreateOpen(true)}>创建用户</Button></span></Tooltip>
    </Space>
    {actionError && <Alert type="error" title={actionError} closable onClose={() => setActionError('')} />}
    <Table<User>
      loading={list.isPending}
      rowKey="id"
      dataSource={list.data ?? []}
      scroll={{ x: 900 }}
      columns={[
        { title: '用户名', dataIndex: 'username' },
        { title: '角色', dataIndex: 'role', render: (role: Role) => roleLabels[role] },
        { title: '状态', dataIndex: 'enabled', render: (enabled: boolean) => enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag> },
        { title: '创建时间', dataIndex: 'created_at', render: (value: string) => new Date(value).toLocaleString() },
        {
          title: '操作',
          render: (_, user) => {
            const self = me.data?.id === user.id
            const statusReason = requiredRole ?? (self && user.enabled ? '不能停用自己' : undefined)
            const resetReason = requiredRole ?? (self ? '请从顶栏修改自己的口令' : undefined)
            return <Space wrap>
              <Tooltip title={statusReason}><span><Button size="small" disabled={!canManage || (self && user.enabled)} onClick={() => changeStatus(user)}>{user.enabled ? '停用' : '启用'}</Button></span></Tooltip>
              <Tooltip title={requiredRole}><span><Button size="small" disabled={!canManage} onClick={() => setRoleTarget(user)}>变更角色</Button></span></Tooltip>
              <Tooltip title={resetReason}><span><Button size="small" disabled={!canManage || self} loading={resetPassword.isPending} onClick={() => reset(user)}>重置口令</Button></span></Tooltip>
            </Space>
          },
        },
      ]}
    />
    <Modal title="创建用户" open={createOpen} footer={null} destroyOnHidden onCancel={() => setCreateOpen(false)}>
      <Form layout="vertical" onFinish={createUser}>
        <Form.Item name="username" label="用户名" rules={[{ required: true, whitespace: true }]}><Input autoComplete="off" /></Form.Item>
        <Form.Item name="role" label="角色" initialValue="READONLY" rules={[{ required: true }]}><Select options={roleOptions} /></Form.Item>
        <Button type="primary" htmlType="submit" loading={create.isPending}>创建</Button>
      </Form>
    </Modal>
    <Modal title={`变更 ${roleTarget?.username ?? ''} 的角色`} open={roleTarget !== null} footer={null} destroyOnHidden onCancel={() => setRoleTarget(null)}>
      <Form layout="vertical" onFinish={changeRole} initialValues={{ role: roleTarget?.role }}>
        <Form.Item name="role" label="角色" rules={[{ required: true }]}><Select options={roleOptions} /></Form.Item>
        <Button type="primary" htmlType="submit" loading={updateRole.isPending}>保存</Button>
      </Form>
    </Modal>
    <OneTimePasswordModal issued={issued} onClose={() => setIssued(null)} />
  </Space>
}

export function OneTimePasswordModal({ issued, onClose }: { issued: IssuedPassword; onClose: () => void }) {
  return <Modal title={issued?.title} open={issued !== null} onCancel={onClose} onOk={onClose} okText="关闭" cancelButtonProps={{ style: { display: 'none' } }} destroyOnHidden>
    <Alert type="warning" showIcon title="口令仅显示一次，关闭后不再显示" />
    <Space.Compact style={{ width: '100%', marginTop: 16 }}><Input value={issued?.password ?? ''} readOnly /><Tooltip title="复制口令"><Button aria-label="复制口令" icon={<CopyOutlined />} onClick={() => { if (issued) void navigator.clipboard.writeText(issued.password) }} /></Tooltip></Space.Compact>
  </Modal>
}

function apiErrorMessage(error: unknown): string {
  if (typeof error === 'object' && error !== null && 'error' in error) {
    const detail = (error as { error?: { message?: unknown } }).error
    if (typeof detail?.message === 'string') return detail.message
  }
  return '操作失败，请重试'
}
