import { Link } from '@carbon/react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { unavailabilityCopy, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { queryStatisticsView } from './queryStatistics'
import { CopyableValue, fullTimeLabel } from './sessionCells'
import { SessionDensitySwitcher, queryStatisticsPageHref, type SessionTabPanelProps } from './sessionLayout'
import type { SessionSearch } from './sessionSearch'

type QueryStatisticsEntry = components['schemas']['QueryStatisticsEntry']

const pollingOptions = { refetchInterval: pollingIntervals.sessions }

/// 「查询统计排行」标签。
///
/// 合并之前这是 `/instances/$id/sessions/query-statistics` 一个独立页面；现在它只是
/// 会话页的一个标签。时效性提示、十三个不可用原因码各自的说明与去处、统计时刻与数据新鲜度、
/// 排行表全部原样保留 —— 判定逻辑仍然在纯模块 `queryStatistics.ts` 里，一个字没动。
export function QueryStatisticsPanel({ id, search, density, onDensityChange }: SessionTabPanelProps) {
  const statistics = $api.useQuery('get', '/api/v1/instances/{id}/query-stats', { params: { path: { id } } }, pollingOptions)

  return <>
    {/* queryid 的时效性说明。它解释的是「为什么这个数字明天可能对不上」，不是一句装饰。 */}
    <NotificationBar tone="info" title="标识具有时效性">
      queryid 可能因统计重置、条目淘汰或 PostgreSQL 版本变化而失效，仅作为数据库侧排查线索。
    </NotificationBar>

    {statistics.isError && <NotificationBar tone="critical" title={apiErrorMessage(statistics.error, '无法加载查询统计')} />}

    {statistics.isPending
      ? <Panel flush title="查询统计排行" loading />
      : statistics.data !== undefined && <QueryStatisticsContent
        id={id}
        search={search}
        response={statistics.data}
        dataUpdatedAt={statistics.dataUpdatedAt}
        density={density}
        onDensityChange={onDensityChange}
      />}
  </>
}

function QueryStatisticsContent({ id, search, response, dataUpdatedAt, density, onDensityChange }: {
  id: string
  search: SessionSearch
  response: components['schemas']['QueryStatisticsSnapshot']
  dataUpdatedAt: number
  density: SessionTabPanelProps['density']
  onDensityChange: SessionTabPanelProps['onDensityChange']
}) {
  const view = queryStatisticsView(response)

  if (view.kind === 'unavailable') {
    // 「为什么没有排行」加一个去处，而不是一张空表 —— 缺数不是 0，也不是零行。
    // 去处是真链接（`<a href>`），中键新开与复制链接都得留着；组件库的通知条正文不收
    // 可交互内容，所以链接另起一行放在通知外面（做法与 domain/UnavailabilityBlock 一致）。
    const href = unavailabilityHref(view.code, {
      current: queryStatisticsPageHref(id, search),
      collection: `/instances/${encodeURIComponent(id)}/collection`,
    })
    return <div className="sessions-unavailable">
      <NotificationBar tone="warning" title={view.title}>{view.description}</NotificationBar>
      <Link href={href} renderIcon={Icon.glyph.arrowRight}>{unavailabilityCopy(view.code).action}</Link>
    </div>
  }

  return <>
    <div className="sessions-toolbar">
      <div className="sessions-meta">
        <span className="dbs-body">统计时间截至：{fullTimeLabel(view.sampledAt)}</span>
        {dataUpdatedAt > 0 && <Freshness dataUpdatedAt={dataUpdatedAt} collectionInterval={pollingIntervals.sessions} />}
      </div>
    </div>

    <Panel
      flush
      title={`查询统计排行（${view.items.length}）`}
      actions={<SessionDensitySwitcher density={density} onChange={onDensityChange} />}
    >
      <DataGrid<QueryStatisticsEntry>
        label="查询统计排行"
        density={density}
        rows={view.items}
        rowKey={(item) => `${item.queryid}-${item.database_oid}-${item.user_oid}`}
        rowTestId="query-statistics-row"
        columns={queryStatisticsColumns}
        empty={{ title: '最近一次查询统计快照没有可排行的记录' }}
      />
    </Panel>
  </>
}

/// 五列合计 770px，装得进 1280px 下实测的 976px 可用宽度。
/// queryid 是 int64 的十进制串（最长 20 位），等宽显示、装不下就省略号 + 悬停看全文 ——
/// **等宽标识符从中间折行比截断更难读**，所以这一列永远不换行。
const queryStatisticsColumns: DataGridColumn<QueryStatisticsEntry>[] = [
  {
    key: 'queryid',
    header: 'queryid',
    minWidth: 220,
    cell: (item) => <CopyableValue className="dbs-numeric" value={item.queryid} label="queryid" />,
  },
  {
    key: 'database-oid',
    header: '数据库 OID',
    minWidth: 140,
    cell: (item) => <CopyableValue className="dbs-numeric" value={String(item.database_oid)} label="数据库 OID" />,
  },
  {
    key: 'user-oid',
    header: '数据库用户 OID',
    minWidth: 170,
    cell: (item) => <CopyableValue className="dbs-numeric" value={String(item.user_oid)} label="数据库用户 OID" />,
  },
  { key: 'calls', header: '调用次数', minWidth: 110, numeric: true, cell: (item) => String(item.calls) },
  {
    key: 'total-exec',
    header: '总执行时间',
    minWidth: 140,
    numeric: true,
    cell: (item) => `${item.total_exec_time_ms.toFixed(1)} ms`,
  },
]
