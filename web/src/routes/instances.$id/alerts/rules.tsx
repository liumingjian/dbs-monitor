import {
  Button,
  ComboBox,
  ContentSwitcher,
  Modal,
  MultiSelect,
  NumberInput,
  OverflowMenu,
  OverflowMenuItem,
  Pagination,
  Select,
  SelectItem,
  Switch,
  Tab,
  TabList,
  Tabs,
  TextInput,
  Toggle,
} from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath, UseFormReturn } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../../api/errors'
import type { components } from '../../../api/schema'
import { zodResolver } from '../../../forms/zodResolver'
import type { DataGridColumn } from '../../../primitives/DataGrid'
import { DataGrid } from '../../../primitives/DataGrid'
import { Drawer } from '../../../primitives/Drawer'
import { FormField } from '../../../primitives/FormField'
import { Icon } from '../../../primitives/Icon'
import { NotificationBar } from '../../../primitives/NotificationBar'
import { Panel } from '../../../primitives/Panel'
import { StatusBadge } from '../../../primitives/StatusBadge'
import type { StatusTone } from '../../../primitives/StatusBadge'
import { TruncatedText } from '../../../primitives/TruncatedText'
import { rootRoute } from '../../root'
import { browserStorage } from '../../root/navCollapse'
import type { TableDensity } from '../../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../../root/tableDensity'
import { defaultTimeRange } from '../timeRange'
import { metricOptions } from '../metricOptions'
import type { MetricOption } from '../metricOptions'
import { WorkbenchHeader } from '../workbench'
import './rules.css'

type AlertRule = components['schemas']['AlertRule']
type AlertRuleInput = components['schemas']['AlertRuleInput']
type AlertRuleTemplate = components['schemas']['AlertRuleTemplate']
type AlertAggregation = components['schemas']['AlertAggregation']
type AlertOperator = components['schemas']['AlertOperator']
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

export const alertRulesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts/rules',
  component: AlertRulesPage,
})

/// 告警规则页。
///
/// 版式沿用列表页样板（`routes/instances/index.tsx`）：页头 → 面板包表格 → 分页在面板 footer。
/// 编辑器是**抽屉**而不是模态框，理由只有一条：改一条规则时，另外几条规则必须还看得见 ——
/// 阈值与连续次数是相对着定的，遮住列表就只能靠记忆。
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
  const enableMutation = $api.useMutation('put', '/api/v1/alert-rules/{id}/enabled')
  const copyMutation = $api.useMutation('post', '/api/v1/alert-rules/{id}/copies')
  const deleteMutation = $api.useMutation('delete', '/api/v1/alert-rules/{id}')
  const templateMutation = $api.useMutation('post', '/api/v1/alert-rule-templates/{id}/alert-rules')

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [pendingDeletion, setPendingDeletion] = useState<AlertRule | null>(null)
  const [actionError, setActionError] = useState('')
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)

  const canWrite = canWriteAlertRules(currentUserQuery.data?.role)
  const disabledReason = canWrite ? undefined : '需要告警管理员角色'

  const rules = rulesQuery.data ?? []
  const lastPage = Math.max(1, Math.ceil(rules.length / pageSize))
  const currentPage = Math.min(page, lastPage)
  const pageRules = rules.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  function refreshRules() {
    void rulesQuery.refetch()
  }

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  function openCreate() {
    setActionError('')
    setEditingRule(null)
    setEditorOpen(true)
  }

  function openEdit(rule: AlertRule) {
    setActionError('')
    setEditingRule(rule)
    setEditorOpen(true)
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

  function deleteRule(rule: AlertRule) {
    setActionError('')
    deleteMutation.mutate({ params: { path: { id: rule.id } } }, {
      onSuccess: () => {
        setPendingDeletion(null)
        refreshRules()
      },
      onError: (failure) => {
        setPendingDeletion(null)
        setActionError(apiErrorMessage(failure, '删除告警规则失败'))
      },
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

  const actionPending = enableMutation.isPending || copyMutation.isPending || deleteMutation.isPending

  return <div className="alert-rules-page">
    <WorkbenchHeader id={id} instanceName={instanceQuery.data?.name} activeKey="alerts" search={defaultTimeRange()} />
    <AlertSectionTabs id={id} />

    <header className="alert-rules-page__header">
      <h2 className="dbs-panel-title">告警规则</h2>
      <div className="alert-rules-page__actions">
        <Button kind="tertiary" size="md" renderIcon={TemplateIcon} onClick={() => setTemplateOpen(true)}>规则模板</Button>
        <span title={disabledReason}>
          <Button size="md" renderIcon={AddIcon} disabled={!canWrite} onClick={openCreate}>新建规则</Button>
        </span>
      </div>
    </header>

    {!canWrite && <NotificationBar tone="info" title="只读模式：修改告警规则需要告警管理员角色。" />}
    {rulesQuery.isError && <NotificationBar tone="critical" title={apiErrorMessage(rulesQuery.error, '告警规则加载失败')} />}
    {actionError !== '' && <NotificationBar tone="critical" title={actionError} onClose={() => setActionError('')} />}

    <Panel
      flush
      title={`规则（${rules.length}）`}
      actions={<DensitySwitcher density={density} onChange={changeDensity} />}
      footer={<Pagination
        className="alert-rules-pagination"
        size="md"
        page={currentPage}
        pageSize={pageSize}
        pageSizes={[25, 50, 100]}
        totalItems={rules.length}
        backwardText="上一页"
        forwardText="下一页"
        itemsPerPageText="每页条数"
        itemRangeText={(min, max, total) => `第 ${min}–${max} 条，共 ${total} 条`}
        pageRangeText={(_current, total) => `共 ${total} 页`}
        pageNumberText="页码"
        onChange={({ page: nextPage, pageSize: nextPageSize }) => {
          setPage(nextPage)
          setPageSize(nextPageSize)
        }}
      />}
    >
      <DataGrid<AlertRule>
        label="告警规则列表"
        density={density}
        loading={rulesQuery.isPending}
        skeletonRows={8}
        rows={pageRules}
        rowKey={(rule) => rule.id}
        rowTestId="alert-rule-row"
        rowTone={severityRowTone}
        columns={alertRuleColumns({
          canWrite,
          disabledReason,
          currentInstance: instanceQuery.data,
          tasks: tasksQuery.data ?? [],
          capabilities: capabilitiesQuery.data ?? [],
          onEdit: openEdit,
          onCopy: copyRule,
          onDelete: setPendingDeletion,
          onEnabledChange: setEnabled,
          actionPending,
        })}
        empty={{
          title: '还没有告警规则',
          description: '从内置模板一键创建，或者新建一条自定义规则。',
        }}
      />
    </Panel>

    <RuleDrawer
      open={editorOpen}
      editingRule={editingRule}
      instances={instancesQuery.data ?? []}
      policies={policiesQuery.data ?? []}
      onClose={() => setEditorOpen(false)}
      onSaved={() => {
        setEditorOpen(false)
        refreshRules()
      }}
    />

    <TemplateModal
      open={templateOpen}
      templates={templatesQuery.data ?? []}
      loading={templatesQuery.isPending}
      canWrite={canWrite}
      disabledReason={disabledReason}
      actionPending={templateMutation.isPending}
      onClose={() => setTemplateOpen(false)}
      onCreate={createFromTemplate}
    />

    {/* 删除是破坏性动作：菜单项本身是 Carbon 的删除样式，真正执行前还要在这个
        danger 模态框里再确认一次。内置规则删不掉（服务端 409），所以它的菜单项直接禁用。 */}
    <Modal
      danger
      open={pendingDeletion !== null}
      modalHeading="删除告警规则"
      primaryButtonText="删除"
      secondaryButtonText="取消"
      primaryButtonDisabled={deleteMutation.isPending}
      onRequestSubmit={() => { if (pendingDeletion) deleteRule(pendingDeletion) }}
      onRequestClose={() => setPendingDeletion(null)}
      onSecondarySubmit={() => setPendingDeletion(null)}
      size="sm"
    >
      {pendingDeletion !== null && <p className="dbs-body">
        {`删除后「${pendingDeletion.name}」不再评估，它当前的 ${pendingDeletion.current_alert_count} 条告警也会随之停止更新。此操作不可撤销。`}
      </p>}
    </Modal>
  </div>
}

function AddIcon() {
  return <Icon name="add" />
}

function TemplateIcon() {
  return <Icon name="template" />
}

/// 告警区的二级页签条。页签就是地址，所以按 `web/CLAUDE.md` 的先例写成 `<Tab as={链接组件}>`：
/// 真锚点（中键新开、复制链接）与 `role="tab"` / `aria-selected` 同时保住。
/// 每个去处包成一个已经知道自己去哪儿的组件，并用 `useMemo` 固定身份，否则锚点每次渲染都重挂。
function AlertSectionTabs({ id }: { id: string }) {
  const links = useMemo(() => ({
    rules: (props: object) => <Link {...props} to="/instances/$id/alerts/rules" params={{ id }} />,
    current: (props: object) => <Link
      {...props}
      to="/instances/$id/alerts"
      params={{ id }}
      search={{ tab: 'current' as const, include_paused: false }}
    />,
    history: (props: object) => <Link
      {...props}
      to="/instances/$id/alerts"
      params={{ id }}
      search={{ tab: 'history' as const, include_paused: false }}
    />,
  }), [id])

  return <Tabs selectedIndex={0}>
    <TabList aria-label="告警" activation="manual">
      <Tab as={links.rules}>告警规则</Tab>
      <Tab as={links.current}>当前告警</Tab>
      <Tab as={links.history}>告警历史</Tab>
    </TabList>
  </Tabs>
}

function DensitySwitcher({ density, onChange }: { density: TableDensity; onChange: (density: TableDensity) => void }) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return (
    <ContentSwitcher
      className="alert-rules-density"
      size="sm"
      selectedIndex={densities.indexOf(density)}
      onChange={({ index }) => {
        const next = index === undefined ? undefined : densities[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {densities.map((value) => <Switch key={value} name={value} text={densityLabel(value)} />)}
    </ContentSwitcher>
  )
}

/* ------------------------------------------------------------------ *
 * 表单
 * ------------------------------------------------------------------ */

const alertAggregations = ['latest', 'avg', 'max', 'min', 'sum', 'count'] as const satisfies readonly AlertAggregation[]
const alertOperators = ['>', '>=', '<', '<=', '=', '!='] as const satisfies readonly AlertOperator[]
const alertSeverities = ['critical', 'warning', 'info'] as const satisfies readonly AlertSeverity[]
const noDataPolicies = ['ignore', 'mark_no_data'] as const satisfies readonly NoDataPolicy[]
const alertRuleScopes = ['ALL', 'INSTANCES'] as const satisfies readonly AlertRuleScope[]

/// 表单值的目标形状。`AlertRuleInput` 把 `recovery_consecutive_count` 标成可选（老规则可以没有），
/// 但表单里它是必填 —— 让它默认成什么是产品决定，不是表单该猜的。所以这里显式收窄一次，
/// 其余字段仍然直接来自生成的请求体类型，漂了就编译不过。
type AlertRuleFormShape = AlertRuleInput & { recovery_consecutive_count: number }

/// 规则表单的校验规则。与生成的请求体类型对齐靠两处：`satisfies z.ZodType<AlertRuleFormShape>`
/// 要求 schema 的出参就是请求体，`alertRuleBody` 再把出参真的当请求体用。
/// schema 里不写 `transform` / `default` —— 表单值就是提交值，trim 放在提交处。
const alertRuleSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入规则名称'),
  metric_id: z.string().refine((value) => value !== '', '请选择指标'),
  aggregation: z.enum(alertAggregations),
  operator: z.enum(alertOperators),
  threshold: z.number({ error: '请输入触发阈值' }),
  recovery_operator: z.enum(alertOperators),
  recovery_threshold: z.number({ error: '请输入独立恢复阈值' }),
  window_seconds: z.number({ error: '请输入窗口' }).int('窗口必须是整数秒').min(1, '窗口至少 1 秒'),
  consecutive_count: z.number({ error: '请输入连续次数' }).int('连续次数必须是整数').min(1, '连续次数至少 1 次'),
  recovery_consecutive_count: z.number({ error: '请输入恢复连续次数' }).int('恢复连续次数必须是整数').min(1, '恢复连续次数至少 1 次'),
  severity: z.enum(alertSeverities),
  no_data_policy: z.enum(noDataPolicies),
  scope: z.enum(alertRuleScopes),
  instance_ids: z.array(z.string()),
  evaluation_interval_seconds: z.number({ error: '请输入评估周期' }).int('评估周期必须是整数秒').min(5, '评估周期至少 5 秒'),
  enabled: z.boolean(),
  notification_policy_id: z.string().optional(),
}) satisfies z.ZodType<AlertRuleFormShape>

type AlertRuleValues = z.infer<typeof alertRuleSchema>

/// 服务端字段错误只接受这些名字 —— 每一个都有渲染出来的控件可以聚焦。
/// 清单之外的字段名落回整表单的错误条；`setError` 一个表单里没有的名字会挂出一条
/// 永远显示不出来、也永远清不掉的错误。
const alertRuleFields = [
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
] as const satisfies readonly FieldPath<AlertRuleValues>[]

function alertRuleBody(values: AlertRuleValues): AlertRuleInput {
  return {
    ...values,
    name: values.name.trim(),
    notification_policy_id: values.notification_policy_id === '' ? undefined : values.notification_policy_id,
  }
}

const defaultRuleValues: AlertRuleValues = {
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
  notification_policy_id: undefined,
}

function ruleValues(rule: AlertRule): AlertRuleValues {
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

/// 规则编辑器。抽屉而不是模态框 —— 焦点陷阱、Esc 关闭、`role="dialog"` 由
/// `primitives/Drawer` 给，这里只管表单。
///
/// 抽屉关闭时整棵子树从 DOM 里移除，所以表单状态跟着消失；`key` 换一条规则就换一份表单，
/// 不需要在 effect 里手工 `reset`（那条路会在「打开 → 服务端返回新数据 → 覆盖用户输入」上翻车）。
function RuleDrawer({ open, editingRule, instances, policies, onClose, onSaved }: {
  open: boolean
  editingRule: AlertRule | null
  instances: Instance[]
  policies: NotificationPolicy[]
  onClose: () => void
  onSaved: () => void
}) {
  if (!open) return null
  return <RuleDrawerForm
    key={editingRule?.id ?? 'new'}
    editingRule={editingRule}
    instances={instances}
    policies={policies}
    onClose={onClose}
    onSaved={onSaved}
  />
}

function RuleDrawerForm({ editingRule, instances, policies, onClose, onSaved }: {
  editingRule: AlertRule | null
  instances: Instance[]
  policies: NotificationPolicy[]
  onClose: () => void
  onSaved: () => void
}) {
  const createMutation = $api.useMutation('post', '/api/v1/alert-rules')
  const updateMutation = $api.useMutation('put', '/api/v1/alert-rules/{id}')
  const form = useForm<AlertRuleValues>({
    resolver: zodResolver(alertRuleSchema),
    defaultValues: editingRule === null ? defaultRuleValues : ruleValues(editingRule),
  })
  const { control, formState, handleSubmit, register, setError, setValue, watch } = form
  const [failure, setFailure] = useState('')
  const isBuiltin = editingRule?.is_builtin === true
  const pending = createMutation.isPending || updateMutation.isPending

  const scope = watch('scope')
  const consecutiveCount = watch('consecutive_count')
  const evaluationInterval = watch('evaluation_interval_seconds')

  const submit = handleSubmit((values) => {
    setFailure('')
    const body = alertRuleBody(values)
    const onError = (error: unknown) => {
      // 字段级错误落到对应控件并聚焦第一个；一条都落不下时才退回整表单的错误条。
      if (applyApiFieldErrors<AlertRuleValues>(error, alertRuleFields, setError).length === 0) {
        setFailure(apiErrorMessage(error, '保存告警规则失败'))
      }
    }
    if (editingRule !== null) {
      updateMutation.mutate({ params: { path: { id: editingRule.id } }, body }, { onSuccess: onSaved, onError })
      return
    }
    createMutation.mutate({ body }, { onSuccess: onSaved, onError })
  })

  return <Drawer
    open
    size="lg"
    data-testid="alert-rule-editor"
    title={editingRule === null ? '新建告警规则' : '编辑告警规则'}
    description={isBuiltin ? '内置规则：指标、作用范围与启停由平台锁定，只能调整阈值与通知。' : undefined}
    onClose={onClose}
    footer={<div className="alert-rules-drawer__footer">
      <Button kind="secondary" size="md" onClick={onClose}>取消</Button>
      {/* 底部操作条在 <form> 之外，点它到不了 onSubmit，所以提交口是这里的 onClick；
          <form> 仍然留着，让回车提交与原生表单语义都落在同一个 handleSubmit 上。
          这个按钮**不能**是 type="submit"，那会提交两次。 */}
      <Button size="md" disabled={pending} onClick={() => void submit()}>保存</Button>
    </div>}
  >
    <form className="alert-rules-form" onSubmit={submit} noValidate>
      {failure !== '' && <NotificationBar tone="critical" title={failure} />}

      <FormField className="alert-rules-form__wide" label="规则名称" required errorText={formState.errors.name?.message}>
        {(field) => <TextInput
          id={field.id}
          labelText=""
          hideLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('name')}
        />}
      </FormField>

      <FormField
        className="alert-rules-form__wide"
        label="指标"
        required
        helperText={isBuiltin ? '内置规则的指标不可更改' : undefined}
        errorText={formState.errors.metric_id?.message}
      >
        {(field) => <Controller
          name="metric_id"
          control={control}
          render={({ field: metric }) => <ComboBox<MetricOption>
            id={field.id}
            titleText=""
            placeholder="搜索指标"
            disabled={isBuiltin}
            items={alertableMetricOptions}
            itemToString={(item) => (item === null ? '' : `${item.label} · ${item.id}`)}
            selectedItem={alertableMetricOptions.find((option) => option.id === metric.value) ?? null}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            onChange={({ selectedItem }) => metric.onChange(selectedItem?.id ?? '')}
          />}
        />}
      </FormField>

      <FormField label="窗口聚合" required errorText={formState.errors.aggregation?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('aggregation')}
        >
          {alertAggregations.map((value) => <SelectItem key={value} value={value} text={aggregationLabel(value)} />)}
        </Select>}
      </FormField>

      <FormField label="级别" required errorText={formState.errors.severity?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('severity')}
        >
          {alertSeverities
            .filter((value) => !isBuiltin || value !== 'info')
            .map((value) => <SelectItem key={value} value={value} text={severityLabel(value)} />)}
        </Select>}
      </FormField>

      <FormField label="触发条件" required errorText={formState.errors.operator?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('operator')}
        >
          {alertOperators.map((value) => <SelectItem key={value} value={value} text={value} />)}
        </Select>}
      </FormField>

      <NumberFormField
        control={control}
        name="threshold"
        label="触发阈值"
        errorText={formState.errors.threshold?.message}
      />

      <FormField label="恢复条件" required errorText={formState.errors.recovery_operator?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('recovery_operator')}
        >
          {alertOperators.map((value) => <SelectItem key={value} value={value} text={value} />)}
        </Select>}
      </FormField>

      <NumberFormField
        control={control}
        name="recovery_threshold"
        label="恢复阈值"
        errorText={formState.errors.recovery_threshold?.message}
      />

      <NumberFormField
        control={control}
        name="evaluation_interval_seconds"
        label="评估周期（秒）"
        min={5}
        errorText={formState.errors.evaluation_interval_seconds?.message}
      />

      <NumberFormField
        control={control}
        name="window_seconds"
        label="窗口（秒）"
        min={1}
        errorText={formState.errors.window_seconds?.message}
      />

      <NumberFormField
        control={control}
        name="consecutive_count"
        label="连续次数"
        min={1}
        helperText={positiveInteger(consecutiveCount) && positiveInteger(evaluationInterval)
          ? consecutiveDurationLabel(consecutiveCount, evaluationInterval)
          : undefined}
        errorText={formState.errors.consecutive_count?.message}
      />

      <NumberFormField
        control={control}
        name="recovery_consecutive_count"
        label="恢复连续次数"
        min={1}
        errorText={formState.errors.recovery_consecutive_count?.message}
      />

      <FormField label="无数据策略" required errorText={formState.errors.no_data_policy?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('no_data_policy')}
        >
          {noDataPolicies.map((value) => <SelectItem key={value} value={value} text={noDataLabel(value)} />)}
        </Select>}
      </FormField>

      <FormField label="作用范围" required errorText={formState.errors.scope?.message}>
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          disabled={isBuiltin}
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('scope', {
            // 切回「全部实例」时把已选实例清空：留着一份看不见的选择，保存时会把它一起发出去。
            onChange: (event: { target: { value: string } }) => {
              if (event.target.value === 'ALL') setValue('instance_ids', [])
            },
          })}
        >
          {alertRuleScopes.map((value) => <SelectItem key={value} value={value} text={scopeLabel(value, 0)} />)}
        </Select>}
      </FormField>

      <FormField
        className="alert-rules-form__wide"
        label="实例"
        required={scope === 'INSTANCES'}
        errorText={formState.errors.instance_ids?.message}
      >
        {(field) => <Controller
          name="instance_ids"
          control={control}
          render={({ field: selected }) => <MultiSelect<Instance>
            id={field.id}
            titleText=""
            label={scope === 'INSTANCES' ? '选择实例' : '全部实例'}
            disabled={scope !== 'INSTANCES' || isBuiltin}
            items={instances}
            itemToString={(item) => item?.name ?? ''}
            selectedItems={instances.filter((instance) => selected.value.includes(instance.id))}
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            onChange={({ selectedItems }) => selected.onChange((selectedItems ?? []).map((item) => item.id))}
          />}
        />}
      </FormField>

      <FormField
        className="alert-rules-form__wide"
        label="通知策略"
        helperText={editingRule?.notification_policy_id
          ? `当前生效：${editingRule.effective_notification_policy_name}`
          : '当前生效：默认策略（继承）'}
        errorText={formState.errors.notification_policy_id?.message}
      >
        {(field) => <Select
          id={field.id}
          labelText=""
          noLabel
          invalid={field.invalid}
          aria-describedby={field.describedBy}
          {...register('notification_policy_id', {
            setValueAs: (value: string) => (value === '' ? undefined : value),
          })}
        >
          <SelectItem value="" text="默认策略（继承）" />
          {policies.filter((policy) => !policy.is_default).map((policy) => (
            <SelectItem key={policy.id} value={policy.id} text={policy.name} />
          ))}
        </Select>}
      </FormField>

      <FormField label="启用" errorText={formState.errors.enabled?.message}>
        {(field) => <Controller
          name="enabled"
          control={control}
          render={({ field: enabled }) => <Toggle
            id={field.id}
            size="sm"
            labelText=""
            hideLabel
            aria-label="启用规则"
            labelA="停用"
            labelB="启用"
            disabled={isBuiltin}
            toggled={enabled.value}
            onToggle={(next) => enabled.onChange(next)}
          />}
        />}
      </FormField>
    </form>
  </Drawer>
}

/// 数字字段。Carbon 的 `NumberInput` 把取值放在 `onChange` 的**第二个**参数里
/// （加减按钮点的是按钮，不是输入框），用 `register` 会把按钮元素当成值提交出去，
/// 所以数字字段一律走 `Controller`。空串是「清空了」，不是 0。
function NumberFormField({ control, name, label, min, helperText, errorText }: {
  control: UseFormReturn<AlertRuleValues>['control']
  name: 'threshold' | 'recovery_threshold' | 'window_seconds' | 'consecutive_count'
    | 'recovery_consecutive_count' | 'evaluation_interval_seconds'
  label: string
  min?: number
  helperText?: string
  errorText?: string
}) {
  return <FormField label={label} required helperText={helperText} errorText={errorText}>
    {(field) => <Controller
      name={name}
      control={control}
      render={({ field: value }) => <NumberInput
        id={field.id}
        label=""
        hideLabel
        min={min}
        invalid={field.invalid}
        aria-describedby={field.describedBy}
        ref={value.ref}
        name={value.name}
        value={value.value}
        onBlur={value.onBlur}
        onChange={(_event, state) => value.onChange(state.value === '' ? undefined : Number(state.value))}
      />}
    />}
  </FormField>
}

/* ------------------------------------------------------------------ *
 * 模板
 * ------------------------------------------------------------------ */

function TemplateModal({ open, templates, loading, canWrite, disabledReason, actionPending, onClose, onCreate }: {
  open: boolean
  templates: AlertRuleTemplate[]
  loading: boolean
  canWrite: boolean
  disabledReason: string | undefined
  actionPending: boolean
  onClose: () => void
  onCreate: (template: AlertRuleTemplate) => void
}) {
  const columns: DataGridColumn<AlertRuleTemplate>[] = [
    { key: 'name', header: '模板', minWidth: 150, grow: 1.4, cell: (template) => <TruncatedText>{template.name}</TruncatedText> },
    { key: 'version', header: '版本', minWidth: 56, numeric: true, cell: (template) => `v${template.version}` },
    { key: 'metric', header: '指标', minWidth: 130, cell: (template) => <TruncatedText title={template.metric_id}>{metricName(template.metric_id)}</TruncatedText> },
    {
      key: 'condition',
      header: '条件',
      minWidth: 120,
      cell: (template) => <TruncatedText>{`${aggregationLabel(template.aggregation)} ${template.operator} ${template.threshold}`}</TruncatedText>,
    },
    {
      key: 'duration',
      header: '持续',
      minWidth: 170,
      cell: (template) => <TruncatedText>{consecutiveDurationLabel(template.consecutive_count, template.evaluation_interval_seconds)}</TruncatedText>,
    },
    { key: 'severity', header: '级别', minWidth: 72, cell: (template) => <SeverityBadge severity={template.severity} /> },
    {
      key: 'actions',
      header: '操作',
      minWidth: 104,
      align: 'end',
      cell: (template) => <span title={disabledReason}>
        <Button
          kind="ghost"
          size="sm"
          disabled={!canWrite || actionPending}
          onClick={() => onCreate(template)}
        >一键创建</Button>
      </span>,
    },
  ]

  return <Modal
    passiveModal
    open={open}
    modalHeading="内置规则模板"
    onRequestClose={onClose}
    size="lg"
  >
    <div className="alert-rules-templates">
      {!canWrite && <NotificationBar tone="info" title="只读模式：从模板创建规则需要告警管理员角色。" />}
      <DataGrid<AlertRuleTemplate>
        label="内置规则模板"
        density="dense"
        stickyTop="0"
        loading={loading}
        rows={templates}
        rowKey={(template) => template.id}
        rowTestId="alert-rule-template-row"
        columns={columns}
        empty={{ title: '暂无内置模板' }}
      />
    </div>
  </Modal>
}

/* ------------------------------------------------------------------ *
 * 表格
 * ------------------------------------------------------------------ */

/// 列定义。**只给 `minWidth`，页面不设任何 `overflow-x`** —— 1280px 不横向滚动、不丢列
/// 由 `primitives/DataGrid` 结构性地保证，页面只负责说明每列至少值多少像素，
/// 以及 —— 这一页真正的功课 —— **每一格到底写什么**。
///
/// 迁移前这张表声明了 2100px 的列宽，1280px 下大约只有 976px 可用。光调最小宽度关不上
/// 2.1 倍的差，所以内容本身做了三件事：
///
///  1. **多行格拆成列。**「名称 + 内置规则」拆成「名称」与「类型」，「条件」的触发行与恢复行
///     拆成两列 —— 40px 的行放不下两行，挤在一格里的第二行本来就是看不清的。
///  2. **两列合成一句话。**「评估周期」与「连续次数」合成「触发节奏」，格里写
///     `连续 3 次 × 30 秒 ≈ 1 分 30 秒`：评估周期本来就在这句话里出现，拆成两列是把同一个数
///     写了两遍，还丢掉了「大约多久才会响」这个真正有用的推导值。一行，一件事，两个事实都在。
///  3. **长值换短读法，全文进 `title`。**「最近触发时间」从 `2026-08-30 14:03:11`（19 字符）
///     换成「3 分钟前」，绝对时刻留在悬停提示里。扫视这一列问的是「最近响过没有」，
///     不是「精确到秒是几点」。
///
/// `grow` 是优先级旋钮：宽度固定的格子（徽章、开关、图标、等宽计数）给 >1，把它们压回接近
/// 自己自然宽度；长文本列留 1，压不下的那一截由省略号截断，全文在悬停提示里。
function alertRuleColumns({ canWrite, disabledReason, currentInstance, tasks, capabilities, onEdit, onCopy, onDelete, onEnabledChange, actionPending }: {
  canWrite: boolean
  disabledReason: string | undefined
  currentInstance: Instance | undefined
  tasks: CollectionTask[]
  capabilities: Capability[]
  onEdit: (rule: AlertRule) => void
  onCopy: (rule: AlertRule) => void
  onDelete: (rule: AlertRule) => void
  onEnabledChange: (rule: AlertRule, enabled: boolean) => void
  actionPending: boolean
}): DataGridColumn<AlertRule>[] {
  return [
    {
      key: 'name',
      header: '名称',
      // 规则名是这一行的身份，截断它等于让读者认不出这是谁：富余宽度优先给它。
      minWidth: 170,
      grow: 1.1,
      cell: (rule) => <TruncatedText className="alert-rules-table__name">{rule.name}</TruncatedText>,
    },
    {
      key: 'kind',
      header: '类型',
      minWidth: 74,
      grow: 0.95,
      cell: (rule) => (rule.is_builtin ? '内置' : '自定义'),
    },
    {
      key: 'scope',
      header: '范围',
      minWidth: 90,
      grow: 0.85,
      cell: (rule) => <TruncatedText>{scopeLabel(rule.scope, rule.instance_ids.length)}</TruncatedText>,
    },
    {
      key: 'metric',
      header: '指标',
      minWidth: 122,
      grow: 0.85,
      cell: (rule) => <TruncatedText title={`${metricName(rule.metric_id)}（${rule.metric_id}）`}>{metricName(rule.metric_id)}</TruncatedText>,
    },
    {
      key: 'trigger',
      header: '触发条件',
      minWidth: 124,
      grow: 0.8,
      cell: (rule) => <TruncatedText>{`${aggregationLabel(rule.aggregation)} ${rule.operator} ${rule.threshold}`}</TruncatedText>,
    },
    {
      key: 'recovery',
      header: '恢复条件',
      minWidth: 72,
      grow: 1,
      cell: (rule) => <TruncatedText>{`${rule.recovery_operator} ${rule.recovery_threshold}`}</TruncatedText>,
    },
    {
      key: 'window',
      header: '窗口',
      minWidth: 72,
      grow: 1,
      cell: (rule) => <TruncatedText>{formatRuleDuration(rule.window_seconds)}</TruncatedText>,
    },
    {
      key: 'cadence',
      header: '触发节奏',
      minWidth: 96,
      grow: 1,
      cell: (rule) => <TruncatedText title={consecutiveDurationLabel(rule.consecutive_count, rule.evaluation_interval_seconds)}>
        {compactCadenceLabel(rule.consecutive_count, rule.evaluation_interval_seconds)}
      </TruncatedText>,
    },
    {
      key: 'severity',
      header: '级别',
      minWidth: 74,
      grow: 1.4,
      cell: (rule) => <SeverityBadge severity={rule.severity} />,
    },
    {
      key: 'enabled',
      header: '启停',
      minWidth: 98,
      grow: 1.05,
      cell: (rule) => {
        // 内置规则停不掉（服务端会 409），所以这里根本不给开关 —— 点了才报错是最差的读法。
        if (rule.is_builtin) return <StatusBadge tone="normal">不可停用</StatusBadge>
        return <span title={disabledReason}>
          <Toggle
            id={`alert-rule-enabled-${rule.id}`}
            size="sm"
            labelText=""
            hideLabel
            aria-label={`启用 ${rule.name}`}
            labelA=""
            labelB=""
            disabled={!canWrite || actionPending}
            toggled={rule.enabled}
            onToggle={(checked) => onEnabledChange(rule, checked)}
          />
        </span>
      },
    },
    {
      key: 'policy',
      header: '通知策略',
      minWidth: 146,
      grow: 0.7,
      cell: (rule) => <TruncatedText>{rule.effective_notification_policy_name}</TruncatedText>,
    },
    {
      key: 'lastTriggered',
      header: '最近触发',
      minWidth: 90,
      grow: 0.9,
      cell: (rule) => <TruncatedText title={absoluteTimeLabel(rule.last_triggered_at)}>
        {lastTriggeredLabel(rule.last_triggered_at, Date.now())}
      </TruncatedText>,
    },
    {
      key: 'alertCount',
      header: '告警数',
      minWidth: 54,
      grow: 1.25,
      numeric: true,
      cell: (rule) => String(rule.current_alert_count),
    },
    {
      key: 'capability',
      header: '能力',
      minWidth: 74,
      grow: 1.4,
      cell: (rule) => <CapabilityFitBadge fit={capabilityFit(rule.metric_id, tasks, capabilities, currentInstance)} />,
    },
    {
      key: 'actions',
      header: '操作',
      minWidth: 96,
      grow: 1.35,
      align: 'end',
      // 编辑留在行里（抽屉是这一页的主路径），复制与删除收进溢出菜单：三个图标要 112px，
      // 而这一页每一列都在抢宽度。删除在菜单里是 Carbon 的删除样式，执行前还有二次确认。
      // 图标按钮的可访问名是「编辑」/「更多操作」，不带规则名：组件库的提示文案会作为
      // `aria-labelledby` 的目标留在 DOM 里，每行再塞一遍规则名就等于把同一个名字写三遍
      // （名称格 + 两个提示），定位与读屏都被这份重复噪声拖累。行的身份由行本身给出，
      // 定位某一行的按钮用 `getByRole('row', { name })` 再往里找。
      cell: (rule) => <span className="alert-rules-table__actions">
        <span title={disabledReason}>
          <Button
            kind="ghost"
            size="sm"
            hasIconOnly
            iconDescription="编辑"
            tooltipPosition="left"
            renderIcon={EditIcon}
            disabled={!canWrite}
            onClick={() => onEdit(rule)}
          />
        </span>
        <OverflowMenu
          size="sm"
          flipped
          aria-label="更多操作"
          iconDescription="更多操作"
          disabled={!canWrite || actionPending}
        >
          <OverflowMenuItem itemText="复制" onClick={() => onCopy(rule)} />
          <OverflowMenuItem
            isDelete
            hasDivider
            disabled={rule.is_builtin}
            itemText="删除"
            onClick={() => onDelete(rule)}
          />
        </OverflowMenu>
      </span>,
    },
  ]
}

function EditIcon() {
  return <Icon name="edit" />
}

/// 行首 3px 色条只画严重与警告两档，且只画给「此刻真的有未恢复告警」的规则 ——
/// 规则本身的级别不是状态，一条从没响过的 critical 规则不该一直红着。
function severityRowTone(rule: AlertRule): StatusTone | undefined {
  if (rule.current_alert_count === 0) return undefined
  switch (rule.severity) {
    case 'critical':
      return 'critical'
    case 'warning':
      return 'warning'
    case 'info':
      return undefined
    default:
      return assertNever(rule.severity)
  }
}

/* ------------------------------------------------------------------ *
 * 纯函数与文案映射
 * ------------------------------------------------------------------ */

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

/// 表格里的短读法：`3 × 30 秒`。整句（含「≈ 1 分 30 秒」这个推导值）留在悬停提示里。
/// 这一列在 1280px 下只分得到六十来个像素，整句写进去只会剩「连续 3…」——
/// 那既没说清次数也没说清周期。
export function compactCadenceLabel(count: number, intervalSeconds: number): string {
  return `${count} × ${formatRuleDuration(intervalSeconds)}`
}

/// 「最近触发」的短读法。绝对时刻有 19 个字符，在这张表里它只会剩下一个日期加省略号；
/// 相对时刻四五个字就说完了同一件事，精确到秒的那份留在悬停提示里。
/// 未来时刻（时钟不同步）退回「刚刚」而不是负数。
export function lastTriggeredLabel(triggeredAt: string | undefined, now: number): string {
  if (triggeredAt === undefined) return '尚未触发'
  const elapsed = Math.floor((now - new Date(triggeredAt).getTime()) / 1000)
  if (Number.isNaN(elapsed)) return '尚未触发'
  if (elapsed < 60) return '刚刚'
  if (elapsed < 3600) return `${Math.floor(elapsed / 60)} 分钟前`
  if (elapsed < 86400) return `${Math.floor(elapsed / 3600)} 小时前`
  return `${Math.floor(elapsed / 86400)} 天前`
}

function absoluteTimeLabel(triggeredAt: string | undefined): string {
  return triggeredAt === undefined ? '尚未触发' : new Date(triggeredAt).toLocaleString()
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

function capabilityFitLabel(fit: CapabilityFit): string {
  switch (fit) {
    case 'SATISFIED': return '满足'
    case 'UNSATISFIED': return '不满足'
    case 'UNKNOWN': return '未知'
    default: return assertNever(fit)
  }
}

function capabilityFitTone(fit: CapabilityFit): StatusTone {
  switch (fit) {
    case 'SATISFIED': return 'normal'
    case 'UNSATISFIED': return 'critical'
    case 'UNKNOWN': return 'unknown'
    default: return assertNever(fit)
  }
}

function CapabilityFitBadge({ fit }: { fit: CapabilityFit }) {
  return <StatusBadge tone={capabilityFitTone(fit)}>{capabilityFitLabel(fit)}</StatusBadge>
}

function metricName(metricID: string): string {
  return metricOptions.find((option) => option.id === metricID)?.label ?? metricID
}

function positiveInteger(value: number | undefined): value is number {
  return value !== undefined && Number.isInteger(value) && value > 0
}

export function canWriteAlertRules(role: Role | undefined): boolean {
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

function severityTone(value: AlertSeverity): StatusTone {
  switch (value) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return 'unknown'
    default: return assertNever(value)
  }
}

function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  return <StatusBadge tone={severityTone(severity)}>{severityLabel(severity)}</StatusBadge>
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
