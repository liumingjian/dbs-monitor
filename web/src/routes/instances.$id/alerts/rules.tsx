import {
  AppstoreAddOutlined,
  BellOutlined,
  CopyOutlined,
  EditOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { $api } from '../../../api/client'
import { apiErrorMessage, apiFieldErrors } from '../../../api/errors'
import type { components } from '../../../api/schema'
import { rootRoute } from '../../root'
import { defaultTimeRange } from '../timeRange'
import { metricOptions } from '../metricOptions'
import { WorkbenchHeader } from '../workbench'

type AlertRule = components['schemas']['AlertRule']
type AlertRuleInput = components['schemas']['AlertRuleInput']
type AlertRuleTemplate = components['schemas']['AlertRuleTemplate']
type AlertAggregation = components['schemas']['AlertAggregation']
type AlertSeverity = components['schemas']['AlertSeverity']
type AlertRuleScope = components['schemas']['AlertRuleScope']
type NoDataPolicy = components['schemas']['NoDataPolicy']
type Role = components['schemas']['Role']
type CollectionTask = components['schemas']['CollectionTaskState']
type Capability = components['schemas']['CapabilitySnapshotEntry']
type Instance = components['schemas']['Instance']
type NotificationPolicy = components['schemas']['NotificationPolicy']
type CapabilityFit = 'SATISFIED' | 'UNSATISFIED' | 'UNKNOWN'

const alertableMetricOptions = metricOptions.filter(({ id }) => (
  id !== 'pg.replication.role' && id !== 'pg.replication.replay_lag_ms'
))

const defaultRule: AlertRuleInput = {
  name: '',
  metric_id: 'pg.connection.total',
  aggregation: 'latest',
  operator: '>',
  threshold: 80,
  recovery_operator: '<',
  recovery_threshold: 70,
  window_seconds: 60,
  consecutive_count: 3,
  recovery_consecutive_count: 3,
  severity: 'warning',
  no_data_policy: 'mark_no_data',
  scope: 'ALL',
  instance_ids: [],
  evaluation_interval_seconds: 30,
  enabled: true,
}

export const alertRulesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts/rules',
  component: AlertRulesPage,
})

function AlertRulesPage() {
  const { id } = alertRulesRoute.useParams()
  const rulesQuery = $api.useQuery('get', '/api/v1/alert-rules')
  const templatesQuery = $api.useQuery('get', '/api/v1/alert-rule-templates')
  const instancesQuery = $api.useQuery('get', '/api/v1/instances')
  const instanceQuery = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const tasksQuery = $api.useQuery('get', '/api/v1/instances/{id}/collection/tasks', { params: { path: { id } } })
  const capabilitiesQuery = $api.useQuery('get', '/api/v1/instances/{id}/collection/capabilities', { params: { path: { id } } })
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const policiesQuery = $api.useQuery('get', '/api/v1/notification-policies')
  const createMutation = $api.useMutation('post', '/api/v1/alert-rules')
  const updateMutation = $api.useMutation('put', '/api/v1/alert-rules/{id}')
  const enableMutation = $api.useMutation('put', '/api/v1/alert-rules/{id}/enabled')
  const copyMutation = $api.useMutation('post', '/api/v1/alert-rules/{id}/copies')
  const templateMutation = $api.useMutation('post', '/api/v1/alert-rule-templates/{id}/alert-rules')
  const [editorOpen, setEditorOpen] = useState(false)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null)
  const [actionError, setActionError] = useState('')
  const [form] = Form.useForm<AlertRuleInput>()
  const canWrite = canWriteAlertRules(currentUserQuery.data?.role)
  const disabledReason = canWrite ? undefined : '需要告警管理员角色'

  function refreshRules() {
    void rulesQuery.refetch()
  }

  function openCreate() {
    setActionError('')
    setEditingRule(null)
    form.resetFields()
    form.setFieldsValue(defaultRule)
    setEditorOpen(true)
  }

  function openEdit(rule: AlertRule) {
    setActionError('')
    setEditingRule(rule)
    form.resetFields()
    form.setFieldsValue(ruleInput(rule))
    setEditorOpen(true)
  }

  function submitRule(values: AlertRuleInput) {
    setActionError('')
    const onSuccess = () => {
      setEditorOpen(false)
      form.resetFields()
      refreshRules()
    }
    const onError = (failure: unknown) => {
      form.setFields(alertRuleFieldErrors(failure))
      setActionError(apiErrorMessage(failure, '保存告警规则失败'))
    }
    if (editingRule) {
      updateMutation.mutate({ params: { path: { id: editingRule.id } }, body: values }, { onSuccess, onError })
      return
    }
    createMutation.mutate({ body: values }, { onSuccess, onError })
  }

  function setEnabled(rule: AlertRule, enabled: boolean) {
    setActionError('')
    enableMutation.mutate({ params: { path: { id: rule.id } }, body: { enabled } }, {
      onSuccess: refreshRules,
      onError: (failure) => setActionError(apiErrorMessage(failure, '更新启停状态失败')),
    })
  }

  function copyRule(rule: AlertRule) {
    setActionError('')
    copyMutation.mutate({ params: { path: { id: rule.id } }, body: { name: `${rule.name} 副本` } }, {
      onSuccess: refreshRules,
      onError: (failure) => setActionError(apiErrorMessage(failure, '复制告警规则失败')),
    })
  }

  function createFromTemplate(template: AlertRuleTemplate) {
    setActionError('')
    templateMutation.mutate({ params: { path: { id: template.id } }, body: {} }, {
      onSuccess: () => {
        setTemplateOpen(false)
        refreshRules()
      },
      onError: (failure) => setActionError(apiErrorMessage(failure, '从模板创建规则失败')),
    })
  }

  const columns = alertRuleColumns({
    canWrite,
    disabledReason,
    currentInstance: instanceQuery.data,
    tasks: tasksQuery.data ?? [],
    capabilities: capabilitiesQuery.data ?? [],
    onEdit: openEdit,
    onCopy: copyRule,
    onEnabledChange: setEnabled,
    actionPending: enableMutation.isPending || copyMutation.isPending,
  })

  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="alerts" search={defaultTimeRange()} />
    <Tabs activeKey="rules" items={[
      { key: 'rules', label: <><BellOutlined /> 告警规则</> },
      { key: 'current', label: <Link to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current', include_paused: false }}>当前告警</Link> },
      { key: 'history', label: <Link to="/instances/$id/alerts" params={{ id }} search={{ tab: 'history', include_paused: false }}>告警历史</Link> },
    ]} />

    <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
      <Typography.Title level={3} style={{ margin: 0 }}>告警规则</Typography.Title>
      <Space wrap>
        <Button icon={<AppstoreAddOutlined />} onClick={() => setTemplateOpen(true)}>规则模板</Button>
        <Tooltip title={disabledReason}><span>
          <Button type="primary" icon={<PlusOutlined />} disabled={!canWrite} onClick={openCreate}>新建规则</Button>
        </span></Tooltip>
      </Space>
    </Space>
    {!canWrite && <Alert type="info" showIcon title="只读模式：修改告警规则需要告警管理员角色。" />}
    {actionError && <Alert type="error" showIcon title={actionError} closable onClose={() => setActionError('')} />}
    <Table<AlertRule>
      rowKey="id"
      loading={rulesQuery.isPending}
      dataSource={rulesQuery.data ?? []}
      columns={columns}
      pagination={{ pageSize: 50, showSizeChanger: false }}
      scroll={{ x: 2100 }}
    />

    <RuleEditor
      form={form}
      open={editorOpen}
      editingRule={editingRule}
      instances={instancesQuery.data ?? []}
      policies={policiesQuery.data ?? []}
      pending={createMutation.isPending || updateMutation.isPending}
      onCancel={() => setEditorOpen(false)}
      onSubmit={submitRule}
    />
    <TemplateModal
      open={templateOpen}
      templates={templatesQuery.data ?? []}
      loading={templatesQuery.isPending}
      canWrite={canWrite}
      disabledReason={disabledReason}
      actionPending={templateMutation.isPending}
      onCancel={() => setTemplateOpen(false)}
      onCreate={createFromTemplate}
    />
  </Space>
}

function RuleEditor({ form, open, editingRule, instances, policies, pending, onCancel, onSubmit }: {
  form: ReturnType<typeof Form.useForm<AlertRuleInput>>[0]
  open: boolean
  editingRule: AlertRule | null
  instances: Instance[]
  policies: NotificationPolicy[]
  pending: boolean
  onCancel: () => void
  onSubmit: (values: AlertRuleInput) => void
}) {
  const consecutiveCount = Form.useWatch('consecutive_count', form)
  const evaluationInterval = Form.useWatch('evaluation_interval_seconds', form)
  const scope = Form.useWatch('scope', form)
  const isBuiltin = editingRule?.is_builtin === true

  return <Modal
    title={editingRule ? '编辑告警规则' : '新建告警规则'}
    open={open}
    width={860}
    footer={null}
    destroyOnHidden
    onCancel={onCancel}
  >
    <Form<AlertRuleInput>
      form={form}
      layout="vertical"
      initialValues={defaultRule}
      onFinish={onSubmit}
      onValuesChange={(changed) => {
        if (changed.scope === 'ALL') form.setFieldValue('instance_ids', [])
      }}
    >
      <div className="rule-editor-grid">
        <Form.Item name="name" label="规则名称" rules={[{ required: true, whitespace: true, message: '请输入规则名称' }]}>
          <Input />
        </Form.Item>
        <Form.Item name="metric_id" label="指标" rules={[{ required: true }]}>
          <Select
            showSearch
            disabled={isBuiltin}
            optionFilterProp="label"
            options={alertableMetricOptions.map((option) => ({ value: option.id, label: `${option.label} · ${option.id}` }))}
          />
        </Form.Item>
        <Form.Item name="aggregation" label="窗口聚合" rules={[{ required: true }]}>
          <Select options={aggregationOptions} />
        </Form.Item>
        <Space.Compact block>
          <Form.Item name="operator" label="触发条件" rules={[{ required: true }]} style={{ width: '40%' }}>
            <Select options={operatorOptions} />
          </Form.Item>
          <Form.Item name="threshold" label="触发阈值" rules={[{ required: true }]} style={{ width: '60%' }}>
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
        </Space.Compact>
        <Space.Compact block>
          <Form.Item name="recovery_operator" label="恢复条件" rules={[{ required: true }]} style={{ width: '40%' }}>
            <Select options={operatorOptions} />
          </Form.Item>
          <Form.Item name="recovery_threshold" label="恢复阈值" rules={[{ required: true, message: '请输入独立恢复阈值' }]} style={{ width: '60%' }}>
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
        </Space.Compact>
        <Form.Item name="evaluation_interval_seconds" label="评估周期（秒）" rules={[{ required: true }]}>
          <InputNumber min={5} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="window_seconds" label="窗口（秒）" rules={[{ required: true }]}>
          <InputNumber min={1} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item
          name="consecutive_count"
          label="连续次数"
          rules={[{ required: true }]}
          extra={positiveInteger(consecutiveCount) && positiveInteger(evaluationInterval)
            ? consecutiveDurationLabel(consecutiveCount, evaluationInterval)
            : undefined}
        >
          <InputNumber min={1} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="recovery_consecutive_count" label="恢复连续次数" rules={[{ required: true }]}>
          <InputNumber min={1} precision={0} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="severity" label="级别" rules={[{ required: true }]}>
          <Select options={isBuiltin ? builtinSeverityOptions : severityOptions} />
        </Form.Item>
        <Form.Item name="no_data_policy" label="无数据策略" rules={[{ required: true }]}>
          <Select options={noDataOptions} />
        </Form.Item>
        <Form.Item name="scope" label="作用范围" rules={[{ required: true }]}>
          <Select disabled={isBuiltin} options={scopeOptions} />
        </Form.Item>
        <Form.Item
          name="instance_ids"
          label="实例"
          rules={scope === 'INSTANCES' ? [{ required: true, message: '请至少选择一个实例' }] : undefined}
        >
          <Select
            mode="multiple"
            disabled={scope !== 'INSTANCES' || isBuiltin}
            options={instances.map((instance) => ({ value: instance.id, label: instance.name }))}
          />
        </Form.Item>
        <Form.Item
          name="notification_policy_id"
          label="通知策略"
          extra={editingRule?.notification_policy_id ? `当前生效：${editingRule.effective_notification_policy_name}` : '当前生效：默认策略（继承）'}
        >
          <Select
            allowClear
            placeholder="默认策略（继承）"
            options={policies.filter((policy) => !policy.is_default).map((policy) => ({ value: policy.id, label: policy.name }))}
          />
        </Form.Item>
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch disabled={isBuiltin} />
        </Form.Item>
      </div>
      <Button type="primary" htmlType="submit" loading={pending}>保存</Button>
    </Form>
  </Modal>
}

function TemplateModal({ open, templates, loading, canWrite, disabledReason, actionPending, onCancel, onCreate }: {
  open: boolean
  templates: AlertRuleTemplate[]
  loading: boolean
  canWrite: boolean
  disabledReason: string | undefined
  actionPending: boolean
  onCancel: () => void
  onCreate: (template: AlertRuleTemplate) => void
}) {
  const columns: TableColumnsType<AlertRuleTemplate> = [
    {
      title: '模板',
      render: (_, template) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{template.name}</Typography.Text>
          <Typography.Text type="secondary">v{template.version}</Typography.Text>
        </Space>
      ),
    },
    { title: '指标', render: (_, template) => metricName(template.metric_id) },
    { title: '条件', render: (_, template) => `${aggregationLabel(template.aggregation)} ${template.operator} ${template.threshold}` },
    { title: '持续', render: (_, template) => consecutiveDurationLabel(template.consecutive_count, template.evaluation_interval_seconds) },
    { title: '级别', render: (_, template) => severityTag(template.severity) },
    {
      title: '操作',
      fixed: 'right',
      width: 140,
      render: (_, template) => <Tooltip title={disabledReason}><span>
        <Button
          type="link"
          icon={<AppstoreAddOutlined />}
          disabled={!canWrite}
          loading={actionPending}
          onClick={() => onCreate(template)}
        >一键创建</Button>
      </span></Tooltip>,
    },
  ]
  return <Modal title="内置规则模板" open={open} width={1000} footer={null} onCancel={onCancel}>
    <Table rowKey="id" loading={loading} dataSource={templates} columns={columns} pagination={false} scroll={{ x: 900 }} />
  </Modal>
}

function alertRuleColumns({ canWrite, disabledReason, currentInstance, tasks, capabilities, onEdit, onCopy, onEnabledChange, actionPending }: {
  canWrite: boolean
  disabledReason: string | undefined
  currentInstance: Instance | undefined
  tasks: CollectionTask[]
  capabilities: Capability[]
  onEdit: (rule: AlertRule) => void
  onCopy: (rule: AlertRule) => void
  onEnabledChange: (rule: AlertRule, enabled: boolean) => void
  actionPending: boolean
}): TableColumnsType<AlertRule> {
  return [
    {
      title: '名称',
      fixed: 'left',
      width: 190,
      render: (_, rule) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{rule.name}</Typography.Text>
          {rule.is_builtin && <Typography.Text type="secondary">内置规则</Typography.Text>}
        </Space>
      ),
    },
    { title: '范围', width: 120, render: (_, rule) => scopeLabel(rule.scope, rule.instance_ids.length) },
    {
      title: '指标',
      width: 210,
      render: (_, rule) => (
        <Tooltip title={rule.metric_id}>
          <span>{metricName(rule.metric_id)}</span>
        </Tooltip>
      ),
    },
    {
      title: '条件',
      width: 250,
      render: (_, rule) => (
        <Space direction="vertical" size={0}>
          <span>{aggregationLabel(rule.aggregation)} {rule.operator} {rule.threshold}</span>
          <Typography.Text type="secondary">
            恢复 {rule.recovery_operator} {rule.recovery_threshold}
          </Typography.Text>
        </Space>
      ),
    },
    { title: '评估周期', width: 110, render: (_, rule) => formatRuleDuration(rule.evaluation_interval_seconds) },
    { title: '窗口', width: 100, render: (_, rule) => formatRuleDuration(rule.window_seconds) },
    { title: '连续次数', width: 245, render: (_, rule) => consecutiveDurationLabel(rule.consecutive_count, rule.evaluation_interval_seconds) },
    { title: '级别', width: 90, render: (_, rule) => severityTag(rule.severity) },
    {
      title: '启停状态',
      width: 120,
      render: (_, rule) => {
        if (rule.is_builtin) {
          return <Tag color="blue">不可停用</Tag>
        }
        return <Tooltip title={disabledReason}>
          <span>
            <Switch
              size="small"
              checked={rule.enabled}
              disabled={!canWrite || actionPending}
              onChange={(checked) => onEnabledChange(rule, checked)}
            />
          </span>
        </Tooltip>
      },
    },
    { title: '生效通知策略', width: 180, dataIndex: 'effective_notification_policy_name' },
    {
      title: '最近触发时间',
      width: 180,
      render: (_, rule) => rule.last_triggered_at
        ? new Date(rule.last_triggered_at).toLocaleString()
        : '尚未触发',
    },
    { title: '当前告警数', width: 110, dataIndex: 'current_alert_count' },
    { title: '所需能力', width: 120, render: (_, rule) => capabilityFitTag(capabilityFit(rule.metric_id, tasks, capabilities, currentInstance)) },
    {
      title: '操作',
      fixed: 'right',
      width: 170,
      render: (_, rule) => (
        <Space size="small">
          <Tooltip title={disabledReason}>
            <span>
              <Button
                aria-label={`编辑 ${rule.name}`}
                type="text"
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => onEdit(rule)}
              />
            </span>
          </Tooltip>
          <Tooltip title={disabledReason}>
            <span>
              <Button
                aria-label={`复制 ${rule.name}`}
                type="text"
                icon={<CopyOutlined />}
                disabled={!canWrite || actionPending}
                onClick={() => onCopy(rule)}
              />
            </span>
          </Tooltip>
        </Space>
      ),
    },
  ]
}

export function formatRuleDuration(totalSeconds: number): string {
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts: string[] = []

  if (hours > 0) parts.push(`${hours} 小时`)
  if (minutes > 0) parts.push(`${minutes} 分`)
  if (seconds > 0) parts.push(`${seconds} 秒`)

  return parts.join(' ')
}

export function consecutiveDurationLabel(count: number, intervalSeconds: number): string {
  return `连续 ${count} 次 × ${formatRuleDuration(intervalSeconds)} ≈ ${formatRuleDuration(count * intervalSeconds)}`
}

export function capabilityFit(
  metricID: string,
  tasks: CollectionTask[],
  capabilities: Capability[],
  instance: Instance | undefined,
): CapabilityFit {
  if (metricID.startsWith('host.')) {
    if (!instance) return 'UNKNOWN'
    if (instance.agent_metrics_enabled && instance.agent_status === 'online') return 'SATISFIED'
    return 'UNSATISFIED'
  }
  if (metricID === 'agent.status') {
    if (!instance) return 'UNKNOWN'
    if (instance.agent_status === 'not_installed') return 'UNSATISFIED'
    return 'SATISFIED'
  }
  const task = tasks.find((item) => item.metric_ids.includes(metricID))
  if (!task || task.required_capabilities.length === 0) return 'SATISFIED'

  let fit: CapabilityFit = 'SATISFIED'
  for (const required of task.required_capabilities) {
    const capability = capabilities.find((item) => item.capability_id === required)
    if (!capability) return 'UNKNOWN'
    switch (capability.status) {
      case 'PRESENT':
        break
      case 'MISSING':
      case 'NOT_APPLICABLE':
        return 'UNSATISFIED'
      case 'UNKNOWN':
        fit = 'UNKNOWN'
        break
      default:
        return assertNever(capability.status)
    }
  }
  return fit
}

function capabilityFitTag(fit: CapabilityFit) {
  switch (fit) {
    case 'SATISFIED': return <Tag color="green">满足</Tag>
    case 'UNSATISFIED': return <Tag color="red">不满足</Tag>
    case 'UNKNOWN': return <Tag>未知</Tag>
    default: return assertNever(fit)
  }
}

function ruleInput(rule: AlertRule): AlertRuleInput {
  return {
    name: rule.name,
    metric_id: rule.metric_id,
    aggregation: rule.aggregation,
    operator: rule.operator,
    threshold: rule.threshold,
    recovery_operator: rule.recovery_operator,
    recovery_threshold: rule.recovery_threshold,
    window_seconds: rule.window_seconds,
    consecutive_count: rule.consecutive_count,
    recovery_consecutive_count: rule.recovery_consecutive_count,
    severity: rule.severity,
    no_data_policy: rule.no_data_policy,
    scope: rule.scope,
    instance_ids: rule.instance_ids,
    evaluation_interval_seconds: rule.evaluation_interval_seconds,
    enabled: rule.enabled,
    notification_policy_id: rule.notification_policy_id,
  }
}

function alertRuleFieldErrors(failure: unknown): { name: keyof AlertRuleInput; errors: string[] }[] {
  return apiFieldErrors(failure).flatMap((item) => (
    isAlertRuleField(item.name) ? [{ name: item.name, errors: item.errors }] : []
  ))
}

const alertRuleFieldNames = [
  'name',
  'metric_id',
  'aggregation',
  'operator',
  'threshold',
  'recovery_operator',
  'recovery_threshold',
  'window_seconds',
  'consecutive_count',
  'recovery_consecutive_count',
  'severity',
  'no_data_policy',
  'scope',
  'instance_ids',
  'evaluation_interval_seconds',
  'enabled',
  'notification_policy_id',
] as const satisfies readonly (keyof AlertRuleInput)[]

function isAlertRuleField(value: string): value is keyof AlertRuleInput {
  return (alertRuleFieldNames as readonly string[]).includes(value)
}

function metricName(metricID: string): string {
  return metricOptions.find((option) => option.id === metricID)?.label ?? metricID
}

function positiveInteger(value: number | undefined): value is number {
  return value !== undefined && Number.isInteger(value) && value > 0
}

function canWriteAlertRules(role: Role | undefined): boolean {
  if (!role) return false
  switch (role) {
    case 'READONLY': return false
    case 'ALERT_ADMIN':
    case 'PLATFORM_ADMIN': return true
    default: return assertNever(role)
  }
}

function aggregationLabel(value: AlertAggregation): string {
  switch (value) {
    case 'latest': return '最新值'
    case 'avg': return '平均值'
    case 'max': return '最大值'
    case 'min': return '最小值'
    case 'sum': return '总和'
    case 'count': return '样本数'
    default: return assertNever(value)
  }
}

function severityLabel(value: AlertSeverity): string {
  switch (value) {
    case 'critical': return '严重'
    case 'warning': return '警告'
    case 'info': return '提示'
    default: return assertNever(value)
  }
}

function severityTag(value: AlertSeverity) {
  switch (value) {
    case 'critical': return <Tag color="red">{severityLabel(value)}</Tag>
    case 'warning': return <Tag color="orange">{severityLabel(value)}</Tag>
    case 'info': return <Tag color="blue">{severityLabel(value)}</Tag>
    default: return assertNever(value)
  }
}

function noDataLabel(value: NoDataPolicy): string {
  switch (value) {
    case 'ignore': return '忽略缺数'
    case 'mark_no_data': return '标记 NO_DATA'
    default: return assertNever(value)
  }
}

function scopeLabel(value: AlertRuleScope, instanceCount: number): string {
  switch (value) {
    case 'ALL': return '全部实例'
    case 'INSTANCES': return `${instanceCount} 个实例`
    default: return assertNever(value)
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled alert rule value: ${value}`)
}

const aggregationOptions = (['latest', 'avg', 'max', 'min', 'sum', 'count'] as const).map((value) => ({ value, label: aggregationLabel(value) }))
const operatorOptions = (['>', '>=', '<', '<=', '=', '!='] as const).map((value) => ({ value, label: value }))
const severityOptions = (['critical', 'warning', 'info'] as const).map((value) => ({ value, label: severityLabel(value) }))
const builtinSeverityOptions = severityOptions.filter((option) => option.value !== 'info')
const noDataOptions = (['ignore', 'mark_no_data'] as const).map((value) => ({ value, label: noDataLabel(value) }))
const scopeOptions = (['ALL', 'INSTANCES'] as const).map((value) => ({ value, label: scopeLabel(value, 0) }))
