import { CopyOutlined, PlusOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import { Alert, Button, Form, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'

type Role = components['schemas']['Role']
type User = components['schemas']['User']
type UserCreateInput = components['schemas']['UserCreateInput']
type UserRoleInput = components['schemas']['UserRoleInput']
type IssuedPassword = { title: string; password: string }

const roles = ['READONLY', 'ALERT_ADMIN', 'PLATFORM_ADMIN'] as const satisfies readonly Role[]

const roleOptions = roles.map((role) => ({ value: role, label: roleLabel(role) }))

export const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/users',
  component: UsersPage,
})

function UsersPage() {
  const usersQuery = $api.useQuery('get', '/api/v1/users')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const createUserMutation = $api.useMutation('post', '/api/v1/users')
  const updateStatusMutation = $api.useMutation('put', '/api/v1/users/{id}/status')
  const updateRoleMutation = $api.useMutation('put', '/api/v1/users/{id}/role')
  const resetPasswordMutation = $api.useMutation('post', '/api/v1/users/{id}/password')
  const [createOpen, setCreateOpen] = useState(false)
  const [roleTarget, setRoleTarget] = useState<User | null>(null)
  const [issuedPassword, setIssuedPassword] = useState<IssuedPassword | null>(null)
  const [actionError, setActionError] = useState('')
  const canManageUsers = currentUserQuery.data?.role === 'PLATFORM_ADMIN'

  function refreshUsers() {
    setActionError('')
    void usersQuery.refetch()
  }

  function reportError(error: unknown) {
    setActionError(apiErrorMessage(error, '操作失败，请重试'))
  }

  function createUser(values: UserCreateInput) {
    createUserMutation.mutate({ body: values }, {
      onSuccess: (result) => {
        setCreateOpen(false)
        setIssuedPassword({ title: `${result.user.username} 的初始口令`, password: result.initial_password })
        refreshUsers()
      },
      onError: reportError,
    })
  }

  function toggleUserStatus(user: User) {
    updateStatusMutation.mutate({ params: { path: { id: user.id } }, body: { enabled: !user.enabled } }, {
      onSuccess: refreshUsers,
      onError: reportError,
    })
  }

  function updateSelectedUserRole(values: UserRoleInput) {
    if (!roleTarget) return
    updateRoleMutation.mutate({ params: { path: { id: roleTarget.id } }, body: values }, {
      onSuccess: () => {
        setRoleTarget(null)
        refreshUsers()
      },
      onError: reportError,
    })
  }

  function resetUserPassword(user: User) {
    resetPasswordMutation.mutate({ params: { path: { id: user.id } } }, {
      onSuccess: (result) => setIssuedPassword({ title: `${user.username} 的重置口令`, password: result.password }),
      onError: reportError,
    })
  }

  const manageDisabledReason = canManageUsers ? undefined : '需要平台管理员角色'

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={2} style={{ margin: 0 }}>用户管理</Typography.Title>
        <Tooltip title={manageDisabledReason}>
          <span>
            <Button type="primary" icon={<PlusOutlined />} disabled={!canManageUsers} onClick={() => setCreateOpen(true)}>
              创建用户
            </Button>
          </span>
        </Tooltip>
      </Space>
      {actionError && <Alert type="error" title={actionError} closable onClose={() => setActionError('')} />}
      <Table<User>
        loading={usersQuery.isPending}
        rowKey="id"
        dataSource={usersQuery.data ?? []}
        scroll={{ x: 900 }}
        columns={[
          { title: '用户名', dataIndex: 'username' },
          { title: '角色', dataIndex: 'role', render: (role: Role) => roleLabel(role) },
          { title: '状态', dataIndex: 'enabled', render: (enabled: boolean) => enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag> },
          { title: '创建时间', dataIndex: 'created_at', render: (value: string) => new Date(value).toLocaleString() },
          {
            title: '操作',
            render: (_, user) => {
              const isCurrentUser = currentUserQuery.data?.id === user.id
              let statusDisabledReason = manageDisabledReason
              if (!statusDisabledReason && isCurrentUser && user.enabled) {
                statusDisabledReason = '不能停用自己'
              }
              let resetDisabledReason = manageDisabledReason
              if (!resetDisabledReason && isCurrentUser) {
                resetDisabledReason = '请从顶栏修改自己的口令'
              }

              return (
                <Space wrap>
                  <Tooltip title={statusDisabledReason}>
                    <span>
                      <Button size="small" disabled={!canManageUsers || (isCurrentUser && user.enabled)} onClick={() => toggleUserStatus(user)}>
                        {user.enabled ? '停用' : '启用'}
                      </Button>
                    </span>
                  </Tooltip>
                  <Tooltip title={manageDisabledReason}>
                    <span>
                      <Button size="small" disabled={!canManageUsers} onClick={() => setRoleTarget(user)}>变更角色</Button>
                    </span>
                  </Tooltip>
                  <Tooltip title={resetDisabledReason}>
                    <span>
                      <Button
                        size="small"
                        disabled={!canManageUsers || isCurrentUser}
                        loading={resetPasswordMutation.isPending}
                        onClick={() => resetUserPassword(user)}
                      >
                        重置口令
                      </Button>
                    </span>
                  </Tooltip>
                </Space>
              )
            },
          },
        ]}
      />
      <Modal title="创建用户" open={createOpen} footer={null} destroyOnHidden onCancel={() => setCreateOpen(false)}>
        <Form layout="vertical" onFinish={createUser}>
          <Form.Item name="username" label="用户名" rules={[{ required: true, whitespace: true }]}>
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item name="role" label="角色" initialValue="READONLY" rules={[{ required: true }]}>
            <Select options={roleOptions} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={createUserMutation.isPending}>创建</Button>
        </Form>
      </Modal>
      <Modal title={`变更 ${roleTarget?.username ?? ''} 的角色`} open={roleTarget !== null} footer={null} destroyOnHidden onCancel={() => setRoleTarget(null)}>
        <Form layout="vertical" onFinish={updateSelectedUserRole} initialValues={{ role: roleTarget?.role }}>
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={roleOptions} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={updateRoleMutation.isPending}>保存</Button>
        </Form>
      </Modal>
      <OneTimePasswordModal issued={issuedPassword} onClose={() => setIssuedPassword(null)} />
    </Space>
  )
}

export function OneTimePasswordModal({ issued, onClose }: { issued: IssuedPassword | null; onClose: () => void }) {
  function copyPassword() {
    if (issued) {
      void navigator.clipboard.writeText(issued.password)
    }
  }

  return (
    <Modal
      title={issued?.title}
      open={issued !== null}
      onCancel={onClose}
      onOk={onClose}
      okText="关闭"
      cancelButtonProps={{ style: { display: 'none' } }}
      destroyOnHidden
    >
      <Alert type="warning" showIcon title="口令仅显示一次，关闭后不再显示" />
      <Space.Compact style={{ width: '100%', marginTop: 16 }}>
        <Input value={issued?.password ?? ''} readOnly />
        <Tooltip title="复制口令">
          <Button aria-label="复制口令" icon={<CopyOutlined />} onClick={copyPassword} />
        </Tooltip>
      </Space.Compact>
    </Modal>
  )
}

function roleLabel(role: Role): string {
  switch (role) {
    case 'READONLY': return '只读运维'
    case 'ALERT_ADMIN': return '告警管理员'
    case 'PLATFORM_ADMIN': return '平台管理员'
    default: return assertNever(role)
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled role: ${value}`)
}
