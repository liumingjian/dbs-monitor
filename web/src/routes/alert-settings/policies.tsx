import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { createRoute } from '@tanstack/react-router'
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'
import { AlertSettingsHeader } from './header'

type Policy = components['schemas']['NotificationPolicy']
type PolicyInput = components['schemas']['NotificationPolicyInput']
type PolicyForm = Omit<PolicyInput, 'channels' | 'repeat_interval'> & { smtp_enabled: boolean; webhook_target_ids: string[]; repeat_minutes: number }

export const policySettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/policies',
  component: PolicySettingsPage,
})

function PolicySettingsPage() {
  const policiesQuery = $api.useQuery('get', '/api/v1/notification-policies')
  const contactsQuery = $api.useQuery('get', '/api/v1/notification-contacts')
  const groupsQuery = $api.useQuery('get', '/api/v1/notification-contact-groups')
  const webhooksQuery = $api.useQuery('get', '/api/v1/notification-channels/webhooks')
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const createMutation = $api.useMutation('post', '/api/v1/notification-policies')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-policies/{id}')
  const deleteMutation = $api.useMutation('delete', '/api/v1/notification-policies/{id}')
  const [form] = Form.useForm<PolicyForm>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Policy | null>(null)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const role = currentUserQuery.data?.role
  const canManage = role === 'ALERT_ADMIN' || role === 'PLATFORM_ADMIN'

  function openEditor(policy?: Policy) {
    setEditing(policy ?? null)
    form.resetFields()
    form.setFieldsValue(policy ? policyFormValues(policy) : {
      name: '', contact_ids: [], contact_group_ids: [], severity_filter: ['critical', 'warning', 'info'],
      notify_on_fire: true, notify_on_recovery: true, repeat_minutes: 60, smtp_enabled: true, webhook_target_ids: [],
    })
    setOpen(true)
  }

  function save(values: PolicyForm) {
    const body = policyInput(values)
    const options = {
      onSuccess: () => {
        setOpen(false)
        setFeedback({ type: 'success' as const, text: editing ? '通知策略已更新' : '通知策略已创建' })
        void policiesQuery.refetch()
      },
      onError: (error: unknown) => setFeedback({ type: 'error' as const, text: apiErrorMessage(error, '保存通知策略失败') }),
    }
    if (editing) updateMutation.mutate({ params: { path: { id: editing.id } }, body }, options)
    else createMutation.mutate({ body }, options)
  }

  function remove(policy: Policy) {
    deleteMutation.mutate({ params: { path: { id: policy.id } } }, {
      onSuccess: () => void policiesQuery.refetch(),
      onError: (error) => setFeedback({ type: 'error', text: apiErrorMessage(error, '删除通知策略失败') }),
    })
  }

  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <AlertSettingsHeader active="policies" />
      {!canManage && <Alert type="info" showIcon title="只读模式" description="需要告警管理员角色才能修改通知策略" />}
      {feedback && <Alert type={feedback.type} title={feedback.text} closable onClose={() => setFeedback(null)} />}
      <Space className="settings-section-heading" wrap>
        <Typography.Title level={4} style={{ margin: 0 }}>通知策略</Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} disabled={!canManage} onClick={() => openEditor()}>新建策略</Button>
      </Space>
      <Table<Policy> rowKey="id" loading={policiesQuery.isPending} dataSource={policiesQuery.data ?? []} pagination={false} scroll={{ x: 1080 }} columns={[
        { title: '名称', render: (_, policy) => <Space>{policy.name}{policy.is_default && <Tag color="blue">全局默认</Tag>}</Space> },
        { title: '级别过滤', render: (_, policy) => policy.severity_filter.join('、') },
        { title: '触发 / 恢复', render: (_, policy) => `${policy.notify_on_fire ? '开启' : '关闭'} / ${policy.notify_on_recovery ? '开启' : '关闭'}` },
        { title: '重复间隔', render: (_, policy) => repeatLabel(policy.repeat_interval) },
        { title: '接收范围', render: (_, policy) => `${policy.contact_ids.length} 联系人 · ${policy.contact_group_ids.length} 组 · ${policy.channels.length} 渠道` },
        { title: '操作', width: 120, render: (_, policy) => <Space>
          <Tooltip title="编辑策略"><Button aria-label={`编辑 ${policy.name}`} icon={<EditOutlined />} disabled={!canManage} onClick={() => openEditor(policy)} /></Tooltip>
          <Popconfirm title="删除此通知策略？" disabled={!canManage || policy.is_default} onConfirm={() => remove(policy)}><Tooltip title={policy.is_default ? '全局默认策略不可删除' : '删除策略'}><span><Button aria-label={`删除 ${policy.name}`} danger icon={<DeleteOutlined />} disabled={!canManage || policy.is_default} /></span></Tooltip></Popconfirm>
        </Space> },
      ]} />
      <Modal title={editing ? '编辑通知策略' : '新建通知策略'} open={open} width={760} footer={null} destroyOnHidden onCancel={() => setOpen(false)}>
        <Form<PolicyForm> form={form} layout="vertical" onFinish={save}>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          <div className="settings-form-grid">
            <Form.Item name="contact_ids" label="联系人"><Select mode="multiple" options={(contactsQuery.data ?? []).map((contact) => ({ value: contact.id, label: `${contact.name} · ${contact.email}` }))} /></Form.Item>
            <Form.Item name="contact_group_ids" label="联系人组"><Select mode="multiple" options={(groupsQuery.data ?? []).map((group) => ({ value: group.id, label: group.name }))} /></Form.Item>
            <Form.Item name="severity_filter" label="级别过滤" rules={[{ required: true, message: '请至少选择一个级别' }]}><Select mode="multiple" options={[{ value: 'critical', label: '严重' }, { value: 'warning', label: '警告' }, { value: 'info', label: '提示' }]} /></Form.Item>
            <Form.Item name="repeat_minutes" label="重复间隔（分钟）" rules={[{ required: true }]}><InputNumber min={15} max={1440} precision={0} style={{ width: '100%' }} /></Form.Item>
            <Form.Item name="notify_on_fire" label="触发通知" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="notify_on_recovery" label="恢复通知" valuePropName="checked"><Switch /></Form.Item>
          </div>
          <Form.Item name="smtp_enabled" valuePropName="checked"><Checkbox>SMTP</Checkbox></Form.Item>
          <Form.Item name="webhook_target_ids" label="Webhook 目标"><Select mode="multiple" options={(webhooksQuery.data ?? []).map((target) => ({ value: target.id, label: target.name, disabled: !target.enabled }))} /></Form.Item>
          <Button type="primary" htmlType="submit" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
        </Form>
      </Modal>
    </Space>
  )
}

export function policyFormValues(policy: Policy): PolicyForm {
  return {
    name: policy.name,
    contact_ids: policy.contact_ids,
    contact_group_ids: policy.contact_group_ids,
    severity_filter: policy.severity_filter,
    notify_on_fire: policy.notify_on_fire,
    notify_on_recovery: policy.notify_on_recovery,
    template_id: policy.template_id,
    repeat_minutes: policy.repeat_interval / 60,
    smtp_enabled: policy.channels.some((channel) => channel.channel === 'SMTP'),
    webhook_target_ids: policy.channels.flatMap((channel) => channel.channel === 'WEBHOOK' && channel.target_id ? [channel.target_id] : []),
  }
}

export function policyInput(values: PolicyForm): PolicyInput {
  return {
    name: values.name,
    contact_ids: values.contact_ids,
    contact_group_ids: values.contact_group_ids,
    severity_filter: values.severity_filter,
    notify_on_fire: values.notify_on_fire,
    notify_on_recovery: values.notify_on_recovery,
    repeat_interval: values.repeat_minutes * 60,
    template_id: values.template_id,
    channels: [
      ...(values.smtp_enabled ? [{ channel: 'SMTP' as const }] : []),
      ...values.webhook_target_ids.map((target_id) => ({ channel: 'WEBHOOK' as const, target_id })),
    ],
  }
}

function repeatLabel(seconds: number) {
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  return `${seconds / 60} 分钟`
}
