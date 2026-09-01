import { CopyButton } from '@carbon/react'
import { useMemo } from 'react'
import { NotificationBar } from '../primitives/NotificationBar'
import { formatSql, sqlTokens } from './sqlText'
import './SqlStatement.css'

export type SqlStatementProps = {
  /**
   * 归一化之后的语句全文。字面量已经是 `$1` 占位符，这里不做也不需要做任何脱敏。
   *
   * `undefined` 表示这条语句的文本还没采到 —— 那是一句要说出来的话，不是一块空白。
   */
  sql: string | undefined
  /** 可访问名。这一块可滚动、可聚焦，读屏用户需要知道聚上来的是什么。 */
  label: string
}

/// 一条 SQL 的全文：先折行缩进，再按词类着色，右上角一个复制按钮。
///
/// 列表里放的是摘要（`statementSummary`），这一件是摘要点开之后的去处 —— 一条两百字符的
/// 语句挤在 40px 的行里谁也读不出结构，而结构正是「这条为什么慢」的第一个线索。
///
/// **格式化与着色都不改文本本身**：折行只插空白，着色只包 `<span>`，复制出去的就是数据库里
/// 那一条（差别只在折行处的空白，而 SQL 不在乎空白）。
///
/// 跨实例榜单与实例内排行两处共用：同一条语句在两个尺度上显示，读法必须是同一套。
export function SqlStatement({ sql, label }: SqlStatementProps) {
  // hooks 不能排在提前返回后面，所以词法切分先做——缺文本时切的是空串，代价可以忽略。
  const tokens = useMemo(() => sqlTokens(formatSql(sql ?? '')), [sql])

  if (sql === undefined) {
    return (
      <NotificationBar tone="warning" title="这条语句的文本还没采到">
        排行来自查询统计快照，文本按 (实例, queryid) 另外存一份。扩展刚重置过统计、条目刚被
        淘汰，或这一轮采集只拿到了指标时，就会只有 queryid 而没有文本。
      </NotificationBar>
    )
  }

  return (
    <div className="sql-statement-block">
      <div className="sql-statement-block__toolbar">
        <span className="dbs-caption">语句全文（已折行缩进，文本本身未改动）</span>
        <CopyButton
          align="left"
          iconDescription="复制 SQL 全文"
          feedback="已复制"
          onClick={() => void navigator.clipboard.writeText(sql)}
        />
      </div>
      {/* 可滚动区按 `role="region"` + `tabIndex` 处理：长语句会溢出，
          而只有鼠标能滚的区域对键盘用户等于读不到。 */}
      <pre className="sql-statement" role="region" aria-label={label} tabIndex={0}>
        <code className="dbs-numeric">
          {tokens.map((token, index) => (
            // 运算符、标识符与空白不着色：把每个逗号和括号都包一层 span，颜色没多一种，
            // DOM 却翻一倍。
            token.kind === 'whitespace' || token.kind === 'operator' || token.kind === 'identifier'
              ? token.text
              : <span key={index} className={`sql-statement__${token.kind}`}>{token.text}</span>
          ))}
        </code>
      </pre>
    </div>
  )
}
