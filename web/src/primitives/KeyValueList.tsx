import type { ReactNode } from 'react'
import './KeyValueList.css'

export type KeyValueItem = {
  /** React key。 */
  key: string
  label: string
  value: ReactNode
}

export type KeyValueListProps = {
  /** 这份清单的可访问名，例如「Agent 接入状态」。读屏用它区分同一页上的几份清单。 */
  label: string
  items: readonly KeyValueItem[]
  /**
   * 宽屏最多分几栏，默认 2。窄屏一律收成一栏 —— 标签与值本来就是竖着读的一对，
   * 挤成三栏之后每栏只剩四十几个像素，值全变省略号。
   */
  columns?: 1 | 2 | 3
  className?: string
}

/// 键值清单：一组「标签 + 值」的成对事实。
///
/// 语义是 `<dl>` / `<dt>` / `<dd>` 而不是表格或一堆 div：读屏按对播报，
/// 「这个值是什么的值」不依赖视觉上的左右关系。每一对包在一个 `<div>` 里 ——
/// 规范允许，且这是让 grid 一次布一对（而不是把 dt 和 dd 分到两栏）的唯一办法。
///
/// 展示件：不认识任何业务概念，值是调用方交进来的节点。
export function KeyValueList({ label, items, columns = 2, className }: KeyValueListProps) {
  return (
    <dl
      className={['dbs-kv', className].filter(Boolean).join(' ')}
      aria-label={label}
      data-columns={columns}
    >
      {items.map((item) => (
        <div className="dbs-kv__item" key={item.key}>
          <dt className="dbs-kv__label dbs-caption">{item.label}</dt>
          <dd className="dbs-kv__value dbs-body">{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}
