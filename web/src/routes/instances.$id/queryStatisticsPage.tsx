import { Link as CarbonLink } from '@carbon/react'
import { Link } from '@tanstack/react-router'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { SqlStatement } from '../../domain/SqlStatement'
import { statementSummary, statementText } from '../../domain/topSql'
import { unavailabilityCopy, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { Icon } from '../../primitives/Icon'
import { KeyValueList } from '../../primitives/KeyValueList'
import { Modal } from '../../primitives/Modal'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { TruncatedText } from '../../primitives/TruncatedText'
import { queryStatisticsView } from './queryStatistics'
import { CopyableValue, fullTimeLabel } from './sessionCells'
import { SessionDensitySwitcher, queryStatisticsPageHref, type SessionTabPanelProps } from './sessionLayout'
import type { SessionSearch } from './sessionSearch'
import { withSessionTab } from './sessionTabs'

type QueryStatisticsEntry = components['schemas']['QueryStatisticsEntry']

const pollingOptions = { refetchInterval: pollingIntervals.sessions }

/// 「查询统计排行」标签。
///
/// 合并之前这是 `/instances/$id/sessions/query-statistics` 一个独立页面；现在它只是
/// 会话页的一个标签。时效性提示、十三个不可用原因码各自的说明与去处、统计时刻与数据新鲜度、
/// 排行表全部原样保留 —— 判定逻辑仍然在纯模块 `queryStatistics.ts` 里，一个字没动。
export function QueryStatisticsPanel({ id, search, density, onDensityChange, onSearchChange }: SessionTabPanelProps) {
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
        onSearchChange={onSearchChange}
      />}
  </>
}

function QueryStatisticsContent({ id, search, response, dataUpdatedAt, density, onDensityChange, onSearchChange }: {
  id: string
  search: SessionSearch
  response: components['schemas']['QueryStatisticsSnapshot']
  dataUpdatedAt: number
  density: SessionTabPanelProps['density']
  onDensityChange: SessionTabPanelProps['onDensityChange']
  onSearchChange: SessionTabPanelProps['onSearchChange']
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
      <CarbonLink href={href} renderIcon={Icon.glyph.arrowRight}>{unavailabilityCopy(view.code).action}</CarbonLink>
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
        columns={queryStatisticsColumns(id, search)}
        empty={{ title: '最近一次查询统计快照没有可排行的记录' }}
      />
    </Panel>

    <QueryStatisticsDetails
      entry={view.items.find((item) => item.queryid === search.queryid)}
      onClose={() => onSearchChange({ ...search, queryid: undefined })}
    />
  </>
}

/// 一条语句的详情。
///
/// 打开与否由地址说了算（`queryid` 参数），所以它没有自己的开关状态。同一个 queryid 在
/// 多个库 / 用户下会有好几行，详情只认 queryid —— 语句文本按 (实例, queryid) 存一份，
/// 那几行看到的本来就是同一段文本。落在哪个库上是列表那一行的事，不是文本的事。
function QueryStatisticsDetails({ entry, onClose }: {
  entry: QueryStatisticsEntry | undefined
  onClose: () => void
}) {
  return <Modal
    passiveModal
    size="lg"
    open={entry !== undefined}
    modalHeading="SQL 详情"
    closeButtonLabel="关闭 SQL 详情"
    onRequestClose={onClose}
  >
    {entry !== undefined && <div className="sessions-sql-details">
      {/* 列表里合成一格的两个 OID 在这里各自成对、各自可复制 ——
          列宽契约第 5 条把它们并成一列，信息不丢，只是搬了个地方。 */}
      <KeyValueList
        label="这条语句的统计"
        columns={2}
        items={[
          { key: 'queryid', label: 'queryid', value: <CopyableValue className="dbs-numeric" value={entry.queryid} label="queryid" /> },
          { key: 'database-oid', label: '数据库 OID', value: <CopyableValue className="dbs-numeric" value={String(entry.database_oid)} label="数据库 OID" /> },
          { key: 'user-oid', label: '数据库用户 OID', value: <CopyableValue className="dbs-numeric" value={String(entry.user_oid)} label="数据库用户 OID" /> },
          { key: 'calls', label: '调用次数', value: <span className="dbs-numeric">{entry.calls}</span> },
          { key: 'total-exec', label: '总执行时间', value: <span className="dbs-numeric">{entry.total_exec_time_ms.toFixed(1)} ms</span> },
        ]}
      />
      <SqlStatement sql={statementText(entry)} label="SQL 全文" />
    </div>}
  </Modal>
}

/// 五列合计 940px，装得进 1280px 下实测的 976px 可用宽度。
///
/// SQL 摘要是这张表的第一列，也是唯一说得出「这一行是什么」的一列：从前这里只有 queryid，
/// 而 queryid 回答不了任何问题——读者拿到它还得再去别处换成语句。摘要同时是详情入口，
/// 用**真链接**而不是按钮：详情在地址里，中键新开与复制链接都还在。
///
/// 两个 OID 并成一列，是列宽契约第 5 条（「两列合成一句话」）而不是丢列：加上 SQL 摘要
/// 之后六列合计 1020px 超预算，而这两列是同一件事的两半（哪个库、哪个用户），并排读也
/// 从不单独出现。各自的完整取值与复制按钮在详情里。
function queryStatisticsColumns(id: string, search: SessionSearch): DataGridColumn<QueryStatisticsEntry>[] {
  return [
    {
      key: 'statement',
      header: 'SQL 摘要',
      minWidth: 300,
      cell: (item) => (
        <Link
          className="cds--link"
          to="/instances/$id/sessions"
          params={{ id }}
          search={withSessionTab({ ...search, queryid: item.queryid }, 'query-statistics')}
        >
          <TruncatedText className="sessions-statement">{statementSummary(item)}</TruncatedText>
        </Link>
      ),
    },
    {
      key: 'queryid',
      header: 'queryid',
      minWidth: 220,
      cell: (item) => <CopyableValue className="dbs-numeric" value={item.queryid} label="queryid" />,
    },
    {
      key: 'oids',
      header: '数据库 / 用户 OID',
      minWidth: 170,
      cell: (item) => <TruncatedText className="dbs-numeric">{`${item.database_oid} / ${item.user_oid}`}</TruncatedText>,
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
}
