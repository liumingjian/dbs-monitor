import { Button, PasswordInput, Select, SelectItem, TextInput } from '@carbon/react'
import { useEffect, useId, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import type { components } from '../../api/schema'
import { zodResolver } from '../../forms/zodResolver'
import { DataGrid } from '../../primitives/DataGrid'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { Modal } from '../../primitives/Modal'
import { NotificationBar } from '../../primitives/NotificationBar'
import { NumberInput } from '../../primitives/NumberInput'
import { Panel } from '../../primitives/Panel'
import { StatusBadge } from '../../primitives/StatusBadge'
import { Toggle } from '../../primitives/Toggle'
import { TruncatedText } from '../../primitives/TruncatedText'
import { ConfirmedAction, InlineAction } from './ConfirmedAction'
import { emailPattern, readOnlyReason } from './shared'
import type { Feedback } from './shared'

type SMTPChannelInput = components['schemas']['SMTPChannelInput']
type SMTPAuthType = components['schemas']['SMTPAuthType']
type SMTPTransportSecurity = components['schemas']['SMTPTransportSecurity']
type SMTPTestInput = components['schemas']['SMTPTestInput']
type WebhookTargetInput = components['schemas']['WebhookTargetInput']
type WebhookTarget = components['schemas']['WebhookTarget']
type ChannelFailureSummary = components['schemas']['ChannelFailureSummary']
type ChannelFailureRecord = components['schemas']['ChannelFailureRecord']

type WebhookTargetsTableProps = {
  targets: WebhookTarget[]
  failures: ChannelFailureSummary[]
  canManage: boolean
  onEdit: (target: WebhookTarget) => void
  onDelete: (target: WebhookTarget) => void
  onTest: (target: WebhookTarget) => void
  loading?: boolean
}

const authTypes = ['NONE', 'PLAIN', 'LOGIN'] as const satisfies readonly SMTPAuthType[]
const transportSecurities = ['STARTTLS', 'IMPLICIT'] as const satisfies readonly SMTPTransportSecurity[]

function authTypeLabel(value: SMTPAuthType): string {
  switch (value) {
    case 'NONE':
      return '无认证'
    case 'PLAIN':
      return 'PLAIN'
    case 'LOGIN':
      return 'LOGIN'
    default:
      return assertNever(value)
  }
}

function transportSecurityLabel(value: SMTPTransportSecurity): string {
  switch (value) {
    case 'STARTTLS':
      return 'STARTTLS'
    case 'IMPLICIT':
      return '隐式 TLS'
    default:
      return assertNever(value)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected notification channel value: ${String(value)}`)
}

// ---------------------------------------------------------------------------
// SMTP
// ---------------------------------------------------------------------------

const smtpSchema = z.object({
  enabled: z.boolean(),
  host: z.string().refine((value) => value.trim() !== '', '请输入服务器地址'),
  port: z.number({ error: '请输入端口' }).int('端口必须是整数').min(1, '端口范围 1–65535').max(65535, '端口范围 1–65535'),
  from_address: z.string().regex(emailPattern, '请输入有效的发件人邮箱'),
  recipient: z.string().regex(emailPattern, '请输入有效的默认收件人邮箱'),
  tls_mode: z.enum(transportSecurities),
  auth_type: z.enum(authTypes),
  username: z.string(),
  password: z.string(),
})

type SMTPValues = z.infer<typeof smtpSchema>

const smtpFields = [
  'enabled',
  'host',
  'port',
  'from_address',
  'recipient',
  'tls_mode',
  'auth_type',
  'username',
  'password',
] as const satisfies readonly FieldPath<SMTPValues>[]

/// 表单值 → 请求体。`satisfies` 盯不住形状不同的两侧，所以这里用返回类型把生成的
/// 请求体类型真的用出去：字段漂了就编译不过。
export function smtpChannelBody(values: SMTPValues): SMTPChannelInput {
  const body: SMTPChannelInput = {
    enabled: values.enabled,
    host: values.host.trim(),
    port: values.port,
    from_address: values.from_address.trim(),
    recipient: values.recipient.trim(),
    auth_type: values.auth_type,
    tls_mode: values.tls_mode,
  }
  if (values.auth_type !== 'NONE') {
    body.username = values.username.trim()
    // 留空即保持原值，接口语义如此；空串会把已存的凭据擦掉。
    if (values.password !== '') body.password = values.password
  }
  return body
}

const emptySMTPValues: SMTPValues = {
  enabled: false,
  host: '',
  port: 587,
  from_address: '',
  recipient: '',
  tls_mode: 'STARTTLS',
  auth_type: 'PLAIN',
  username: '',
  password: '',
}

function SMTPSection({ canManage }: { canManage: boolean }) {
  const smtpQuery = $api.useQuery('get', '/api/v1/notification-channels/smtp')
  const failureQuery = $api.useQuery('get', '/api/v1/notification-channels/failures')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-channels/smtp')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const { control, formState, handleSubmit, register, reset, setError, watch } = useForm<SMTPValues>({
    resolver: zodResolver(smtpSchema),
    defaultValues: emptySMTPValues,
  })
  const authType = watch('auth_type')
  const channel = smtpQuery.data

  // 已保存的配置回填表单。`reset` 而不是逐字段 setValue：回填之后表单应当是「未修改」的。
  useEffect(() => {
    if (channel === undefined || !channel.configured) return
    reset({
      enabled: channel.enabled ?? false,
      host: channel.host ?? '',
      port: channel.port ?? emptySMTPValues.port,
      from_address: channel.from_address ?? '',
      recipient: channel.recipient ?? '',
      tls_mode: channel.tls_mode ?? emptySMTPValues.tls_mode,
      auth_type: channel.auth_type ?? emptySMTPValues.auth_type,
      username: channel.username ?? '',
      password: '',
    })
  }, [channel, reset])

  const submit = handleSubmit((values) => {
    setFeedback(null)
    updateMutation.mutate({ body: smtpChannelBody(values) }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: 'SMTP 配置已保存' })
        reset({ ...values, password: '' })
        void smtpQuery.refetch()
      },
      onError: (error) => {
        if (applyApiFieldErrors<SMTPValues>(error, smtpFields, setError).length === 0) {
          setFeedback({ tone: 'critical', text: apiErrorMessage(error, '保存 SMTP 配置失败') })
        }
      },
    })
  })

  const smtpFailure = failureQuery.data?.channels.find((summary) => summary.channel === 'SMTP')

  return (
    <Panel title="SMTP" description="邮件通知的发信通道。凭据只写不读，留空即保持原值。" loading={smtpQuery.isPending}>
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {feedback !== null && (
          <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
        )}
        <FormField label="启用" errorText={formState.errors.enabled?.message}>
          {(field) => <Controller
            name="enabled"
            control={control}
            render={({ field: enabled }) => <Toggle
              id={field.id}
              size="sm"
              labelText=""
              hideLabel
              labelA="已停用"
              labelB="已启用"
              disabled={!canManage}
              toggled={enabled.value}
              onToggle={(next) => enabled.onChange(next)}
            />}
          />}
        </FormField>
        <div className="alert-settings-form__row">
          <FormField label="服务器" required errorText={formState.errors.host?.message}>
            {(field) => <TextInput
              id={field.id}
              labelText=""
              hideLabel
              disabled={!canManage}
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
                disabled={!canManage}
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                ref={port.ref}
                name={port.name}
                value={port.value}
                onBlur={port.onBlur}
                // 取值在 onChange 的第二个参数里，所以走 Controller 而不是 register。
                onChange={(_event, state) => port.onChange(state.value === '' ? undefined : Number(state.value))}
              />}
            />}
          </FormField>
        </div>
        <FormField label="发件人" required errorText={formState.errors.from_address?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            type="email"
            disabled={!canManage}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('from_address')}
          />}
        </FormField>
        <FormField label="默认收件人" required errorText={formState.errors.recipient?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            type="email"
            disabled={!canManage}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('recipient')}
          />}
        </FormField>
        <div className="alert-settings-form__row">
          <FormField label="传输安全" required errorText={formState.errors.tls_mode?.message}>
            {(field) => <Select
              id={field.id}
              labelText=""
              noLabel
              disabled={!canManage}
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              {...register('tls_mode')}
            >
              {transportSecurities.map((value) => (
                <SelectItem key={value} value={value} text={transportSecurityLabel(value)} />
              ))}
            </Select>}
          </FormField>
          <FormField label="认证方式" required errorText={formState.errors.auth_type?.message}>
            {(field) => <Select
              id={field.id}
              labelText=""
              noLabel
              disabled={!canManage}
              invalid={field.invalid}
              aria-describedby={field.describedBy}
              {...register('auth_type')}
            >
              {authTypes.map((value) => (
                <SelectItem key={value} value={value} text={authTypeLabel(value)} />
              ))}
            </Select>}
          </FormField>
        </div>
        {authType !== 'NONE' && (
          <>
            <FormField label="用户名" required errorText={formState.errors.username?.message}>
              {(field) => <TextInput
                id={field.id}
                labelText=""
                hideLabel
                autoComplete="off"
                disabled={!canManage}
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                {...register('username')}
              />}
            </FormField>
            <FormField
              label="认证信息"
              helperText={channel?.auth_configured === true ? '已设置。留空保持不变。' : '未设置。'}
              errorText={formState.errors.password?.message}
            >
              {(field) => <PasswordInput
                id={field.id}
                labelText=""
                hideLabel
                autoComplete="new-password"
                showPasswordLabel="显示认证信息"
                hidePasswordLabel="隐藏认证信息"
                disabled={!canManage}
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                placeholder={channel?.auth_configured === true ? '留空保持不变' : '请输入认证信息'}
                {...register('password')}
              />}
            </FormField>
          </>
        )}
        <div className="alert-settings-actions">
          <span title={canManage ? undefined : readOnlyReason.channels}>
            <Button type="submit" size="md" renderIcon={SaveIcon} disabled={!canManage || updateMutation.isPending}>
              保存
            </Button>
          </span>
        </div>
      </form>
      <SMTPTestForm canManage={canManage} defaultTarget={channel?.recipient ?? ''} />
      <ChannelFailureDetails summary={smtpFailure} regionLabel="SMTP" />
    </Panel>
  )
}

function SMTPTestForm({ canManage, defaultTarget }: { canManage: boolean; defaultTarget: string }) {
  const testMutation = $api.useMutation('post', '/api/v1/notification-channels/smtp/test')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const { formState, handleSubmit, register, reset, setError } = useForm<SMTPTestValues>({
    resolver: zodResolver(smtpTestSchema),
    defaultValues: { target: defaultTarget },
  })

  useEffect(() => {
    reset({ target: defaultTarget })
  }, [defaultTarget, reset])

  const submit = handleSubmit((values) => {
    setFeedback(null)
    testMutation.mutate({ body: smtpTestBody(values) }, {
      onSuccess: () => setFeedback({ tone: 'normal', text: 'SMTP 测试通知已进入发送队列' }),
      onError: (error) => {
        if (applyApiFieldErrors<SMTPTestValues>(error, smtpTestFields, setError).length === 0) {
          setFeedback({ tone: 'critical', text: apiErrorMessage(error, 'SMTP 测试通知发送失败') })
        }
      },
    })
  })

  return (
    <form className="alert-settings-form alert-settings-form--inline" onSubmit={submit} noValidate>
      <h3 className="dbs-panel-title">测试发送</h3>
      {feedback !== null && (
        <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
      )}
      <div className="alert-settings-form__row alert-settings-form__row--bottom">
        <FormField label="收件人" required errorText={formState.errors.target?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            type="email"
            disabled={!canManage}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('target')}
          />}
        </FormField>
        <span title={canManage ? undefined : readOnlyReason.channels}>
          <Button type="submit" size="md" kind="tertiary" renderIcon={SendIcon} disabled={!canManage || testMutation.isPending}>
            发送测试
          </Button>
        </span>
      </div>
    </form>
  )
}

const smtpTestSchema = z.object({
  target: z.string().regex(emailPattern, '请输入有效的收件人邮箱'),
})

type SMTPTestValues = z.infer<typeof smtpTestSchema>

const smtpTestFields = ['target'] as const satisfies readonly FieldPath<SMTPTestValues>[]

function smtpTestBody(values: SMTPTestValues): SMTPTestInput {
  return { target: values.target.trim() }
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

const webhookSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入名称'),
  enabled: z.boolean(),
  url: z.string().refine((value) => /^https?:\/\/\S+$/.test(value.trim()), '请输入以 http(s):// 开头的地址'),
  signing_value: z.string(),
  signature_header: z.string(),
})

type WebhookValues = z.infer<typeof webhookSchema>

const webhookFields = [
  'name',
  'enabled',
  'url',
  'signing_value',
  'signature_header',
] as const satisfies readonly FieldPath<WebhookValues>[]

export function webhookTargetBody(values: WebhookValues): WebhookTargetInput {
  const body: WebhookTargetInput = {
    name: values.name.trim(),
    enabled: values.enabled,
    url: values.url.trim(),
  }
  // 新建时必填、编辑时留空即保持原值，所以空串是「不改」而不是「清空」。
  if (values.signing_value !== '') body.signing_value = values.signing_value
  if (values.signature_header !== '') body.signature_header = values.signature_header.trim()
  return body
}

function WebhookModal({ target, onClose, onSaved }: {
  target: WebhookTarget | null
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/notification-channels/webhooks')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-channels/webhooks/{id}')
  const [failure, setFailure] = useState('')
  const editing = target !== null
  const { control, formState, handleSubmit, register, setError } = useForm<WebhookValues>({
    resolver: zodResolver(editing ? webhookSchema : webhookCreateSchema),
    defaultValues: {
      name: target?.name ?? '',
      enabled: target?.enabled ?? true,
      url: target?.url ?? '',
      signing_value: '',
      signature_header: '',
    },
  })

  const submit = handleSubmit((values) => {
    setFailure('')
    const options = {
      onSuccess: () => onSaved(editing ? 'Webhook 目标已更新' : 'Webhook 目标已创建'),
      onError: (error: unknown) => {
        if (applyApiFieldErrors<WebhookValues>(error, webhookFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存 Webhook 目标失败'))
        }
      },
    }
    if (target !== null) {
      updateMutation.mutate({ params: { path: { id: target.id } }, body: webhookTargetBody(values) }, options)
      return
    }
    createMutation.mutate({ body: webhookTargetBody(values) }, options)
  })

  return (
    <Modal
      open
      modalHeading={editing ? '编辑 Webhook 目标' : '新建 Webhook 目标'}
      primaryButtonText="保存目标"
      secondaryButtonText="取消"
      primaryButtonDisabled={createMutation.isPending || updateMutation.isPending}
      size="sm"
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
    >
      {/* Modal 的主按钮在 children 之外，点它到不了这里的 onSubmit —— 提交口是
          `onRequestSubmit`；<form> 仍然留着，让回车走同一个 handleSubmit。 */}
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        <FormField label="启用" errorText={formState.errors.enabled?.message}>
          {(field) => <Controller
            name="enabled"
            control={control}
            render={({ field: enabled }) => <Toggle
              id={field.id}
              size="sm"
              labelText=""
              hideLabel
              labelA="已停用"
              labelB="已启用"
              toggled={enabled.value}
              onToggle={(next) => enabled.onChange(next)}
            />}
          />}
        </FormField>
        <FormField label="名称" required errorText={formState.errors.name?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('name')}
          />}
        </FormField>
        <FormField label="URL" required errorText={formState.errors.url?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            placeholder="https://gateway.example.com/alerts"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('url')}
          />}
        </FormField>
        <FormField
          label="签名密钥"
          required={!editing}
          helperText={target?.signing_configured === true ? '已设置。留空保持不变。' : '未设置。'}
          errorText={formState.errors.signing_value?.message}
        >
          {(field) => <PasswordInput
            id={field.id}
            labelText=""
            hideLabel
            autoComplete="new-password"
            showPasswordLabel="显示签名密钥"
            hidePasswordLabel="隐藏签名密钥"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            placeholder={editing ? '留空保持不变' : undefined}
            {...register('signing_value')}
          />}
        </FormField>
        <FormField
          label="签名头"
          required={!editing}
          errorText={formState.errors.signature_header?.message}
        >
          {(field) => <PasswordInput
            id={field.id}
            labelText=""
            hideLabel
            autoComplete="new-password"
            showPasswordLabel="显示签名头"
            hidePasswordLabel="隐藏签名头"
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            placeholder={editing ? '留空保持不变' : 'X-DBS-Signature'}
            {...register('signature_header')}
          />}
        </FormField>
      </form>
    </Modal>
  )
}

/// 新建时签名密钥与签名头是必填的；编辑时留空表示保持原值。两条规则的差别只在这里。
const webhookCreateSchema = webhookSchema.extend({
  signing_value: z.string().min(1, '请输入签名密钥'),
  signature_header: z.string().refine((value) => value.trim() !== '', '请输入签名头'),
})

export function WebhookTargetsTable({
  targets,
  failures,
  canManage,
  onEdit,
  onDelete,
  onTest,
  loading = false,
}: WebhookTargetsTableProps) {
  // 失败明细展开在表格**下方**而不是行内：单元格是 40px 定高 + 省略号截断的，
  // 在里面展开一段列表只会被裁掉。
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const regionID = useId()
  const failureOf = (target: WebhookTarget) =>
    failures.find((summary) => summary.channel === 'WEBHOOK' && summary.target_id === target.id)
  const expanded = targets.find((target) => target.id === expandedID)
  const expandedFailure = expanded === undefined ? undefined : failureOf(expanded)

  const columns: DataGridColumn<WebhookTarget>[] = [
    {
      key: 'name',
      header: '名称',
      minWidth: 150,
      grow: 1.4,
      cell: (target) => <TruncatedText className="alert-settings-strong">{target.name}</TruncatedText>,
    },
    {
      key: 'url',
      header: '地址',
      minWidth: 200,
      cell: (target) => (
        <a className="cds--link alert-settings-cell-link" href={target.url} target="_blank" rel="noreferrer" title={target.url}>
          {target.url}
        </a>
      ),
    },
    {
      key: 'enabled',
      header: '状态',
      minWidth: 88,
      grow: 1.5,
      cell: (target) => (
        <StatusBadge tone={target.enabled ? 'normal' : 'unknown'}>{target.enabled ? '已启用' : '已停用'}</StatusBadge>
      ),
    },
    {
      key: 'signing',
      header: '签名',
      minWidth: 104,
      grow: 1.4,
      cell: (target) => (
        <span className="alert-settings-muted">{target.signing_configured ? '签名已设置' : '签名未设置'}</span>
      ),
    },
    {
      key: 'failures',
      header: '最近失败',
      minWidth: 132,
      grow: 1.3,
      cell: (target) => {
        const summary = failureOf(target)
        if (summary === undefined) return <span className="alert-settings-muted">最近无失败</span>
        return (
          <Button
            kind="ghost"
            size="sm"
            aria-expanded={expandedID === target.id}
            aria-controls={regionID}
            onClick={() => setExpandedID(expandedID === target.id ? null : target.id)}
          >
            {`最近失败 ${summary.recent_failure_count} 次`}
          </Button>
        )
      },
    },
    {
      key: 'reason',
      header: '最近失败原因',
      minWidth: 150,
      cell: (target) => {
        const summary = failureOf(target)
        return summary === undefined
          ? <span className="alert-settings-muted">—</span>
          : <TruncatedText>{summary.last_failure_reason}</TruncatedText>
      },
    },
    {
      key: 'actions',
      header: '操作',
      minWidth: 128,
      grow: 1.6,
      align: 'end',
      cell: (target) => (
        <span className="alert-settings-row-actions">
          <InlineAction
            name={`测试 ${target.name}`}
            icon="send"
            disabled={!canManage || !target.enabled}
            disabledReason={canManage ? '目标已停用，无法发送测试请求' : readOnlyReason.channels}
            onClick={() => onTest(target)}
          />
          <InlineAction
            name={`编辑 ${target.name}`}
            icon="edit"
            disabled={!canManage}
            disabledReason={readOnlyReason.channels}
            onClick={() => onEdit(target)}
          />
          <ConfirmedAction
            name={`删除 ${target.name}`}
            icon="trashCan"
            destructive
            heading="删除 Webhook 目标"
            description={`删除后 ${target.name} 不再接收任何通知，引用它的通知策略会失去这个渠道。此操作不可撤销。`}
            confirmLabel="删除目标"
            disabled={!canManage}
            disabledReason={readOnlyReason.channels}
            onConfirm={() => onDelete(target)}
          />
        </span>
      ),
    },
  ]

  return (
    <>
      <DataGrid<WebhookTarget>
        label="Webhook 目标"
        loading={loading}
        rows={targets}
        rowKey={(target) => target.id}
        rowTestId="webhook-target-row"
        rowTone={(target) => (failureOf(target) === undefined ? undefined : 'critical')}
        columns={columns}
        empty={{ title: '暂无 Webhook 目标', description: '新建一个目标，把告警推送到外部网关。' }}
      />
      <div id={regionID}>
        {expanded !== undefined && expandedFailure !== undefined && (
          <FailureRecords records={expandedFailure.recent_failures} regionLabel={expanded.name} />
        )}
      </div>
    </>
  )
}

/// 一个渠道的失败摘要 + 可展开的明细。表格之外的地方（SMTP）用它，表格里的展开由
/// `WebhookTargetsTable` 自己管，因为明细要落在表格外面。
export function ChannelFailureDetails({ summary, regionLabel }: {
  summary?: ChannelFailureSummary
  regionLabel: string
}) {
  const [open, setOpen] = useState(false)
  const regionID = useId()
  if (summary === undefined) {
    return <p className="alert-settings-muted dbs-caption">最近无失败</p>
  }
  return (
    <div className="alert-settings-failures">
      <Button kind="ghost" size="sm" aria-expanded={open} aria-controls={regionID} onClick={() => setOpen(!open)}>
        {`最近失败 ${summary.recent_failure_count} 次`}
      </Button>
      <p className="alert-settings-muted dbs-caption">{summary.last_failure_reason}</p>
      <div id={regionID}>
        {open && <FailureRecords records={summary.recent_failures} regionLabel={regionLabel} />}
      </div>
    </div>
  )
}

function FailureRecords({ records, regionLabel }: { records: ChannelFailureRecord[]; regionLabel: string }) {
  return (
    <div className="alert-settings-failures__records">
      <DataGrid<ChannelFailureRecord>
        label={`${regionLabel} 最近失败记录`}
        rows={records}
        rowKey={(record) => `${record.failed_at}-${record.target}`}
        rowTestId="channel-failure-row"
        density="dense"
        columns={[
          { key: 'failed_at', header: '时间', minWidth: 160, cell: (record) => formatTime(record.failed_at) },
          { key: 'target', header: '目标', minWidth: 180, cell: (record) => <TruncatedText>{record.target}</TruncatedText> },
          { key: 'reason', header: '原因', minWidth: 200, cell: (record) => <TruncatedText>{record.reason}</TruncatedText> },
          { key: 'retry_count', header: '重试次数', minWidth: 96, numeric: true, cell: (record) => record.retry_count },
        ]}
        empty={{ title: '暂无失败记录' }}
      />
    </div>
  )
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function SaveIcon() {
  return <Icon name="save" />
}

function SendIcon() {
  return <Icon name="send" />
}

function AddIcon() {
  return <Icon name="add" />
}

// ---------------------------------------------------------------------------
// 标签页
// ---------------------------------------------------------------------------

/// 「通知渠道」标签：SMTP 通道 + Webhook 目标。
export function NotificationChannelsPanel({ canManage }: { canManage: boolean }) {
  const webhookQuery = $api.useQuery('get', '/api/v1/notification-channels/webhooks')
  const failureQuery = $api.useQuery('get', '/api/v1/notification-channels/failures')
  const deleteMutation = $api.useMutation('delete', '/api/v1/notification-channels/webhooks/{id}')
  const testMutation = $api.useMutation('post', '/api/v1/notification-channels/webhooks/{id}/test')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const [editorTarget, setEditorTarget] = useState<WebhookTarget | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)

  function openEditor(target: WebhookTarget | null) {
    setEditorTarget(target)
    setEditorOpen(true)
  }

  function deleteTarget(target: WebhookTarget) {
    setFeedback(null)
    deleteMutation.mutate({ params: { path: { id: target.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: 'Webhook 目标已删除' })
        void webhookQuery.refetch()
        void failureQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '删除 Webhook 目标失败') }),
    })
  }

  function testTarget(target: WebhookTarget) {
    setFeedback(null)
    testMutation.mutate({ params: { path: { id: target.id } } }, {
      onSuccess: () => setFeedback({ tone: 'normal', text: `${target.name} 测试请求已进入发送队列` }),
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, 'Webhook 测试请求失败') }),
    })
  }

  return (
    <div className="alert-settings-stack">
      <SMTPSection canManage={canManage} />
      {feedback !== null && (
        <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
      )}
      {webhookQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(webhookQuery.error, 'Webhook 目标加载失败')} />
      )}
      <Panel
        flush
        title="Webhook"
        actions={<span title={canManage ? undefined : readOnlyReason.channels}>
          <Button size="sm" renderIcon={AddIcon} disabled={!canManage} onClick={() => openEditor(null)}>
            新建目标
          </Button>
        </span>}
      >
        <WebhookTargetsTable
          targets={webhookQuery.data ?? []}
          failures={failureQuery.data?.channels ?? []}
          canManage={canManage}
          loading={webhookQuery.isPending}
          onEdit={(target) => openEditor(target)}
          onDelete={deleteTarget}
          onTest={testTarget}
        />
      </Panel>
      {editorOpen && (
        <WebhookModal
          target={editorTarget}
          onClose={() => setEditorOpen(false)}
          onSaved={(message) => {
            setEditorOpen(false)
            setFeedback({ tone: 'normal', text: message })
            void webhookQuery.refetch()
          }}
        />
      )}
    </div>
  )
}
