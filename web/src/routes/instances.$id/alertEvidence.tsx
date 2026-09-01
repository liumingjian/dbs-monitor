import { Button, Select, SelectItem, TextArea } from '@carbon/react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
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
import { KeyValueList } from '../../primitives/KeyValueList'
import { Modal } from '../../primitives/Modal'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import { StatusBadge } from '../../primitives/StatusBadge'
import type { StatusTone } from '../../primitives/StatusBadge'
import { TruncatedText } from '../../primitives/TruncatedText'
import './alertEvidence.css'

type AlertDisposition = components['schemas']['AlertDisposition']
type AlertDispositionDetail = components['schemas']['AlertDispositionDetail']
type AlertDispositionEvent = components['schemas']['AlertDispositionEvent']
type AlertDispositionInput = components['schemas']['AlertDispositionInput']
type AlertTriggerSnapshotResult = components['schemas']['AlertTriggerSnapshotResult']
type AlertTriggerSnapshotSession = components['schemas']['AlertTriggerSnapshotSession']
type IgnoreReasonCode = components['schemas']['IgnoreReasonCode']
type DispositionTarget = Extract<AlertDisposition, 'ACKED' | 'IGNORED'>
type TriggerSnapshotPresentation = {
  label: string
  kind: 'success' | 'error' | 'not-applicable'
}

const dispositionTargets = ['ACKED', 'IGNORED'] as const satisfies readonly DispositionTarget[]

const ignoreReasonCodes = [
  'KNOWN_ISSUE',
  'FALSE_POSITIVE',
  'DUPLICATE',
  'IMPACT_ACCEPTABLE',
  'OTHER',
] as const satisfies readonly IgnoreReasonCode[]

/// 处置表单的校验规则。
///
/// 与接口生成的类型对齐靠三处，都会在漂移时编译失败：字段取值的两张清单
/// `as const satisfies readonly ...[]` 锚在生成的联合类型上；`satisfies z.ZodType<...>`
/// 要求 schema 的出参是生成的请求体类型的子集；`dispositionBody` 再把出参真的当请求体用。
///
/// 联动规则照抄服务端 `internal/httpapi/alert_dispositions.go` 的那段 switch：同一条规则
/// 客户端先讲一遍，服务端仍然是权威，两边说的是同一件事。
const dispositionFormSchema = z.object({
  disposition: z.enum(dispositionTargets),
  note: z.string().max(500, '备注最多 500 字').optional(),
  ignore_reason_code: z.enum(ignoreReasonCodes, { error: '请选择忽略原因' }).optional(),
  ignore_reason_detail: z.string().max(500, '补充说明最多 500 字').optional(),
}).superRefine((values, context) => {
  if (values.disposition === 'ACKED') {
    if (values.ignore_reason_code !== undefined) {
      context.addIssue({ code: 'custom', path: ['ignore_reason_code'], message: '确认告警不能带忽略原因' })
    }
    if (values.ignore_reason_detail !== undefined) {
      context.addIssue({ code: 'custom', path: ['ignore_reason_detail'], message: '确认告警不能带补充说明' })
    }
    return
  }
  if (values.note !== undefined) {
    context.addIssue({ code: 'custom', path: ['note'], message: '忽略告警不能带备注' })
  }
  if (values.ignore_reason_code === undefined) {
    context.addIssue({ code: 'custom', path: ['ignore_reason_code'], message: '请选择忽略原因' })
    return
  }
  if (values.ignore_reason_code === 'OTHER' && optionalTrimmed(values.ignore_reason_detail) === undefined) {
    context.addIssue({ code: 'custom', path: ['ignore_reason_detail'], message: '请输入补充说明' })
  }
}) satisfies z.ZodType<AlertDispositionInput>

type DispositionFormValues = z.infer<typeof dispositionFormSchema>

/// 服务端字段错误只接受这三个 —— 它们各自有输入框可以聚焦。
/// 服务端还会对 `disposition` 报错，那个字段没有输入框，落回整表单的错误条。
const dispositionFieldNames = [
  'note',
  'ignore_reason_code',
  'ignore_reason_detail',
] as const satisfies readonly FieldPath<DispositionFormValues>[]

function dispositionBody(values: DispositionFormValues): AlertDispositionInput {
  if (values.disposition === 'ACKED') {
    return { disposition: 'ACKED', note: optionalTrimmed(values.note) }
  }
  return {
    disposition: 'IGNORED',
    ignore_reason_code: values.ignore_reason_code,
    ignore_reason_detail: optionalTrimmed(values.ignore_reason_detail),
  }
}

export function DispositionSection({ alertInstanceID, recovered, onChanged }: {
  alertInstanceID: string
  recovered: boolean
  onChanged?: () => void
}) {
  const disposition = $api.useQuery('get', '/api/v1/alert-instances/{id}/disposition', {
    params: { path: { id: alertInstanceID } },
  })
  const currentUser = $api.useQuery('get', '/api/v1/me')
  const updateDisposition = $api.useMutation('put', '/api/v1/alert-instances/{id}/disposition')
  const { formState, handleSubmit, register, reset, setError, watch } = useForm<DispositionFormValues>({
    resolver: zodResolver(dispositionFormSchema),
    defaultValues: { disposition: 'ACKED' },
  })
  const ignoreReason = watch('ignore_reason_code')
  const [target, setTarget] = useState<DispositionTarget | null>(null)
  const [failure, setFailure] = useState('')
  const canManage = currentUser.data?.role === 'ALERT_ADMIN' || currentUser.data?.role === 'PLATFORM_ADMIN'
  const disabledReason = dispositionDisabledReason(recovered, canManage)

  function open(next: DispositionTarget) {
    reset({ disposition: next })
    setFailure('')
    setTarget(next)
  }

  function close() {
    setTarget(null)
  }

  const submit = handleSubmit((values) => {
    setFailure('')
    updateDisposition.mutate(
      { params: { path: { id: alertInstanceID } }, body: dispositionBody(values) },
      {
        onSuccess: () => {
          setTarget(null)
          reset({ disposition: values.disposition })
          void disposition.refetch()
          onChanged?.()
        },
        onError: (error) => {
          // 字段级错误落到对应输入框并聚焦第一个；一条都落不下时才退回整表单的错误条。
          if (applyApiFieldErrors<DispositionFormValues>(error, dispositionFieldNames, setError).length === 0) {
            setFailure(apiErrorMessage(error, '更新处置状态失败'))
          }
        },
      },
    )
  })

  return <Panel
    className="alert-detail-section"
    title="处置记录"
    headingLevel={3}
    actions={<div className="alert-evidence-actions">
      <span title={disabledReason}>
        <Button kind="tertiary" size="md" disabled={disabledReason !== undefined} renderIcon={Icon.glyph.checkmark} onClick={() => open('ACKED')}>确认</Button>
      </span>
      <span title={disabledReason}>
        <Button kind="tertiary" size="md" disabled={disabledReason !== undefined} renderIcon={Icon.glyph.stop} onClick={() => open('IGNORED')}>忽略</Button>
      </span>
    </div>}
  >
    {disposition.isPending && <SkeletonBlock lines={4} label="处置记录加载中" />}
    {disposition.error && <NotificationBar tone="critical" title={apiErrorMessage(disposition.error, '处置记录加载失败')} />}
    {disposition.data && <DispositionContent detail={disposition.data} />}
    <Modal
      open={target !== null}
      modalHeading={target === 'ACKED' ? '确认告警' : '忽略告警'}
      primaryButtonText="提交"
      secondaryButtonText="取消"
      primaryButtonDisabled={updateDisposition.isPending}
      onRequestSubmit={() => void submit()}
      onRequestClose={close}
      onSecondarySubmit={close}
    >
      {/* Modal 的主按钮在 children 之外，提交走 `onRequestSubmit`；这里仍然是 form，
          是为了回车提交与原生表单语义都落在同一个 `handleSubmit` 上。 */}
      <form className="alert-evidence-form" onSubmit={submit} noValidate>
        {failure !== '' && <NotificationBar tone="critical" title={failure} />}
        {target === 'ACKED' && <FormField label="备注" errorText={formState.errors.note?.message}>
          {(control) => <TextArea
            id={control.id}
            labelText=""
            hideLabel
            rows={3}
            maxLength={500}
            invalid={control.invalid}
            aria-describedby={control.describedBy}
            {...register('note')}
          />}
        </FormField>}
        {target === 'IGNORED' && <>
          <FormField label="忽略原因" required errorText={formState.errors.ignore_reason_code?.message}>
            {(control) => <Select
              id={control.id}
              labelText=""
              noLabel
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              {...register('ignore_reason_code', { setValueAs: (value: string) => value === '' ? undefined : value })}
            >
              <SelectItem value="" text="请选择" />
              {ignoreReasonCodes.map((code) => <SelectItem key={code} value={code} text={ignoreReasonLabel(code)} />)}
            </Select>}
          </FormField>
          {ignoreReason === 'OTHER' && <FormField label="补充说明" required errorText={formState.errors.ignore_reason_detail?.message}>
            {(control) => <TextArea
              id={control.id}
              labelText=""
              hideLabel
              rows={3}
              maxLength={500}
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              {...register('ignore_reason_detail')}
            />}
          </FormField>}
        </>}
      </form>
    </Modal>
  </Panel>
}

function DispositionContent({ detail }: { detail: AlertDispositionDetail }) {
  return <div className="alert-evidence-stack">
    <KeyValueList label="处置概览" columns={3} items={[
      { key: 'status', label: '当前状态', value: <DispositionTag disposition={detail.disposition} /> },
      { key: 'at', label: '最近处置时间', value: optionalTime(detail.disposition_at) },
      { key: 'actor', label: '处置人', value: detail.disposition_by ?? '—' },
      { key: 'detail', label: '备注 / 原因', value: dispositionDetail(detail) },
      { key: 'notifications', label: '停止重复通知', value: detail.stops_repeat_notifications ? '是' : '否' },
      { key: 'health', label: '退出健康归并', value: detail.excluded_from_health_rollup ? '是' : '否' },
    ]} />
    <DataGrid<AlertDispositionEvent>
      label="处置历史"
      density="dense"
      rows={detail.history}
      rowKey={(event) => `${event.acted_at}-${event.actor_id}`}
      columns={dispositionHistoryColumns}
      empty={{ title: '暂无处置历史' }}
    />
  </div>
}

export function TriggerSnapshotSection({ alertInstanceID, eventEvidence = false }: {
  alertInstanceID: string
  eventEvidence?: boolean
}) {
  const snapshot = $api.useQuery('get', '/api/v1/alert-instances/{id}/trigger-snapshot', {
    params: { path: { id: alertInstanceID } },
  })
  const heading = eventEvidence ? '告警触发时现场' : '触发现场快照'

  return <Panel className="alert-detail-section" title={heading} headingLevel={3}>
    <div className="alert-evidence-stack">
      {eventEvidence && <NotificationBar tone="info" title="以下证据捕获于关联告警触发时，不代表当前状态" />}
      {snapshot.isPending && <SkeletonBlock lines={4} label="触发现场快照加载中" />}
      {snapshot.error && <NotificationBar tone="critical" title={apiErrorMessage(snapshot.error, '触发现场快照加载失败')} />}
      {snapshot.data && <TriggerSnapshotContent snapshot={snapshot.data} />}
    </div>
  </Panel>
}

function TriggerSnapshotContent({ snapshot }: { snapshot: components['schemas']['AlertTriggerSnapshot'] }) {
  const presentation = triggerSnapshotPresentation(snapshot.result)
  const summary = <KeyValueList label="快照概览" columns={2} items={[
    { key: 'result', label: '采集结果', value: <StatusBadge tone={triggerSnapshotTone(presentation.kind)}>{presentation.label}</StatusBadge> },
    { key: 'metric', label: '适用类型 / 指标', value: snapshot.metric_id },
    { key: 'captured', label: '捕获时间', value: optionalTime(snapshot.captured_at) },
    { key: 'matches', label: '原始匹配数', value: String(snapshot.original_match_count) },
    { key: 'truncated', label: '截断状态', value: snapshot.truncated ? '已截断' : '未截断' },
  ]} />

  switch (snapshot.result) {
    case 'NOT_APPLICABLE':
      return <NotificationBar tone="info" title={presentation.label}>{`指标 ${snapshot.metric_id}`}</NotificationBar>
    case 'FAILED':
      return <div className="alert-evidence-stack">
        {summary}
        <NotificationBar tone="critical" title="现场快照采集失败">{snapshot.failure_reason ?? '未记录失败原因'}</NotificationBar>
      </div>
    case 'SUCCESS':
      return <div className="alert-evidence-stack">
        {summary}
        {snapshot.truncated && <NotificationBar tone="warning" title="快照已截断">
          {`原始匹配 ${snapshot.original_match_count} 条，当前保留 ${snapshot.sessions.length} 条。`}
        </NotificationBar>}
        <DataGrid<AlertTriggerSnapshotSession>
          label="触发时会话"
          density="dense"
          cellPadding="compact"
          rows={snapshot.sessions}
          rowKey={(session) => String(session.pid)}
          columns={snapshotSessionColumns}
          empty={{ title: '触发时未捕获会话条目' }}
        />
      </div>
    default:
      return assertNever(snapshot.result)
  }
}

export function triggerSnapshotPresentation(result: AlertTriggerSnapshotResult): TriggerSnapshotPresentation {
  switch (result) {
    case 'SUCCESS': return { label: '采集成功', kind: 'success' }
    case 'FAILED': return { label: '采集失败', kind: 'error' }
    case 'NOT_APPLICABLE': return { label: '该类型不采集现场快照', kind: 'not-applicable' }
    default: return assertNever(result)
  }
}

/// 列宽按 web/CLAUDE.md 的列宽契约算：`minWidth` = max(表头自然宽, 压不动的内容宽) + 内边距，
/// 各列 `grow: 1`。合计 962 ≤ 974，所以表头一个都不会被截成「规...」「评...」。
///
/// 这两张证据表原先被排在告警详情页的半幅栅格里（≈446px），八列 / 十一列在那个宽度下
/// 连表头都写不下 —— 缺的是容器宽度，不是列。它们现在整行铺开（`alertDetail.css`）。
const dispositionHistoryColumns: DataGridColumn<AlertDispositionEvent>[] = [
  { key: 'kind', header: '动作', minWidth: 60, cell: (event) => dispositionEventLabel(event.kind) },
  { key: 'change', header: '状态变化', minWidth: 138, cell: (event) => `${dispositionLabel(event.from_disposition)} → ${dispositionLabel(event.to_disposition)}` },
  { key: 'actor', header: '处置人', minWidth: 102, cell: (event) => <TruncatedText>{event.actor_id}</TruncatedText> },
  { key: 'acted', header: '处置时间', minWidth: 182, cell: (event) => optionalTime(event.acted_at) },
  { key: 'detail', header: '备注 / 原因', minWidth: 142, cell: (event) => <TruncatedText>{dispositionDetail(event)}</TruncatedText> },
  { key: 'version', header: '规则版本', minWidth: 84, numeric: true, cell: (event) => String(event.rule_version) },
  { key: 'value', header: '评估值', minWidth: 72, numeric: true, cell: (event) => optionalNumber(event.current_value) },
  { key: 'evaluated', header: '评估时间', minWidth: 182, cell: (event) => optionalTime(event.evaluated_at) },
]

/// 十一列，两列是完整时刻（各 148px 字形）。整行铺开后可用 974px，但标准内边距要吃掉
/// 352px，表头会全线被截；因此开紧凑档（`cellPadding="compact"`），合计 957 ≤ 974。
/// 用户名、客户端地址、等待事件、阻塞关系四列仍会截断 —— 它们都带悬停全文。
const snapshotSessionColumns: DataGridColumn<AlertTriggerSnapshotSession>[] = [
  { key: 'pid', header: 'PID', minWidth: 66, numeric: true, cell: (session) => String(session.pid) },
  { key: 'username', header: '用户', minWidth: 76, cell: (session) => optionalText(session.username) },
  { key: 'database', header: '数据库', minWidth: 62, cell: (session) => optionalText(session.database_name) },
  { key: 'client', header: '客户端', minWidth: 86, cell: (session) => optionalText(session.client_address) },
  { key: 'state', header: '状态', minWidth: 56, cell: (session) => optionalText(session.state) },
  { key: 'query-start', header: '查询开始', minWidth: 164, cell: (session) => optionalTime(session.query_started_at) },
  { key: 'transaction-start', header: '事务开始', minWidth: 164, cell: (session) => optionalTime(session.transaction_started_at) },
  { key: 'query-duration', header: '查询时长', minWidth: 70, numeric: true, cell: (session) => durationLabel(session.query_duration_ms) },
  { key: 'transaction-duration', header: '事务时长', minWidth: 70, numeric: true, cell: (session) => durationLabel(session.transaction_duration_ms) },
  { key: 'wait', header: '等待事件', minWidth: 78, cell: (session) => [session.wait_event_type, session.wait_event].filter(Boolean).join(' / ') || '—' },
  { key: 'blocking', header: '阻塞关系', minWidth: 78, cell: (session) => session.blocking_pids.length === 0 ? '无' : `被 PID ${session.blocking_pids.join(', ')} 阻塞` },
]

function DispositionTag({ disposition }: { disposition: AlertDisposition }) {
  return <StatusBadge tone={dispositionTone(disposition)}>{dispositionLabel(disposition)}</StatusBadge>
}

/// 处置状态的视觉档位。蓝色表示「可交互」，所以「已确认」不能是蓝的。
function dispositionTone(disposition: AlertDisposition): StatusTone {
  switch (disposition) {
    case 'NONE': return 'warning'
    case 'ACKED': return 'normal'
    case 'IGNORED': return 'unknown'
    default: return assertNever(disposition)
  }
}

function dispositionLabel(disposition: AlertDisposition): string {
  switch (disposition) {
    case 'NONE': return '未处置'
    case 'ACKED': return '已确认'
    case 'IGNORED': return '已忽略'
    default: return assertNever(disposition)
  }
}

function dispositionEventLabel(kind: AlertDispositionEvent['kind']): string {
  switch (kind) {
    case 'ACKED': return '确认'
    case 'IGNORED': return '忽略'
    default: return assertNever(kind)
  }
}

function dispositionDetail(value: Pick<AlertDispositionDetail, 'note' | 'ignore_reason_code' | 'ignore_reason_detail'>): string {
  if (value.note) return value.note
  if (value.ignore_reason_code) {
    const reason = ignoreReasonLabel(value.ignore_reason_code)
    return value.ignore_reason_detail ? `${reason}：${value.ignore_reason_detail}` : reason
  }
  return '—'
}

function ignoreReasonLabel(reason: IgnoreReasonCode): string {
  switch (reason) {
    case 'KNOWN_ISSUE': return '已知问题，暂不处理'
    case 'FALSE_POSITIVE': return '误报'
    case 'DUPLICATE': return '重复告警'
    case 'IMPACT_ACCEPTABLE': return '影响可接受'
    case 'OTHER': return '其他'
    default: return assertNever(reason)
  }
}

function dispositionDisabledReason(recovered: boolean, canManage: boolean): string | undefined {
  if (recovered) return '已恢复告警不能再处置'
  if (!canManage) return '需要告警管理员角色'
  return undefined
}

function triggerSnapshotTone(kind: TriggerSnapshotPresentation['kind']): StatusTone {
  switch (kind) {
    case 'success': return 'normal'
    case 'error': return 'critical'
    case 'not-applicable': return 'unknown'
    default: return assertNever(kind)
  }
}

function optionalTrimmed(value: string | undefined): string | undefined {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}

function optionalText(value: string | undefined): string {
  return value === undefined ? '—' : value
}

function optionalNumber(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

function optionalTime(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString()
}

function durationLabel(milliseconds: number | undefined): string {
  if (milliseconds === undefined) return '—'
  if (milliseconds < 1000) return `${milliseconds} ms`
  return `${Math.round(milliseconds / 1000)} 秒`
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert evidence value: ${value}`)
}
