import {
  CheckCircleOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  SaveOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import {
  Alert,
  Button,
  Descriptions,
  Divider,
  Input,
  InputNumber,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'

type Capability = components['schemas']['CapabilitySnapshotEntry']
type CollectionPause = components['schemas']['CollectionPauseStatus']
type CollectionTask = components['schemas']['CollectionTaskState']
type AgentRegistration = components['schemas']['AgentRegistration']
type TaskID = CollectionTask['task_id']
type StatusPresentation = { label: string; color?: string }

type CollectionManagementViewProps = {
  instanceName: string
  capabilities: Capability[]
  tasks: CollectionTask[]
  registration: AgentRegistration
  pause: CollectionPause
  agentMetricsEnabled: boolean
  canManage: boolean
  intervalPending: boolean
  pausePending: boolean
  error: string
  onIntervalChange: (taskID: TaskID, intervalSeconds: number) => void
  onPauseChange: (paused: boolean, reason: string) => void
}

type CollectionConfigurationProps = Pick<
  CollectionManagementViewProps,
  | 'tasks'
  | 'pause'
  | 'agentMetricsEnabled'
  | 'canManage'
  | 'intervalPending'
  | 'pausePending'
  | 'onIntervalChange'
  | 'onPauseChange'
>

export const collectionManagementRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/collection',
  component: CollectionManagementPage,
})

const pollingOptions = { refetchInterval: pollingIntervals.collectionManagement }

function CollectionManagementPage() {
  const { id } = collectionManagementRoute.useParams()
  const instanceQuery = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } }, pollingOptions)
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const tasksQuery = $api.useQuery('get', '/api/v1/instances/{id}/collection/tasks', { params: { path: { id } } }, pollingOptions)
  const capabilitiesQuery = $api.useQuery('get', '/api/v1/instances/{id}/collection/capabilities', { params: { path: { id } } }, pollingOptions)
  const registrationQuery = $api.useQuery('get', '/api/v1/instances/{id}/agent/registration', { params: { path: { id } } }, pollingOptions)
  const pauseQuery = $api.useQuery('get', '/api/v1/instances/{id}/collection/pause', { params: { path: { id } } }, pollingOptions)
  const intervalMutation = $api.useMutation('put', '/api/v1/instances/{id}/collection/tasks/{task_id}')
  const pauseMutation = $api.useMutation('put', '/api/v1/instances/{id}/collection/pause')
  const [error, setError] = useState('')

  function updateInterval(taskID: TaskID, intervalSeconds: number) {
    setError('')
    intervalMutation.mutate({
      params: { path: { id, task_id: taskID } },
      body: { interval_seconds: intervalSeconds },
    }, {
      onSuccess: () => void tasksQuery.refetch(),
      onError: (failure) => setError(apiErrorMessage(failure, '保存采样周期失败')),
    })
  }

  function updatePause(paused: boolean, reason: string) {
    setError('')
    pauseMutation.mutate({
      params: { path: { id } },
      body: { paused, ...(reason ? { reason } : {}) },
    }, {
      onSuccess: () => {
        void pauseQuery.refetch()
        void instanceQuery.refetch()
      },
      onError: (failure) => setError(apiErrorMessage(failure, paused ? '暂停采集失败' : '恢复采集失败')),
    })
  }

  const loading = instanceQuery.isPending || currentUserQuery.isPending || tasksQuery.isPending ||
    capabilitiesQuery.isPending || registrationQuery.isPending || pauseQuery.isPending
  if (loading) return <Spin />

  const registration = registrationQuery.data
  const pause = pauseQuery.data
  const instance = instanceQuery.data
  if (!registration || !pause || !instance || !tasksQuery.data || !capabilitiesQuery.data) {
    return <Alert type="error" showIcon title="无法加载采集管理数据" />
  }

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    <Link to="/instances/$id" params={{ id }} search={defaultTimeRange()}>返回实例详情</Link>
    <CollectionManagementView
      instanceName={instance.name}
      capabilities={capabilitiesQuery.data}
      tasks={tasksQuery.data}
      registration={registration}
      pause={pause}
      agentMetricsEnabled={instance.agent_metrics_enabled}
      canManage={currentUserQuery.data?.role === 'PLATFORM_ADMIN'}
      intervalPending={intervalMutation.isPending}
      pausePending={pauseMutation.isPending}
      error={error}
      onIntervalChange={updateInterval}
      onPauseChange={updatePause}
    />
  </Space>
}

export function CollectionManagementView({
  instanceName,
  capabilities,
  tasks,
  registration,
  pause,
  agentMetricsEnabled,
  canManage,
  intervalPending,
  pausePending,
  error,
  onIntervalChange,
  onPauseChange,
}: CollectionManagementViewProps) {
  const extensionCapabilities = capabilities.filter((capability) => capability.capability_id.startsWith('ext.'))
  const databaseCapabilities = capabilities.filter((capability) => !capability.capability_id.startsWith('ext.'))

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    <div>
      <Typography.Title level={2} style={{ margin: 0 }}>{instanceName}</Typography.Title>
      <Typography.Text type="secondary">采集管理</Typography.Text>
    </div>
    {error && <Alert type="error" showIcon title={error} />}

    <ConfigurationTodo capabilities={capabilities} tasks={tasks} />
    <Divider />
    <CollectionOverview tasks={tasks} pause={pause} />
    <Divider />
    <AgentStatus registration={registration} />
    <Divider />
    <CapabilityModule title="数据库连接与权限检查" capabilities={databaseCapabilities} />
    <Divider />
    <CapabilityModule title="扩展与插件能力" capabilities={extensionCapabilities} />
    <Divider />
    <MetricStatus tasks={tasks} />
    <Divider />
    <section aria-labelledby="collection-configuration-heading">
      <Typography.Title id="collection-configuration-heading" level={4}>采集配置</Typography.Title>
      <CollectionConfiguration
        tasks={tasks}
        pause={pause}
        agentMetricsEnabled={agentMetricsEnabled}
        canManage={canManage}
        intervalPending={intervalPending}
        pausePending={pausePending}
        onIntervalChange={onIntervalChange}
        onPauseChange={onPauseChange}
      />
    </section>
  </Space>
}

export function ConfigurationTodo({ capabilities, tasks }: { capabilities: Capability[]; tasks: CollectionTask[] }) {
  const unknownCapabilities = capabilities.filter((capability) => capability.status === 'UNKNOWN')
  const missingCapabilities = capabilities.filter((capability) => capability.class === 'fixable' && capability.status === 'MISSING')
  const latestObservation = latestTimestamp(capabilities.map((capability) => capability.observed_at))
  const showReadyState = capabilities.length > 0 && unknownCapabilities.length === 0 &&
    missingCapabilities.length === 0 && latestObservation !== undefined

  return <section role="region" aria-labelledby="configuration-todo-heading" aria-label="配置缺失待办">
    <Typography.Title id="configuration-todo-heading" level={4}>配置缺失待办</Typography.Title>
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      {(unknownCapabilities.length > 0 || capabilities.length === 0) && <Alert
        type="warning"
        showIcon
        icon={<WarningOutlined />}
        title="无法检查采集能力"
        description={unknownCapabilities.length > 0 ? `以下 ${unknownCapabilities.length} 项状态未知` : '能力快照不可用，状态未知'}
      />}
      {missingCapabilities.map((capability) => {
        const metrics = affectedMetrics(capability.capability_id, tasks)
        return <details className="collection-todo-item" key={capability.capability_id}>
          <summary>
            <span>缺少 {capabilityLabel(capability.capability_id)}</span>
            <Tag>影响 {capability.affected_metric_count} 项指标</Tag>
          </summary>
          <Typography.Paragraph>{capability.fix_hint}</Typography.Paragraph>
          {metrics.length > 0 && <Space wrap>{metrics.map((metricID) => <a key={metricID} href={`#metric-${metricID}`}>{metricID}</a>)}</Space>}
        </details>
      })}
      {showReadyState && <Alert
        type="success"
        showIcon
        icon={<CheckCircleOutlined />}
        title="无待办——所有可修复的采集能力均已就绪"
        description={`最近检查 ${formatTime(latestObservation)}`}
      />}
    </Space>
  </section>
}

function CollectionOverview({ tasks, pause }: { tasks: CollectionTask[]; pause: CollectionPause }) {
  const lastSuccess = latestTimestamp(tasks.map((task) => task.last_success_at))
  const consecutiveFailures = tasks.reduce((total, task) => total + task.consecutive_failures, 0)
  const status = collectionStatusPresentation(
    pause.paused,
    consecutiveFailures,
    tasks.some((task) => task.last_result === 'SUCCESS'),
  )

  return <section aria-labelledby="collection-overview-heading">
    <Typography.Title id="collection-overview-heading" level={4}>采集总状态</Typography.Title>
    <Descriptions size="small" column={{ xs: 1, sm: 2, md: 4 }} items={[
      { key: 'status', label: '当前状态', children: <Tag color={status.color}>{status.label}</Tag> },
      { key: 'connection', label: '数据库连接', children: taskResult(tasks.find((task) => task.task_id === 'pg.probe')?.last_result) },
      { key: 'success', label: '最近成功采集', children: formatOptionalTime(lastSuccess) },
      { key: 'freshness', label: '数据新鲜度', children: lastSuccess ? formatAge(lastSuccess) : '未知' },
      { key: 'failures', label: '连续失败数', children: consecutiveFailures },
    ]} />
  </section>
}

function AgentStatus({ registration }: { registration: AgentRegistration }) {
  const statePresentation = agentRegistrationPresentation(registration.state)
  return <section aria-labelledby="agent-status-heading">
    <Typography.Title id="agent-status-heading" level={4}>Agent 状态</Typography.Title>
    <Descriptions size="small" column={{ xs: 1, sm: 2 }} items={[
      { key: 'state', label: '登记状态', children: <Tag color={statePresentation.color}>{statePresentation.label}</Tag> },
      { key: 'heartbeat', label: '最近心跳', children: formatOptionalTime(registration.last_reported_at) },
      { key: 'version', label: '版本', children: registration.agent_version ?? '未上报' },
      { key: 'permission', label: '权限状态', children: agentPermissionStatus(registration) },
    ]} />
  </section>
}

function CapabilityModule({ title, capabilities }: { title: string; capabilities: Capability[] }) {
  return <section aria-labelledby={`${title}-heading`}>
    <Typography.Title id={`${title}-heading`} level={4}>{title}</Typography.Title>
    {capabilities.length === 0 ? <Typography.Text type="secondary">暂无能力项</Typography.Text> : <Table
      size="small"
      pagination={false}
      rowKey="capability_id"
      dataSource={capabilities}
      columns={[
        { title: '能力', render: (_, item) => capabilityLabel(item.capability_id) },
        { title: '状态', render: (_, item) => capabilityStatus(item) },
        { title: '最近检查', render: (_, item) => formatOptionalTime(item.observed_at) },
        { title: '影响指标', dataIndex: 'affected_metric_count' },
      ]}
    />}
  </section>
}

function MetricStatus({ tasks }: { tasks: CollectionTask[] }) {
  const metricRows = tasks.flatMap((task) => task.metric_ids.map((metricID) => ({ metricID, task })))
  return <section aria-labelledby="metric-status-heading" id="metric-status">
    <Typography.Title id="metric-status-heading" level={4}>指标采集状态</Typography.Title>
    <Table
      size="small"
      pagination={false}
      rowKey="metricID"
      dataSource={metricRows}
      onRow={(row) => ({ id: `metric-${row.metricID}` })}
      scroll={{ x: 780 }}
      columns={[
        { title: '指标 ID', dataIndex: 'metricID' },
        { title: '最近采集时间', render: (_, row) => formatOptionalTime(row.task.last_success_at) },
        { title: '当前状态', render: (_, row) => taskResult(row.task.last_result) },
        { title: '失败原因', render: (_, row) => row.task.last_error_message ?? '—' },
        { title: '所需能力', render: (_, row) => row.task.required_capabilities.length > 0 ? <Space wrap>{row.task.required_capabilities.map((item) => <Tag key={item}>{item}</Tag>)}</Space> : '无' },
      ]}
    />
  </section>
}

export function CollectionConfiguration({
  tasks,
  pause,
  agentMetricsEnabled,
  canManage,
  intervalPending,
  pausePending,
  onIntervalChange,
  onPauseChange,
}: CollectionConfigurationProps) {
  const [draftIntervals, setDraftIntervals] = useState<Partial<Record<TaskID, number>>>({})
  const [reason, setReason] = useState('')
  const disabledReason = canManage ? undefined : '需要平台管理员角色'

  return <Space orientation="vertical" size="large" style={{ width: '100%' }}>
    {!canManage && <Typography.Text type="secondary">需要平台管理员角色</Typography.Text>}
    <Table
      size="small"
      pagination={false}
      rowKey="task_id"
      dataSource={tasks}
      scroll={{ x: 680 }}
      columns={[
        { title: '任务', dataIndex: 'task_id' },
        { title: '配置周期', render: (_, task) => `${task.interval_seconds} 秒` },
        { title: '退避至', render: (_, task) => formatOptionalTime(task.next_eligible_at) },
        {
          title: '修改周期',
          render: (_, task) => <Space.Compact>
            <InputNumber
              aria-label={`${task.task_id} 采样周期`}
              min={5}
              suffix="秒"
              disabled={!canManage}
              value={draftIntervals[task.task_id] ?? task.interval_seconds}
              onChange={(value) => {
                if (value !== null) setDraftIntervals((current) => ({ ...current, [task.task_id]: value }))
              }}
            />
            <Tooltip title={disabledReason}>
              <Button
                aria-label={`保存 ${task.task_id} 采样周期`}
                icon={<SaveOutlined />}
                disabled={!canManage}
                loading={intervalPending}
                onClick={() => onIntervalChange(task.task_id, draftIntervals[task.task_id] ?? task.interval_seconds)}
              />
            </Tooltip>
          </Space.Compact>,
        },
      ]}
    />

    <Descriptions size="small" column={1} items={[
      {
        key: 'agent-metrics',
        label: '启用 Agent 指标',
        children: <Tooltip title="只读"><Switch checked={agentMetricsEnabled} disabled /></Tooltip>,
      },
      {
        key: 'pause',
        label: '暂停采集',
        children: <Space wrap>
          <Input aria-label="暂停原因" placeholder="原因（选填）" value={reason} disabled={!canManage || pausePending} onChange={(event) => setReason(event.target.value)} style={{ width: 240 }} />
          <Tooltip title={disabledReason}>
            <Switch
              aria-label="暂停采集"
              checked={pause.paused}
              checkedChildren={<PauseCircleOutlined />}
              unCheckedChildren={<PlayCircleOutlined />}
              disabled={!canManage}
              loading={pausePending}
              onChange={(checked) => onPauseChange(checked, reason)}
            />
          </Tooltip>
          {pause.updated_at && <Typography.Text type="secondary">最近操作 {formatTime(pause.updated_at)}</Typography.Text>}
        </Space>,
      },
    ]} />
  </Space>
}

function affectedMetrics(capabilityID: Capability['capability_id'], tasks: CollectionTask[]): string[] {
  return [...new Set(tasks
    .filter((task) => task.required_capabilities.includes(capabilityID))
    .flatMap((task) => task.metric_ids))]
}

function capabilityLabel(id: Capability['capability_id']): string {
  switch (id) {
    case 'role.pg_monitor': return 'pg_monitor 角色'
    case 'ext.pg_stat_statements': return 'pg_stat_statements 扩展'
    case 'topo.has_replication': return '复制拓扑'
    case 'topo.has_slot': return 'replication slot'
    default: return assertNever(id)
  }
}

function capabilityStatus(capability: Capability) {
  switch (capability.status) {
    case 'PRESENT': return <Tag color="success">已具备</Tag>
    case 'MISSING': return <Tag color="warning">缺失</Tag>
    case 'NOT_APPLICABLE': return <Space><Tag>不适用</Tag><Typography.Text type="secondary">{capability.na_reason}</Typography.Text></Space>
    case 'UNKNOWN': return <Tag icon={<WarningOutlined />} color="default">状态未知</Tag>
    default: return assertNever(capability.status)
  }
}

function taskResult(result: CollectionTask['last_result']): string {
  if (result === undefined) return '尚未采集'
  switch (result) {
    case 'SUCCESS': return '成功'
    case 'FAILED': return '失败'
    case 'TIMED_OUT': return '超时'
    case 'SKIPPED_BACKPRESSURE': return '平台背压跳过'
    case 'BACKOFF': return '退避中'
    default: return assertNever(result)
  }
}

function collectionStatusPresentation(
  paused: boolean,
  consecutiveFailures: number,
  hasSuccessfulTask: boolean,
): StatusPresentation {
  if (paused) return { label: '已暂停', color: 'default' }
  if (consecutiveFailures > 0) return { label: '存在采集失败', color: 'error' }
  if (hasSuccessfulTask) return { label: '正常', color: 'success' }
  return { label: '尚未采集', color: 'default' }
}

function agentRegistrationPresentation(state: AgentRegistration['state']): StatusPresentation {
  switch (state) {
    case 'NEVER_REGISTERED': return { label: '未安装' }
    case 'EXPECTED_ONLINE': return { label: '应在线', color: 'success' }
    case 'REVOKED': return { label: '令牌已吊销', color: 'error' }
    case 'DISABLED': return { label: '已停用' }
    default: return assertNever(state)
  }
}

function agentPermissionStatus(registration: AgentRegistration): string {
  if (registration.state === 'REVOKED') return '令牌已吊销'
  if (registration.agent_expected) return '允许上报'
  return '未启用'
}

function latestTimestamp(values: (string | undefined)[]): string | undefined {
  return values.filter((value): value is string => Boolean(value)).sort().at(-1)
}

function formatOptionalTime(value: string | undefined): string {
  return value ? formatTime(value) : '暂无'
}

function formatTime(value: string): string {
  return new Date(value).toISOString().slice(0, 19).replace('T', ' ')
}

function formatAge(value: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  return `${Math.floor(seconds / 3600)} 小时前`
}

function assertNever(value: never): never {
  throw new Error(`unhandled collection value: ${value}`)
}
