import { SaveOutlined, SendOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import { Alert, Button, Form, Input, InputNumber, Select, Space, Switch, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'

type SMTPChannelInput = components['schemas']['SMTPChannelInput']
type TestNotificationInput = { target: string }
type Feedback = { type: 'success' | 'error'; text: string }

export const notificationSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/notifications',
  component: NotificationSettingsPage,
})

function NotificationSettingsPage() {
  const channelQuery = $api.useQuery('get', '/api/v1/notification-channels/smtp')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-channels/smtp')
  const testMutation = $api.useMutation('post', '/api/v1/notification-channels/smtp/test')
  const [form] = Form.useForm<SMTPChannelInput>()
  const [testForm] = Form.useForm<TestNotificationInput>()
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const authType = Form.useWatch('auth_type', form)
  const canManage = currentUserQuery.data?.role === 'ALERT_ADMIN' || currentUserQuery.data?.role === 'PLATFORM_ADMIN'

  useEffect(() => {
    const channel = channelQuery.data
    if (!channel?.configured) return
    form.setFieldsValue({
      enabled: channel.enabled,
      host: channel.host,
      port: channel.port,
      from_address: channel.from_address,
      recipient: channel.recipient,
      auth_type: channel.auth_type,
      username: channel.username,
      tls_mode: channel.tls_mode,
    })
    testForm.setFieldValue('target', channel.recipient)
  }, [channelQuery.data, form, testForm])

  function saveChannel(values: SMTPChannelInput) {
    setFeedback(null)
    updateMutation.mutate(
      { body: values },
      {
        onSuccess: () => {
          setFeedback({ type: 'success', text: 'SMTP 配置已保存' })
          form.setFieldValue('password', undefined)
          void channelQuery.refetch()
        },
        onError: (error) =>
          setFeedback({
            type: 'error',
            text: apiErrorMessage(error, '保存 SMTP 配置失败'),
          }),
      },
    )
  }

  function sendTestNotification(values: TestNotificationInput) {
    setFeedback(null)
    testMutation.mutate(
      { body: values },
      {
        onSuccess: () => setFeedback({ type: 'success', text: '测试通知已进入发送队列' }),
        onError: (error) =>
          setFeedback({
            type: 'error',
            text: apiErrorMessage(error, '测试通知发送失败'),
          }),
      },
    )
  }

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <div>
        <Typography.Title level={2} style={{ marginBottom: 4 }}>
          告警设置
        </Typography.Title>
        <Typography.Title level={4} style={{ margin: 0 }}>
          通知渠道
        </Typography.Title>
      </div>
      {!canManage && (
        <Alert type="info" showIcon title="只读模式" description="需要告警管理员角色才能修改配置或发送测试通知" />
      )}
      {feedback && <Alert type={feedback.type} title={feedback.text} closable onClose={() => setFeedback(null)} />}
      <section className="settings-section">
        <Typography.Title level={4}>SMTP</Typography.Title>
        <Form<SMTPChannelInput>
          form={form}
          layout="vertical"
          disabled={!canManage}
          onFinish={saveChannel}
          initialValues={{
            enabled: false,
            port: 587,
            auth_type: 'PLAIN',
            tls_mode: 'STARTTLS',
          }}
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
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Typography.Text>
                    {channelQuery.data?.auth_configured ? '已设置  ********' : '未设置'}
                  </Typography.Text>
                  <Form.Item name="password" noStyle>
                    <Input.Password
                      autoComplete="new-password"
                      placeholder={channelQuery.data?.auth_configured ? '留空保持不变' : '请输入认证信息'}
                    />
                  </Form.Item>
                </Space>
              </Form.Item>
            </>
          )}
          <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={updateMutation.isPending}>
            保存
          </Button>
        </Form>
      </section>
      <section className="settings-section">
        <Typography.Title level={4}>测试发送</Typography.Title>
        <Form form={testForm} layout="inline" disabled={!canManage} onFinish={sendTestNotification}>
          <Form.Item name="target" label="收件人" rules={[{ required: true, type: 'email' }]}>
            <Input style={{ width: 320 }} />
          </Form.Item>
          <Button type="primary" htmlType="submit" icon={<SendOutlined />} loading={testMutation.isPending}>
            发送测试
          </Button>
        </Form>
      </section>
    </Space>
  )
}
