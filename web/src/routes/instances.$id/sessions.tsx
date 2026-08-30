import { Button, ContentSwitcher, Switch } from '@carbon/react'
import { Link, createRoute, redirect } from '@tanstack/react-router'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import { Freshness } from '../../domain/Freshness'
import { UnavailabilityBlock, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { readTableDensity, writeTableDensity } from '../root/tableDensity'
import { LongQuerySamplesPanel } from './longQuerySamples'
import { QueryStatisticsPanel } from './queryStatisticsPage'
import {
  CopyableValue,
  blockingPidsLabel,
  durationLabel,
  fullTimeLabel,
  optionalCell,
  clockCell,
  optionalCopyableCell,
  waitEventLabel,
} from './sessionCells'
import {
  SessionDensitySwitcher,
  SessionTabStrip,
  sessionPageHref,
  type SessionTabPanelProps,
} from './sessionLayout'
import { parseSessionSearch, type SessionFilter, type SessionSearch } from './sessionSearch'
import { parseSessionPageSearch, withSessionTab, type SessionTab } from './sessionTabs'
import { groupSessionSnapshot, type SessionSnapshotEntry } from './sessionViews'
import { defaultTimeRange } from './timeRange'
import { WorkbenchHeader } from './workbench'
import './sessions.css'

/// 会话与阻塞：一个多标签页面。
///
/// **这是一次路由合并（票 #200）。** 会话快照 / 长查询采样记录 / 查询统计排行原本是三个地址、
/// 三个页面，各自顶着同一条「其实是三组链接」的页签条，也各自重复取一遍实例名。现在收拢成
/// `/instances/$id/sessions` 一个地址，「在哪个标签」是 search param `tab`
/// （web/CLAUDE.md：URL 状态 → search params）。
///
/// 两个旧子地址一个都没删，全部改成重定向（见本文件末尾）：它们出现在监控页的长查询下钻、
/// 端到端用例与用户的书签里，合并不该让任何一条既有链接失效。入口清单与重定向映射记在票 #200。
export const sessionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions',
  validateSearch: parseSessionPageSearch,
  component: SessionsPage,
})

function SessionsPage() {
  const { id } = sessionsRoute.useParams()
  const search = sessionsRoute.useSearch()
  // 解析永远填得出一个标签；`?? 'current'` 只是把「可省略」这件事在类型上收口。
  const tab = search.tab ?? 'current'

  if ('error' in search) {
    // 地址坏了也要落在正确的标签上：合并之前每个页面各自解释自己的错，之后也一样。
    return <div className="sessions-page">
      <NotificationBar tone="critical" title={search.error} />
      <div>
        <Link
          to="/instances/$id/sessions"
          params={{ id }}
          search={withSessionTab(defaultTimeRange(), tab)}
        ><Button size="md">使用最近一小时</Button></Link>
      </div>
    </div>
  }

  return <SessionWorkbench id={id} search={search} tab={tab} />
}

/// 页面版式照列表页样板（web/CLAUDE.md 先例）：工作台页头 → 页内标题 → 二级页签 →
/// 工具条 → 一个 flush 面板包住表格。三个标签各取各的数据，**只渲染选中的那个** ——
/// 三份轮询同时跑起来是合并唯一会带来的新成本，不接。
function SessionWorkbench({ id, search, tab }: { id: string; search: SessionSearch; tab: SessionTab }) {
  const navigate = sessionsRoute.useNavigate()
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  // 调查上下文改了就换地址（停在同一个标签上），而不是只改本地变量 ——
  // 时间范围一直是这一页可分享、可收藏的一部分。
  function changeSearch(next: SessionSearch) {
    void navigate({ search: withSessionTab(next, tab) })
  }

  return <div className="sessions-page">
    <WorkbenchHeader
      id={id}
      instanceName={instance.data?.name}
      activeKey="sessions"
      search={search.metric === undefined
        ? { from: search.from, to: search.to }
        : { from: search.from, to: search.to, metric: search.metric }}
    />

    <div className="sessions-page__heading">
      <h2 className="dbs-panel-title">会话与阻塞</h2>
      <p className="dbs-caption">当前会话快照、长查询采样记录与查询统计排行收拢在同一页</p>
    </div>

    <SessionTabStrip id={id} search={search} active={tab} />

    <SessionTabContent
      id={id}
      search={search}
      tab={tab}
      density={density}
      onDensityChange={changeDensity}
      onSearchChange={changeSearch}
    />
  </div>
}

function SessionTabContent({ tab, ...props }: SessionTabPanelProps & { tab: SessionTab }) {
  switch (tab) {
    case 'current':
      return <SessionSnapshotTab {...props} />
    case 'long-query-samples':
      return <LongQuerySamplesPanel {...props} />
    case 'query-statistics':
      return <QueryStatisticsPanel {...props} />
    default:
      return assertNever(tab)
  }
}

// ---------------------------------------------------------------------------
// 标签一：当前会话快照
// ---------------------------------------------------------------------------

const pollingOptions = { refetchInterval: pollingIntervals.sessions }

function SessionSnapshotTab({ id, search, density, onDensityChange }: SessionTabPanelProps) {
  const snapshot = $api.useQuery('get', '/api/v1/instances/{id}/sessions', { params: { path: { id } } }, pollingOptions)
  const [activeView, setActiveView] = useState<SessionView>(initialView(search.filter))

  if (snapshot.isError) {
    return <NotificationBar tone="critical" title={apiErrorMessage(snapshot.error, '无法加载会话快照')} />
  }

  if (snapshot.data?.unavailability !== undefined) {
    return <UnavailabilityBlock
      code={snapshot.data.unavailability}
      href={unavailabilityHref(snapshot.data.unavailability, {
        current: sessionPageHref(id, search),
        collection: `/instances/${encodeURIComponent(id)}/collection`,
      })}
    />
  }

  const items = snapshot.data?.items ?? []
  const groups = groupSessionSnapshot(items)
  const rows = groups[viewGroup(activeView)]
  const details = activeView === 'details'

  return <>
    <div className="sessions-toolbar">
      <SessionSnapshotMeta
        sampledAt={snapshot.data?.sampled_at}
        dataUpdatedAt={snapshot.dataUpdatedAt}
        originalCount={snapshot.data?.original_count}
        itemCount={items.length}
      />
    </div>

    {/* 服务端 500 行上限的截断提示。它说的是「你看到的不是全部」，
        丢了它就等于让人拿着一份残缺的阻塞链下结论。 */}
    {snapshot.data?.truncated === true && <NotificationBar tone="warning" title="快照已截断">
      本次响应达到 500 行服务端上限，阻塞链与会话列表可能不完整。
    </NotificationBar>}

    <Panel
      flush
      // 面板标题不再重复视图名与计数：下面的切换器已经把五个切面的名字和条数都写着了。
      title="会话快照"
      actions={<SessionDensitySwitcher density={density} onChange={onDensityChange} />}
    >
      <SessionViewSwitcher
        activeView={activeView}
        onChange={setActiveView}
        counts={{
          active: groups.active.length,
          'long-transactions': groups.longTransactions.length,
          'lock-waits': groups.lockWaits.length,
          'blocking-chains': groups.blockingChains.length,
          details: groups.details.length,
        }}
      />
      <DataGrid<SessionSnapshotEntry>
        label={`${sessionViewLabel(activeView)}列表`}
        density={density}
        // 二级视图切换器就在表格上方，粘性表头要贴在它下面而不是钻到它后面去。
        stickyTop="calc(var(--dbs-header-height) + 3rem)"
        loading={snapshot.isPending}
        skeletonRows={8}
        rows={rows}
        rowKey={(item) => String(item.pid)}
        rowTestId="session-row"
        // 十一列的会话详情视图开紧凑内边距：标准档下光左右内边距就吃掉 976px 里的 352px，
        // 每格只剩五十来像素，取值全变成两三个字加省略号。七列的另外四个视图不开，度量不变。
        cellPadding={details ? 'compact' : 'standard'}
        columns={details ? sessionDetailColumns : sessionColumns}
        empty={{ title: sessionViewEmpty(activeView), description: '快照每 10 秒刷新一次，也可以切到别的视图看看其他会话。' }}
      />
    </Panel>
  </>
}

/// 快照的元信息条：采集时刻、数据新鲜度、原始会话数。
///
/// **采集时间与数据新鲜度是两件事**：前者是数据库侧取样的时刻（响应里的 `sampled_at`），
/// 后者是本地这份响应有多旧（`dataUpdatedAt`）。轮询停了、请求失败了，`sampled_at` 还会
/// 停在一个很像「刚刚」的时刻上，只有 `dataUpdatedAt` 说得出实话，所以两者都写。
///
/// 「原始会话数」是服务端截断前的行数，和表格里的行数不是一回事 —— 它和「快照已截断」
/// 那条提示一起，回答「我看到的是不是全部」。
export function SessionSnapshotMeta({ sampledAt, dataUpdatedAt, originalCount, itemCount }: {
  sampledAt: string | undefined
  dataUpdatedAt: number
  originalCount: number | undefined
  itemCount: number
}) {
  return <div className="sessions-meta">
    <span className="dbs-body">采集时间：{fullTimeLabel(sampledAt)}</span>
    {dataUpdatedAt > 0 && <Freshness dataUpdatedAt={dataUpdatedAt} collectionInterval={pollingIntervals.sessions} />}
    <span className="dbs-caption sessions-meta__secondary">原始会话数：{originalCount ?? itemCount}</span>
  </div>
}

type SessionView = 'active' | 'long-transactions' | 'lock-waits' | 'blocking-chains' | 'details'

const sessionViews = [
  'active',
  'long-transactions',
  'lock-waits',
  'blocking-chains',
  'details',
] as const satisfies readonly SessionView[]

/// 同一份快照的五个切面。**这不是导航**：五个切面读的是同一次响应、同一个地址，
/// 切换它不该在浏览历史里留下一条记录，所以它是分段单选（Carbon `ContentSwitcher`，
/// 组件库给每一档 `role="tab"` + `aria-selected`）而不是第三层链接页签。
///
/// 进来时停在哪个切面由地址里的 `filter` 决定 —— 实例总览的「锁等待」下钻就是这么落地的。
function SessionViewSwitcher({ activeView, onChange, counts }: {
  activeView: SessionView
  onChange: (view: SessionView) => void
  counts: Record<SessionView, number>
}) {
  return <div className="sessions-views">
    <ContentSwitcher
      size="md"
      selectedIndex={sessionViews.indexOf(activeView)}
      onChange={({ index }) => {
        const next = index === undefined ? undefined : sessionViews[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {sessionViews.map((view) => (
        <Switch key={view} name={view} text={`${sessionViewLabel(view)} ${counts[view]}`} />
      ))}
    </ContentSwitcher>
  </div>
}

function sessionViewLabel(view: SessionView): string {
  switch (view) {
    case 'active': return '活跃会话'
    case 'long-transactions': return '长事务'
    case 'lock-waits': return '锁等待'
    case 'blocking-chains': return '阻塞链'
    case 'details': return '会话详情'
    default: return assertNever(view)
  }
}

function sessionViewEmpty(view: SessionView): string {
  switch (view) {
    case 'active': return '当前快照无活跃会话'
    case 'long-transactions': return '当前快照无长事务'
    case 'lock-waits': return '当前快照无锁等待'
    case 'blocking-chains': return '当前快照无阻塞链'
    case 'details': return '当前快照无会话'
    default: return assertNever(view)
  }
}

function viewGroup(view: SessionView): keyof ReturnType<typeof groupSessionSnapshot> {
  switch (view) {
    case 'active': return 'active'
    case 'long-transactions': return 'longTransactions'
    case 'lock-waits': return 'lockWaits'
    case 'blocking-chains': return 'blockingChains'
    case 'details': return 'details'
    default: return assertNever(view)
  }
}

/// 进入时停在哪个切面：地址里的 `filter` 是实例总览下钻带过来的。
function initialView(filter: SessionFilter | undefined): SessionView {
  switch (filter) {
    case 'active': return 'active'
    case 'long_transaction': return 'long-transactions'
    case 'lock_wait': return 'lock-waits'
    case 'blocked': return 'blocking-chains'
    case undefined: return 'active'
    default: return assertNever(filter)
  }
}

/// 列宽只给 `minWidth`：它既是 1280px 上分配比例的依据，也是窄于 1280px 时的下限。
/// 七列合计 876px，装得进 1280px 下实测的 976px 可用宽度，所以标准内边距不用动。
/// 一格一个事实：等待事件的「类型 / 事件」本来就是一件事，其余各自成列。
const sessionColumns: DataGridColumn<SessionSnapshotEntry>[] = [
  {
    key: 'pid',
    header: 'PID',
    minWidth: 96,
    numeric: true,
    cell: (item) => <CopyableValue className="dbs-numeric" value={String(item.pid)} label="PID" />,
  },
  { key: 'database', header: '数据库', minWidth: 130, cell: (item) => optionalCopyableCell(item.database_name, '数据库名') },
  { key: 'username', header: '数据库用户', minWidth: 130, cell: (item) => optionalCopyableCell(item.username, '数据库用户') },
  { key: 'state', header: '状态', minWidth: 96, cell: (item) => optionalCell(item.state) },
  {
    key: 'transaction-duration',
    header: '事务持续时间',
    minWidth: 124,
    numeric: true,
    cell: (item) => durationLabel(item.transaction_duration_ms),
  },
  {
    key: 'wait-event',
    header: '等待事件',
    minWidth: 170,
    cell: (item) => optionalCell(waitEventLabel(item.wait_event_type, item.wait_event)),
  },
  {
    key: 'blocking',
    header: '阻塞源 PID',
    minWidth: 130,
    cell: (item) => optionalCell(blockingPidsLabel(item.blocking_pids)),
  },
]

/// 会话详情视图：十一列。
///
/// 1280px 下页面可用宽度实测 976px，紧凑内边距每列吃 16px，留给字形的一共 800px。
/// 十一列各自的自然宽度加起来远超这个数，所以这里做了两件真实的取舍，而不是让每一格
/// 都变成两三个字加省略号：
///   1. **表头收短**（事务持续时间 → 事务时长，查询开始时间 → 查询开始）。表头是常量，
///      被截成「事务持续…」的信息量比收短更低。
///   2. **时刻只写时:分:秒**，年月日进悬停提示 —— 当前快照里的会话都是「此刻在跑」的，
///      日期每行都一样。
/// 剩下的压缩全部落在数据库名与用户名两列上：它们是自由文本，本来就靠省略号 + 悬停全文。
const sessionDetailColumns: DataGridColumn<SessionSnapshotEntry>[] = [
  {
    key: 'pid',
    header: 'PID',
    minWidth: 72,
    numeric: true,
    cell: (item) => <CopyableValue className="dbs-numeric" value={String(item.pid)} label="PID" />,
  },
  { key: 'database', header: '数据库', minWidth: 84, cell: (item) => optionalCopyableCell(item.database_name, '数据库名') },
  { key: 'username', header: '用户', minWidth: 84, cell: (item) => optionalCopyableCell(item.username, '数据库用户') },
  { key: 'state', header: '状态', minWidth: 72, cell: (item) => optionalCell(item.state) },
  {
    key: 'transaction-duration',
    header: '事务时长',
    minWidth: 72,
    numeric: true,
    cell: (item) => durationLabel(item.transaction_duration_ms),
  },
  {
    key: 'wait-event',
    header: '等待事件',
    minWidth: 110,
    cell: (item) => optionalCell(waitEventLabel(item.wait_event_type, item.wait_event)),
  },
  {
    key: 'blocking',
    header: '阻塞源 PID',
    minWidth: 92,
    cell: (item) => optionalCell(blockingPidsLabel(item.blocking_pids)),
  },
  {
    key: 'client',
    header: '客户端地址',
    minWidth: 92,
    cell: (item) => optionalCopyableCell(item.client_address, '客户端地址', 'dbs-numeric'),
  },
  { key: 'query-started', header: '查询开始', minWidth: 88, cell: (item) => clockCell(item.query_started_at) },
  { key: 'transaction-started', header: '事务开始', minWidth: 88, cell: (item) => clockCell(item.transaction_started_at) },
  {
    key: 'query-duration',
    header: '查询时长',
    minWidth: 72,
    numeric: true,
    cell: (item) => durationLabel(item.query_duration_ms),
  },
]

function assertNever(value: never): never {
  throw new Error(`unhandled session view: ${String(value)}`)
}

// ---------------------------------------------------------------------------
// 旧地址 → 新标签
//
// 合并前的两个子地址保留为重定向路由。`/instances/$id/sessions` 本身没有变，
// 所以实例总览的锁等待下钻、`overview.ts` 拼出来的地址与用户书签全部照旧可用。
// 映射记在票 #200 上：
//
//   /instances/$id/sessions/long-query-samples?<ctx>  →  /instances/$id/sessions?<ctx>&tab=long-query-samples
//   /instances/$id/sessions/query-statistics?<ctx>    →  /instances/$id/sessions?<ctx>&tab=query-statistics
//
// 调查上下文（from / to / metric / sampled_at / filter）原样带过去 —— 重定向丢了参数，
// 等于把人从「他正在查的那一刻」踢回默认时间范围。坏参数也原样转交，由合并页照旧解释。
// ---------------------------------------------------------------------------

export const longQuerySamplesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions/long-query-samples',
  validateSearch: parseSessionSearch,
  beforeLoad: ({ params, search }) => {
    throw redirect({
      to: '/instances/$id/sessions',
      params: { id: params.id },
      search: withSessionTab(search, 'long-query-samples'),
      replace: true,
    })
  },
})

export const queryStatisticsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/sessions/query-statistics',
  validateSearch: parseSessionSearch,
  beforeLoad: ({ params, search }) => {
    throw redirect({
      to: '/instances/$id/sessions',
      params: { id: params.id },
      search: withSessionTab(search, 'query-statistics'),
      replace: true,
    })
  },
})
