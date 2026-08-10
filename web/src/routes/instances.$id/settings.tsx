import { KeyOutlined, SaveOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Descriptions, Divider, Form, Input, InputNumber, Modal, Space, Tooltip, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'

type InstanceMetadataInput = components['schemas']['InstanceMetadataInput']
type InstanceCredentialInput = components['schemas']['InstanceCredentialInput']

const passwordMask = '************'

export const instanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/settings',
  component: InstanceSettingsPage,
})

function InstanceSettingsPage() {
  const { id } = instanceSettingsRoute.useParams()
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const currentUser = $api.useQuery('get', '/api/v1/me')
  const updateMetadata = $api.useMutation('put', '/api/v1/instances/{id}')
  const updateCredential = $api.useMutation('put', '/api/v1/instances/{id}/credentials')
  const [metadataForm] = Form.useForm<InstanceMetadataInput>()
  const [credentialForm] = Form.useForm<InstanceCredentialInput>()
  const [credentialOpen, setCredentialOpen] = useState(false)
  const [error, setError] = useState('')
  const canEditMetadata = currentUser.data?.role === 'ALERT_ADMIN' || currentUser.data?.role === 'PLATFORM_ADMIN'
  const canEditCredential = currentUser.data?.role === 'PLATFORM_ADMIN'

  useEffect(() => {
    if (instance.data) {
      metadataForm.setFieldsValue({
        name: instance.data.name,
        host: instance.data.host,
        port: instance.data.port,
        database: instance.data.database,
      })
    }
  }, [instance.data, metadataForm])

  function saveMetadata(values: InstanceMetadataInput) {
    setError('')
    updateMetadata.mutate({ params: { path: { id } }, body: values }, {
      onSuccess: () => void instance.refetch(),
      onError: (failure) => setError(apiErrorMessage(failure, '保存元数据失败')),
    })
  }

  function saveCredential(values: InstanceCredentialInput) {
    setError('')
    updateCredential.mutate({ params: { path: { id } }, body: values }, {
      onSuccess: () => {
        setCredentialOpen(false)
        credentialForm.resetFields()
        void instance.refetch()
      },
      onError: (failure) => setError(apiErrorMessage(failure, '更新凭据失败')),
    })
  }

  function openCredentialModal() {
    credentialForm.setFieldsValue({ username: instance.data?.username })
    setCredentialOpen(true)
  }

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Link to="/instances">返回实例列表</Link>
      <Typography.Title level={2} style={{ margin: 0 }}>{instance.data?.name ?? '接入设置'}</Typography.Title>
      {error && <Alert type="error" title={error} closable onClose={() => setError('')} />}

      <section aria-labelledby="metadata-heading">
        <Typography.Title id="metadata-heading" level={4}>元数据</Typography.Title>
        <Form<InstanceMetadataInput> form={metadataForm} layout="vertical" onFinish={saveMetadata} disabled={!canEditMetadata}>
          <div className="settings-form-grid">
            <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
            <Form.Item name="host" label="主机" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
            <Form.Item name="port" label="端口" rules={[{ required: true }]}><InputNumber min={1} max={65535} style={{ width: '100%' }} /></Form.Item>
            <Form.Item name="database" label="数据库" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
          </div>
          <Tooltip title={canEditMetadata ? undefined : '需要告警管理员角色'}>
            <span><Button type="primary" icon={<SaveOutlined />} htmlType="submit" disabled={!canEditMetadata} loading={updateMetadata.isPending}>保存元数据</Button></span>
          </Tooltip>
        </Form>
      </section>

      <Divider />
      <section aria-labelledby="credential-heading">
        <Space style={{ width: '100%', justifyContent: 'space-between' }} align="start">
          <div>
            <Typography.Title id="credential-heading" level={4}>PG 凭据</Typography.Title>
            <CredentialSummary username={instance.data?.username ?? ''} />
          </div>
          <Tooltip title={canEditCredential ? undefined : '需要平台管理员角色'}>
            <span><Button icon={<KeyOutlined />} disabled={!canEditCredential} onClick={openCredentialModal}>更新凭据</Button></span>
          </Tooltip>
        </Space>
      </section>

      <Divider />
      <section aria-labelledby="agent-heading">
        <Typography.Title id="agent-heading" level={4}>Agent</Typography.Title>
        <Typography.Text type="secondary">暂不可用</Typography.Text>
      </section>

      <Divider />
      <section aria-labelledby="danger-heading">
        <Typography.Title id="danger-heading" level={4} type="danger">危险区</Typography.Title>
        <Typography.Text type="secondary">暂不可用</Typography.Text>
      </section>

      <Modal title="更新 PG 凭据" open={credentialOpen} footer={null} destroyOnHidden onCancel={() => setCredentialOpen(false)}>
        <Form<InstanceCredentialInput> form={credentialForm} layout="vertical" onFinish={saveCredential}>
          <Form.Item name="username" label="新用户名" rules={[{ required: true, whitespace: true }]}><Input autoComplete="off" /></Form.Item>
          <Form.Item name="password" label="新密码" rules={[{ required: true }]}><Input type="password" autoComplete="new-password" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={updateCredential.isPending}>连接测试并更新</Button>
        </Form>
      </Modal>
    </Space>
  )
}

export function CredentialSummary({ username }: { username: string }) {
  return (
    <Descriptions size="small" column={1} items={[
      { key: 'username', label: '用户名', children: username },
      {
        key: 'password',
        label: '密码',
        children: <Space><Input aria-label="密码状态" type="password" value={passwordMask} readOnly style={{ width: 150 }} /><Typography.Text type="secondary">已设置</Typography.Text></Space>,
      },
    ]} />
  )
}
