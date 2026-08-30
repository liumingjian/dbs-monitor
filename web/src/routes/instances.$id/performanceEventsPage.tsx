import { Button, ContentSwitcher, Pagination, Switch, Tab, TabList, Tabs } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness } from '../../domain/Freshness'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import type { StatusTone } from '../../primitives/StatusBadge'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../root/tableDensity'
import {
  parsePerformanceEventSearch,
  performanceEventRecoveryFilter,
  serializePerformanceEventSearch,
  type PerformanceEventDisposition,
  type PerformanceEventSearch,
  type PerformanceEventTab,
} from './performanceEvents'
import {
  PerformanceEventSeverityTag,
  PerformanceEventMaintenanceTag,
  performanceEventDispositionLabel,
  performanceEventDurationLabel,
  performanceEventTimeLabel,
  performanceEventTypeLabel,
} from './performanceEventPresentation'
import { WorkbenchHeader } from './workbench'
import './performanceEvents.css'

type PerformanceEvent = components['schemas']['PerformanceEvent']
type AlertSeverity = components['schemas']['AlertSeverity']

const eventPageSize = 50

const tabOrder = ['firing', 'recovered', 'disposed'] as const satisfies readonly PerformanceEventTab[]

export const performanceEventsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/performance-events',
  validateSearch: (search): PerformanceEventSearch | { error: string } => parsePerformanceEventSearch(search),
  component: PerformanceEventsPage,
})

function PerformanceEventsPage() {
  const { id } = performanceEventsRoute.useParams()
  const search = performanceEventsRoute.useSearch()
  const navigate = performanceEventsRoute.useNavigate()

  if ('error' in search) {
    const now = new Date()
    const defaults: PerformanceEventSearch = {
      from: new Date(now.getTime() - 60 * 60_000).toISOString(),
      to: now.toISOString(),
      tab: 'firing',
      disposition: 'ACKED',
      page: 1,
    }
    return <div className="performance-events-page">
      <NotificationBar tone="critical" title={search.error} />
      <Link
        to="/instances/$id/performance-events"
        params={{ id }}
        search={serializePerformanceEventSearch(defaults)}
      ><Button size="md">使用默认筛选</Button></Link>
    </div>
  }

  return <PerformanceEventLists
    instanceID={id}
    search={search}
    onSearchChange={(next) => void navigate({ search: serializePerformanceEventSearch(next) })}
  />
}

/// 性能事件列表。版式照列表页样板：工作台页头 → 页内标题 → 页签 → 工具条 →
/// 一个 flush 面板包住表格，分页放在面板的 footer 里。
function PerformanceEventLists({ instanceID, search, onSearchChange }: {
  instanceID: string
  search: PerformanceEventSearch
  onSearchChange: (search: PerformanceEventSearch) => void
}) {
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', {
    params: { path: { id: instanceID } },
  })
  const offset = (search.page - 1) * eventPageSize
  const events = $api.useQuery('get', '/api/v1/instances/{id}/performance-events', {
    params: {
      path: { id: instanceID },
      query: {
        from: search.from,
        to: search.to,
        recovered: performanceEventRecoveryFilter(search.tab),
        disposition: search.tab === 'disposed' ? search.disposition : undefined,
        limit: eventPageSize,
        offset,
        sort: '-derived_at',
      },
    },
  }, { refetchInterval: search.tab === 'firing' ? pollingIntervals.firingPerformanceEvents : false })

  // 页签就是地址。每个去处包成一个「已经知道自己去哪儿」的组件，身份用 memo 固定住 ——
  // 身份一变锚点就重挂，键盘焦点会被甩掉（先例见 workbench.tsx）。
  const tabLinks = useMemo(() => {
    const destination = (tab: PerformanceEventTab) => (props: object) => <Link
      {...props}
      to="/instances/$id/performance-events"
      params={{ id: instanceID }}
      search={serializePerformanceEventSearch({ ...search, tab, page: 1 })}
    />
    return {
      firing: destination('firing'),
      recovered: destination('recovered'),
      disposed: destination('disposed'),
    }
  }, [instanceID, search])

  const total = events.data?.total
  // 计数只挂在当前页签上：另外两档的总数从来没有请求过，写个数字上去就是编的。
  const tabLabel = (tab: PerformanceEventTab, label: string) =>
    search.tab === tab && total !== undefined ? `${label} ${total}` : label

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  return <div className="performance-events-page">
    <WorkbenchHeader
      id={instanceID}
      instanceName={instance.data?.name}
      activeKey="events"
      search={{ from: search.from, to: search.to }}
    />

    <div className="performance-events-page__heading">
      <h2 className="dbs-panel-title">性能事件</h2>
      <p className="dbs-caption">查看告警派生的异常、原因与处置证据</p>
    </div>

    <Tabs selectedIndex={tabOrder.indexOf(search.tab)}>
      <TabList aria-label="性能事件状态" activation="manual">
        <Tab as={tabLinks.firing}>{tabLabel('firing', '触发中')}</Tab>
        <Tab as={tabLinks.recovered}>{tabLabel('recovered', '已恢复')}</Tab>
        <Tab as={tabLinks.disposed}>{tabLabel('disposed', '已确认 / 已忽略')}</Tab>
      </TabList>
    </Tabs>

    <div className="performance-events-toolbar" role="group" aria-label="性能事件筛选">
      <TimeRangePicker
        from={search.from}
        to={search.to}
        onChange={(range) => onSearchChange({ ...search, ...range, page: 1 })}
      />
      {search.tab === 'disposed' && <DispositionSwitcher
        disposition={search.disposition}
        onChange={(disposition) => onSearchChange({ ...search, disposition, page: 1 })}
      />}
      {search.tab === 'firing' && events.dataUpdatedAt > 0 && <span className="performance-events-toolbar__freshness">
        <Freshness
          dataUpdatedAt={events.dataUpdatedAt}
          collectionInterval={pollingIntervals.firingPerformanceEvents}
        />
      </span>}
    </div>

    {events.error && <NotificationBar tone="critical" title={apiErrorMessage(events.error, '性能事件加载失败')} />}

    <Panel
      flush
      title={eventPanelTitle(search, total)}
      actions={<DensitySwitcher density={density} onChange={changeDensity} />}
      footer={<Pagination
        className="performance-events-pagination"
        size="md"
        page={search.page}
        pageSize={eventPageSize}
        pageSizes={[eventPageSize]}
        // 每页条数在这一页是给死的（迁移前也没有切换器）。禁用而不是藏起来：
        // 藏了就没人知道一页是 50 条。
        pageSizeInputDisabled
        // 总数还没回来时页数就是未知的，不是 0：`pagesUnknown` 让分页条照实说
        // 「第 N 页」，而不是编一个总数出来。
        totalItems={total}
        pagesUnknown={total === undefined}
        backwardText="上一页"
        forwardText="下一页"
        itemsPerPageText="每页条数"
        itemRangeText={(min, max, count) => `第 ${min}–${max} 条，共 ${count} 条`}
        itemText={(min, max) => `第 ${min}–${max} 条`}
        pageText={(page) => `第 ${page} 页`}
        pageRangeText={(_current, count) => `共 ${count} 页`}
        pageNumberText="页码"
        onChange={({ page }) => onSearchChange({ ...search, page })}
      />}
    >
      <DataGrid<PerformanceEvent>
        label="性能事件列表"
        density={density}
        loading={events.isPending}
        skeletonRows={8}
        rows={events.data?.items ?? []}
        rowKey={(event) => event.id}
        rowTestId="performance-event-row"
        rowTone={(event) => severityBarTone(event.severity)}
        columns={eventColumns(search)}
        empty={{ title: eventEmptyText(search), description: '换一个时间范围，或切到别的页签看看其他状态的事件。' }}
      />
    </Panel>
  </div>
}

/// 密集模式切换。读写只有 `routes/root/tableDensity.ts` 一个去处，键名不要各自再发明。
function DensitySwitcher({ density, onChange }: { density: TableDensity; onChange: (density: TableDensity) => void }) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return (
    <ContentSwitcher
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

/// 「已确认 / 已忽略」页签里的二级筛选。分段单选而不是第三层页签：它不是地址的主结构，
/// 只是这张表的取值筛选，和时间范围同一档，所以留在工具条里。
function DispositionSwitcher({ disposition, onChange }: {
  disposition: PerformanceEventDisposition
  onChange: (disposition: PerformanceEventDisposition) => void
}) {
  const dispositions = ['ACKED', 'IGNORED'] as const satisfies readonly PerformanceEventDisposition[]
  return (
    <ContentSwitcher
      size="md"
      selectedIndex={dispositions.indexOf(disposition)}
      onChange={({ index }) => {
        const next = index === undefined ? undefined : dispositions[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {dispositions.map((value) => (
        <Switch key={value} name={value} text={performanceEventDispositionLabel(value)} />
      ))}
    </ContentSwitcher>
  )
}

/// 行首 3px 色条只是把「级别」那一列的取值重复一遍，不是唯一信号；info 不上条。
function severityBarTone(severity: AlertSeverity): StatusTone | undefined {
  switch (severity) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return undefined
    default: return assertNever(severity)
  }
}

function eventPanelTitle(search: PerformanceEventSearch, total: number | undefined): string {
  const label = eventTabTitle(search)
  return total === undefined ? label : `${label}（${total}）`
}

function eventTabTitle(search: PerformanceEventSearch): string {
  switch (search.tab) {
    case 'firing': return '触发中的事件'
    case 'recovered': return '已恢复的事件'
    case 'disposed': return `${performanceEventDispositionLabel(search.disposition)}的事件`
    default: return assertNever(search.tab)
  }
}

/// 列宽是在 1280px 上量出来的，不是估的。
///
/// 页面可用宽度实测 976px（1280 − 256px 侧栏 − 2×24px 页边距），而表格外壳沿用组件库的
/// 单元格左右各 16px 内边距，所以每多一列就先扣掉 32px。迁移前这张表有 12 列、声明
/// `scroll={{ x: 1700 }}`，靠横向滚动活着；规范不允许 1280px 横向滚动，于是 976px 要装下
/// 全部列。真去量了一遍：13 列时每格只剩 40 上下的内容宽，「告警中」被截成「告警」、
/// 「严重」被截成「严」，一整屏没有一个完整的词 —— 那不是「装下了」，只是把丢失伪装成省略号。
///
/// 因此列表这一层收成十列，每列都留得下完整取值（见下方各列的 `minWidth`，合计正好 976）。
/// 挪去详情页的四项，以及为什么它们不是信息损失：
///   - **最近发生**：等于「首次发生 + 持续时间」，同一行里已经有这两列。
///   - **维护窗口 ID**：一串 UUID 前缀，列表里读不出意思；「维护中」这个可扫视的标记留下。
///   - **原因摘要 / 建议动作**：40px 的行里它们只能得到 4 个汉字加省略号。摘要本身是由
///     事件类型、触发指标、触发值与阈值生成的一句话，而这四列都在；完整文本在详情页，
///     每一行都有「详情」链接。
/// 这是一次产品取舍，写进结题报告；不做的选择是「留着列但每格只显示半个词」。
///
/// 一格一个事实：40px 的行放不下两行，所以迁移前挤在「状态 / 级别」一格里的三件事
/// （告警状态、严重度、维护归因）在这里是三列。
///
/// 列只给 `minWidth`：它既是 1280px 上分配比例的依据，也是窄于 1280px 时的列宽下限。
/// 这里的每个值都是「该列取值的自然宽度 + 32px 内边距」，所以不需要再用 `grow` 拨回来。
function eventColumns(search: PerformanceEventSearch): DataGridColumn<PerformanceEvent>[] {
  return [
    {
      key: 'status',
      header: '状态',
      minWidth: 84, // 「告警中」徽标 52
      cell: (event) => <AlertStatus status={event.alert_status} />,
    },
    {
      key: 'severity',
      header: '级别',
      minWidth: 76, // 「严重」徽标 40
      cell: (event) => <PerformanceEventSeverityTag severity={event.severity} />,
    },
    {
      key: 'maintenance',
      header: '维护',
      minWidth: 84, // 「维护中」徽标 52
      cell: (event) => (event.in_maintenance ? <PerformanceEventMaintenanceTag inMaintenance /> : '—'),
    },
    {
      key: 'type',
      header: '事件类型',
      minWidth: 128, // 「临时文件突增」84
      cell: (event) => <TruncatedText>{performanceEventTypeLabel(event.event_type)}</TruncatedText>,
    },
    {
      key: 'metric',
      header: '触发指标',
      minWidth: 124, // `pg.lock.waiting_count` 是最长的一档，会带省略号 + 悬停全文
      cell: (event) => <TruncatedText className="dbs-numeric">{event.metric_id}</TruncatedText>,
    },
    {
      key: 'value',
      header: '触发值 / 阈值',
      minWidth: 110,
      numeric: true,
      cell: (event) => `${event.trigger_value} / ${event.threshold}`,
    },
    {
      key: 'derived',
      header: '首次发生',
      minWidth: 140, // 等宽的「08/11 10:15」101（英文区域设置多一个逗号）
      cell: (event) => eventTimeCell(event.derived_at),
    },
    {
      key: 'duration',
      header: '持续时间',
      minWidth: 88,
      numeric: true,
      cell: (event) => performanceEventDurationLabel(event.duration_ms),
    },
    {
      key: 'disposition',
      header: '处置',
      minWidth: 78,
      cell: (event) => performanceEventDispositionLabel(event.disposition),
    },
    {
      key: 'detail',
      header: '操作',
      minWidth: 64, // 「详情」28
      cell: (event) => <Link
        className="cds--link"
        to="/instances/$id/performance-events/$eventId"
        params={{ id: event.instance_id, eventId: event.id }}
        search={serializePerformanceEventSearch(search)}
      >详情</Link>,
    },
  ]
}

/// 表格里的时刻。一整个 `toLocaleString()` 要 125px；格子里只写「月-日 时:分」，
/// 完整时刻（含年与秒）进 `title` —— 和长文本截断是同一个约定：显示收窄，信息不丢。
/// 详情页仍然写完整时刻。`hour12: false` 是为了不让 AM/PM 再多吃 24px。
function eventTimeCell(value: string) {
  const compact = new Date(value).toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  return <TruncatedText className="dbs-numeric" title={performanceEventTimeLabel(value)}>{compact}</TruncatedText>
}

function eventEmptyText(search: PerformanceEventSearch): string {
  switch (search.tab) {
    case 'firing': return '所选时间范围内没有触发中的性能事件'
    case 'recovered': return '所选时间范围内没有已恢复的性能事件'
    case 'disposed':
      if (search.disposition === 'ACKED') return '所选时间范围内没有已确认的性能事件'
      return '所选时间范围内没有已忽略的性能事件'
    default: return assertNever(search.tab)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected performance event value: ${value}`)
}
