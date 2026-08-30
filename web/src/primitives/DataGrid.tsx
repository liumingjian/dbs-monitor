import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@carbon/react'
import type { CSSProperties, ReactNode } from 'react'
import { SkeletonBlock } from './SkeletonBlock'
import type { StatusTone } from './StatusBadge'
import './DataGrid.css'

export type DataGridColumn<Row> = {
  /** 列标识，用于排序与 React key。 */
  key: string
  header: ReactNode
  cell: (row: Row) => ReactNode
  /**
   * 该列的最小像素宽度，默认 120。它有两个作用：
   * 1. 决定这一列在可用宽度里分到多少比例（按各列最小宽度的比例分配）；
   * 2. 低于 1280px 时作为表格的最小宽度下限，此时才出现横向滚动。
   * 在 1280px 及以上，这个值**不会**撑出横向滚动 —— 装不下就靠省略号截断。
   */
  minWidth?: number
  /** 分配权重，默认 1。想让某列比它的最小宽度分到更多富余空间时调它。 */
  grow?: number
  /** 数值列：等宽表格数字 + 右对齐。 */
  numeric?: boolean
  /** 覆盖对齐方式；不给时数值列右对齐、其余左对齐。 */
  align?: 'start' | 'end'
  /** 该列可排序。排序状态由调用方持有（`sort` / `onSortChange`）。 */
  sortable?: boolean
}

export type DataGridSort = { key: string; direction: 'asc' | 'desc' }

export type DataGridProps<Row> = {
  /** 表格的可访问名，例如「实例列表」。读屏用它区分同一页上的多张表。 */
  label: string
  columns: DataGridColumn<Row>[]
  rows: Row[]
  rowKey: (row: Row) => string
  /**
   * 行首 3px 色条的颜色。返回 undefined 就没有色条。
   * 色条只是重复行内已有的状态信息，不是唯一信号。
   */
  rowTone?: (row: Row) => StatusTone | undefined
  /** 行高档位：`standard` 40px（默认），`dense` 32px。永远显式，没有第三种。 */
  density?: 'standard' | 'dense'
  sort?: DataGridSort
  onSortChange?: (sort: DataGridSort) => void
  /** 载入中：表体换成骨架行，表头照常显示。 */
  loading?: boolean
  /** 骨架行数，默认 5。 */
  skeletonRows?: number
  /** 空态。不给就用「暂无数据」。 */
  empty?: { title: string; description?: ReactNode; action?: ReactNode }
  /**
   * 粘性表头贴住的位置，任何 CSS 长度，默认页头高度。
   * 表格上方还有页签或工具条时，把它们的高度加进来。
   */
  stickyTop?: string
  'data-testid'?: string
}

function assertNever(value: never): never {
  throw new Error(`unhandled sort direction: ${String(value)}`)
}

function carbonSortDirection(direction: DataGridSort['direction']) {
  switch (direction) {
    case 'asc':
      return 'ASC' as const
    case 'desc':
      return 'DESC' as const
    default:
      return assertNever(direction)
  }
}

/// 数据表格外壳。
///
/// **1280px 不横向滚动、不丢列，机制在这里，页面不要另想办法：**
/// 表格是 `table-layout: fixed` + `width: 100%`，各列宽度由 `<colgroup>` 按
/// 「各列 minWidth 的比例」给成百分比，单元格一律 `overflow: hidden` + 省略号 + `title`
/// 悬停提示。所以在任何宽度下列宽都只是被压缩，never 被丢弃，也 never 撑出滚动条。
/// 只有窗口窄于 1280px 时，外层容器才变成横向滚动容器，并把表格的
/// `min-width` 设成各列 minWidth 之和。
///
/// **粘性表头与横向滚动容器不是同一个元素。** 滚动容器会成为粘性定位的参照系，
/// 表头就会落到表格内部去。1280px 及以上外层容器 `overflow: visible`，表头贴的是页头
/// （`stickyTop`）；窄于 1280px 时外层才成为滚动容器，表头改贴容器顶部（`top: 0`）。
/// Carbon 自己在 `<table>` 外面套的 `.cds--data-table-content` 默认就是
/// `overflow-x: auto`，正好踩中这个坑，所以本组件的样式表把它按平了。
export function DataGrid<Row>({
  label,
  columns,
  rows,
  rowKey,
  rowTone,
  density = 'standard',
  sort,
  onSortChange,
  loading = false,
  skeletonRows = 5,
  empty,
  stickyTop,
  'data-testid': testId,
}: DataGridProps<Row>) {
  const minWidths = columns.map((column) => column.minWidth ?? 120)
  const weights = columns.map((column, index) => minWidths[index] * (column.grow ?? 1))
  const weightTotal = weights.reduce((sum, weight) => sum + weight, 0)
  const minWidthTotal = minWidths.reduce((sum, width) => sum + width, 0)

  const containerStyle = {
    '--dbs-datagrid-min-width': `${minWidthTotal}px`,
    '--dbs-datagrid-sticky-top': stickyTop,
  } as CSSProperties

  const columnAlign = (column: DataGridColumn<Row>) => column.align ?? (column.numeric === true ? 'end' : 'start')

  const handleSort = (column: DataGridColumn<Row>) => {
    if (onSortChange === undefined) return
    const nextDirection = sort?.key === column.key && sort.direction === 'asc' ? 'desc' : 'asc'
    onSortChange({ key: column.key, direction: nextDirection })
  }

  return (
    <div
      className="dbs-datagrid"
      style={containerStyle}
      data-density={density}
      data-testid={testId}
      aria-busy={loading ? 'true' : undefined}
    >
      <Table
        className="dbs-datagrid__table"
        size={density === 'dense' ? 'sm' : 'md'}
        isSortable={columns.some((column) => column.sortable === true)}
        aria-label={label}
      >
        <colgroup>
          {columns.map((column, index) => (
            <col key={column.key} style={{ inlineSize: `${(weights[index] / weightTotal) * 100}%` }} />
          ))}
        </colgroup>
        <TableHead>
          <TableRow>
            {columns.map((column) => (
              <TableHeader
                key={column.key}
                // 组件库在可排序表头里把 `...rest` 摊到内部的 <button> 上、只把
                // className 留在 <th> 上，所以对齐只能走类名，不能走 data 属性。
                className={
                  columnAlign(column) === 'end'
                    ? 'dbs-table-header dbs-datagrid__th--end'
                    : 'dbs-table-header'
                }
                isSortable={column.sortable === true && onSortChange !== undefined}
                isSortHeader={sort?.key === column.key}
                sortDirection={sort === undefined ? 'NONE' : carbonSortDirection(sort.direction)}
                onClick={() => handleSort(column)}
              >
                {column.header}
              </TableHeader>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {loading &&
            Array.from({ length: skeletonRows }, (_, rowIndex) => (
              <TableRow key={`skeleton-${rowIndex}`} className="dbs-datagrid__row">
                {columns.map((column) => (
                  <TableCell key={column.key} className="dbs-datagrid__td">
                    <SkeletonBlock lines={1} decorative />
                  </TableCell>
                ))}
              </TableRow>
            ))}

          {!loading && rows.length === 0 && (
            <TableRow className="dbs-datagrid__row">
              <TableCell className="dbs-datagrid__empty" colSpan={columns.length}>
                <p className="dbs-datagrid__empty-title dbs-body">{empty?.title ?? '暂无数据'}</p>
                {empty?.description !== undefined && (
                  <p className="dbs-datagrid__empty-description dbs-caption">{empty.description}</p>
                )}
                {empty?.action !== undefined && <div className="dbs-datagrid__empty-action">{empty.action}</div>}
              </TableCell>
            </TableRow>
          )}

          {!loading &&
            rows.map((row) => (
              <TableRow key={rowKey(row)} className="dbs-datagrid__row" data-tone={rowTone?.(row)}>
                {columns.map((column) => (
                  <TableCell
                    key={column.key}
                    className={column.numeric === true ? 'dbs-datagrid__td dbs-numeric' : 'dbs-datagrid__td'}
                    data-align={columnAlign(column)}
                  >
                    {column.cell(row)}
                  </TableCell>
                ))}
              </TableRow>
            ))}
        </TableBody>
      </Table>
    </div>
  )
}
