import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { Link, Outlet, createRootRoute, useLocation } from '@tanstack/react-router'
import { Badge, Button, Dropdown, Form, Input, Layout, Modal, Space, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../api/client'
import { apiErrorMessage } from '../api/errors'
import { pollingIntervals } from '../api/polling'
import type { components } from '../api/schema'
import { pausedInstanceCount } from '../domain/CollectionPausedTag'

type PasswordChangeInput = components['schemas']['PasswordChangeInput']

type PasswordChangeModalProps = {
  open: boolean
  pending: boolean
  error: string
  onClose: () => void
  onSubmit: (values: PasswordChangeInput) => void
}

export const rootRoute = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const location = useLocation()
  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header className="app-header">
        <Typography.Title level={3} style={{ color: 'white', margin: 0 }}>DBS Monitor</Typography.Title>
        {location.pathname !== '/login' && <AuthenticatedHeader />}
      </Layout.Header>
      <Layout.Content style={{ padding: 24, maxWidth: 1520, width: '100%', margin: '0 auto' }}>
        <Outlet />
      </Layout.Content>
    </Layout>
  )
}

function AuthenticatedHeader() {
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const failureQuery = $api.useQuery(
    'get',
    '/api/v1/notification-channels/failures',
    {},
    { refetchInterval: 15_000 },
  )
  const instancesQuery = $api.useQuery(
    'get',
    '/api/v1/instances',
    {},
    { refetchInterval: pollingIntervals.instances },
  )
  const changePasswordMutation = $api.useMutation('put', '/api/v1/password')
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [error, setError] = useState('')

  function changeOwnPassword(values: PasswordChangeInput) {
    setError('')
    changePasswordMutation.mutate({ body: values }, {
      onSuccess: () => setPasswordOpen(false),
      onError: (failure) => setError(apiErrorMessage(failure, '修改口令失败，请重试')),
    })
  }

  function closePasswordModal() {
    setPasswordOpen(false)
    setError('')
  }

  return (
    <>
      <Space size="large">
        <Link to="/instances" className="header-link">
          <InstanceListLabel pausedCount={instancesQuery.data && pausedInstanceCount(instancesQuery.data)} />
        </Link>
        <Link to="/alerts" search={{ tab: 'current', include_paused: false }} className="header-link">全局告警</Link>
        <Link to="/users" className="header-link">用户管理</Link>
        <Link to="/alert-settings/notifications" className="header-link">
          <NotificationSettingsLabel hasFailures={failureQuery.data?.has_failures === true} />
        </Link>
        <Dropdown menu={{ items: [{ key: 'password', icon: <LockOutlined />, label: '修改口令', onClick: () => setPasswordOpen(true) }] }}>
          <Button type="text" className="header-user" icon={<UserOutlined />}>
            {currentUserQuery.data?.username ?? ''}
          </Button>
        </Dropdown>
      </Space>
      <PasswordChangeModal
        open={passwordOpen}
        pending={changePasswordMutation.isPending}
        error={error}
        onClose={closePasswordModal}
        onSubmit={changeOwnPassword}
      />
    </>
  )
}

// 角标计的是已暂停实例数；实例列表还没到手时不渲染角标，免得把「还不知道」显示成 0。
export function InstanceListLabel({ pausedCount }: { pausedCount: number | undefined }) {
  const label = <span className="header-link-label" title={pausedCount ? `${pausedCount} 个实例已暂停采集` : undefined}>实例列表</span>
  if (pausedCount === undefined) return label
  return <Badge count={pausedCount} color="gold" offset={[8, 0]}>{label}</Badge>
}

export function NotificationSettingsLabel({ hasFailures }: { hasFailures: boolean }) {
  return (
    <Badge dot={hasFailures} status="error" offset={[5, 0]}>
      <span className="header-link-label" title={hasFailures ? '通知渠道存在未清除失败' : undefined}>告警设置</span>
    </Badge>
  )
}

export function PasswordChangeModal({ open, pending, error, onClose, onSubmit }: PasswordChangeModalProps) {
  return (
    <Modal title="修改口令" open={open} footer={null} destroyOnHidden onCancel={onClose}>
      <Form layout="vertical" onFinish={onSubmit}>
        <Form.Item name="old_password" label="旧口令" rules={[{ required: true, message: '请输入旧口令' }]}>
          <Input.Password autoComplete="current-password" />
        </Form.Item>
        <Form.Item name="new_password" label="新口令" rules={[{ required: true, message: '请输入新口令' }, { min: 12, message: '新口令至少 12 个字符' }]}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        {error && <Typography.Text type="danger">{error}</Typography.Text>}
        <div style={{ marginTop: 16 }}>
          <Button type="primary" htmlType="submit" loading={pending}>保存</Button>
        </div>
      </Form>
    </Modal>
  )
}
