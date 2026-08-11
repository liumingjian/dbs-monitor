import { CheckOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Empty, Form, Input, Modal, Select, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'

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

type DispositionFormValues = {
  note?: string
  ignore_reason_code?: IgnoreReasonCode
  ignore_reason_detail?: string
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
  const [form] = Form.useForm<DispositionFormValues>()
  const ignoreReason = Form.useWatch('ignore_reason_code', form)
  const [target, setTarget] = useState<DispositionTarget | null>(null)
  const [failure, setFailure] = useState('')
  const canManage = currentUser.data?.role === 'ALERT_ADMIN' || currentUser.data?.role === 'PLATFORM_ADMIN'
  const disabledReason = dispositionDisabledReason(recovered, canManage)

  function open(next: DispositionTarget) {
    form.resetFields()
    setFailure('')
    setTarget(next)
  }

  function submit(values: DispositionFormValues) {
    if (!target) return

    let body: AlertDispositionInput
    if (target === 'ACKED') {
      body = { disposition: target, note: optionalTrimmed(values.note) }
    } else {
      body = {
        disposition: target,
        ignore_reason_code: values.ignore_reason_code,
        ignore_reason_detail: optionalTrimmed(values.ignore_reason_detail),
      }
    }

    setFailure('')
    updateDisposition.mutate(
      { params: { path: { id: alertInstanceID } }, body },
      {
        onSuccess: () => {
          setTarget(null)
          void disposition.refetch()
          onChanged?.()
        },
        onError: (error) => setFailure(apiErrorMessage(error, '更新处置状态失败')),
      },
    )
  }

  return <section className="alert-detail-section" aria-labelledby="disposition-heading">
    <Space align="center" wrap style={{ width: '100%', justifyContent: 'space-between' }}>
      <Typography.Title id="disposition-heading" level={3}>处置记录</Typography.Title>
      <Space wrap>
        <Tooltip title={disabledReason}><span>
          <Button icon={<CheckOutlined />} disabled={disabledReason !== undefined} onClick={() => open('ACKED')}>确认</Button>
        </span></Tooltip>
        <Tooltip title={disabledReason}><span>
          <Button icon={<StopOutlined />} disabled={disabledReason !== undefined} onClick={() => open('IGNORED')}>忽略</Button>
        </span></Tooltip>
      </Space>
    </Space>
    {disposition.isPending && <Spin />}
    {disposition.error && <Alert type="error" showIcon title={apiErrorMessage(disposition.error, '处置记录加载失败')} />}
    {disposition.data && <DispositionContent detail={disposition.data} />}
    <Modal
      title={target === 'ACKED' ? '确认告警' : '忽略告警'}
      open={target !== null}
      footer={null}
      destroyOnHidden
      onCancel={() => setTarget(null)}
    >
      <Form form={form} layout="vertical" onFinish={submit}>
        {target === 'ACKED' && <Form.Item name="note" label="备注">
          <Input.TextArea rows={3} maxLength={500} />
        </Form.Item>}
        {target === 'IGNORED' && <>
          <Form.Item name="ignore_reason_code" label="忽略原因" rules={[{ required: true, message: '请选择忽略原因' }]}>
            <Select options={ignoreReasonOptions} />
          </Form.Item>
          {ignoreReason === 'OTHER' && <Form.Item
            name="ignore_reason_detail"
            label="补充说明"
            rules={[{ required: true, whitespace: true, message: '请输入补充说明' }]}
          >
            <Input.TextArea rows={3} maxLength={500} />
          </Form.Item>}
        </>}
        {failure && <Alert type="error" showIcon title={failure} style={{ marginBottom: 16 }} />}
        <Button type="primary" htmlType="submit" loading={updateDisposition.isPending}>提交</Button>
      </Form>
    </Modal>
  </section>
}

function DispositionContent({ detail }: { detail: AlertDispositionDetail }) {
  return <Space direction="vertical" size="middle" style={{ width: '100%' }}>
    <Descriptions size="small" bordered column={{ xs: 1, sm: 2 }} items={[
      { key: 'status', label: '当前状态', children: <DispositionTag disposition={detail.disposition} /> },
      { key: 'at', label: '最近处置时间', children: optionalTime(detail.disposition_at) },
      { key: 'actor', label: '处置人', children: detail.disposition_by ?? '—' },
      { key: 'detail', label: '备注 / 原因', children: dispositionDetail(detail) },
      { key: 'notifications', label: '停止重复通知', children: detail.stops_repeat_notifications ? '是' : '否' },
      { key: 'health', label: '退出健康归并', children: detail.excluded_from_health_rollup ? '是' : '否' },
    ]} />
    <Table<AlertDispositionEvent>
      rowKey={(record) => `${record.acted_at}-${record.actor_id}`}
      size="small"
      pagination={false}
      dataSource={detail.history}
      columns={dispositionHistoryColumns}
      scroll={{ x: 980 }}
      locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无处置历史" /> }}
    />
  </Space>
}

export function TriggerSnapshotSection({ alertInstanceID, eventEvidence = false }: {
  alertInstanceID: string
  eventEvidence?: boolean
}) {
  const snapshot = $api.useQuery('get', '/api/v1/alert-instances/{id}/trigger-snapshot', {
    params: { path: { id: alertInstanceID } },
  })
  const heading = eventEvidence ? '告警触发时现场' : '触发现场快照'

  return <section className="alert-detail-section" aria-labelledby="trigger-snapshot-heading">
    <Typography.Title id="trigger-snapshot-heading" level={3}>{heading}</Typography.Title>
    {eventEvidence && <Alert
      type="info"
      showIcon
      title="以下证据捕获于关联告警触发时，不代表当前状态"
      style={{ marginBottom: 16 }}
    />}
    {snapshot.isPending && <Spin />}
    {snapshot.error && <Alert type="error" showIcon title={apiErrorMessage(snapshot.error, '触发现场快照加载失败')} />}
    {snapshot.data && <TriggerSnapshotContent snapshot={snapshot.data} />}
  </section>
}

function TriggerSnapshotContent({ snapshot }: { snapshot: components['schemas']['AlertTriggerSnapshot'] }) {
  const presentation = triggerSnapshotPresentation(snapshot.result)
  const summary = <Descriptions size="small" bordered column={{ xs: 1, sm: 2, lg: 4 }} items={[
    { key: 'result', label: '采集结果', children: <Tag color={triggerSnapshotTagColor(presentation.kind)}>{presentation.label}</Tag> },
    { key: 'metric', label: '适用类型 / 指标', children: snapshot.metric_id },
    { key: 'captured', label: '捕获时间', children: optionalTime(snapshot.captured_at) },
    { key: 'matches', label: '原始匹配数', children: String(snapshot.original_match_count) },
    { key: 'truncated', label: '截断状态', children: snapshot.truncated ? '已截断' : '未截断' },
  ]} />

  switch (snapshot.result) {
    case 'NOT_APPLICABLE':
      return <Alert type="info" showIcon title={presentation.label} description={`指标 ${snapshot.metric_id}`} />
    case 'FAILED':
      return <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {summary}
        <Alert type="error" showIcon title="现场快照采集失败" description={snapshot.failure_reason ?? '未记录失败原因'} />
      </Space>
    case 'SUCCESS':
      return <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {summary}
        {snapshot.truncated && <Alert type="warning" showIcon title="快照已截断" description={`原始匹配 ${snapshot.original_match_count} 条，当前保留 ${snapshot.sessions.length} 条。`} />}
        <Table<AlertTriggerSnapshotSession>
          rowKey="pid"
          size="small"
          pagination={false}
          dataSource={snapshot.sessions}
          columns={snapshotSessionColumns}
          scroll={{ x: 1380 }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="触发时未捕获会话条目" /> }}
        />
      </Space>
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

const ignoreReasonCodes = [
  'KNOWN_ISSUE',
  'FALSE_POSITIVE',
  'DUPLICATE',
  'IMPACT_ACCEPTABLE',
  'OTHER',
] as const satisfies readonly IgnoreReasonCode[]

const ignoreReasonOptions = ignoreReasonCodes.map((code) => ({
  value: code,
  label: ignoreReasonLabel(code),
}))

const dispositionHistoryColumns: TableColumnsType<AlertDispositionEvent> = [
  { title: '动作', width: 90, render: (_, event) => dispositionEventLabel(event.kind) },
  { title: '状态变化', width: 150, render: (_, event) => `${dispositionLabel(event.from_disposition)} → ${dispositionLabel(event.to_disposition)}` },
  { title: '处置人', width: 300, dataIndex: 'actor_id' },
  { title: '处置时间', width: 190, render: (_, event) => optionalTime(event.acted_at) },
  { title: '备注 / 原因', width: 230, render: (_, event) => dispositionDetail(event) },
  { title: '规则版本', width: 100, dataIndex: 'rule_version' },
  { title: '评估值', width: 100, render: (_, event) => optionalNumber(event.current_value) },
  { title: '评估时间', width: 190, render: (_, event) => optionalTime(event.evaluated_at) },
]

const snapshotSessionColumns: TableColumnsType<AlertTriggerSnapshotSession> = [
  { title: 'PID', width: 90, dataIndex: 'pid', fixed: 'left' },
  { title: '用户', width: 130, render: (_, session) => optionalText(session.username) },
  { title: '数据库', width: 130, render: (_, session) => optionalText(session.database_name) },
  { title: '客户端', width: 150, render: (_, session) => optionalText(session.client_address) },
  { title: '状态', width: 140, render: (_, session) => optionalText(session.state) },
  { title: '查询开始', width: 190, render: (_, session) => optionalTime(session.query_started_at) },
  { title: '事务开始', width: 190, render: (_, session) => optionalTime(session.transaction_started_at) },
  { title: '查询时长', width: 120, render: (_, session) => durationLabel(session.query_duration_ms) },
  { title: '事务时长', width: 120, render: (_, session) => durationLabel(session.transaction_duration_ms) },
  { title: '等待事件', width: 190, render: (_, session) => [session.wait_event_type, session.wait_event].filter(Boolean).join(' / ') || '—' },
  { title: '阻塞关系', width: 170, render: (_, session) => session.blocking_pids.length === 0 ? '无' : `被 PID ${session.blocking_pids.join(', ')} 阻塞` },
]

function DispositionTag({ disposition }: { disposition: AlertDisposition }) {
  switch (disposition) {
    case 'NONE': return <Tag>未处置</Tag>
    case 'ACKED': return <Tag color="processing">已确认</Tag>
    case 'IGNORED': return <Tag color="default">已忽略</Tag>
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

function triggerSnapshotTagColor(kind: TriggerSnapshotPresentation['kind']): 'success' | 'error' | 'default' {
  switch (kind) {
    case 'success': return 'success'
    case 'error': return 'error'
    case 'not-applicable': return 'default'
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
