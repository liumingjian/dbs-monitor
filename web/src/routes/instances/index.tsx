import { Button, Checkbox, ContentSwitcher, Dropdown, Modal, MultiSelect, NumberInput, Pagination, Switch, TextInput } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { collectionPausePresentation } from '../../domain/CollectionPausedTag'
import { Freshness } from '../../domain/Freshness'
import { HEALTH_STATUSES } from '../../domain/HealthStatus'
import { zodResolver } from '../../forms/zodResolver'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import { Sparkline } from '../../primitives/Sparkline'
import type { StatusTone } from '../../primitives/StatusBadge'
import { StatusBadge } from '../../primitives/StatusBadge'
import { StatusDot } from '../../primitives/StatusDot'
import { TruncatedText } from '../../primitives/TruncatedText'
import {
  attributionLabel,
  dataFreshnessLabel,
  lastCollectedAtLabel,
} from '../instanceProjection'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../root/tableDensity'
import './instances.css'

type InstanceCreateInput = components['schemas']['InstanceCreateInput']
type Instance = components['schemas']['Instance']
type HealthStatusValue = components['schemas']['HealthStatus']
type AlertSeverity = components['schemas']['AlertSeverity']
export type OrthogonalFlag = 'NO_DATA' | 'MAINTENANCE' | 'RECENTLY_RECOVERED' | 'IGNORED' | 'CONFIGURATION_MISSING'

export type InstanceFilters = {
  statuses?: readonly HealthStatusValue[]
  flags?: readonly OrthogonalFlag[]
  alertSeverity?: AlertSeverity
  hasInfo?: boolean
  hasConfigurationMissing?: boolean
}

function assertNever(value: never): never {
  throw new Error(`unexpected instance projection value: ${value}`)
}

function healthRank(status: HealthStatusValue): number {
  switch (status) {
    case 'CRITICAL':
      return 5
    case 'WARNING':
      return 4
    case 'UNKNOWN':
      return 3
    case 'HEALTHY':
      return 2
    case 'PAUSED':
      return 1
    default:
      return assertNever(status)
  }
}

function hasFlag(instance: Instance, flag: OrthogonalFlag): boolean {
  switch (flag) {
    case 'NO_DATA':
      return instance.health.flags.no_data
    case 'MAINTENANCE':
      return instance.health.flags.in_maintenance
    case 'RECENTLY_RECOVERED':
      return instance.health.flags.recently_recovered
    case 'IGNORED':
      return instance.health.flags.ignored > 0
    case 'CONFIGURATION_MISSING':
      return instance.health.flags.configuration_missing > 0
    default:
      return assertNever(flag)
  }
}

function hasSeverity(instance: Instance, severity: AlertSeverity): boolean {
  switch (severity) {
    case 'critical':
      return instance.health.counts.critical > 0
    case 'warning':
      return instance.health.counts.warning > 0
    case 'info':
      return instance.health.counts.info > 0
    default:
      return assertNever(severity)
  }
}

export function filterAndSortInstances(instances: readonly Instance[], filters: InstanceFilters): Instance[] {
  return instances.filter((instance) => {
    if (filters.statuses?.length && !filters.statuses.includes(instance.health.status)) {
      return false
    }
    if (filters.flags?.length && !filters.flags.every((flag) => hasFlag(instance, flag))) {
      return false
    }
    if (filters.alertSeverity && !hasSeverity(instance, filters.alertSeverity)) {
      return false
    }
    if (filters.hasInfo && instance.health.counts.info === 0) {
      return false
    }
    if (filters.hasConfigurationMissing && instance.health.flags.configuration_missing === 0) {
      return false
    }
    return true
  }).sort(compareInstances)
}

function compareInstances(left: Instance, right: Instance): number {
  const healthDifference = healthRank(right.health.status) - healthRank(left.health.status)
  if (healthDifference !== 0) {
    return healthDifference
  }
  return left.name.localeCompare(right.name)
}

export const instancesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances',
  component: InstancesPage,
})

/// 实例列表。
///
/// 页面版式是后续页面切片的样板，三段：
///   1. 页头 —— `h1` + 该页唯一的主操作；
///   2. 工具条 —— 筛选控件与数据新鲜度，和表格分开，不塞进面板标题栏；
///   3. 一个 `flush` 的 `Panel` 包住 `DataGrid`，分页放在面板的 footer 里。
/// 面板标题栏右侧放的是「作用于这张表的视图开关」（这里是密集模式），主操作留在页头 ——
/// 这条分工让后面每一页的按钮都有一个不用再想的去处。
function InstancesPage() {
  const instancesQuery = $api.useQuery('get', '/api/v1/instances', {}, { refetchInterval: pollingIntervals.instances })
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const [createOpen, setCreateOpen] = useState(false)
  const [filters, setFilters] = useState<InstanceFilters>({})
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const canCreate = currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const createDisabledReason = canCreate ? undefined : '需要平台管理员角色'

  const visibleInstances = filterAndSortInstances(instancesQuery.data ?? [], filters)
  // 数据变少（改了筛选、实例被删）之后停在一个空页上，看起来和「没有实例」一样，所以夹住页码。
  const lastPage = Math.max(1, Math.ceil(visibleInstances.length / pageSize))
  const currentPage = Math.min(page, lastPage)
  const pageInstances = visibleInstances.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  // 每次改筛选都回到第一页：留在第 3 页看一个只剩 4 条的结果集是没有意义的。
  function changeFilters(next: (current: InstanceFilters) => InstanceFilters) {
    setFilters(next)
    setPage(1)
  }

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  return (
    <div className="instances-page">
      <header className="instances-page__header">
        <h1 className="dbs-page-title">PostgreSQL 实例</h1>
        <span title={createDisabledReason}>
          <Button size="md" renderIcon={CreateIcon} disabled={!canCreate} onClick={() => setCreateOpen(true)}>
            新建实例
          </Button>
        </span>
      </header>

      {instancesQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(instancesQuery.error, '实例列表加载失败')} />
      )}

      <InstanceFilterBar
        filters={filters}
        onChange={changeFilters}
        freshness={instancesQuery.dataUpdatedAt > 0
          ? <Freshness dataUpdatedAt={instancesQuery.dataUpdatedAt} collectionInterval={pollingIntervals.instances} />
          : undefined}
      />

      <Panel
        flush
        title={`实例（${visibleInstances.length}）`}
        actions={<DensitySwitcher density={density} onChange={changeDensity} />}
        footer={<Pagination
          className="instances-pagination"
          size="md"
          page={currentPage}
          pageSize={pageSize}
          pageSizes={[25, 50, 100]}
          totalItems={visibleInstances.length}
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
        <DataGrid<Instance>
          label="实例列表"
          density={density}
          loading={instancesQuery.isPending}
          skeletonRows={8}
          rows={pageInstances}
          rowKey={(instance) => instance.id}
          rowTone={severityBarTone}
          columns={instanceColumns(density)}
          empty={{
            title: '没有符合条件的实例',
            description: '调整筛选条件，或新建一个实例开始采集。',
          }}
        />
      </Panel>

      <CreateInstanceModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={() => {
          setCreateOpen(false)
          void instancesQuery.refetch()
        }}
      />
    </div>
  )
}

function CreateIcon() {
  return <Icon name="add" />
}

function ClearFiltersIcon() {
  return <Icon name="filterRemove" />
}

/// 密集模式切换。
///
/// 分段单选而不是开关：两档都是明确的档位，开关只说得出「开 / 关」，读起来会是
/// 「密集模式：关」而不是「标准行高」。当前档位由 Carbon `ContentSwitcher` 的
/// `aria-selected` 表达，颜色不是唯一信号。
///
/// 偏好跨页面保持（`routes/root/tableDensity.ts`）：在实例列表调紧了行高，走到会话列表
/// 还是紧的。后续页面照这十行接一遍即可 —— 存储读写只有那一个模块，不要各自再写一份。
function DensitySwitcher({ density, onChange }: { density: TableDensity; onChange: (density: TableDensity) => void }) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return (
    <ContentSwitcher
      className="instances-density"
      size="sm"
      selectedIndex={densities.indexOf(density)}
      onChange={({ index }) => {
        // 组件库把选中下标标成可选；拿不到下标就是没换档，什么都不做，别兜底成第一档。
        const next = index === undefined ? undefined : densities[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {densities.map((value) => <Switch key={value} name={value} text={densityLabel(value)} />)}
    </ContentSwitcher>
  )
}

function InstanceFilterBar({ filters, onChange, freshness }: {
  filters: InstanceFilters
  onChange: (next: (current: InstanceFilters) => InstanceFilters) => void
  freshness: ReactNode
}) {
  return (
    <div className="instances-filters" role="group" aria-label="实例筛选">
      <MultiSelect<HealthStatusOption>
        id="instance-filter-health-status"
        className="instances-filters__control"
        titleText="主状态"
        label="全部状态"
        items={healthStatusOptions}
        itemToString={(item) => item?.label ?? ''}
        // 浮层里可点的是选项自己的标签，「第几个选项」用角色和名字表达不出来，
        // 所以挂稳定测试标识（web/CLAUDE.md 的第 2 档定位方式）。
        itemToElement={(item) => <span data-testid={`health-status-option-${item.value}`}>{item.label}</span>}
        selectedItems={healthStatusOptions.filter((option) => filters.statuses?.includes(option.value) ?? false)}
        onChange={({ selectedItems }) => onChange((current) => ({
          ...current,
          statuses: (selectedItems ?? []).map((item) => item.value),
        }))}
      />
      <MultiSelect<OrthogonalFlagOption>
        id="instance-filter-flags"
        className="instances-filters__control"
        titleText="正交标记"
        label="全部标记"
        items={orthogonalFlagOptions}
        itemToString={(item) => item?.label ?? ''}
        selectedItems={orthogonalFlagOptions.filter((option) => filters.flags?.includes(option.value) ?? false)}
        onChange={({ selectedItems }) => onChange((current) => ({
          ...current,
          flags: (selectedItems ?? []).map((item) => item.value),
        }))}
      />
      <Dropdown<AlertSeverityOption>
        id="instance-filter-alert-severity"
        className="instances-filters__control"
        titleText="至少一条该级告警"
        label="不限"
        items={alertSeverityOptions}
        itemToString={(item) => item?.label ?? ''}
        // 「不限」是清单里的一项而不是一个清除按钮：单选下拉没有可控的「空选中项」，
        // 用 undefined 当选中项会让控件退回非受控，清除筛选之后显示的还是上一次的选择。
        selectedItem={alertSeverityOptions.find((option) => option.value === filters.alertSeverity) ?? alertSeverityOptions[0]}
        onChange={({ selectedItem }) => onChange((current) => ({
          ...current,
          alertSeverity: selectedItem?.value,
        }))}
      />
      <div className="instances-filters__checks">
        <Checkbox
          id="instance-filter-has-info"
          labelText="存在 info"
          checked={filters.hasInfo === true}
          onChange={(_event, { checked }) => onChange((current) => ({ ...current, hasInfo: checked }))}
        />
        <Checkbox
          id="instance-filter-has-configuration-missing"
          labelText="存在配置缺失"
          checked={filters.hasConfigurationMissing === true}
          onChange={(_event, { checked }) => onChange((current) => ({ ...current, hasConfigurationMissing: checked }))}
        />
      </div>
      <Button kind="ghost" size="md" renderIcon={ClearFiltersIcon} onClick={() => onChange(() => ({}))}>清除筛选</Button>
      {freshness !== undefined && <span className="instances-filters__freshness">{freshness}</span>}
    </div>
  )
}

type HealthStatusOption = { value: HealthStatusValue; label: string }
type OrthogonalFlagOption = { value: OrthogonalFlag; label: string }
type AlertSeverityOption = { value?: AlertSeverity; label: string }

const orthogonalFlagOptions: OrthogonalFlagOption[] = [
  { value: 'NO_DATA', label: '无数据' },
  { value: 'MAINTENANCE', label: '维护中' },
  { value: 'RECENTLY_RECOVERED', label: '近期恢复' },
  { value: 'IGNORED', label: '已忽略' },
  { value: 'CONFIGURATION_MISSING', label: '配置缺失' },
]

const healthStatusOptions: HealthStatusOption[] = HEALTH_STATUSES.map((value) => ({ value, label: healthLabel(value) }))

const alertSeverityOptions: AlertSeverityOption[] = [
  { label: '不限' },
  { value: 'critical', label: '严重告警' },
  { value: 'warning', label: '警告告警' },
  { value: 'info', label: 'Info 告警' },
]

function healthLabel(status: HealthStatusValue): string {
  switch (status) {
    case 'CRITICAL':
      return '严重'
    case 'WARNING':
      return '警告'
    case 'UNKNOWN':
      return '未知'
    case 'HEALTHY':
      return '正常'
    case 'PAUSED':
      return '已暂停'
    default:
      return assertNever(status)
  }
}

/// 健康状态的**呈现文案**。暂停时带上已暂停时长，与实例总览页说的是同一句话
/// （两边都走 `collectionPausePresentation`），列表与详情因此不会各说各的。
function healthStatusText(instance: Instance): string {
  if (instance.health.status === 'PAUSED' && instance.collection_pause.updated_at !== undefined) {
    return collectionPausePresentation(new Date(instance.collection_pause.updated_at), new Date()).label
  }
  return healthLabel(instance.health.status)
}

/// 健康状态 → 展示层的状态档位。业务枚举到视觉档位的映射留在页面里：
/// `primitives/` 不认识 `CRITICAL` 是什么，这条边界是它能被别的产品直接搬走的原因。
function healthTone(status: HealthStatusValue): StatusTone {
  switch (status) {
    case 'CRITICAL':
      return 'critical'
    case 'WARNING':
      return 'warning'
    case 'HEALTHY':
      return 'normal'
    case 'UNKNOWN':
      return 'unknown'
    case 'PAUSED':
      return 'unknown'
    default:
      return assertNever(status)
  }
}

/// 行首色条只画严重与警告两档 —— 规范里这条 3px 色条说的是「这一行要处理」，
/// 每一行都上色等于没有色条。它重复的是同一行「健康」列已经写着的字，不是唯一信号。
function severityBarTone(instance: Instance): StatusTone | undefined {
  switch (instance.health.status) {
    case 'CRITICAL':
      return 'critical'
    case 'WARNING':
      return 'warning'
    case 'HEALTHY':
    case 'UNKNOWN':
    case 'PAUSED':
      return undefined
    default:
      return assertNever(instance.health.status)
  }
}

function agentStatusLabel(status: components['schemas']['InstanceAgentStatus']): string {
  switch (status) {
    case 'online':
      return '在线'
    case 'offline':
      return '离线'
    case 'not_installed':
      return '未安装'
    case 'permission_denied':
      return '权限不足'
    case 'error':
      return '异常'
    default:
      return assertNever(status)
  }
}

function agentStatusTone(status: components['schemas']['InstanceAgentStatus']): StatusTone {
  switch (status) {
    case 'online':
      return 'normal'
    case 'offline':
      return 'unknown'
    case 'not_installed':
      return 'unknown'
    case 'permission_denied':
      return 'warning'
    case 'error':
      return 'critical'
    default:
      return assertNever(status)
  }
}

/// 告警计数的档位：有就是那一档，没有就是中性。计数自带 C / W / I 前缀，
/// 颜色只是让「有严重告警」在扫视时先跳出来，不是唯一信号。
function countTone(severity: AlertSeverity, count: number): StatusTone {
  if (count === 0) return 'unknown'
  switch (severity) {
    case 'critical':
      return 'critical'
    case 'warning':
      return 'warning'
    case 'info':
      return 'unknown'
    default:
      return assertNever(severity)
  }
}

/// 列定义。**只给 `minWidth`，页面不设任何 `overflow-x`** —— 1280px 不横向滚动、不丢列
/// 由 `primitives/DataGrid` 结构性地保证（fixed 布局 + 按最小宽度比例分配的百分比列宽 +
/// 省略号悬停提示），页面只负责说明每列至少值多少像素。
///
/// 每一格都是**一行**内容：40px 的标准行高放不下两行，所以迁移前挤在「实例健康」一格里的
/// 名称 / 归因 / 计数 / 标记被拆成四列 —— 一格一个事实，列与列之间扫视时对得齐。
///
/// 十列的最小宽度加起来比 1280px 下的可用宽度（约 976px）大，所以每列都会被按比例压一点。
/// **`grow` 是优先级旋钮**：宽度固定的格子（状态点 + 两三个字、徽章、图标）给 >1，让它们
/// 拿回接近自己最小宽度的空间；长文本列留 1，压不下的那一截由省略号截断，全文在悬停提示里
/// —— 规范要的正是这个次序，而不是把某一列藏掉。
function instanceColumns(density: TableDensity): DataGridColumn<Instance>[] {
  const columns: DataGridColumn<Instance>[] = [
    {
      key: 'health',
      header: '健康',
      minWidth: 96,
      grow: 1.4,
      cell: (instance) => (
        // data-testid 与迁移前一致：实例总览页读的是同一个标识，两处文案必须对得上。
        <span data-testid="health-status">
          <StatusDot tone={healthTone(instance.health.status)}>{healthStatusText(instance)}</StatusDot>
        </span>
      ),
    },
    {
      key: 'name',
      header: '实例',
      minWidth: 160,
      // 实例名是这一行的身份，截断它等于让读者认不出这是谁：富余宽度优先给它。
      grow: 1.7,
      cell: (instance) => <TruncatedText className="instances-table__name">{instance.name}</TruncatedText>,
    },
    {
      key: 'counts',
      header: '告警',
      minWidth: 100,
      grow: 1.7,
      align: 'end',
      // 计数写成带状态色的等宽数字，而不是三个徽章：规范里「越界的数值用状态色」说的就是
      // 这种数字，徽章那圈底色乘以三会在 40px 的行里变成一片色块，也吃掉一列的宽度。
      cell: (instance) => (
        <span className="instances-table__counts dbs-numeric">
          <span data-tone={countTone('critical', instance.health.counts.critical)}>{`C${instance.health.counts.critical}`}</span>
          <span data-tone={countTone('warning', instance.health.counts.warning)}>{`W${instance.health.counts.warning}`}</span>
          <span data-tone={countTone('info', instance.health.counts.info)}>{`I${instance.health.counts.info}`}</span>
        </span>
      ),
    },
    {
      key: 'attribution',
      header: '告警归因',
      minWidth: 136,
      grow: 1.15,
      cell: (instance) => <TruncatedText data-testid="instance-attribution">{attributionLabel(instance)}</TruncatedText>,
    },
    {
      key: 'markers',
      header: '标记',
      minWidth: 96,
      cell: (instance) => <InstanceMarkers flags={instance.health.flags} />,
    },
    {
      key: 'address',
      header: '地址',
      minWidth: 132,
      grow: 1.45,
      cell: (instance) => <TruncatedText className="dbs-numeric">{`${instance.host}:${instance.port}`}</TruncatedText>,
    },
    {
      key: 'agent',
      header: 'Agent',
      minWidth: 84,
      grow: 1.45,
      cell: (instance) => (
        <StatusDot tone={agentStatusTone(instance.agent_status)}>{agentStatusLabel(instance.agent_status)}</StatusDot>
      ),
    },
    {
      key: 'collected',
      header: '采集新鲜度',
      minWidth: 148,
      grow: 0.95,
      // 数值列：右对齐 + 等宽表格数字，行与行之间小数点和冒号都对得齐。
      numeric: true,
      // 采集时刻与新鲜度是同一件事的两种读法（「多久以前」与「什么时候」），并成一格
      // 比各占一列省一列宽度，两个事实都还在。**先写新鲜度**：绝对时间戳有 20 多个字符，
      // 放前面一挤就只剩日期，而扫视时先看的本来就是「多久没采到了」。
      // 装不下的那一截由单元格省略号截断，全文在悬停提示里。
      cell: (instance) => (
        <TruncatedText>
          {`${dataFreshnessLabel(instance.data_freshness_seconds)} · ${lastCollectedAtLabel(instance.last_collected_at)}`}
        </TruncatedText>
      ),
    },
  ]

  // 规范原话：32px 的密集行**丢掉缩略图，而不是把它压扁** —— 20px 的折线塞进 32px 的行里
  // 只剩一团噪声。整列一起走，否则留下一列空格子，白占 80px 宽度。
  // 这不是「在最小支持宽度下隐藏列」——那条规则说的是宽度，这一列的去留由用户自己按的档位决定。
  if (density === 'standard') {
    columns.push({
      key: 'trend',
      header: '趋势',
      minWidth: 80,
      grow: 1.1,
      cell: (instance) => <InstanceTrend instanceID={instance.id} instanceName={instance.name} />,
    })
  }

  // 行内操作是图标链接：可访问名由 `aria-label` 显式给出（图标本身 aria-hidden），
  // 悬停提示是同一句话。写成文字要 112px，而这两个去处在每一行都一样，
  // 那 112px 花在「这一行到底怎么了」的列上更值。
  columns.push({
    key: 'actions',
    header: '操作',
    minWidth: 72,
    grow: 1.7,
    align: 'end',
    cell: (instance) => (
      <span className="instances-table__actions">
        <Link
          className="instances-table__action"
          to="/instances/$id"
          params={{ id: instance.id }}
          search={defaultTimeRange()}
          aria-label="总览"
          title="总览"
        ><Icon name="dashboard" /></Link>
        <Link
          className="instances-table__action"
          to="/instances/$id/settings"
          params={{ id: instance.id }}
          aria-label="接入设置"
          title="接入设置"
        ><Icon name="settings" /></Link>
      </span>
    ),
  })

  return columns
}

function InstanceMarkers({ flags }: { flags: components['schemas']['HealthFlags'] }) {
  return (
    <span className="instances-table__markers">
      {flags.no_data && <StatusBadge tone="unknown">无数据</StatusBadge>}
      {flags.in_maintenance && <StatusBadge tone="unknown">维护中</StatusBadge>}
      {flags.recently_recovered && <StatusBadge tone="normal">近期恢复</StatusBadge>}
      {flags.ignored > 0 && <StatusBadge tone="unknown">{`已忽略 ${flags.ignored}`}</StatusBadge>}
      {flags.configuration_missing > 0 && <StatusBadge tone="warning">{`配置缺失 ${flags.configuration_missing}`}</StatusBadge>}
    </span>
  )
}

const trendMetric = 'pg.connection.total'
const trendWindowMinutes = 60

/// 缩略图的时间窗，**按 5 分钟对齐**。直接拿 `Date.now()` 的话每次渲染都是一个新的查询键，
/// 一屏 50 行就会永远在重新取数；对齐之后同一个 5 分钟里键是稳定的，到点自然翻新，
/// 不需要再给它单独配一个轮询。
export function trendWindow(now: number): { from: string; to: string } {
  const bucket = 5 * 60 * 1000
  const to = Math.floor(now / bucket) * bucket
  return {
    from: new Date(to - trendWindowMinutes * 60 * 1000).toISOString(),
    to: new Date(to).toISOString(),
  }
}

/// 从指标响应里取出缩略图要画的那串值。缺数保持 `null`（缩略图在那里断开），
/// 不补零 —— 补零会把「没采到」画成「掉到 0」。
export function trendValues(response: components['schemas']['MetricSeriesResponse'] | undefined): (number | null)[] {
  const points = response?.metrics.find((metric) => metric.metric === trendMetric)?.series[0]?.points
  if (points === undefined) return []
  return points.map((point) => (typeof point[1] === 'number' ? point[1] : null))
}

/// 行内趋势缩略图。
///
/// 一行一个查询：接口只有 `/instances/{id}/metrics/series`，没有一次取多个实例的入口，
/// 所以整页缩略图就是「当前这一页的行数」个请求（组件只在渲染出来的行里挂载）。
/// 失败不重试、不轮询 —— 一个行内缩略图不值得为它反复打服务端。
/// 批量取多实例趋势的接口是值得补的，见结题报告。
function InstanceTrend({ instanceID, instanceName }: { instanceID: string; instanceName: string }) {
  const range = trendWindow(Date.now())
  const series = $api.useQuery(
    'get',
    '/api/v1/instances/{id}/metrics/series',
    { params: { path: { id: instanceID }, query: { metric: [trendMetric], from: range.from, to: range.to, step: '5m' } } },
    { retry: false, refetchOnWindowFocus: false },
  )

  if (series.isPending) {
    return <SkeletonBlock lines={1} height="1.25rem" decorative />
  }
  return <Sparkline values={trendValues(series.data)} label={`${instanceName} 近 ${trendWindowMinutes} 分钟连接数趋势`} />
}

/// 新建实例表单的校验规则。
///
/// 与生成的请求体类型对齐靠两处，漂了就编译不过：`satisfies z.ZodType<InstanceCreateInput>`
/// 要求 schema 的出参就是请求体，`instanceCreateBody` 再把出参真的当请求体用。
/// schema 里不写 `transform` / `default` —— 表单值就是提交值，trim 放在提交处，看得见。
const instanceCreateSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入实例名称'),
  host: z.string().refine((value) => value.trim() !== '', '请输入主机地址'),
  port: z.number({ error: '请输入端口' }).int('端口必须是整数').min(1, '端口范围 1–65535').max(65535, '端口范围 1–65535'),
  database: z.string().refine((value) => value.trim() !== '', '请输入数据库名'),
  username: z.string().refine((value) => value.trim() !== '', '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
}) satisfies z.ZodType<InstanceCreateInput>

type InstanceCreateValues = z.infer<typeof instanceCreateSchema>

/// 服务端字段错误只接受这六个 —— 每一个都有渲染出来的输入框可以聚焦。
/// 清单之外的字段名落回整表单的错误条；`setError` 一个表单里没有的名字会挂出一条
/// 永远显示不出来、也永远清不掉的错误。
const instanceCreateFields = [
  'name',
  'host',
  'port',
  'database',
  'username',
  'password',
] as const satisfies readonly FieldPath<InstanceCreateValues>[]

function instanceCreateBody(values: InstanceCreateValues): InstanceCreateInput {
  return {
    name: values.name.trim(),
    host: values.host.trim(),
    port: values.port,
    database: values.database.trim(),
    username: values.username.trim(),
    password: values.password,
  }
}

const emptyInstanceCreateValues: InstanceCreateValues = {
  name: '',
  host: '',
  port: 5432,
  database: '',
  username: '',
  password: '',
}

function CreateInstanceModal({ open, onClose, onCreated }: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const createInstance = $api.useMutation('post', '/api/v1/instances')
  const { control, formState, handleSubmit, register, reset, setError } = useForm<InstanceCreateValues>({
    resolver: zodResolver(instanceCreateSchema),
    defaultValues: emptyInstanceCreateValues,
  })
  const [failure, setFailure] = useState('')

  function close() {
    setFailure('')
    reset(emptyInstanceCreateValues)
    onClose()
  }

  const submit = handleSubmit((values) => {
    setFailure('')
    createInstance.mutate({ body: instanceCreateBody(values) }, {
      onSuccess: () => {
        reset(emptyInstanceCreateValues)
        onCreated()
      },
      onError: (error) => {
        // 字段级错误落到对应输入框并聚焦第一个；一条都落不下时才退回整表单的错误条。
        if (applyApiFieldErrors<InstanceCreateValues>(error, instanceCreateFields, setError).length === 0) {
          setFailure(apiErrorMessage(error, '创建实例失败，请检查连接信息'))
        }
      },
    })
  })

  return (
    <Modal
      open={open}
      modalHeading="新建实例"
      primaryButtonText="连接测试并创建"
      secondaryButtonText="取消"
      primaryButtonDisabled={createInstance.isPending}
      onRequestSubmit={() => void submit()}
      onRequestClose={close}
      onSecondarySubmit={close}
      size="sm"
    >
      {/* Modal 的主按钮渲染在 children 之外，点它到不了这里的 onSubmit，所以提交口是
          `onRequestSubmit`；`<form>` 仍然留着，让回车提交与原生表单语义走同一个 handleSubmit。
          主按钮**不能**是 type="submit"，那会提交两次。 */}
      <form className="instances-create-form" onSubmit={submit} noValidate>
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
        <FormField label="主机" required errorText={formState.errors.host?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
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
        <FormField label="数据库" required errorText={formState.errors.database?.message}>
          {(field) => <TextInput
            id={field.id}
            labelText=""
            hideLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('database')}
          />}
        </FormField>
        <FormField label="用户名" required errorText={formState.errors.username?.message}>
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
        <FormField label="密码" required errorText={formState.errors.password?.message}>
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
