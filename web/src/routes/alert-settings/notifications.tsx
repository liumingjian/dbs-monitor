import { DeleteOutlined, EditOutlined, PlusOutlined, SaveOutlined, SendOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import {
  Alert,
  Button,
  Collapse,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'
import { AlertSettingsHeader } from './header'

type SMTPChannelInput = components['schemas']['SMTPChannelInput']
type WebhookTargetInput = components['schemas']['WebhookTargetInput']
type WebhookTarget = components['schemas']['WebhookTarget']
type ChannelFailureSummary = components['schemas']['ChannelFailureSummary']
type ChannelFailureRecord = components['schemas']['ChannelFailureRecord']
type TestNotificationInput = { target: string }
type Feedback = { type: 'success' | 'error'; text: string }
type WebhookTargetsTableProps = {
  targets: WebhookTarget[]
  failures: ChannelFailureSummary[]
  canManage: boolean
  onEdit: (target: WebhookTarget) => void
  onDelete: (target: WebhookTarget) => void
  onTest: (target: WebhookTarget) => void
}

export const notificationSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/notifications',
  component: NotificationSettingsPage,
})

function NotificationSettingsPage() {
  const smtpQuery = $api.useQuery('get', '/api/v1/notification-channels/smtp')
  const webhookQuery = $api.useQuery('get', '/api/v1/notification-channels/webhooks')
  const failureQuery = $api.useQuery('get', '/api/v1/notification-channels/failures')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const updateSMTPMutation = $api.useMutation('put', '/api/v1/notification-channels/smtp')
  const testSMTPMutation = $api.useMutation('post', '/api/v1/notification-channels/smtp/test')
  const createWebhookMutation = $api.useMutation('post', '/api/v1/notification-channels/webhooks')
  const updateWebhookMutation = $api.useMutation('put', '/api/v1/notification-channels/webhooks/{id}')
  const deleteWebhookMutation = $api.useMutation('delete', '/api/v1/notification-channels/webhooks/{id}')
  const testWebhookMutation = $api.useMutation('post', '/api/v1/notification-channels/webhooks/{id}/test')
  const [smtpForm] = Form.useForm<SMTPChannelInput>()
  const [smtpTestForm] = Form.useForm<TestNotificationInput>()
  const [webhookForm] = Form.useForm<WebhookTargetInput>()
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const [webhookOpen, setWebhookOpen] = useState(false)
  const [editingWebhook, setEditingWebhook] = useState<WebhookTarget | null>(null)
  const authType = Form.useWatch('auth_type', smtpForm)
  const canManage = currentUserQuery.data?.role === 'ALERT_ADMIN' || currentUserQuery.data?.role === 'PLATFORM_ADMIN'

  useEffect(() => {
    const channel = smtpQuery.data
    if (!channel?.configured) return
    smtpForm.setFieldsValue({
      enabled: channel.enabled,
      host: channel.host,
      port: channel.port,
      from_address: channel.from_address,
      recipient: channel.recipient,
      auth_type: channel.auth_type,
      username: channel.username,
      tls_mode: channel.tls_mode,
    })
    smtpTestForm.setFieldValue('target', channel.recipient)
  }, [smtpForm, smtpQuery.data, smtpTestForm])

  function showFeedback(type: Feedback['type'], text: string) {
    setFeedback({ type, text })
  }

  function saveSMTPChannel(values: SMTPChannelInput) {
    setFeedback(null)
    updateSMTPMutation.mutate(
      { body: values },
      {
        onSuccess: () => {
          showFeedback('success', 'SMTP 配置已保存')
          smtpForm.setFieldValue('password', undefined)
          void smtpQuery.refetch()
        },
        onError: (error) => showFeedback('error', apiErrorMessage(error, '保存 SMTP 配置失败')),
      },
    )
  }

  function sendSMTPTestNotification(values: TestNotificationInput) {
    setFeedback(null)
    testSMTPMutation.mutate(
      { body: values },
      {
        onSuccess: () => showFeedback('success', 'SMTP 测试通知已进入发送队列'),
        onError: (error) => showFeedback('error', apiErrorMessage(error, 'SMTP 测试通知发送失败')),
      },
    )
  }

  function openCreateWebhook() {
    setEditingWebhook(null)
    webhookForm.resetFields()
    webhookForm.setFieldsValue({ enabled: true })
    setWebhookOpen(true)
  }

  function openEditWebhook(target: WebhookTarget) {
    setEditingWebhook(target)
    webhookForm.setFieldsValue({ name: target.name, enabled: target.enabled, url: target.url })
    setWebhookOpen(true)
  }

  function saveWebhook(values: WebhookTargetInput) {
    setFeedback(null)
    const options = {
      onSuccess: () => {
        showFeedback('success', editingWebhook ? 'Webhook 目标已更新' : 'Webhook 目标已创建')
        setWebhookOpen(false)
        setEditingWebhook(null)
        webhookForm.resetFields()
        void webhookQuery.refetch()
      },
      onError: (error: unknown) => showFeedback('error', apiErrorMessage(error, '保存 Webhook 目标失败')),
    }
    if (editingWebhook) {
      updateWebhookMutation.mutate({ params: { path: { id: editingWebhook.id } }, body: values }, options)
      return
    }
    createWebhookMutation.mutate({ body: values }, options)
  }

  function deleteWebhook(target: WebhookTarget) {
    setFeedback(null)
    deleteWebhookMutation.mutate(
      { params: { path: { id: target.id } } },
      {
        onSuccess: () => {
          showFeedback('success', 'Webhook 目标已删除')
          void webhookQuery.refetch()
          void failureQuery.refetch()
        },
        onError: (error) => showFeedback('error', apiErrorMessage(error, '删除 Webhook 目标失败')),
      },
    )
  }

  function testWebhook(target: WebhookTarget) {
    setFeedback(null)
    testWebhookMutation.mutate(
      { params: { path: { id: target.id } } },
      {
        onSuccess: () => showFeedback('success', `${target.name} 测试请求已进入发送队列`),
        onError: (error) => showFeedback('error', apiErrorMessage(error, 'Webhook 测试请求失败')),
      },
    )
  }

  const smtpFailure = failureQuery.data?.channels.find((summary) => summary.channel === 'SMTP')

  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <AlertSettingsHeader active="notifications" />
      {!canManage && (
        <Alert type="info" showIcon title="只读模式" description="需要告警管理员角色才能修改配置或发送测试通知" />
      )}
      {feedback && <Alert type={feedback.type} title={feedback.text} closable onClose={() => setFeedback(null)} />}
      <section className="settings-section">
        <Typography.Title level={4}>SMTP</Typography.Title>
        <Form<SMTPChannelInput>
          form={smtpForm}
          layout="vertical"
          disabled={!canManage}
          onFinish={saveSMTPChannel}
          initialValues={{ enabled: false, port: 587, auth_type: 'PLAIN', tls_mode: 'STARTTLS' }}
          style={{ maxWidth: 720 }}
        >
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Space align="start" wrap size="middle">
            <Form.Item name="host" label="服务器" rules={[{ required: true }]}>
              <Input style={{ width: 320 }} />
            </Form.Item>
            <Form.Item name="port" label="端口" rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: 140 }} />
            </Form.Item>
          </Space>
          <Form.Item name="from_address" label="发件人" rules={[{ required: true, type: 'email' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="recipient" label="默认收件人" rules={[{ required: true, type: 'email' }]}>
            <Input />
          </Form.Item>
          <Space align="start" wrap size="middle">
            <Form.Item name="tls_mode" label="传输安全" rules={[{ required: true }]}>
              <Select
                style={{ width: 180 }}
                options={[
                  { value: 'STARTTLS', label: 'STARTTLS' },
                  { value: 'IMPLICIT', label: '隐式 TLS' },
                ]}
              />
            </Form.Item>
            <Form.Item name="auth_type" label="认证方式" rules={[{ required: true }]}>
              <Select
                style={{ width: 180 }}
                options={[
                  { value: 'NONE', label: '无认证' },
                  { value: 'PLAIN', label: 'PLAIN' },
                  { value: 'LOGIN', label: 'LOGIN' },
                ]}
              />
            </Form.Item>
          </Space>
          {authType !== 'NONE' && (
            <>
              <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
                <Input autoComplete="off" />
              </Form.Item>
              <Form.Item label="认证信息">
                <Space orientation="vertical" style={{ width: '100%' }}>
                  <Typography.Text>{smtpQuery.data?.auth_configured ? '已设置  ********' : '未设置'}</Typography.Text>
                  <Form.Item name="password" noStyle>
                    <Input.Password
                      autoComplete="new-password"
                      placeholder={smtpQuery.data?.auth_configured ? '留空保持不变' : '请输入认证信息'}
                    />
                  </Form.Item>
                </Space>
              </Form.Item>
            </>
          )}
          <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={updateSMTPMutation.isPending}>
            保存
          </Button>
        </Form>
        <Typography.Title level={5}>测试发送</Typography.Title>
        <Form form={smtpTestForm} layout="inline" disabled={!canManage} onFinish={sendSMTPTestNotification}>
          <Form.Item name="target" label="收件人" rules={[{ required: true, type: 'email' }]}>
            <Input style={{ width: 320 }} />
          </Form.Item>
          <Button type="primary" htmlType="submit" icon={<SendOutlined />} loading={testSMTPMutation.isPending}>
            发送测试
          </Button>
        </Form>
        <ChannelFailureDetails summary={smtpFailure} />
      </section>
      <section className="settings-section">
        <Space align="center" style={{ width: '100%', justifyContent: 'space-between', marginBottom: 16 }}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            Webhook
          </Typography.Title>
          <Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={openCreateWebhook}>
            新建目标
          </Button>
        </Space>
        <WebhookTargetsTable
          targets={webhookQuery.data ?? []}
          failures={failureQuery.data?.channels ?? []}
          canManage={canManage}
          onEdit={openEditWebhook}
          onDelete={deleteWebhook}
          onTest={testWebhook}
        />
      </section>
      <Modal
        title={editingWebhook ? '编辑 Webhook 目标' : '新建 Webhook 目标'}
        open={webhookOpen}
        footer={null}
        destroyOnHidden
        onCancel={() => setWebhookOpen(false)}
      >
        <Form<WebhookTargetInput> form={webhookForm} layout="vertical" onFinish={saveWebhook}>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="url" label="URL" rules={[{ required: true, type: 'url' }]}>
            <Input placeholder="https://gateway.example.com/alerts" />
          </Form.Item>
          <Form.Item label="签名配置">
            <Typography.Text>{editingWebhook?.signing_configured ? '已设置' : '未设置'}</Typography.Text>
          </Form.Item>
          <Form.Item
            name="signing_value"
            label="签名密钥"
            rules={[{ required: !editingWebhook, message: '请输入签名密钥' }]}
          >
            <Input.Password autoComplete="new-password" placeholder={editingWebhook ? '留空保持不变' : undefined} />
          </Form.Item>
          <Form.Item
            name="signature_header"
            label="签名头"
            rules={[{ required: !editingWebhook, message: '请输入签名头' }]}
          >
            <Input.Password autoComplete="new-password" placeholder={editingWebhook ? '留空保持不变' : 'X-DBS-Signature'} />
          </Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            icon={<SaveOutlined />}
            loading={createWebhookMutation.isPending || updateWebhookMutation.isPending}
          >
            保存
          </Button>
        </Form>
      </Modal>
    </Space>
  )
}

export function WebhookTargetsTable({
  targets,
  failures,
  canManage,
  onEdit,
  onDelete,
  onTest,
}: WebhookTargetsTableProps) {
  return (
    <Table<WebhookTarget>
      rowKey="id"
      dataSource={targets}
      pagination={false}
      scroll={{ x: 960 }}
      locale={{ emptyText: '暂无 Webhook 目标' }}
      columns={[
        {
          title: '目标',
          key: 'target',
          render: (_, target) => (
            <Space orientation="vertical" size={0}>
              <Typography.Text strong>{target.name}</Typography.Text>
              <Typography.Link href={target.url} target="_blank" rel="noreferrer">
                {target.url}
              </Typography.Link>
            </Space>
          ),
        },
        {
          title: '状态',
          key: 'status',
          width: 150,
          render: (_, target) => (
            <Space orientation="vertical" size={2}>
              <Tag color={target.enabled ? 'green' : 'default'}>{target.enabled ? '已启用' : '已停用'}</Tag>
              <Typography.Text type="secondary">{target.signing_configured ? '签名已设置' : '签名未设置'}</Typography.Text>
            </Space>
          ),
        },
        {
          title: '失败摘要',
          key: 'failure',
          render: (_, target) => (
            <ChannelFailureDetails
              summary={failures.find((summary) => summary.channel === 'WEBHOOK' && summary.target_id === target.id)}
            />
          ),
        },
        {
          title: '操作',
          key: 'actions',
          width: 150,
          render: (_, target) => (
            <Space>
              <Tooltip title="发送测试请求">
                <Button
                  aria-label={`测试 ${target.name}`}
                  icon={<SendOutlined />}
                  disabled={!canManage || !target.enabled}
                  onClick={() => onTest(target)}
                />
              </Tooltip>
              <Tooltip title="编辑目标">
                <Button aria-label={`编辑 ${target.name}`} icon={<EditOutlined />} disabled={!canManage} onClick={() => onEdit(target)} />
              </Tooltip>
              <Popconfirm title="删除此 Webhook 目标？" disabled={!canManage} onConfirm={() => onDelete(target)}>
                <Tooltip title="删除目标">
                  <Button aria-label={`删除 ${target.name}`} danger icon={<DeleteOutlined />} disabled={!canManage} />
                </Tooltip>
              </Popconfirm>
            </Space>
          ),
        },
      ]}
    />
  )
}

export function ChannelFailureDetails({ summary }: { summary?: ChannelFailureSummary }) {
  if (!summary) return <Typography.Text type="secondary">最近无失败</Typography.Text>
  return (
    <Collapse
      ghost
      size="small"
      items={[
        {
          key: 'failures',
          label: (
            <Space orientation="vertical" size={0}>
              <Typography.Text type="danger">最近失败 {summary.recent_failure_count} 次</Typography.Text>
              <Typography.Text type="secondary">{summary.last_failure_reason}</Typography.Text>
            </Space>
          ),
          children: <FailureRecords records={summary.recent_failures} />,
        },
      ]}
    />
  )
}

function FailureRecords({ records }: { records: ChannelFailureRecord[] }) {
  return (
    <Table<ChannelFailureRecord>
      rowKey={(record) => `${record.failed_at}-${record.target}`}
      size="small"
      pagination={false}
      scroll={{ x: 720 }}
      dataSource={records}
      columns={[
        { title: '时间', dataIndex: 'failed_at', render: (value: string) => new Date(value).toLocaleString() },
        { title: '目标', dataIndex: 'target' },
        { title: '原因', dataIndex: 'reason' },
        { title: '重试次数', dataIndex: 'retry_count', width: 100 },
      ]}
    />
  )
}
