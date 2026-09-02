import { Button, CopyButton, TextArea, TextInput } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import type { components } from '../../api/schema'
import {
  bootstrapDatabaseHelperText,
  bootstrapDatabaseLabel,
  instanceEngineLabel,
} from '../../domain/instanceEngine'
import { zodResolver } from '../../forms/zodResolver'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import type { Glyph } from '../../primitives/Icon'
import { KeyValueList } from '../../primitives/KeyValueList'
import { Modal } from '../../primitives/Modal'
import { NotificationBar } from '../../primitives/NotificationBar'
import { NumberInput } from '../../primitives/NumberInput'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import type { StatusTone } from '../../primitives/StatusBadge'
import { StatusDot } from '../../primitives/StatusDot'
import { rootRoute } from '../root'
import './settings.css'

type Instance = components['schemas']['Instance']
type InstanceMetadataInput = components['schemas']['InstanceMetadataInput']
type InstanceCredentialInput = components['schemas']['InstanceCredentialInput']
type AgentRegistration = components['schemas']['AgentRegistration']
type AgentRegistrationState = components['schemas']['AgentRegistrationState']
type AgentTokenIssued = components['schemas']['AgentTokenIssued']
type IssuedAgentToken = { instanceId: string; token: string; registration: AgentRegistration }

/// 密码从来不回传，所以界面上显示的是一串固定长度的掩码，不是密码本身，
/// 也**没有**「显示密码」的开关 —— 没有可显示的东西（CONTEXT.md：用户名不是秘密，密码是）。
const passwordMask = '************'

const platformAdminReason = '需要平台管理员角色'
const alertAdminReason = '需要告警管理员角色'

export const instanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/settings',
  component: InstanceSettingsPage,
})

/// 接入设置。四个区块：实例元数据、数据库凭据、Agent 接入（令牌与安装指引）、危险区。
///
/// 三个写操作的权限边界不一样，页面把它们分开说：元数据是告警管理员起步，凭据与 Agent
/// 只有平台管理员。**没有权限时看到的是禁用的控件加一句原因**，不是点下去才报错的控件。
function InstanceSettingsPage() {
  const { id } = instanceSettingsRoute.useParams()
  const instanceQuery = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const agentRegistrationQuery = $api.useQuery('get', '/api/v1/instances/{id}/agent/registration', { params: { path: { id } } })
  const registerAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/registration')
  const rotateAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/token/rotation')
  const revokeAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/token/revocation')
  const disableAgentMutation = $api.useMutation('post', '/api/v1/instances/{id}/agent/disable')
  const deleteInstanceMutation = $api.useMutation('delete', '/api/v1/instances/{id}')
  const navigate = instanceSettingsRoute.useNavigate()
  const [credentialModalOpen, setCredentialModalOpen] = useState(false)
  const [issuedAgentToken, setIssuedAgentToken] = useState<IssuedAgentToken | null>(null)
  const [actionError, setActionError] = useState('')
  const canEditMetadata = currentUserQuery.data?.role === 'ALERT_ADMIN' || currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const isPlatformAdmin = currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const agentActionPending = registerAgentMutation.isPending || rotateAgentMutation.isPending ||
    revokeAgentMutation.isPending || disableAgentMutation.isPending

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
    setActionError('')
    registerAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: (result) => showIssuedAgentToken(result, 'Agent 令牌签发响应无效'),
      onError: (failure) => setActionError(apiErrorMessage(failure, '登记 Agent 失败')),
    })
  }

  function rotateAgentToken() {
    setActionError('')
    rotateAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: (result) => showIssuedAgentToken(result, 'Agent 令牌轮换响应无效'),
      onError: (failure) => setActionError(apiErrorMessage(failure, '轮换 Agent 令牌失败')),
    })
  }

  function revokeAgentToken() {
    setActionError('')
    revokeAgentMutation.mutate({ params: { path: { id } } }, {
      onSuccess: refreshAgentRegistration,
      onError: (failure) => setActionError(apiErrorMessage(failure, '吊销 Agent 令牌失败')),
    })
  }

  function disableAgent() {
    setActionError('')
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

  function removeInstance() {
    setActionError('')
    deleteInstanceMutation.mutate({ params: { path: { id } } }, {
      onSuccess: () => void navigate({ to: '/instances' }),
      onError: (failure) => setActionError(apiErrorMessage(failure, '移除实例失败')),
    })
  }

  const instance = instanceQuery.data

  return (
    <div className="settings-page">
      <Link className="cds--link settings-page__back" to="/instances"><Icon name="arrowLeft" /> 返回实例列表</Link>

      <header className="settings-page__header">
        <h1 className="dbs-page-title">{instance?.name ?? '接入设置'}</h1>
        <p className="dbs-caption">接入设置</p>
      </header>

      {actionError !== '' && (
        <NotificationBar tone="critical" title={actionError} onClose={() => setActionError('')} />
      )}
      {instanceQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(instanceQuery.error, '实例信息加载失败')} />
      )}

      <Panel title="实例元数据" description="平台用什么地址连这台数据库。改动只影响之后的采集。">
        {instance === undefined
          ? <SkeletonBlock lines={4} label="实例元数据加载中" />
          : <InstanceMetadataSection
            instance={instance}
            canEdit={canEditMetadata}
            onSaved={() => void instanceQuery.refetch()}
          />}
      </Panel>

      <Panel
        title="数据库凭据"
        description="平台连接这台数据库用的账号。用户名不是秘密，密码是——密码只写入、不回传。"
        actions={<span title={isPlatformAdmin ? undefined : platformAdminReason}>
          <Button
            kind="tertiary"
            size="md"
            renderIcon={Icon.glyph.password}
            disabled={!isPlatformAdmin || instance === undefined}
            onClick={() => setCredentialModalOpen(true)}
          >更新凭据</Button>
        </span>}
      >
        {!isPlatformAdmin && <NotificationBar tone="info" title={platformAdminReason}>
          <p className="dbs-caption">当前角色可以看到用户名，但不能更新凭据。</p>
        </NotificationBar>}
        {instance === undefined
          ? <SkeletonBlock lines={2} label="数据库凭据加载中" />
          : <CredentialSummary username={instance.username} />}
      </Panel>

      <Panel
        title="Agent 接入"
        description="Agent 登记由显式签发令牌开始，只有停用 Agent 才会结束它；吊销令牌不结束登记。"
      >
        {agentRegistrationQuery.data === undefined
          ? <SkeletonBlock lines={4} label="Agent 接入状态加载中" />
          : <AgentRegistrationPanel
            registration={agentRegistrationQuery.data}
            canManage={isPlatformAdmin}
            actionPending={agentActionPending}
            onRegister={issueAgentToken}
            onRotate={rotateAgentToken}
            onRevoke={revokeAgentToken}
            onDisable={disableAgent}
          />}
      </Panel>

      <Panel
        className="settings-danger"
        title="危险区"
        description="这里的操作会立即生效，且没有撤销入口。"
      >
        <InstanceRemovalPanel
          instanceName={instance?.name ?? ''}
          canRemove={isPlatformAdmin}
          actionPending={deleteInstanceMutation.isPending}
          onRemove={removeInstance}
        />
      </Panel>

      {credentialModalOpen && instance !== undefined && (
        <CredentialModal
          instanceId={id}
          username={instance.username}
          onClose={() => setCredentialModalOpen(false)}
          onSaved={() => {
            setCredentialModalOpen(false)
            void instanceQuery.refetch()
          }}
        />
      )}
      <AgentTokenModal issued={issuedAgentToken} onClose={closeIssuedAgentToken} />
    </div>
  )
}

/// 元数据表单的校验规则。与生成的请求体类型对齐靠两处，漂了就编译不过：
/// `satisfies z.ZodType<InstanceMetadataInput>` 要求 schema 的出参就是请求体，
/// `metadataBody` 再把出参真的当请求体用。schema 里不写 `transform` / `default`。
const metadataSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入实例名称'),
  host: z.string().refine((value) => value.trim() !== '', '请输入主机地址'),
  port: z.number({ error: '请输入端口' }).int('端口必须是整数').min(1, '端口范围 1–65535').max(65535, '端口范围 1–65535'),
  // bootstrap database 只是建连接用的库名，不限定监控范围，所以它可以留空：
  // 留空时由服务端按引擎补默认库（PostgreSQL 是 postgres）。引擎本身接入后不可改，不在表单里。
  database: z.string(),
}) satisfies z.ZodType<InstanceMetadataInput>

type MetadataValues = z.infer<typeof metadataSchema>

/// 服务端字段错误只接受这四个 —— 每一个都有渲染出来的输入框可以聚焦。清单之外的字段名
/// 落回整表单的错误条；`setError` 一个表单里没有的名字会挂出一条永远显示不出来、
/// 也永远清不掉的错误。
const metadataFields = ['name', 'host', 'port', 'database'] as const satisfies readonly FieldPath<MetadataValues>[]

function metadataBody(values: MetadataValues): InstanceMetadataInput {
  const database = values.database.trim()
  return {
    name: values.name.trim(),
    host: values.host.trim(),
    port: values.port,
    // 留空就整个字段不发：默认库由服务端按引擎决定，前端不替它挑。
    ...(database === '' ? {} : { database }),
  }
}

function InstanceMetadataSection({ instance, canEdit, onSaved }: {
  instance: Instance
  canEdit: boolean
  onSaved: () => void
}) {
  const updateMetadata = $api.useMutation('put', '/api/v1/instances/{id}')
  const { control, formState, handleSubmit, register, setError } = useForm<MetadataValues>({
    resolver: zodResolver(metadataSchema),
    defaultValues: {
      name: instance.name,
      host: instance.host,
      port: instance.port,
      database: instance.database ?? '',
    },
  })
  const [failure, setFailure] = useState('')
  const disabledReason = canEdit ? undefined : alertAdminReason

  const submit = handleSubmit((values) => {
    setFailure('')
    updateMetadata.mutate({ params: { path: { id: instance.id } }, body: metadataBody(values) }, {
      onSuccess: onSaved,
      onError: (error) => {
        // 字段级错误落到对应输入框并聚焦第一个；一条都落不下时才退回整表单的错误条。
        if (applyApiFieldErrors<MetadataValues>(error, metadataFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存元数据失败'))
        }
      },
    })
  })

  return (
    <form className="settings-form" onSubmit={submit} noValidate>
      {failure !== '' && <NotificationBar tone="critical" title={failure} />}
      {!canEdit && <NotificationBar tone="info" title={alertAdminReason}>
        <p className="dbs-caption">当前角色可以查看元数据，但不能修改。</p>
      </NotificationBar>}

      <div className="settings-form__grid">
        <FormField label="名称" required errorText={formState.errors.name?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            disabled={!canEdit}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('name')}
          />}
        </FormField>
        {/* 引擎接入时选定、之后不可改：改引擎等于换了一台实例，历史数据不再成立。
            所以它在这里是一条只读事实，不是一个输入框。 */}
        <FormField label="引擎" helperText="接入时选定，不可更改。">
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            readOnly
            value={instanceEngineLabel(instance.engine)}
            aria-describedby={field.describedBy}
            onChange={() => undefined}
          />}
        </FormField>
        <FormField label="主机" required errorText={formState.errors.host?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            disabled={!canEdit}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('host')}
          />}
        </FormField>
        <FormField label="端口" required errorText={formState.errors.port?.message}>
          {(field) => <Controller
            name="port"
            control={control}
            render={({ field: port }) => <NumberInput
              id={field.id}
              label=""
              hideLabel
              min={1}
              max={65535}
              disabled={!canEdit}
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              ref={port.ref}
              name={port.name}
              value={port.value}
              onBlur={port.onBlur}
              // NumberInput 的取值在 onChange 的第二个参数里（加减按钮点的是按钮，不是输入框），
              // 所以这个字段走 Controller 而不是 register。空串是「清空了」，不是 0。
              onChange={(_event, state) => port.onChange(state.value === '' ? undefined : Number(state.value))}
            />}
          />}
        </FormField>
        <FormField
          label={bootstrapDatabaseLabel}
          helperText={bootstrapDatabaseHelperText}
          errorText={formState.errors.database?.message}
        >
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            placeholder="postgres"
            disabled={!canEdit}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('database')}
          />}
        </FormField>
      </div>

      <ConnectionAddress instance={instance} />

      <div className="settings-form__actions">
        <span title={disabledReason}>
          <Button
            type="submit"
            size="md"
            renderIcon={Icon.glyph.save}
            disabled={!canEdit || updateMetadata.isPending}
          >保存元数据</Button>
        </span>
      </div>
    </form>
  )
}

/// 连接地址。一串要粘到别处去的长文本，所以它是只读输入框加一个复制按钮，
/// 不是一行你得手动选中的文字。**里面没有口令**，密码不在界面上出现。
function ConnectionAddress({ instance }: { instance: Instance }) {
  // 库名跟在端点后面，是**建连接落在哪个库**，不是监控范围；没有 bootstrap 数据库
  // 的引擎（MySQL）就只有端点。
  const address = instance.database === undefined
    ? `${instance.host}:${instance.port}`
    : `${instance.host}:${instance.port}/${instance.database}`
  return (
    <FormField label="连接地址" helperText="已保存的地址，末段是建连接用的库，不是监控范围；不含账号与口令。">
      {(field) => (
        <div className="settings-copy-row">
          <TextInput
            id={field.id}
            className="settings-copy-row__value"
            labelText=""
            hideLabel
            readOnly
            value={address}
            aria-describedby={field.describedBy}
            // 只读输入框仍然是受控的：React 要一个 onChange，值永远来自实例。
            onChange={() => undefined}
          />
          <CopyButton
            iconDescription="复制连接地址"
            feedback="已复制"
            onClick={() => void navigator.clipboard.writeText(address)}
          />
        </div>
      )}
    </FormField>
  )
}

const credentialSchema = z.object({
  username: z.string().refine((value) => value.trim() !== '', '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
}) satisfies z.ZodType<InstanceCredentialInput>

type CredentialValues = z.infer<typeof credentialSchema>

const credentialFields = ['username', 'password'] as const satisfies readonly FieldPath<CredentialValues>[]

function credentialBody(values: CredentialValues): InstanceCredentialInput {
  return { username: values.username.trim(), password: values.password }
}

function CredentialModal({ instanceId, username, onClose, onSaved }: {
  instanceId: string
  username: string
  onClose: () => void
  onSaved: () => void
}) {
  const updateCredential = $api.useMutation('put', '/api/v1/instances/{id}/credentials')
  const { formState, handleSubmit, register, setError } = useForm<CredentialValues>({
    resolver: zodResolver(credentialSchema),
    defaultValues: { username, password: '' },
  })
  const [failure, setFailure] = useState('')

  const submit = handleSubmit((values) => {
    setFailure('')
    updateCredential.mutate({ params: { path: { id: instanceId } }, body: credentialBody(values) }, {
      onSuccess: onSaved,
      onError: (error) => {
        if (applyApiFieldErrors<CredentialValues>(error, credentialFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '更新凭据失败'))
        }
      },
    })
  })

  return (
    <Modal
      open
      modalHeading="更新 PG 凭据"
      primaryButtonText="连接测试并更新"
      secondaryButtonText="取消"
      primaryButtonDisabled={updateCredential.isPending}
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
      size="sm"
    >
      {/* Modal 的主按钮渲染在 children 之外，点它到不了这里的 onSubmit，所以提交口是
          `onRequestSubmit`；`<form>` 仍然留着，让回车提交与原生表单语义走同一个
          handleSubmit。主按钮**不能**是 type="submit"，那会提交两次。 */}
      <form className="settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <NotificationBar tone="info" title="平台会先用新凭据连一次，连不上就不会保存。" />
        <FormField label="新用户名" required errorText={formState.errors.username?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            autoComplete="off"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('username')}
          />}
        </FormField>
        <FormField label="新密码" required errorText={formState.errors.password?.message}>
          {(field) => <TextInput
            id={field.id}
            type="password"
            labelText=""
            hideLabel
            autoComplete="new-password"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('password')}
          />}
        </FormField>
      </form>
    </Modal>
  )
}

type InstanceRemovalPanelProps = {
  instanceName: string
  canRemove: boolean
  actionPending: boolean
  onRemove: () => void
}

/// 移除实例。CONTEXT.md 的定义就是确认框里那几句话：配置与凭据立即删除、未恢复的告警
/// 带原因关闭、样本按保留周期过期、重新接入是一个不继承任何东西的新实例。
///
/// 二次确认要求**手打实例名**，因为这条路没有回头：一个「确定吗」挡不住误点。
export function InstanceRemovalPanel({ instanceName, canRemove, actionPending, onRemove }: InstanceRemovalPanelProps) {
  const [open, setOpen] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const disabledReason = canRemove ? undefined : platformAdminReason

  function close() {
    setOpen(false)
    setConfirmation('')
  }

  return (
    <div className="settings-danger__zone">
      {!canRemove && <NotificationBar tone="info" title={platformAdminReason}>
        <p className="dbs-caption">当前角色可以查看这个实例，但不能移除它。</p>
      </NotificationBar>}

      <div className="settings-danger__action">
        <div className="settings-danger__text">
          <p className="dbs-body">移除实例</p>
          <p className="dbs-caption">
            删除配置与数据库凭据，关闭这个实例未恢复的告警并记录原因；已采集的样本按保留周期过期。
          </p>
        </div>
        <span title={disabledReason}>
          <Button
            kind="danger"
            size="md"
            renderIcon={Icon.glyph.trashCan}
            disabled={!canRemove || instanceName === ''}
            onClick={() => setOpen(true)}
          >移除实例</Button>
        </span>
      </div>

      {/* 确认框只在打开时挂载：Carbon 的 Modal 关着的时候 DOM 仍在，
          按可访问名找按钮会同时命中触发按钮与确认按钮。 */}
      {open && (
        <Modal
          open
          danger
          modalHeading={`移除 ${instanceName}`}
          primaryButtonText="确认移除"
          secondaryButtonText="取消"
          primaryButtonDisabled={confirmation !== instanceName || actionPending}
          onRequestSubmit={onRemove}
          onRequestClose={() => {
            if (!actionPending) close()
          }}
          onSecondarySubmit={() => {
            if (!actionPending) close()
          }}
          size="sm"
        >
          <div className="settings-confirm">
            <NotificationBar tone="warning" title="这一步不可撤销">
              <p className="dbs-caption">
                配置与数据库凭据立即删除；这个实例未恢复的告警会被关闭并记录原因；已采集的样本按保留周期过期。
                重新接入同一个数据库会得到一个新实例，不继承任何历史。
              </p>
            </NotificationBar>
            <FormField label={`输入实例名 ${instanceName} 以确认`}>
              {(field) => <TextInput
                id={field.id}
                labelText=""
                hideLabel
                aria-label="输入实例名确认移除"
                autoComplete="off"
                value={confirmation}
                aria-describedby={field.describedBy}
                onChange={(event) => setConfirmation(event.target.value)}
              />}
            </FormField>
          </div>
        </Modal>
      )}
    </div>
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

/// Agent 接入状态与它这一刻允许的操作。
///
/// 三件事在领域里是**三件不同的事**，界面必须一直说清楚是哪一件（CONTEXT.md）：
/// 吊销令牌保留登记，停用 Agent 结束登记但保留历史，移除实例才删配置 —— 所以
/// 每个按钮的二次确认写的都是它自己那件事的后果，没有一句通用的「确定吗」。
export function AgentRegistrationPanel({
  registration,
  canManage,
  actionPending,
  onRegister,
  onRotate,
  onRevoke,
  onDisable,
}: AgentRegistrationPanelProps) {
  const disabledReason = canManage ? undefined : platformAdminReason
  const statePresentation = agentStatePresentation(registration.state)

  let actions: ReactNode
  switch (registration.state) {
    case 'NEVER_REGISTERED':
      actions = <span title={disabledReason}>
        <Button
          size="md"
          renderIcon={Icon.glyph.plug}
          disabled={!canManage || actionPending}
          onClick={onRegister}
        >登记 Agent</Button>
      </span>
      break
    case 'EXPECTED_ONLINE':
      actions = <>
        <ConfirmedAgentAction
          label="轮换令牌"
          icon={Icon.glyph.renew}
          heading="轮换 Agent 令牌"
          description="签发一枚新令牌，当前令牌立即失效。接入登记不变，但在新令牌装到主机上之前，这个 Agent 会表现为掉线。新令牌只显示一次。"
          confirmLabel="轮换并签发新令牌"
          canManage={canManage}
          pending={actionPending}
          onConfirm={onRotate}
        />
        <ConfirmedAgentAction
          label="吊销令牌"
          icon={Icon.glyph.stop}
          destructive
          heading="吊销 Agent 令牌"
          description="令牌立即失效，Agent 无法再上报。接入登记保留——平台仍然期待这个 Agent 在线，所以在装上新令牌之前，实例会一直显示为 Agent 掉线。已采集的数据不受影响。"
          confirmLabel="吊销并保留登记"
          canManage={canManage}
          pending={actionPending}
          onConfirm={onRevoke}
        />
        <ConfirmedAgentAction
          label="停用 Agent"
          icon={Icon.glyph.power}
          destructive
          heading="停用 Agent"
          description="结束这个实例的 Agent 接入登记：平台不再期待它在线，只由 Agent 采集的指标与规则变为结构性不适用。已采集的主机样本保留不删。要重新接入需要重新登记并签发新令牌。"
          confirmLabel="停用并结束登记"
          canManage={canManage}
          pending={actionPending}
          onConfirm={onDisable}
        />
      </>
      break
    case 'REVOKED':
      actions = <ConfirmedAgentAction
        label="停用 Agent"
        icon={Icon.glyph.power}
        destructive
        heading="停用 Agent"
        description="令牌已经吊销，但接入登记还在，实例仍按「应有 Agent」计算。停用会结束登记：只由 Agent 采集的指标与规则变为结构性不适用，已采集的主机样本保留不删。"
        confirmLabel="停用并结束登记"
        canManage={canManage}
        pending={actionPending}
        onConfirm={onDisable}
      />
      break
    case 'DISABLED':
      actions = <span title={disabledReason}>
        <Button
          size="md"
          renderIcon={Icon.glyph.plug}
          disabled={!canManage || actionPending}
          onClick={onRegister}
        >重新启用 Agent</Button>
      </span>
      break
    default:
      actions = assertNever(registration.state)
  }

  return (
    <div className="settings-agent">
      {!canManage && <NotificationBar tone="info" title={platformAdminReason}>
        <p className="dbs-caption">当前角色可以查看接入状态，但不能登记、轮换、吊销或停用。</p>
      </NotificationBar>}

      <KeyValueList
        label="Agent 接入状态"
        columns={3}
        items={[
          { key: 'state', label: '登记状态', value: <StatusDot tone={statePresentation.tone}>{statePresentation.label}</StatusDot> },
          { key: 'expected', label: '期待在线', value: registration.agent_expected ? '是' : '否' },
          { key: 'first', label: '首次登记', value: formatOptionalTime(registration.first_registered_at) },
          { key: 'issued', label: '最近签发', value: formatOptionalTime(registration.issued_at) },
          { key: 'revoked', label: '最近吊销', value: formatOptionalTime(registration.revoked_at) },
          { key: 'token', label: '令牌文件', value: <span className="dbs-numeric">{registration.installation.authentication_path}</span> },
          { key: 'mode', label: '文件权限', value: <span className="dbs-numeric">{registration.installation.file_mode}</span> },
          { key: 'restart', label: '重启命令', value: <span className="dbs-numeric">{registration.installation.restart_command}</span> },
        ]}
      />

      {/* 指纹是 64 个十六进制字符，没有人应该手抄它 —— 它和令牌一样，只读框加一个复制按钮。
          安装指引在这里常驻，不只出现在签发令牌那一次：重装时要的就是这几行。 */}
      <FormField label="平台 CA 指纹（SHA-256）" helperText="安装脚本用它钉住平台证书，装 Agent 时要核对的就是这一串。">
        {(field) => (
          <div className="settings-copy-row">
            <TextInput
              id={field.id}
              className="settings-copy-row__value"
              labelText=""
              hideLabel
              readOnly
              value={registration.installation.ca_fingerprint_sha256}
              aria-describedby={field.describedBy}
              onChange={() => undefined}
            />
            <CopyButton
              iconDescription="复制 CA 指纹"
              feedback="已复制"
              onClick={() => void navigator.clipboard.writeText(registration.installation.ca_fingerprint_sha256)}
            />
          </div>
        )}
      </FormField>

      <div className="settings-agent__actions">{actions}</div>
    </div>
  )
}

/// Agent 的破坏性操作：按钮 + 二次确认。确认框的正文写的是**这一件事**的后果，
/// 不是一句通用的「确定吗」—— 吊销与停用在领域里差得很远，读者必须能分清。
function ConfirmedAgentAction({
  label,
  icon,
  destructive = false,
  heading,
  description,
  confirmLabel,
  canManage,
  pending,
  onConfirm,
}: {
  label: string
  icon: Glyph
  destructive?: boolean
  heading: string
  description: string
  confirmLabel: string
  canManage: boolean
  pending: boolean
  onConfirm: () => void
}) {
  const [open, setOpen] = useState(false)
  const disabledReason = canManage ? undefined : platformAdminReason

  return (
    <>
      <span title={disabledReason}>
        <Button
          kind={destructive ? 'danger--tertiary' : 'tertiary'}
          size="md"
          renderIcon={icon}
          disabled={!canManage || pending}
          onClick={() => setOpen(true)}
        >{label}</Button>
      </span>
      {open && (
        <Modal
          open
          danger={destructive}
          modalHeading={heading}
          primaryButtonText={confirmLabel}
          secondaryButtonText="取消"
          primaryButtonDisabled={pending}
          onRequestSubmit={() => {
            setOpen(false)
            onConfirm()
          }}
          onRequestClose={() => setOpen(false)}
          onSecondarySubmit={() => setOpen(false)}
          size="sm"
        >
          <p className="dbs-body">{description}</p>
        </Modal>
      )}
    </>
  )
}

/// 一次性令牌与安装指引。
///
/// **令牌只从服务端回来这一次**，关掉就再也取不回来（要再拿只能再轮换一次，那是另一次
/// 写操作），所以这里的每一处都为「看清楚并复制走」服务：一条说明它只显示一次的提示条、
/// 两个只读输入框、两个复制按钮。
///
/// 组件自己不留任何令牌状态：显示什么完全由传进来的 `issued` 决定，`null` 就不挂载，
/// DOM 里再也没有那串字符 —— 这一条有单元测试盯着。
export function AgentTokenModal({ issued, onClose }: { issued: IssuedAgentToken | null; onClose: () => void }) {
  if (issued === null) return null

  const token = issued.token
  const command = buildAgentInstallCommand(window.location.origin, issued.instanceId, issued.token, issued.registration)

  return (
    <Modal
      open
      modalHeading="Agent 令牌与安装"
      primaryButtonText="关闭"
      onRequestSubmit={onClose}
      onRequestClose={onClose}
      size="lg"
    >
      <div className="settings-token">
        <NotificationBar tone="warning" title="令牌仅显示一次，关闭后不再显示" />

        <FormField label="Agent 令牌">
          {(field) => (
            <div className="settings-copy-row">
              <TextInput
                id={field.id}
                className="settings-copy-row__value"
                labelText=""
                hideLabel
                readOnly
                value={token}
                aria-describedby={field.describedBy}
                onChange={() => undefined}
              />
              <CopyButton
                iconDescription="复制 Agent 令牌"
                feedback="已复制"
                onClick={() => void navigator.clipboard.writeText(token)}
              />
            </div>
          )}
        </FormField>

        <FormField label="Agent 安装命令" helperText="在目标主机上以 root 执行。命令里已经钉住平台 CA 指纹，没有跳过校验的退路。">
          {(field) => (
            <div className="settings-copy-row">
              <TextArea
                id={field.id}
                className="settings-copy-row__value"
                labelText=""
                hideLabel
                readOnly
                rows={8}
                value={command}
                aria-describedby={field.describedBy}
                onChange={() => undefined}
              />
              <CopyButton
                iconDescription="复制 Agent 安装命令"
                feedback="已复制"
                onClick={() => void navigator.clipboard.writeText(command)}
              />
            </div>
          )}
        </FormField>

        <KeyValueList
          label="安装位置"
          columns={3}
          items={[
            { key: 'token', label: '令牌文件', value: <span className="dbs-numeric">{issued.registration.installation.authentication_path}</span> },
            { key: 'mode', label: '文件权限', value: <span className="dbs-numeric">{issued.registration.installation.file_mode}</span> },
            { key: 'restart', label: '重启命令', value: <span className="dbs-numeric">{issued.registration.installation.restart_command}</span> },
          ]}
        />
      </div>
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

function agentStatePresentation(state: AgentRegistrationState): { label: string; tone: StatusTone } {
  switch (state) {
    case 'NEVER_REGISTERED': return { label: '从未登记', tone: 'unknown' }
    case 'EXPECTED_ONLINE': return { label: '应在线', tone: 'normal' }
    case 'REVOKED': return { label: '已吊销', tone: 'critical' }
    case 'DISABLED': return { label: '已停用', tone: 'unknown' }
    default: return assertNever(state)
  }
}

function formatOptionalTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function assertNever(value: never): never {
  throw new Error(`unhandled value: ${value}`)
}

/// PG 凭据的现状。用户名照实显示（它不是秘密），密码永远只是一串固定掩码 ——
/// 服务端不回传密码，所以这里**没有**、也不该有「显示密码」的按钮。
export function CredentialSummary({ username }: { username: string }) {
  return (
    <KeyValueList
      label="PG 凭据"
      columns={3}
      items={[
        { key: 'username', label: '用户名', value: <span className="dbs-numeric">{username}</span> },
        {
          key: 'password',
          label: '密码',
          value: <span className="settings-credential__password">
            <TextInput
              id="settings-credential-password"
              className="settings-credential__mask"
              labelText=""
              hideLabel
              aria-label="密码状态"
              type="password"
              value={passwordMask}
              readOnly
              onChange={() => undefined}
            />
            <span className="dbs-caption">已设置</span>
          </span>,
        },
      ]}
    />
  )
}
