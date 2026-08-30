import { Accordion, AccordionItem, Button, TextInput } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { elapsedLabel } from '../../domain/Freshness'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { KeyValueList } from '../../primitives/KeyValueList'
import { NotificationBar } from '../../primitives/NotificationBar'
import { NumberInput } from '../../primitives/NumberInput'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import type { StatusTone } from '../../primitives/StatusBadge'
import { StatusDot } from '../../primitives/StatusDot'
import { Toggle } from '../../primitives/Toggle'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'
import './collection.css'

type Capability = components['schemas']['CapabilitySnapshotEntry']
type CollectionPause = components['schemas']['CollectionPauseStatus']
type CollectionTask = components['schemas']['CollectionTaskState']
type AgentRegistration = components['schemas']['AgentRegistration']
type TaskID = CollectionTask['task_id']
type StatusPresentation = { label: string; tone: StatusTone }
type MetricRow = { metricID: string; task: CollectionTask }

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

/// 只读用户看到的禁用原因。**整块说一次就够**：原因写在通知条里，控件自己只在
/// `title` 上带一份给悬停用 —— 同一句话在页面上重复十遍不会让它更清楚。
const manageDisabledReason = '需要平台管理员角色'

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
  const registration = registrationQuery.data
  const pause = pauseQuery.data
  const instance = instanceQuery.data
  const loaded = !loading && registration !== undefined && pause !== undefined && instance !== undefined &&
    tasksQuery.data !== undefined && capabilitiesQuery.data !== undefined

  return <div className="collection-page">
    <Link
      className="cds--link collection-page__back"
      to="/instances/$id"
      params={{ id }}
      search={defaultTimeRange()}
    ><Icon name="arrowLeft" /> 返回实例详情</Link>

    {/* 规范要求骨架占位而不是整页转圈：版式先立起来，读者知道自己在等什么。 */}
    {loading && <div className="collection-page__skeleton">
      <SkeletonBlock lines={2} label="采集管理加载中" />
      <SkeletonBlock lines={4} decorative />
      <SkeletonBlock lines={6} decorative />
    </div>}

    {!loading && !loaded && <NotificationBar tone="critical" title="无法加载采集管理数据" />}

    {!loading && registration && pause && instance && tasksQuery.data && capabilitiesQuery.data && <CollectionManagementView
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
    />}
  </div>
}

/// 采集管理的六个模块。
///
/// 顺序是「要我做什么 → 现在怎么样 → 每一项怎么样 → 我能改什么」：待办排在最前面，
/// 因为它是这一页唯一带动作的模块；采集配置排在最后，改它之前先把前面看完。
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

  return <div className="collection-page__content">
    <header className="collection-page__header">
      <h1 className="dbs-page-title">{instanceName}</h1>
      <p className="dbs-caption">采集管理</p>
    </header>

    {error !== '' && <NotificationBar tone="critical" title={error} />}

    <ConfigurationTodo capabilities={capabilities} tasks={tasks} />

    <div className="collection-page__pair">
      <CollectionOverview tasks={tasks} pause={pause} />
      <AgentStatus registration={registration} />
    </div>

    <CapabilityModule
      title="数据库连接与权限检查"
      capabilities={databaseCapabilities}
      rowTestId="database-capability-row"
    />
    <CapabilityModule
      title="扩展与插件能力"
      capabilities={extensionCapabilities}
      rowTestId="extension-capability-row"
    />
    <MetricStatus tasks={tasks} />

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
  </div>
}

/// 配置缺失待办。
///
/// **这个模块从不隐藏**：没有待办要说「没有待办，最近检查在什么时候」，快照取不到也要
/// 说清楚是「查不了」而不是「没问题」—— 两者看起来一样，代价完全不同。
export function ConfigurationTodo({ capabilities, tasks }: { capabilities: Capability[]; tasks: CollectionTask[] }) {
  const unknownCapabilities = capabilities.filter((capability) => capability.status === 'UNKNOWN')
  const missingCapabilities = capabilities.filter((capability) => capability.class === 'fixable' && capability.status === 'MISSING')
  const latestObservation = latestTimestamp(capabilities.map((capability) => capability.observed_at))
  const showReadyState = capabilities.length > 0 && unknownCapabilities.length === 0 &&
    missingCapabilities.length === 0 && latestObservation !== undefined

  // `role="region"` 显式写出来：`<section>` 只有带可访问名时才隐式是 region，
  // 而这一块是「按角色 + 名字」定位的锚点（web/CLAUDE.md「测试定位」），
  // 不该依赖隐式映射。名字仍然由面板标题给。
  return <Panel role="region" title="配置缺失待办" className="collection-todo">
    <div className="collection-todo__body">
      {(unknownCapabilities.length > 0 || capabilities.length === 0) && <NotificationBar
        tone="warning"
        title="无法检查采集能力"
      >
        <p className="dbs-caption">
          {unknownCapabilities.length > 0 ? `以下 ${unknownCapabilities.length} 项状态未知` : '能力快照不可用，状态未知'}
        </p>
      </NotificationBar>}

      {missingCapabilities.length > 0 && <Accordion className="collection-todo__list">
        {missingCapabilities.map((capability) => {
          const metrics = affectedMetrics(capability.capability_id, tasks)
          return <AccordionItem
            key={capability.capability_id}
            title={<span className="collection-todo__summary">
              <span className="dbs-body">缺少 {capabilityLabel(capability.capability_id)}</span>
              <span className="dbs-caption collection-todo__count">影响 {capability.affected_metric_count} 项指标</span>
            </span>}
          >
            <p className="dbs-body collection-todo__hint">{capability.fix_hint}</p>
            {metrics.length > 0 && <p className="collection-todo__metrics">
              {metrics.map((metricID) => (
                <a className="cds--link dbs-numeric" key={metricID} href={`#metric-${metricID}`}>{metricID}</a>
              ))}
            </p>}
          </AccordionItem>
        })}
      </Accordion>}

      {showReadyState && <NotificationBar tone="normal" title="无待办——所有可修复的采集能力均已就绪">
        <p className="dbs-caption">最近检查 {formatTime(latestObservation)}</p>
      </NotificationBar>}
    </div>
  </Panel>
}

function CollectionOverview({ tasks, pause }: { tasks: CollectionTask[]; pause: CollectionPause }) {
  const lastSuccess = latestTimestamp(tasks.map((task) => task.last_success_at))
  const consecutiveFailures = tasks.reduce((total, task) => total + task.consecutive_failures, 0)
  const status = collectionStatusPresentation(
    pause.paused,
    consecutiveFailures,
    tasks.some((task) => task.last_result === 'SUCCESS'),
  )

  return <Panel title="采集总状态">
    <KeyValueList label="采集总状态" items={[
      { key: 'status', label: '当前状态', value: <StatusDot tone={status.tone}>{status.label}</StatusDot> },
      { key: 'connection', label: '数据库连接', value: taskResultDot(tasks.find((task) => task.task_id === 'pg.probe')?.last_result) },
      { key: 'success', label: '最近成功采集', value: formatOptionalTime(lastSuccess) },
      { key: 'freshness', label: '数据新鲜度', value: lastSuccess ? formatAge(lastSuccess) : '未知' },
      { key: 'failures', label: '连续失败数', value: <span className="dbs-numeric">{consecutiveFailures}</span> },
    ]} />
  </Panel>
}

function AgentStatus({ registration }: { registration: AgentRegistration }) {
  const statePresentation = agentRegistrationPresentation(registration.state)
  return <Panel title="Agent 状态">
    <KeyValueList label="Agent 状态" items={[
      { key: 'state', label: '登记状态', value: <StatusDot tone={statePresentation.tone}>{statePresentation.label}</StatusDot> },
      { key: 'heartbeat', label: '最近心跳', value: formatOptionalTime(registration.last_reported_at) },
      { key: 'version', label: '版本', value: registration.agent_version ?? '未上报' },
      { key: 'permission', label: '权限状态', value: agentPermissionStatus(registration) },
    ]} />
  </Panel>
}

function CapabilityModule({ title, capabilities, rowTestId }: {
  title: string
  capabilities: Capability[]
  rowTestId: string
}) {
  const columns: DataGridColumn<Capability>[] = [
    {
      key: 'capability',
      header: '能力',
      minWidth: 200,
      grow: 1.2,
      cell: (item) => <TruncatedText>{capabilityLabel(item.capability_id)}</TruncatedText>,
    },
    { key: 'status', header: '状态', minWidth: 200, cell: (item) => capabilityStatus(item) },
    {
      key: 'observed',
      header: '最近检查',
      minWidth: 172,
      grow: 1.2,
      cell: (item) => <TruncatedText>{formatOptionalTime(item.observed_at)}</TruncatedText>,
    },
    {
      key: 'affected',
      header: '影响指标',
      minWidth: 96,
      numeric: true,
      cell: (item) => item.affected_metric_count,
    },
  ]

  return <Panel flush title={title}>
    <DataGrid<Capability>
      label={title}
      columns={columns}
      rows={capabilities}
      rowKey={(item) => item.capability_id}
      rowTestId={rowTestId}
      empty={{ title: '暂无能力项' }}
    />
  </Panel>
}

function MetricStatus({ tasks }: { tasks: CollectionTask[] }) {
  const metricRows: MetricRow[] = tasks.flatMap((task) => task.metric_ids.map((metricID) => ({ metricID, task })))
  const columns: DataGridColumn<MetricRow>[] = [
    {
      key: 'metric',
      header: '指标 ID',
      minWidth: 220,
      grow: 1.2,
      cell: (row) => <TruncatedText className="dbs-numeric">{row.metricID}</TruncatedText>,
    },
    {
      key: 'collected',
      header: '最近采集时间',
      minWidth: 172,
      grow: 1.2,
      cell: (row) => <TruncatedText>{formatOptionalTime(row.task.last_success_at)}</TruncatedText>,
    },
    { key: 'result', header: '当前状态', minWidth: 132, grow: 1.4, cell: (row) => taskResultDot(row.task.last_result) },
    {
      key: 'error',
      header: '失败原因',
      minWidth: 240,
      cell: (row) => <TruncatedText>{row.task.last_error_message ?? '—'}</TruncatedText>,
    },
    {
      key: 'capabilities',
      header: '所需能力',
      minWidth: 200,
      cell: (row) => row.task.required_capabilities.length === 0
        ? '无'
        : <span className="collection-metrics__capabilities">
          {row.task.required_capabilities.map((item) => (
            <span className="dbs-caption dbs-numeric" key={item}>{item}</span>
          ))}
        </span>,
    },
  ]

  return <Panel flush title="指标采集状态" id="metric-status">
    <DataGrid<MetricRow>
      label="指标采集状态"
      columns={columns}
      rows={metricRows}
      rowKey={(row) => row.metricID}
      // 待办里的「影响哪些指标」链接指向这里（`#metric-<指标 ID>`）。锚点落在行上而不是
      // 某个单元格里的元素上，跳过去看见的才是整行。
      rowId={(row) => `metric-${row.metricID}`}
      rowTestId="metric-row"
      empty={{ title: '暂无指标', description: '采集任务还没有声明任何指标。' }}
    />
  </Panel>
}

/// 采集配置。可写的只有两件事：每个任务的采样周期、整实例的暂停开关。
///
/// 只读用户拿到的是**禁用的控件加一句原因**，不是点下去才报错的控件。
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
  const disabledReason = canManage ? undefined : manageDisabledReason

  const columns: DataGridColumn<CollectionTask>[] = [
    {
      key: 'task',
      header: '任务',
      minWidth: 180,
      grow: 1.2,
      cell: (task) => <TruncatedText className="dbs-numeric">{task.task_id}</TruncatedText>,
    },
    {
      key: 'interval',
      header: '配置周期',
      minWidth: 104,
      numeric: true,
      cell: (task) => `${task.interval_seconds} 秒`,
    },
    {
      key: 'backoff',
      header: '退避至',
      minWidth: 172,
      grow: 1.2,
      cell: (task) => <TruncatedText>{formatOptionalTime(task.next_eligible_at)}</TruncatedText>,
    },
    {
      key: 'edit',
      header: '修改周期',
      minWidth: 264,
      cell: (task) => <span className="collection-config__editor">
        <NumberInput
          className="collection-config__number"
          id={`collection-interval-${task.task_id}`}
          // 角色显式写出来，不吃组件库的隐式映射：`role="spinbutton"` 是这一格的契约
          // （web/CLAUDE.md「测试定位」），换库不能把它换掉。
          role="spinbutton"
          // 表头「修改周期」不点名是哪一个任务，所以每个输入框自己带全名；
          // `label` 与 `aria-label` 是同一句话，读屏用户听见的就是它。
          label={`${task.task_id} 采样周期`}
          hideLabel
          aria-label={`${task.task_id} 采样周期`}
          size="sm"
          min={5}
          step={1}
          // 清空输入框是「还没填」，不是 0：`allowEmpty` 让空串保持空串，
          // 下面那个判断因此不会把 0 当成一个用户填的周期存进草稿里。
          allowEmpty
          disabled={!canManage}
          value={draftIntervals[task.task_id] ?? task.interval_seconds}
          // 取值在 onChange 的第二个参数里（加减按钮点的是按钮，不是输入框）。
          onChange={(_event, state) => {
            if (state.value !== '') {
              setDraftIntervals((current) => ({ ...current, [task.task_id]: Number(state.value) }))
            }
          }}
        />
        <span title={disabledReason}>
          <Button
            kind="tertiary"
            size="sm"
            aria-label={`保存 ${task.task_id} 采样周期`}
            disabled={!canManage || intervalPending}
            onClick={() => onIntervalChange(task.task_id, draftIntervals[task.task_id] ?? task.interval_seconds)}
          >保存</Button>
        </span>
      </span>,
    },
  ]

  return <Panel title="采集配置" className="collection-config">
    <div className="collection-config__body">
      {!canManage && <NotificationBar tone="info" title={manageDisabledReason}>
        <p className="dbs-caption">采样周期与暂停开关对当前角色只读，控件保持可见但不可操作。</p>
      </NotificationBar>}

      <div className="collection-config__table">
        <DataGrid<CollectionTask>
          label="采集任务周期"
          columns={columns}
          rows={tasks}
          rowKey={(task) => task.task_id}
          rowTestId="collection-task-row"
          empty={{ title: '暂无采集任务' }}
        />
      </div>

      <div className="collection-config__switches">
        <div className="collection-config__switch">
          <Toggle
            id="collection-agent-metrics"
            labelText="启用 Agent 指标"
            labelA="未启用"
            labelB="已启用"
            toggled={agentMetricsEnabled}
            readOnly
          />
          <p className="dbs-caption">只读：本页不提供修改入口，它随 Agent 接入登记变化。</p>
        </div>

        <div className="collection-config__switch">
          <FormField label="暂停原因" helperText="选填。会随暂停状态一起记录，供之后查为什么停。">
            {(field) => <TextInput
              id={field.id}
              labelText=""
              hideLabel
              size="sm"
              placeholder="例如：计划停机"
              value={reason}
              disabled={!canManage || pausePending}
              aria-describedby={field.describedBy}
              onChange={(event) => setReason(event.target.value)}
            />}
          </FormField>
          <span title={disabledReason}>
            <Toggle
              id="collection-pause"
              labelText="暂停采集"
              labelA="采集进行中"
              labelB="已暂停"
              toggled={pause.paused}
              disabled={!canManage || pausePending}
              onToggle={(checked) => onPauseChange(checked, reason)}
            />
          </span>
          <p className="dbs-caption">
            {pause.paused ? '暂停期间不产生采集，缺口按缺数记录，不会补采。' : '暂停会立即停止这个实例的全部采集任务。'}
            {pause.updated_at ? ` 最近操作 ${formatTime(pause.updated_at)}。` : ''}
            {pause.reason ? ` 原因：${pause.reason}` : ''}
          </p>
        </div>
      </div>
    </div>
  </Panel>
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
    case 'PRESENT': return <StatusDot tone="normal">已具备</StatusDot>
    case 'MISSING': return <StatusDot tone="warning">缺失</StatusDot>
    case 'NOT_APPLICABLE': return <span className="collection-capability__na">
      <StatusDot tone="unknown">不适用</StatusDot>
      {capability.na_reason !== undefined && <span className="dbs-caption" title={capability.na_reason}>{capability.na_reason}</span>}
    </span>
    case 'UNKNOWN': return <StatusDot tone="unknown">状态未知</StatusDot>
    default: return assertNever(capability.status)
  }
}

function taskResult(result: CollectionTask['last_result']): StatusPresentation {
  if (result === undefined) return { label: '尚未采集', tone: 'unknown' }
  switch (result) {
    case 'SUCCESS': return { label: '成功', tone: 'normal' }
    case 'FAILED': return { label: '失败', tone: 'critical' }
    case 'TIMED_OUT': return { label: '超时', tone: 'critical' }
    case 'SKIPPED_BACKPRESSURE': return { label: '平台背压跳过', tone: 'warning' }
    case 'BACKOFF': return { label: '退避中', tone: 'warning' }
    default: return assertNever(result)
  }
}

function taskResultDot(result: CollectionTask['last_result']) {
  const presentation = taskResult(result)
  return <StatusDot tone={presentation.tone}>{presentation.label}</StatusDot>
}

function collectionStatusPresentation(
  paused: boolean,
  consecutiveFailures: number,
  hasSuccessfulTask: boolean,
): StatusPresentation {
  if (paused) return { label: '已暂停', tone: 'unknown' }
  if (consecutiveFailures > 0) return { label: '存在采集失败', tone: 'critical' }
  if (hasSuccessfulTask) return { label: '正常', tone: 'normal' }
  return { label: '尚未采集', tone: 'unknown' }
}

function agentRegistrationPresentation(state: AgentRegistration['state']): StatusPresentation {
  switch (state) {
    case 'NEVER_REGISTERED': return { label: '未安装', tone: 'unknown' }
    case 'EXPECTED_ONLINE': return { label: '应在线', tone: 'normal' }
    case 'REVOKED': return { label: '令牌已吊销', tone: 'critical' }
    case 'DISABLED': return { label: '已停用', tone: 'unknown' }
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

/// 「多久以前」只有一套说法：与新鲜度、告警持续时长共用 `elapsedLabel`。
function formatAge(value: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  return `${elapsedLabel(seconds)}前`
}

function assertNever(value: never): never {
  throw new Error(`unhandled collection value: ${value}`)
}
