import { Link, createRoute } from '@tanstack/react-router'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import { Freshness } from '../../domain/Freshness'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import type { TopSqlEntry } from './topSql'
import { elapsedLabel, queryStatisticsDrilldown, statementLabel, topSqlRowKey } from './topSql'
import './sqlInsight.css'

/// SQL 洞察：跨实例的 Top SQL，按总耗时排序。
///
/// 从前慢 SQL 埋在实例工作台的第三层页签里，而 DBA 的动线是「哪条 SQL 在拖垮我的机群」——
/// 那个问题没法一台一台点着回答。这一页把它翻过来：先看语句，再下钻到它所在的实例。
///
/// **显示的是 SQL 文本而不是 queryid。** 文本来自 pg_stat_statements 的归一化形式
/// （字面量已经是 $1 占位符），按 (实例, queryid) 去重存一份。带真实字面量的
/// pg_stat_activity 原文既不采也不存，所以这一页在结构上不可能显示出身份证号或口令。
///
/// 不分页：榜单只有一百行，翻页在「按总耗时排的前一百条」上没有意义 ——
/// 第二页的语句已经不值得先看了。
export const sqlInsightRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sql-insight',
  component: SqlInsightPage,
})

const topSqlLimit = 100

function SqlInsightPage() {
  const topSqlQuery = $api.useQuery(
    'get',
    '/api/v1/top-sql',
    { params: { query: { limit: topSqlLimit } } },
    { refetchInterval: pollingIntervals.sessions },
  )
  const items = topSqlQuery.data?.items ?? []

  return (
    <div className="sql-insight-page">
      <header className="sql-insight-page__header">
        <h1 className="dbs-page-title">SQL 洞察</h1>
        {topSqlQuery.dataUpdatedAt > 0 && (
          <Freshness dataUpdatedAt={topSqlQuery.dataUpdatedAt} collectionInterval={pollingIntervals.sessions} />
        )}
      </header>

      {topSqlQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(topSqlQuery.error, 'Top SQL 加载失败')}>
          排行没有取到。这里的空白不是「没有慢 SQL」，而是还不知道。
        </NotificationBar>
      )}

      {/* 归一化文本的解释。它说明这一页为什么可以放心显示语句，也说明为什么显示的
          是 $1 而不是真实取值 —— 后者是刻意不落库的。 */}
      <NotificationBar tone="info" title="显示的是归一化后的语句">
        字面量已被 pg_stat_statements 换成 $1 占位符。带真实取值的原始语句不会被平台存储，
        因此这里也永远看不到它。统计按每台实例最近一次采集的排行合并。
      </NotificationBar>

      <Panel flush title={`Top SQL（${items.length}）`}>
        <DataGrid<TopSqlEntry>
          label="跨实例 Top SQL"
          rows={items}
          rowKey={topSqlRowKey}
          rowTestId="top-sql-row"
          columns={topSqlColumns}
          loading={topSqlQuery.isPending}
          skeletonRows={8}
          empty={{
            title: '还没有可排行的 SQL',
            description: '查询统计来自 pg_stat_statements，装了这个扩展并采集过一轮之后才有。',
          }}
        />
      </Panel>
    </div>
  )
}

/// 四列合计 770px，装得进 1280px 下的 974px 可用宽度。
///
/// SQL 文本给到 340px 是这张表的重点：它是「这一行是什么」的唯一说明，压到比它更窄
/// 就只剩几个关键字。实例名同时是下钻入口（去那台实例的查询统计），
/// 所以这张表不需要第五列「操作」——每一行的去处就挂在它已经要显示的那个事实上。
const topSqlColumns: DataGridColumn<TopSqlEntry>[] = [
  {
    key: 'statement',
    header: 'SQL 文本',
    minWidth: 340,
    cell: (entry) => <TruncatedText className="sql-insight-statement">{statementLabel(entry)}</TruncatedText>,
  },
  {
    key: 'instance',
    header: '所属实例',
    minWidth: 180,
    cell: (entry) => (
      <Link
        className="cds--link"
        to="/instances/$id/sessions"
        params={{ id: entry.instance_id }}
        search={queryStatisticsDrilldown()}
      >
        <TruncatedText>{entry.instance_name}</TruncatedText>
      </Link>
    ),
  },
  { key: 'calls', header: '调用次数', minWidth: 110, numeric: true, cell: (entry) => String(entry.calls) },
  {
    key: 'total-exec',
    header: '总耗时',
    minWidth: 140,
    numeric: true,
    cell: (entry) => elapsedLabel(entry.total_exec_time_ms),
  },
]
