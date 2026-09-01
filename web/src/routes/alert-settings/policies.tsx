import { Button, Checkbox, TextInput } from '@carbon/react'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import type { components } from '../../api/schema'
import { zodResolver } from '../../forms/zodResolver'
import { DataGrid } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { Modal } from '../../primitives/Modal'
import { MultiSelect } from '../../primitives/MultiSelect'
import { NotificationBar } from '../../primitives/NotificationBar'
import { NumberInput } from '../../primitives/NumberInput'
import { Panel } from '../../primitives/Panel'
import { StatusBadge } from '../../primitives/StatusBadge'
import { Toggle } from '../../primitives/Toggle'
import { TruncatedText } from '../../primitives/TruncatedText'
import { ConfirmedAction, InlineAction } from './ConfirmedAction'
import { readOnlyReason } from './shared'
import type { Feedback } from './shared'

type Policy = components['schemas']['NotificationPolicy']
type PolicyInput = components['schemas']['NotificationPolicyInput']
type AlertSeverity = components['schemas']['AlertSeverity']
type PolicyForm = Omit<PolicyInput, 'channels' | 'repeat_interval'> & {
  smtp_enabled: boolean
  webhook_target_ids: string[]
  repeat_interval_value: number
}
type RepeatIntervalUnitSeconds = 1 | 60
type Option = { id: string; label: string }

const secondsPerMinute = 60
const secondsPerHour = 60 * secondsPerMinute
const fallbackRepeatIntervalMinimumSeconds = 15 * secondsPerMinute
const defaultRepeatIntervalSeconds = secondsPerHour
const maximumRepeatIntervalSeconds = 24 * secondsPerHour

const severities = ['critical', 'warning', 'info'] as const satisfies readonly AlertSeverity[]

function severityLabel(severity: AlertSeverity): string {
  switch (severity) {
    case 'critical':
      return '严重'
    case 'warning':
      return '警告'
    case 'info':
      return '提示'
    default:
      return assertNever(severity)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected notification policy value: ${String(value)}`)
}

// ---------------------------------------------------------------------------
// 表单值 ↔ 请求体（纯函数；`policies.test.ts` 是这段的行为基线）
// ---------------------------------------------------------------------------

export function policyFormValues(policy: Policy, repeatUnitSeconds: RepeatIntervalUnitSeconds): PolicyForm {
  return {
    name: policy.name,
    contact_ids: policy.contact_ids,
    contact_group_ids: policy.contact_group_ids,
    severity_filter: policy.severity_filter,
    notify_on_fire: policy.notify_on_fire,
    notify_on_recovery: policy.notify_on_recovery,
    template_id: policy.template_id,
    repeat_interval_value: policy.repeat_interval / repeatUnitSeconds,
    smtp_enabled: policy.channels.some((channel) => channel.channel === 'SMTP'),
    webhook_target_ids: policy.channels.flatMap((channel) => channel.channel === 'WEBHOOK' && channel.target_id ? [channel.target_id] : []),
  }
}

export function policyInput(values: PolicyForm, repeatUnitSeconds: RepeatIntervalUnitSeconds): PolicyInput {
  return {
    name: values.name,
    contact_ids: values.contact_ids,
    contact_group_ids: values.contact_group_ids,
    severity_filter: values.severity_filter,
    notify_on_fire: values.notify_on_fire,
    notify_on_recovery: values.notify_on_recovery,
    repeat_interval: values.repeat_interval_value * repeatUnitSeconds,
    template_id: values.template_id,
    channels: [
      ...(values.smtp_enabled ? [{ channel: 'SMTP' as const }] : []),
      ...values.webhook_target_ids.map((target_id) => ({ channel: 'WEBHOOK' as const, target_id })),
    ],
  }
}

export function repeatIntervalUnitSeconds(repeatIntervalMinimum: number): RepeatIntervalUnitSeconds {
  return repeatIntervalMinimum % secondsPerMinute === 0 ? secondsPerMinute : 1
}

function repeatLabel(seconds: number) {
  if (seconds < secondsPerMinute) return `${seconds} 秒`
  if (seconds % secondsPerHour === 0) return `${seconds / secondsPerHour} 小时`
  return `${seconds / secondsPerMinute} 分钟`
}

// ---------------------------------------------------------------------------
// 校验
// ---------------------------------------------------------------------------

function policySchema(minimumValue: number, maximumValue: number, unitLabel: string) {
  return z.object({
    name: z.string().refine((value) => value.trim() !== '', '请输入名称'),
    contact_ids: z.array(z.string()),
    contact_group_ids: z.array(z.string()),
    severity_filter: z.array(z.enum(severities)).min(1, '请至少选择一个级别'),
    notify_on_fire: z.boolean(),
    notify_on_recovery: z.boolean(),
    repeat_interval_value: z
      .number({ error: '请输入重复间隔' })
      .int('重复间隔必须是整数')
      .min(minimumValue, `重复间隔不得小于 ${minimumValue} ${unitLabel}`)
      .max(maximumValue, `重复间隔不得大于 ${maximumValue} ${unitLabel}`),
    smtp_enabled: z.boolean(),
    webhook_target_ids: z.array(z.string()),
  })
}

type PolicyValues = z.infer<ReturnType<typeof policySchema>>

const policyFields = [
  'name',
  'contact_ids',
  'contact_group_ids',
  'severity_filter',
  'notify_on_fire',
  'notify_on_recovery',
  'repeat_interval_value',
  'smtp_enabled',
  'webhook_target_ids',
] as const satisfies readonly FieldPath<PolicyValues>[]

// ---------------------------------------------------------------------------
// 标签页
// ---------------------------------------------------------------------------

/// 「通知策略」标签。
export function PoliciesPanel({ canManage }: { canManage: boolean }) {
  const settingsQuery = $api.useQuery('get', '/api/v1/notification-policy-settings')
  const policiesQuery = $api.useQuery('get', '/api/v1/notification-policies')
  const contactsQuery = $api.useQuery('get', '/api/v1/notification-contacts')
  const groupsQuery = $api.useQuery('get', '/api/v1/notification-contact-groups')
  const webhooksQuery = $api.useQuery('get', '/api/v1/notification-channels/webhooks')
  const deleteMutation = $api.useMutation('delete', '/api/v1/notification-policies/{id}')
  const [feedback, setFeedback] = useState<Feedback | null>(null)
  const [editor, setEditor] = useState<{ policy: Policy | null } | null>(null)

  const repeatIntervalMinimum = settingsQuery.data?.repeat_interval_minimum ?? fallbackRepeatIntervalMinimumSeconds
  const repeatUnitSeconds = repeatIntervalUnitSeconds(repeatIntervalMinimum)
  // 部署最小间隔还没取到时不给建策略：拿兜底值建出来的策略可能被服务端拒绝。
  const canManagePolicies = canManage && !settingsQuery.isPending
  const policySettingsReason = canManage ? '正在读取部署的最小重复间隔' : readOnlyReason.policies

  function remove(policy: Policy) {
    setFeedback(null)
    deleteMutation.mutate({ params: { path: { id: policy.id } } }, {
      onSuccess: () => {
        setFeedback({ tone: 'normal', text: '通知策略已删除' })
        void policiesQuery.refetch()
      },
      onError: (error) => setFeedback({ tone: 'critical', text: apiErrorMessage(error, '删除通知策略失败') }),
    })
  }

  const policies = policiesQuery.data ?? []

  return (
    <div className="alert-settings-stack">
      {feedback !== null && (
        <NotificationBar tone={feedback.tone} title={feedback.text} onClose={() => setFeedback(null)} />
      )}
      {policiesQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(policiesQuery.error, '通知策略加载失败')} />
      )}
      <Panel
        flush
        title={`通知策略（${policies.length}）`}
        actions={<span title={canManagePolicies ? undefined : policySettingsReason}>
          <Button size="sm" renderIcon={Icon.glyph.add} disabled={!canManagePolicies} onClick={() => setEditor({ policy: null })}>
            新建策略
          </Button>
        </span>}
      >
        <DataGrid<Policy>
          label="通知策略"
          loading={policiesQuery.isPending}
          rows={policies}
          rowKey={(policy) => policy.id}
          rowTestId="policy-row"
          columns={[
            { key: 'name', header: '名称', minWidth: 170, grow: 1.2, cell: (policy) => <TruncatedText className="alert-settings-strong">{policy.name}</TruncatedText> },
            {
              key: 'default',
              header: '默认',
              minWidth: 88,
              grow: 1.5,
              cell: (policy) => policy.is_default
                ? <StatusBadge tone="normal">全局默认</StatusBadge>
                : <span className="alert-settings-muted">—</span>,
            },
            {
              key: 'severity',
              header: '级别过滤',
              minWidth: 150,
              cell: (policy) => <TruncatedText>{policy.severity_filter.map(severityLabel).join('、')}</TruncatedText>,
            },
            { key: 'fire', header: '触发通知', minWidth: 100, grow: 1.4, cell: (policy) => (policy.notify_on_fire ? '开启' : '关闭') },
            { key: 'recovery', header: '恢复通知', minWidth: 100, grow: 1.4, cell: (policy) => (policy.notify_on_recovery ? '开启' : '关闭') },
            { key: 'repeat', header: '重复间隔', minWidth: 104, numeric: true, grow: 1.3, cell: (policy) => repeatLabel(policy.repeat_interval) },
            { key: 'contacts', header: '联系人', minWidth: 92, numeric: true, grow: 1.4, cell: (policy) => policy.contact_ids.length },
            { key: 'groups', header: '联系人组', minWidth: 96, numeric: true, grow: 1.4, cell: (policy) => policy.contact_group_ids.length },
            { key: 'channels', header: '渠道', minWidth: 88, numeric: true, grow: 1.4, cell: (policy) => policy.channels.length },
            {
              key: 'actions',
              header: '操作',
              minWidth: 96,
              grow: 1.6,
              align: 'end',
              cell: (policy) => (
                <span className="alert-settings-row-actions">
                  <InlineAction
                    name={`编辑 ${policy.name}`}
                    icon="edit"
                    disabled={!canManagePolicies}
                    disabledReason={policySettingsReason}
                    onClick={() => setEditor({ policy })}
                  />
                  <ConfirmedAction
                    name={`删除 ${policy.name}`}
                    icon="trashCan"
                    destructive
                    heading="删除通知策略"
                    description={`删除后按 ${policy.name} 派发的告警将改由其他策略决定去向；没有策略匹配时不会有人收到通知。此操作不可撤销。`}
                    confirmLabel="删除策略"
                    disabled={!canManagePolicies || policy.is_default}
                    disabledReason={policy.is_default ? '全局默认策略不可删除' : policySettingsReason}
                    onConfirm={() => remove(policy)}
                  />
                </span>
              ),
            },
          ]}
          empty={{ title: '暂无通知策略', description: '新建策略，决定哪些告警发给谁、走哪个渠道。' }}
        />
      </Panel>
      {editor !== null && (
        <PolicyModal
          policy={editor.policy}
          repeatUnitSeconds={repeatUnitSeconds}
          repeatIntervalMinimum={repeatIntervalMinimum}
          contactOptions={(contactsQuery.data ?? []).map((contact) => ({ id: contact.id, label: `${contact.name} · ${contact.email}` }))}
          groupOptions={(groupsQuery.data ?? []).map((group) => ({ id: group.id, label: group.name }))}
          webhookOptions={(webhooksQuery.data ?? []).map((target) => ({ id: target.id, label: target.enabled ? target.name : `${target.name}（已停用）` }))}
          onClose={() => setEditor(null)}
          onSaved={(message) => {
            setEditor(null)
            setFeedback({ tone: 'normal', text: message })
            void policiesQuery.refetch()
          }}
        />
      )}
    </div>
  )
}

function PolicyModal({
  policy,
  repeatUnitSeconds,
  repeatIntervalMinimum,
  contactOptions,
  groupOptions,
  webhookOptions,
  onClose,
  onSaved,
}: {
  policy: Policy | null
  repeatUnitSeconds: RepeatIntervalUnitSeconds
  repeatIntervalMinimum: number
  contactOptions: Option[]
  groupOptions: Option[]
  webhookOptions: Option[]
  onClose: () => void
  onSaved: (message: string) => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/notification-policies')
  const updateMutation = $api.useMutation('put', '/api/v1/notification-policies/{id}')
  const [failure, setFailure] = useState('')
  const unitLabel = repeatUnitSeconds === secondsPerMinute ? '分钟' : '秒'
  const minimumValue = repeatIntervalMinimum / repeatUnitSeconds
  const maximumValue = maximumRepeatIntervalSeconds / repeatUnitSeconds
  const severityOptions: { id: AlertSeverity; label: string }[] = severities.map((value) => ({ id: value, label: severityLabel(value) }))

  const { control, formState, handleSubmit, register, setError } = useForm<PolicyValues>({
    resolver: zodResolver(policySchema(minimumValue, maximumValue, unitLabel)),
    defaultValues: policy === null
      ? {
        name: '',
        contact_ids: [],
        contact_group_ids: [],
        severity_filter: [...severities],
        notify_on_fire: true,
        notify_on_recovery: true,
        repeat_interval_value: defaultRepeatIntervalSeconds / repeatUnitSeconds,
        smtp_enabled: true,
        webhook_target_ids: [],
      }
      : toPolicyValues(policyFormValues(policy, repeatUnitSeconds)),
  })

  const submit = handleSubmit((values) => {
    setFailure('')
    const body = policyInput({ ...values, name: values.name.trim(), template_id: policy?.template_id }, repeatUnitSeconds)
    const options = {
      onSuccess: () => onSaved(policy === null ? '通知策略已创建' : '通知策略已更新'),
      onError: (error: unknown) => {
        if (applyApiFieldErrors<PolicyValues>(error, policyFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '保存通知策略失败'))
        }
      },
    }
    if (policy !== null) {
      updateMutation.mutate({ params: { path: { id: policy.id } }, body }, options)
      return
    }
    createMutation.mutate({ body }, options)
  })

  return (
    <Modal
      open
      modalHeading={policy === null ? '新建通知策略' : '编辑通知策略'}
      primaryButtonText="保存策略"
      secondaryButtonText="取消"
      primaryButtonDisabled={createMutation.isPending || updateMutation.isPending}
      size="md"
      onRequestSubmit={() => void submit()}
      onRequestClose={onClose}
      onSecondarySubmit={onClose}
    >
      <form className="alert-settings-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
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
        <div className="alert-settings-form__row">
          <FormField label="联系人" errorText={formState.errors.contact_ids?.message}>
            {(field) => <Controller
              name="contact_ids"
              control={control}
              render={({ field: value }) => <OptionMultiSelect
                id={field.id}
                label="选择联系人"
                options={contactOptions}
                selected={value.value}
                describedBy={field.describedBy}
                onChange={value.onChange}
              />}
            />}
          </FormField>
          <FormField label="联系人组" errorText={formState.errors.contact_group_ids?.message}>
            {(field) => <Controller
              name="contact_group_ids"
              control={control}
              render={({ field: value }) => <OptionMultiSelect
                id={field.id}
                label="选择联系人组"
                options={groupOptions}
                selected={value.value}
                describedBy={field.describedBy}
                onChange={value.onChange}
              />}
            />}
          </FormField>
        </div>
        <div className="alert-settings-form__row">
          <FormField label="级别过滤" required errorText={formState.errors.severity_filter?.message}>
            {(field) => <Controller
              name="severity_filter"
              control={control}
              render={({ field: value }) => <MultiSelect<{ id: AlertSeverity; label: string }>
                id={field.id}
                titleText=""
                hideLabel
                label="选择级别"
                items={severityOptions}
                itemToString={(item) => item?.label ?? ''}
                selectedItems={severityOptions.filter((option) => value.value.includes(option.id))}
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                onChange={({ selectedItems }) => value.onChange((selectedItems ?? []).map((item) => item.id))}
              />}
            />}
          </FormField>
          <FormField
            label={`重复间隔（${unitLabel}）`}
            required
            helperText={`本部署允许的最小间隔是 ${minimumValue} ${unitLabel}。`}
            errorText={formState.errors.repeat_interval_value?.message}
          >
            {(field) => <Controller
              name="repeat_interval_value"
              control={control}
              render={({ field: value }) => <NumberInput
                id={field.id}
                label=""
                hideLabel
                min={minimumValue}
                max={maximumValue}
                invalid={field.invalid}
                aria-describedby={field.describedBy}
                ref={value.ref}
                name={value.name}
                value={value.value}
                onBlur={value.onBlur}
                // 取值在 onChange 的第二个参数里，所以走 Controller 而不是 register。
                onChange={(_event, state) => value.onChange(state.value === '' ? undefined : Number(state.value))}
              />}
            />}
          </FormField>
        </div>
        <div className="alert-settings-form__row">
          <FormField label="触发通知" errorText={formState.errors.notify_on_fire?.message}>
            {(field) => <Controller
              name="notify_on_fire"
              control={control}
              render={({ field: value }) => <Toggle
                id={field.id}
                size="sm"
                labelText=""
                hideLabel
                labelA="关闭"
                labelB="开启"
                toggled={value.value}
                onToggle={(next) => value.onChange(next)}
              />}
            />}
          </FormField>
          <FormField label="恢复通知" errorText={formState.errors.notify_on_recovery?.message}>
            {(field) => <Controller
              name="notify_on_recovery"
              control={control}
              render={({ field: value }) => <Toggle
                id={field.id}
                size="sm"
                labelText=""
                hideLabel
                labelA="关闭"
                labelB="开启"
                toggled={value.value}
                onToggle={(next) => value.onChange(next)}
              />}
            />}
          </FormField>
        </div>
        <FormField label="发送渠道" errorText={formState.errors.smtp_enabled?.message}>
          {(field) => <Controller
            name="smtp_enabled"
            control={control}
            render={({ field: value }) => <Checkbox
              id={field.id}
              labelText="SMTP"
              checked={value.value}
              onChange={(_event, { checked }) => value.onChange(checked)}
            />}
          />}
        </FormField>
        <FormField label="Webhook 目标" errorText={formState.errors.webhook_target_ids?.message}>
          {(field) => <Controller
            name="webhook_target_ids"
            control={control}
            render={({ field: value }) => <OptionMultiSelect
              id={field.id}
              label="选择 Webhook 目标"
              options={webhookOptions}
              selected={value.value}
              describedBy={field.describedBy}
              onChange={value.onChange}
            />}
          />}
        </FormField>
      </form>
    </Modal>
  )
}

/// 表单值里除了 `template_id` 之外与 `PolicyForm` 同型；`template_id` 没有输入框，
/// 所以它不进表单，提交时从被编辑的策略上原样带回（见 `submit`）。
function toPolicyValues(form: PolicyForm): PolicyValues {
  return {
    name: form.name,
    contact_ids: form.contact_ids,
    contact_group_ids: form.contact_group_ids,
    severity_filter: form.severity_filter,
    notify_on_fire: form.notify_on_fire,
    notify_on_recovery: form.notify_on_recovery,
    repeat_interval_value: form.repeat_interval_value,
    smtp_enabled: form.smtp_enabled,
    webhook_target_ids: form.webhook_target_ids,
  }
}

function OptionMultiSelect({ id, label, options, selected, describedBy, onChange }: {
  id: string
  label: string
  options: Option[]
  selected: string[]
  describedBy: string | undefined
  onChange: (next: string[]) => void
}) {
  return (
    <MultiSelect<Option>
      id={id}
      titleText=""
      hideLabel
      label={label}
      items={options}
      itemToString={(item) => item?.label ?? ''}
      selectedItems={options.filter((option) => selected.includes(option.id))}
      aria-describedby={describedBy}
      onChange={({ selectedItems }) => onChange((selectedItems ?? []).map((item) => item.id))}
    />
  )
}
