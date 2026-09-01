import { Button, ContentSwitcher, Select, SelectItem, Switch, TextInput } from '@carbon/react'
import { Link, createRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Controller, useForm } from 'react-hook-form'
import type { FieldPath } from 'react-hook-form'
import { z } from 'zod'
import { $api } from '../../api/client'
import { apiErrorMessage, applyApiFieldErrors } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { HealthStatus, healthLabel } from '../../domain/HealthStatus'
import {
  bootstrapDatabaseHelperText,
  bootstrapDatabaseLabel,
  instanceEngineLabel,
  instanceEngineShortLabel,
  instanceEngines,
} from '../../domain/instanceEngine'
import { SuppressionTags } from '../../domain/SuppressionTags'
import { zodResolver } from '../../forms/zodResolver'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { Dropdown } from '../../primitives/Dropdown'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import { Modal } from '../../primitives/Modal'
import { MultiSelect } from '../../primitives/MultiSelect'
import { NotificationBar } from '../../primitives/NotificationBar'
import { NumberInput } from '../../primitives/NumberInput'
import { Pagination } from '../../primitives/Pagination'
import { Panel } from '../../primitives/Panel'
import { Sparkline } from '../../primitives/Sparkline'
import type { StatusTone } from '../../primitives/StatusBadge'
import { TruncatedText } from '../../primitives/TruncatedText'
import {
  attributionLabel,
  collectionFreshnessLabel,
  collectionFreshnessTitle,
  connectionSaturationLabel,
  connectionSaturationTone,
  instanceMetricEntry,
  latestValue,
  trendValues,
} from '../instanceProjection'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../root/tableDensity'
import type {
  AlertSeverity,
  HealthStatusValue,
  InstanceFlag,
  InstanceListSearch,
  InstanceListSort,
  InvalidInstanceListSearch,
} from './instanceListSearch'
import {
  INSTANCE_ALERT_SEVERITIES,
  INSTANCE_FLAGS,
  INSTANCE_HEALTH_STATUSES,
  INSTANCE_LIST_SORTS,
  INSTANCE_PAGE_SIZES,
  currentPage,
  currentPageSize,
  currentSort,
  defaultInstanceListSearch,
  hasInstanceFilters,
  instanceListQuery,
  parseInstanceListSearch,
  withInstanceFilters,
} from './instanceListSearch'
import './instances.css'

type InstanceCreateInput = components['schemas']['InstanceCreateInput']
type Instance = components['schemas']['Instance']
type InstancesMetricSeriesResponse = components['schemas']['InstancesMetricSeriesResponse']

function assertNever(value: never): never {
  throw new Error(`unexpected instance list value: ${String(value)}`)
}

/// 吞吐与连接饱和度是列表上仅有的两个指标读数，一次批量请求一起取回。
///
/// 趋势从连接数换成**吞吐**：连接数已经由饱和度列用一个百分比说清楚了，
/// 折线再画一遍是同一件事说两次。两者都是语义位（吞吐 / 连接饱和度）在 PostgreSQL 上的绑定。
const throughputMetric = 'pg.tps'
const saturationMetric = 'pg.connection.saturation_percent'
const trendWindowMinutes = 60

export const instancesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances',
  // 筛选与排序进地址栏：一个筛好的视图要能整条发给同事，总览页的下钻链接也直接落成
  // 这套查询参数。解析失败不静默兜底 —— 同一个地址必须给同一个视图。
  validateSearch: (search): InstanceListSearch | InvalidInstanceListSearch =>
    parseInstanceListSearch(search),
  component: InstancesRoutePage,
})

function InstancesRoutePage() {
  const search = instancesRoute.useSearch()
  if ('error' in search) return <InvalidInstanceSearchNotice message={search.error} />
  return <InstancesPage search={search} />
}

function InvalidInstanceSearchNotice({ message }: { message: string }) {
  const navigate = useNavigate()
  return (
    <div className="instances-page">
      <header className="instances-page__header">
        <h1 className="dbs-page-title">PostgreSQL 实例</h1>
      </header>
      <NotificationBar
        tone="critical"
        title={message}
        action={{
          label: '重置筛选',
          onClick: () => void navigate({ to: '/instances', search: defaultInstanceListSearch() }),
        }}
      >
        地址栏里的筛选条件读不懂，所以没有去猜一个视图给你看。
      </NotificationBar>
    </div>
  )
}

/// 实例列表。
///
/// 页面版式是后续页面切片的样板，三段：
///   1. 页头 —— `h1` + 该页唯一的主操作；
///   2. 工具条 —— 筛选控件与数据新鲜度，和表格分开，不塞进面板标题栏；
///   3. 一个 `flush` 的 `Panel` 包住 `DataGrid`，分页放在面板的 footer 里。
///
/// **分页、筛选、排序都在服务端**：接口收 `page` / `page_size` / `q` / `engine` /
/// `status` / `flags` / `severity` / `sort`，返回当页与总数。浏览器不再持有全量实例，
/// 500 台时这是「页面还能用」与「页面卡住」的分界。
function InstancesPage({ search }: { search: InstanceListSearch }) {
  const navigate = useNavigate()
  const instancesQuery = $api.useQuery(
    'get',
    '/api/v1/instances',
    { params: { query: instanceListQuery(search) } },
    { refetchInterval: pollingIntervals.instances },
  )
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const [createOpen, setCreateOpen] = useState(false)
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const canCreate = currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const createDisabledReason = canCreate ? undefined : '需要平台管理员角色'

  const pageInstances = instancesQuery.data?.items ?? []
  // 还没有结果时写 0：这是「这一页还没回来」，骨架行同时在说这件事，
  // 不是「筛出来 0 条」——后者由空态自己说。
  const total = instancesQuery.data === undefined ? 0 : instancesQuery.data.total
  const trends = useInstanceTrends(pageInstances)

  function changeSearch(changes: Partial<Omit<InstanceListSearch, 'page'>>) {
    void navigate({ to: '/instances', search: withInstanceFilters(search, changes) })
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
          <Button size="md" renderIcon={Icon.glyph.add} disabled={!canCreate} onClick={() => setCreateOpen(true)}>
            新建实例
          </Button>
        </span>
      </header>

      {instancesQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(instancesQuery.error, '实例列表加载失败')} />
      )}

      <InstanceFilterBar
        search={search}
        onChange={changeSearch}
        freshness={instancesQuery.dataUpdatedAt > 0
          ? <Freshness dataUpdatedAt={instancesQuery.dataUpdatedAt} collectionInterval={pollingIntervals.instances} />
          : undefined}
      />

      <Panel
        flush
        title={`实例（${total}）`}
        actions={<DensitySwitcher density={density} onChange={changeDensity} />}
        footer={<Pagination
          className="instances-pagination"
          size="md"
          page={currentPage(search)}
          pageSize={currentPageSize(search)}
          pageSizes={[...INSTANCE_PAGE_SIZES]}
          totalItems={total}
          onChange={({ page: nextPage, pageSize: nextPageSize }) => {
            // 页大小变了就回第一页：同一个页码在两种页大小下指的不是同一批行。
            void navigate({
              to: '/instances',
              search: nextPageSize === currentPageSize(search)
                ? { ...search, page: nextPage }
                : withInstanceFilters(search, { page_size: nextPageSize }),
            })
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
          rowTestId="instance-row"
          rowTone={severityBarTone}
          columns={instanceColumns(density, trends)}
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

/// 当页所有实例的吞吐趋势与连接饱和度，**一次请求**。
///
/// 从前每一行各发一次 `/instances/{id}/metrics/series`，一页 50 行就是 50 个并发请求；
/// 500 台的机群里这不只是慢，会打满后端连接池。批量端点按 `instance_id` 收多台，
/// 响应里每台一段，缺数的那台也在，带着缺数原因。
function useInstanceTrends(instances: readonly Instance[]) {
  const range = trendWindow(Date.now())
  const instanceIDs = instances.map((instance) => instance.id)
  const trendsQuery = $api.useQuery(
    'get',
    '/api/v1/instances/metrics/series',
    {
      params: {
        query: {
          instance_id: instanceIDs,
          metric: [throughputMetric, saturationMetric],
          from: range.from,
          to: range.to,
          step: '5m',
        },
      },
    },
    { enabled: instanceIDs.length > 0, retry: false, refetchOnWindowFocus: false },
  )
  return trendsQuery.data
}

/// 缩略图的时间窗，**按 5 分钟对齐**。直接拿 `Date.now()` 的话每次渲染都是一个新的查询键，
/// 整页就会永远在重新取数；对齐之后同一个 5 分钟里键是稳定的，到点自然翻新。
export function trendWindow(now: number): { from: string; to: string } {
  const bucket = 5 * 60 * 1000
  const to = Math.floor(now / bucket) * bucket
  return {
    from: new Date(to - trendWindowMinutes * 60 * 1000).toISOString(),
    to: new Date(to).toISOString(),
  }
}

/// 密集模式切换。
///
/// 分段单选而不是开关：两档都是明确的档位，开关只说得出「开 / 关」，读起来会是
/// 「密集模式：关」而不是「标准行高」。当前档位由 Carbon `ContentSwitcher` 的
/// `aria-selected` 表达，颜色不是唯一信号。
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

/// 工具条。每个控件改的都是地址栏里的一个查询参数，不是组件里的一份局部状态 ——
/// 筛完之后地址本身就是这个视图，可以整条发出去。
function InstanceFilterBar({ search, onChange, freshness }: {
  search: InstanceListSearch
  onChange: (changes: Partial<Omit<InstanceListSearch, 'page'>>) => void
  freshness: ReactNode
}) {
  // 搜索框是受控输入，但地址栏不该每敲一个字就变一次；输入停下来之后才提交。
  const [draft, setDraft] = useState(search.q === undefined ? '' : search.q)
  const committed = search.q === undefined ? '' : search.q
  useEffect(() => {
    setDraft(committed)
  }, [committed])
  useEffect(() => {
    if (draft === committed) return
    const timer = setTimeout(() => onChange({ q: draft === '' ? undefined : draft }), 300)
    return () => clearTimeout(timer)
  }, [draft, committed, onChange])

  return (
    <div className="instances-filters" role="group" aria-label="实例筛选">
      <TextInput
        id="instance-filter-search"
        className="instances-filters__control"
        labelText="搜索"
        placeholder="实例名或地址"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
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
        selectedItems={healthStatusOptions.filter((option) => search.status?.includes(option.value) ?? false)}
        onChange={({ selectedItems }) => onChange({ status: (selectedItems ?? []).map((item) => item.value) })}
      />
      <MultiSelect<OrthogonalFlagOption>
        id="instance-filter-flags"
        className="instances-filters__control"
        titleText="正交标记"
        label="全部标记"
        items={orthogonalFlagOptions}
        itemToString={(item) => item?.label ?? ''}
        selectedItems={orthogonalFlagOptions.filter((option) => search.flags?.includes(option.value) ?? false)}
        onChange={({ selectedItems }) => onChange({ flags: (selectedItems ?? []).map((item) => item.value) })}
      />
      <MultiSelect<AlertSeverityOption>
        id="instance-filter-alert-severity"
        className="instances-filters__control"
        titleText="至少一条该级告警"
        label="不限"
        items={alertSeverityOptions}
        itemToString={(item) => item?.label ?? ''}
        selectedItems={alertSeverityOptions.filter((option) => search.severity?.includes(option.value) ?? false)}
        onChange={({ selectedItems }) => onChange({ severity: (selectedItems ?? []).map((item) => item.value) })}
      />
      <Dropdown<SortOption>
        id="instance-filter-sort"
        className="instances-filters__control"
        titleText="排序"
        label="健康优先"
        items={sortOptions}
        itemToString={(item) => item?.label ?? ''}
        selectedItem={sortOptions.find((option) => option.value === currentSort(search))}
        onChange={({ selectedItem }) => onChange({ sort: selectedItem?.value })}
      />
      <Button
        kind="ghost"
        size="md"
        renderIcon={Icon.glyph.filterRemove}
        // 一个筛选都没设时它无事可做：禁用比点下去什么都不变更诚实。排序不算筛选，
        // 它不改「看到哪些行」，只改「先看到谁」，所以清除筛选不动它。
        disabled={!hasInstanceFilters(search)}
        onClick={() => onChange({ q: undefined, engine: undefined, status: undefined, flags: undefined, severity: undefined })}
      >
        清除筛选
      </Button>
      {freshness !== undefined && <span className="instances-filters__freshness">{freshness}</span>}
    </div>
  )
}

type HealthStatusOption = { value: HealthStatusValue; label: string }
type OrthogonalFlagOption = { value: InstanceFlag; label: string }
type AlertSeverityOption = { value: AlertSeverity; label: string }
type SortOption = { value: InstanceListSort; label: string }

function flagLabel(flag: InstanceFlag): string {
  switch (flag) {
    case 'NO_DATA':
      return '无数据'
    case 'MAINTENANCE':
      return '维护中'
    case 'RECENTLY_RECOVERED':
      return '近期恢复'
    case 'IGNORED':
      return '已忽略'
    case 'CONFIGURATION_MISSING':
      return '配置缺失'
    default:
      return assertNever(flag)
  }
}

function severityLabel(severity: AlertSeverity): string {
  switch (severity) {
    case 'critical':
      return '严重告警'
    case 'warning':
      return '警告告警'
    case 'info':
      return 'Info 告警'
    default:
      return assertNever(severity)
  }
}

function sortLabel(sort: InstanceListSort): string {
  switch (sort) {
    case 'health':
      return '健康优先'
    case 'name':
      return '名称升序'
    case '-name':
      return '名称降序'
    case 'stalest':
      return '最不新鲜优先'
    default:
      return assertNever(sort)
  }
}

const orthogonalFlagOptions: OrthogonalFlagOption[] = INSTANCE_FLAGS.map((value) => ({ value, label: flagLabel(value) }))
const healthStatusOptions: HealthStatusOption[] = INSTANCE_HEALTH_STATUSES.map((value) => ({ value, label: healthLabel(value) }))
const alertSeverityOptions: AlertSeverityOption[] = INSTANCE_ALERT_SEVERITIES.map((value) => ({ value, label: severityLabel(value) }))
const sortOptions: SortOption[] = INSTANCE_LIST_SORTS.map((value) => ({ value, label: sortLabel(value) }))

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
/// 八列，974px 预算（1280 − 256px 侧栏 − 页边距，实测）里正好排满：
/// 实例(230) · 引擎(56) · 健康(90) · 告警(90) · 告警归因(213) · 连接饱和度(96) ·
/// 吞吐趋势(72) · 采集新鲜度(127) = 974。各列一律 `grow: 1`（`grow` 不是优先级旋钮：
/// 一旦不等，低的那列会分到低于自己 `minWidth` 的宽度，先被截掉的是表头）。
///
/// 相对上一版的取舍，判据是「这一列帮不帮我决定要不要点进这一行」：
///   - **地址整列去掉**。500 台时没人靠 IP 认机器，靠名字；地址进详情页，
///     并且仍然在搜索索引里（`q` 命中名称与地址）。
///   - **标记并入健康列**：暂停 / 维护 / 无数据是健康状态的后缀，不是另一个问题。
///   - **Agent 并入采集新鲜度**：「Agent 离线」本来就是新鲜度失效的一种。
///   - **趋势从连接数换成吞吐**：连接数已经由饱和度列用百分比说清楚了。
function instanceColumns(density: TableDensity, trends: InstancesMetricSeriesResponse | undefined): DataGridColumn<Instance>[] {
  const columns: DataGridColumn<Instance>[] = [
    {
      key: 'name',
      header: '实例',
      minWidth: 230,
      // 实例名是这一行的身份，也是它的入口：截断它等于让读者认不出这是谁，
      // 所以富余宽度优先给它，装不下的那一截由省略号截断、全文进悬停提示。
      cell: (instance) => (
        <Link
          className="instances-table__name cds--link"
          to="/instances/$id"
          params={{ id: instance.id }}
          search={defaultTimeRange()}
        >
          <TruncatedText>{instance.name}</TruncatedText>
        </Link>
      ),
    },
    {
      key: 'engine',
      header: '引擎',
      minWidth: 56,
      // 56px 放得下短名放不下产品全名，全名进悬停提示。混着 PG 与 MySQL 时，
      // 「我在看什么」必须一眼看得出来，所以这一列不能省。
      cell: (instance) => (
        <TruncatedText title={instanceEngineLabel(instance.engine)}>
          {instanceEngineShortLabel(instance.engine)}
        </TruncatedText>
      ),
    },
    {
      key: 'health',
      header: '健康',
      minWidth: 90,
      // 健康状态整块交给 domain/HealthStatus：文案、档位、暂停时长都在那里定义一次，
      // 实例总览读的是同一个组件（也是同一个 data-testid），两处因此不可能各说各的。
      // 正交标记接在后面：它们是这个状态的后缀，不是另一个问题，所以不再单独占一列。
      cell: (instance) => (
        <span className="instances-table__health">
          <HealthStatus status={instance.health.status} pausedAt={instance.collection_pause.updated_at} />
          <SuppressionTags className="instances-table__markers" flags={instance.health.flags} />
        </span>
      ),
    },
    {
      key: 'counts',
      header: '告警',
      minWidth: 90,
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
      minWidth: 213,
      cell: (instance) => <TruncatedText data-testid="instance-attribution">{attributionLabel(instance)}</TruncatedText>,
    },
    {
      key: 'saturation',
      header: '连接饱和度',
      minWidth: 96,
      numeric: true,
      // 百分比而不是连接数：500 台里没人记得住每台的 max_connections。
      // 取不到就写破折号，不写 0 —— 0% 是一个具体的读数，缺数不是。
      cell: (instance) => {
        const percent = latestValue(instanceMetricEntry(trends, instance.id, saturationMetric))
        return (
          <span className="instances-table__saturation dbs-numeric" data-tone={connectionSaturationTone(percent)}>
            {connectionSaturationLabel(percent)}
          </span>
        )
      },
    },
  ]

  // 规范原话：32px 的密集行**丢掉缩略图，而不是把它压扁** —— 20px 的折线塞进 32px 的行里
  // 只剩一团噪声。整列一起走，否则留下一列空格子，白占 72px 宽度。
  // 这不是「在最小支持宽度下隐藏列」——那条规则说的是宽度，这一列的去留由用户自己按的档位决定。
  if (density === 'standard') {
    columns.push({
      key: 'trend',
      header: '吞吐趋势',
      minWidth: 72,
      cell: (instance) => (
        <Sparkline
          values={trendValues(instanceMetricEntry(trends, instance.id, throughputMetric))}
          label={`${instance.name} 近 ${trendWindowMinutes} 分钟吞吐趋势`}
        />
      ),
    })
  }

  columns.push({
    key: 'collected',
    header: '采集新鲜度',
    minWidth: 127,
    // 数值列：右对齐 + 等宽表格数字，行与行之间对得齐。
    numeric: true,
    // 「多久没采到了」是扫视时先看的那一个，所以它写在格子里；绝对时刻与 Agent 的
    // 失效原因进悬停提示。Agent 单独一列已经取消：它说的是同一件事。
    cell: (instance) => (
      <TruncatedText title={collectionFreshnessTitle(instance)}>
        {collectionFreshnessLabel(instance)}
      </TruncatedText>
    ),
  })

  return columns
}

/// 新建实例表单的校验规则。
///
/// 与生成的请求体类型对齐靠两处，漂了就编译不过：`satisfies z.ZodType<InstanceCreateInput>`
/// 要求 schema 的出参就是请求体，`instanceCreateBody` 再把出参真的当请求体用。
/// schema 里不写 `transform` / `default` —— 表单值就是提交值，trim 放在提交处，看得见。
const instanceCreateSchema = z.object({
  name: z.string().refine((value) => value.trim() !== '', '请输入实例名称'),
  engine: z.enum(instanceEngines, { error: '请选择数据库引擎' }),
  host: z.string().refine((value) => value.trim() !== '', '请输入主机地址'),
  port: z.number({ error: '请输入端口' }).int('端口必须是整数').min(1, '端口范围 1–65535').max(65535, '端口范围 1–65535'),
  // bootstrap database 只是建连接用的库名，不限定监控范围，所以它可以留空：
  // 留空时由服务端按引擎补默认库（PostgreSQL 是 postgres）。
  database: z.string(),
  username: z.string().refine((value) => value.trim() !== '', '请输入用户名'),
  password: z.string().min(1, '请输入密码'),
}) satisfies z.ZodType<InstanceCreateInput>

type InstanceCreateValues = z.infer<typeof instanceCreateSchema>

/// 服务端字段错误只接受这六个 —— 每一个都有渲染出来的输入框可以聚焦。
/// 清单之外的字段名落回整表单的错误条；`setError` 一个表单里没有的名字会挂出一条
/// 永远显示不出来、也永远清不掉的错误。
const instanceCreateFields = [
  'name',
  'engine',
  'host',
  'port',
  'database',
  'username',
  'password',
] as const satisfies readonly FieldPath<InstanceCreateValues>[]

function instanceCreateBody(values: InstanceCreateValues): InstanceCreateInput {
  const database = values.database.trim()
  return {
    name: values.name.trim(),
    engine: values.engine,
    host: values.host.trim(),
    port: values.port,
    // 留空就整个字段不发：默认库由服务端按引擎决定，前端不替它挑。
    ...(database === '' ? {} : { database }),
    username: values.username.trim(),
    password: values.password,
  }
}

const emptyInstanceCreateValues: InstanceCreateValues = {
  name: '',
  engine: 'POSTGRESQL',
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
        <FormField
          label="引擎"
          required
          helperText="实例运行的数据库产品，接入后不可更改。"
          errorText={formState.errors.engine?.message}
        >
          {(field) => <Select
            id={field.id}
            labelText=""
            noLabel
            invalid={field.invalid}
            aria-describedby={field.describedBy}
            {...register('engine')}
          >
            {instanceEngines.map((engine) => <SelectItem
              key={engine}
              value={engine}
              text={instanceEngineLabel(engine)}
            />)}
          </Select>}
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
