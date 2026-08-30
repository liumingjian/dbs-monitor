import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { TimeRangePicker } from '../../domain/TimeRangePicker'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Pagination } from '../../primitives/Pagination'
import { Panel } from '../../primitives/Panel'
import {
  CopyableValue,
  blockingPidsLabel,
  fullTimeLabel,
  optionalCell,
  optionalCopyableCell,
  timeCell,
} from './sessionCells'
import { SessionDensitySwitcher, type SessionTabPanelProps } from './sessionLayout'

type LongQuerySample = components['schemas']['LongQuerySample']

const pageSize = 50

/// 「长查询采样记录」标签。
///
/// 合并之前这是 `/instances/$id/sessions/long-query-samples` 一个独立页面；现在它只是
/// 会话页的一个标签，路由与页头都归 `sessions.tsx`，这里只剩这一个标签自己的内容。
/// 时间范围、下钻采样时刻、时效性提示、分页四件功能点原样保留。
export function LongQuerySamplesPanel({ id, search, density, onDensityChange, onSearchChange }: SessionTabPanelProps) {
  // 页码是这一屏的局部状态，不进地址 —— 迁移前也是如此（换时间范围会回到第一页）。
  const [page, setPage] = useState(1)
  const samples = $api.useQuery('get', '/api/v1/instances/{id}/long-query-samples', {
    params: {
      path: { id },
      query: { from: search.from, to: search.to, limit: pageSize, offset: (page - 1) * pageSize, sort: '-sampled_at' },
    },
  })

  const total = samples.data?.total

  return <>
    <div className="sessions-toolbar" role="group" aria-label="长查询采样筛选">
      <TimeRangePicker
        from={search.from}
        to={search.to}
        onChange={(range) => {
          // 换了时间范围还停在第 3 页是没有意义的：结果集整个换了一批。
          setPage(1)
          onSearchChange({ ...search, ...range })
        }}
      />
      {search.sampled_at !== undefined && <span className="dbs-caption sessions-toolbar__note">
        下钻采样时间：{fullTimeLabel(search.sampled_at)}
      </span>}
    </div>

    {/* 标识时效性的说明。PID 是拿去数据库侧继续排查的线索，不是稳定主键，这句话不能丢。 */}
    <NotificationBar tone="info" title="标识具有时效性">
      PID 可能因查询结束或后端复用而失效，仅作为数据库侧排查线索。
    </NotificationBar>

    {samples.isError && <NotificationBar tone="critical" title={apiErrorMessage(samples.error, '长查询采样记录加载失败')} />}

    <Panel
      flush
      title={total === undefined ? '长查询采样记录' : `长查询采样记录（${total}）`}
      actions={<SessionDensitySwitcher density={density} onChange={onDensityChange} />}
      footer={<Pagination
        className="sessions-pagination"
        size="md"
        page={page}
        pageSize={pageSize}
        pageSizes={[pageSize]}
        // 每页条数在这一页是给死的（迁移前的 `showSizeChanger: false` 也一样）。
        // 禁用而不是藏起来：藏了就没人知道一页是 50 条。
        pageSizeInputDisabled
        // 总数还没回来时页数就是未知的，不是 0。
        totalItems={total}
        pagesUnknown={total === undefined}
        onChange={({ page: nextPage }) => setPage(nextPage)}
      />}
    >
      <DataGrid<LongQuerySample>
        label="长查询采样记录"
        density={density}
        loading={samples.isPending}
        skeletonRows={8}
        rows={samples.data?.items ?? []}
        rowKey={(item) => `${item.sampled_at}-${item.pid}`}
        rowTestId="long-query-sample-row"
        columns={longQueryColumns}
        empty={{ title: '所选时间范围内没有长查询采样', description: '扩大时间范围，或确认长查询采样是否已启用。' }}
      />
    </Panel>
  </>
}

/// 九列，合计 1136px。1280px 下可用宽度实测 976px，所以各列按比例压缩，
/// 长取值靠单元格自己的省略号 + `title` 悬停看全文 —— 一列都不丢。
const longQueryColumns: DataGridColumn<LongQuerySample>[] = [
  {
    key: 'pid',
    header: 'PID',
    minWidth: 96,
    numeric: true,
    cell: (item) => <CopyableValue className="dbs-numeric" value={String(item.pid)} label="PID" />,
  },
  { key: 'sampled', header: '采样时间', minWidth: 140, cell: (item) => timeCell(item.sampled_at) },
  { key: 'query-started', header: '查询开始时间', minWidth: 140, cell: (item) => timeCell(item.query_started_at) },
  { key: 'database', header: '数据库', minWidth: 130, cell: (item) => optionalCopyableCell(item.database_name, '数据库名') },
  { key: 'username', header: '数据库用户', minWidth: 130, cell: (item) => optionalCopyableCell(item.username, '数据库用户') },
  { key: 'state', header: '状态', minWidth: 96, cell: (item) => optionalCell(item.state) },
  {
    key: 'query-duration',
    header: '查询持续时间',
    minWidth: 124,
    numeric: true,
    cell: (item) => `${(item.query_duration_ms / 1000).toFixed(1)} s`,
  },
  {
    key: 'wait-event',
    header: '等待事件',
    minWidth: 160,
    cell: (item) => optionalCell([item.wait_event_type, item.wait_event].filter(Boolean).join(' / ') || undefined),
  },
  {
    key: 'blocking',
    header: '阻塞源 PID',
    minWidth: 120,
    cell: (item) => optionalCell(blockingPidsLabel(item.blocking_pids)),
  },
]
