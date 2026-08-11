import { ApiOutlined, CopyOutlined, KeyOutlined, PoweroffOutlined, SaveOutlined, StopOutlined, SyncOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Descriptions, Divider, Form, Input, InputNumber, Modal, Space, Tag, Tooltip, Typography } from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'

type InstanceMetadataInput = components['schemas']['InstanceMetadataInput']
type InstanceCredentialInput = components['schemas']['InstanceCredentialInput']
type AgentRegistration = components['schemas']['AgentRegistration']
type AgentRegistrationState = components['schemas']['AgentRegistrationState']
type AgentTokenIssued = components['schemas']['AgentTokenIssued']
type IssuedAgentToken = { instanceId: string; token: string; registration: AgentRegistration }

const passwordMask = '************'

export const instanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/settings',
  component: InstanceSettingsPage,
})

function InstanceSettingsPage() {
  const { id } = instanceSettingsRoute.useParams()
  const instanceQuery = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const updateMetadataMutation = $api.useMutation('put', '/api/v1/instances/{id}')
  const updateCredentialMutation = $api.useMutation('put', '/api/v1/instances/{id}/credentials')
  const agentRegistrationQuery = $api.useQuery('get', '/api/v1/instances/{id}/agent/registration', { params: { path: { id } } })
  const registerAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/registration')
  const rotateAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/token/rotation')
  const revokeAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/token/revocation')
  const disableAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/disable')
  const [metadataForm] = Form.useForm<InstanceMetadataInput>()
  const [credentialForm] = Form.useForm<InstanceCredentialInput>()
  const [credentialModalOpen, setCredentialModalOpen] = useState(false)
  const [issuedAgentToken, setIssuedAgentToken] = useState<IssuedAgentToken | null>(null)
  const [actionError, setActionError] = useState('')
  const canEditMetadata = currentUserQuery.data?.role === 'ALERT_ADMIN' || currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const canEditCredential = currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const metadataDisabledReason = canEditMetadata ? undefined : '需要告警管理员角色'
  const credentialDisabledReason = canEditCredential ? undefined : '需要平台管理员角色'
  const agentActionPending = registerAgentMutation.isPending || rotateAgentMutation.isPending ||
    revokeAgentMutation.isPending || disableAgentMutation.isPending

  useEffect(() => {
    if (instanceQuery.data) {
      metadataForm.setFieldsValue({
        name: instanceQuery.data.name,
        host: instanceQuery.data.host,
        port: instanceQuery.data.port,
        database: instanceQuery.data.database,
      })
    }
  }, [instanceQuery.data, metadataForm])

  function saveMetadata(values: InstanceMetadataInput) {
    setActionError('')
    updateMetadataMutation.mutate({ params: { path: { id } }, body: values }, {
      onSuccess: () => void instanceQuery.refetch(),
      onError: (failure) => setActionError(apiErrorMessage(failure, '保存元数据失败')),
    })
  }

  function saveCredential(values: InstanceCredentialInput) {
    setActionError('')
    updateCredentialMutation.mutate({ params: { path: { id } }, body: values }, {
      onSuccess: () => {
        setCredentialModalOpen(false)
        credentialForm.resetFields()
        void instanceQuery.refetch()
      },
      onError: (failure) => setActionError(apiErrorMessage(failure, '更新凭据失败')),
    })
  }

  function openCredentialModal() {
    credentialForm.setFieldsValue({ username: instanceQuery.data?.username })
    setCredentialModalOpen(true)
  }

  function refreshAgentRegistration() {
    void agentRegistrationQuery.refetch()
  }

  function showIssuedAgentToken(result: AgentTokenIssued, invalidResponseMessage: string) {
    if (!result.agent_token) {
      setActionError(invalidResponseMessage)
      return
    }
    setIssuedAgentToken({ instanceId: id, token: result.agent_token, registration: result.registration })
    refreshAgentRegistration()
  }

  function issueAgentToken() {
    registerAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: (result) => showIssuedAgentToken(result, 'Agent 令牌签发响应无效'),
      onError: (failure) => setActionError(apiErrorMessage(failure, '登记 Agent 失败')),
    })
  }

  function rotateAgentToken() {
    rotateAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: (result) => showIssuedAgentToken(result, 'Agent 令牌轮换响应无效'),
      onError: (failure) => setActionError(apiErrorMessage(failure, '轮换 Agent 令牌失败')),
    })
  }

  function revokeAgentToken() {
    revokeAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: refreshAgentRegistration,
      onError: (failure) => setActionError(apiErrorMessage(failure, '吊销 Agent 令牌失败')),
    })
  }

  function disableAgent() {
    disableAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: refreshAgentRegistration,
      onError: (failure) => setActionError(apiErrorMessage(failure, '停用 Agent 失败')),
    })
  }

  function closeIssuedAgentToken() {
    setIssuedAgentToken(null)
    registerAgentMutation.reset()
    rotateAgentMutation.reset()
  }

  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <Link to="/instances">返回实例列表</Link>
      <Typography.Title level={2} style={{ margin: 0 }}>{instanceQuery.data?.name ?? '接入设置'}</Typography.Title>
      {actionError && <Alert type="error" title={actionError} closable onClose={() => setActionError('')} />}

      <section aria-labelledby="metadata-heading">
        <Typography.Title id="metadata-heading" level={4}>元数据</Typography.Title>
        <Form<InstanceMetadataInput> form={metadataForm} layout="vertical" onFinish={saveMetadata} disabled={!canEditMetadata}>
          <div className="settings-form-grid">
            <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="host" label="主机" rules={[{ required: true, whitespace: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="port" label="端口" rules={[{ required: true }]}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="database" label="数据库" rules={[{ required: true, whitespace: true }]}>
              <Input />
            </Form.Item>
          </div>
          <Tooltip title={metadataDisabledReason}>
            <span>
              <Button type="primary" icon={<SaveOutlined />} htmlType="submit" disabled={!canEditMetadata} loading={updateMetadataMutation.isPending}>
                保存元数据
              </Button>
            </span>
          </Tooltip>
        </Form>
      </section>

      <Divider />
      <section aria-labelledby="credential-heading">
        <Space style={{ width: '100%', justifyContent: 'space-between' }} align="start">
          <div>
            <Typography.Title id="credential-heading" level={4}>PG 凭据</Typography.Title>
            <CredentialSummary username={instanceQuery.data?.username ?? ''} />
          </div>
          <Tooltip title={credentialDisabledReason}>
            <span>
              <Button icon={<KeyOutlined />} disabled={!canEditCredential} onClick={openCredentialModal}>更新凭据</Button>
            </span>
          </Tooltip>
        </Space>
      </section>

      <Divider />
      <section aria-labelledby="agent-heading">
        <Typography.Title id="agent-heading" level={4}>Agent</Typography.Title>
        {agentRegistrationQuery.data && (
          <AgentRegistrationPanel
            registration={agentRegistrationQuery.data}
            canManage={canEditCredential}
            actionPending={agentActionPending}
            onRegister={issueAgentToken}
            onRotate={rotateAgentToken}
            onRevoke={revokeAgentToken}
            onDisable={disableAgent}
          />
        )}
      </section>

      <Divider />
      <section aria-labelledby="danger-heading">
        <Typography.Title id="danger-heading" level={4} type="danger">危险区</Typography.Title>
        <Typography.Text type="secondary">暂不可用</Typography.Text>
      </section>

      <Modal title="更新 PG 凭据" open={credentialModalOpen} footer={null} destroyOnHidden onCancel={() => setCredentialModalOpen(false)}>
        <Form<InstanceCredentialInput> form={credentialForm} layout="vertical" onFinish={saveCredential}>
          <Form.Item name="username" label="新用户名" rules={[{ required: true, whitespace: true }]}>
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item name="password" label="新密码" rules={[{ required: true }]}>
            <Input type="password" autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={updateCredentialMutation.isPending}>连接测试并更新</Button>
        </Form>
      </Modal>
      <AgentTokenModal issued={issuedAgentToken} onClose={closeIssuedAgentToken} />
    </Space>
  )
}

type AgentRegistrationPanelProps = {
  registration: AgentRegistration
  canManage: boolean
  actionPending: boolean
  onRegister: () => void
  onRotate: () => void
  onRevoke: () => void
  onDisable: () => void
}

export function AgentRegistrationPanel({
  registration,
  canManage,
  actionPending,
  onRegister,
  onRotate,
  onRevoke,
  onDisable,
}: AgentRegistrationPanelProps) {
  const disabledReason = canManage ? undefined : '需要平台管理员角色'
  const statePresentation = agentStatePresentation(registration.state)
  const managedAction = (button: ReactNode) => (
    <Tooltip title={disabledReason}><span>{button}</span></Tooltip>
  )

  let actions: ReactNode
  switch (registration.state) {
    case 'NEVER_REGISTERED':
      actions = managedAction(
        <Button icon={<ApiOutlined />} disabled={!canManage} loading={actionPending} onClick={onRegister}>登记</Button>,
      )
      break
    case 'EXPECTED_ONLINE':
      actions = <>
        {managedAction(<Button icon={<SyncOutlined />} disabled={!canManage} loading={actionPending} onClick={onRotate}>轮换</Button>)}
        {managedAction(<Button danger icon={<StopOutlined />} disabled={!canManage} loading={actionPending} onClick={onRevoke}>吊销</Button>)}
        {managedAction(<Button icon={<PoweroffOutlined />} disabled={!canManage} loading={actionPending} onClick={onDisable}>停用</Button>)}
      </>
      break
    case 'REVOKED':
      actions = managedAction(
        <Button icon={<PoweroffOutlined />} disabled={!canManage} loading={actionPending} onClick={onDisable}>停用</Button>,
      )
      break
    case 'DISABLED':
      actions = managedAction(
        <Button icon={<ApiOutlined />} disabled={!canManage} loading={actionPending} onClick={onRegister}>重新启用</Button>,
      )
      break
    default:
      actions = assertNever(registration.state)
  }

  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      <Descriptions size="small" column={1} items={[
        { key: 'state', label: '登记状态', children: <Tag color={statePresentation.color}>{statePresentation.label}</Tag> },
        { key: 'expected', label: '期待在线', children: registration.agent_expected ? '是' : '否' },
        { key: 'first', label: '首次登记', children: formatOptionalTime(registration.first_registered_at) },
        { key: 'issued', label: '最近签发', children: formatOptionalTime(registration.issued_at) },
        { key: 'revoked', label: '最近吊销', children: formatOptionalTime(registration.revoked_at) },
      ]} />
      <Space wrap>{actions}</Space>
    </Space>
  )
}

export function AgentTokenModal({ issued, onClose }: { issued: IssuedAgentToken | null; onClose: () => void }) {
  const token = issued?.token ?? ''
  const command = issued ? buildAgentInstallCommand(window.location.origin, issued.instanceId, issued.token, issued.registration) : ''
  function copy(value: string) {
    void navigator.clipboard.writeText(value)
  }
  return (
    <Modal
      title="Agent 令牌与安装"
      open={issued !== null}
      onCancel={onClose}
      onOk={onClose}
      okText="关闭"
      cancelButtonProps={{ style: { display: 'none' } }}
      destroyOnHidden
    >
      <Alert type="warning" showIcon title="令牌仅显示一次，关闭后不再显示" />
      <Typography.Text strong>令牌</Typography.Text>
      <Space.Compact style={{ width: '100%', marginTop: 8, marginBottom: 16 }}>
        <Input aria-label="Agent 令牌" value={token} readOnly />
        <Tooltip title="复制令牌">
          <Button aria-label="复制 Agent 令牌" icon={<CopyOutlined />} onClick={() => copy(token)} />
        </Tooltip>
      </Space.Compact>
      <Typography.Text strong>安装命令</Typography.Text>
      <Space.Compact style={{ width: '100%', marginTop: 8, marginBottom: 16 }}>
        <Input.TextArea aria-label="Agent 安装命令" value={command} readOnly rows={6} />
        <Tooltip title="复制安装命令">
          <Button aria-label="复制 Agent 安装命令" icon={<CopyOutlined />} onClick={() => copy(command)} />
        </Tooltip>
      </Space.Compact>
      <Descriptions size="small" column={1} items={issued ? [
        { key: 'path', label: '令牌文件', children: issued.registration.installation.authentication_path },
        { key: 'mode', label: '文件权限', children: issued.registration.installation.file_mode },
        { key: 'restart', label: '重启命令', children: issued.registration.installation.restart_command },
      ] : []} />
    </Modal>
  )
}

export function buildAgentInstallCommand(platformOrigin: string, instanceId: string, token: string, registration: AgentRegistration): string {
  const origin = new URL(platformOrigin)
  const connectAddress = origin.host
  const serverName = origin.hostname
  const fingerprint = registration.installation.ca_fingerprint_sha256
  const installerURL = `${origin.origin}${registration.installation.installer_path}`
  const extractCertificates = `openssl s_client -showcerts -connect ${shellQuote(connectAddress)} -servername ${shellQuote(serverName)} </dev/null 2>/dev/null | awk -v directory="$work" '/BEGIN CERTIFICATE/{n++; file=sprintf("%s/cert-%d.pem",directory,n)} file{print > file} /END CERTIFICATE/{file=""}'`
  return [
    'work=$(mktemp -d)',
    'trap \'rm -rf "$work"\' EXIT INT TERM',
    extractCertificates,
    'ca=$(ls "$work"/cert-*.pem | sort -V | tail -n 1)',
    'actual=$(openssl x509 -in "$ca" -outform DER | sha256sum | cut -d\' \' -f1)',
    `test "$actual" = ${shellQuote(fingerprint)}`,
    `curl --fail --silent --show-error --cacert "$ca" ${shellQuote(installerURL)} -o "$work/install.sh"`,
    `printf '%s\\n' ${shellQuote(token)} | sudo sh "$work/install.sh" ${shellQuote(origin.origin)} ${shellQuote(instanceId)} ${shellQuote(fingerprint)} "$ca"`,
  ].join('\n')
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

function agentStatePresentation(state: AgentRegistrationState): { label: string; color?: string } {
  switch (state) {
    case 'NEVER_REGISTERED': return { label: '从未登记' }
    case 'EXPECTED_ONLINE': return { label: '应在线', color: 'green' }
    case 'REVOKED': return { label: '已吊销', color: 'red' }
    case 'DISABLED': return { label: '已停用', color: 'default' }
    default: return assertNever(state)
  }
}

function formatOptionalTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function assertNever(value: never): never {
  throw new Error(`unhandled value: ${value}`)
}

export function CredentialSummary({ username }: { username: string }) {
  return (
    <Descriptions size="small" column={1} items={[
      { key: 'username', label: '用户名', children: username },
      {
        key: 'password',
        label: '密码',
        children: (
          <Space>
            <Input aria-label="密码状态" type="password" value={passwordMask} readOnly style={{ width: 150 }} />
            <Typography.Text type="secondary">已设置</Typography.Text>
          </Space>
        ),
      },
    ]} />
  )
}
