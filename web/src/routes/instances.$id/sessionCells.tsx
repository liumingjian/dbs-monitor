import { useEffect, useRef, useState } from 'react'
import { Icon } from '../../primitives/Icon'
import { TruncatedText } from '../../primitives/TruncatedText'

/// 会话三张表共用的单元格。
///
/// **长字符串一律截断，绝不从中间折行。** 这一页的取值几乎全是等宽的标识符
/// （PID、queryid、OID、客户端地址、等待事件、阻塞源 PID 列表），等宽文本折成两行之后
/// 既读不出来也对不齐，而 40px 的行本来也只放得下一行。所以每一格都是
/// `primitives/TruncatedText`：单行 + 省略号 + 原生 `title` 悬停看全文。
///
/// 注意本页**不显示 SQL 正文**：`SessionSnapshotEntry` / `LongQuerySample` /
/// `QueryStatisticsEntry` 三个响应体里都没有查询文本字段（端到端用例专门断言页面上
/// 出现不了 `select * from`）。真正的长 SQL 一旦有一天进到这几张表里，它落进的就是
/// 这里已经铺好的截断约定，不需要再另想办法。

/// 可复制的取值。
///
/// 迁移前这些格子是 `Typography.Text copyable`：文字后面永远跟着一个复制图标。
/// 复制是真功能点（PID / queryid / OID 是拿去数据库侧继续排查的输入），不能丢；
/// 但一行里挂五个常驻按钮，本来就不宽的列会再被吃掉 5×32px，字都剩不下。
///
/// 所以按钮**不占布局宽度**：绝对定位贴在格子尾端，悬停或键盘聚焦时才显形，
/// 而它始终在 tab 序列里 —— 键盘和读屏用户任何时候都够得着（和迁移前一样）。
/// 反馈不用组件库的 `CopyButton`：它的「已复制」提示是浮层，而表格单元格是
/// `overflow: hidden`，浮层会被裁掉，等于没有反馈。这里改成图标就地变成对勾。
export function CopyableValue({ value, label, className }: { value: string; label: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => () => clearTimeout(timer.current), [])

  function copy() {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 2000)
    })
  }

  return <span className="session-copyable">
    <TruncatedText className={className}>{value}</TruncatedText>
    <button
      type="button"
      className="session-copyable__button"
      // 可访问名说清复制的是什么：一行里有好几个复制按钮，都叫「复制」就没法分辨。
      aria-label={copied ? `已复制${label}` : `复制${label}`}
      title={copied ? `已复制${label}` : `复制${label}`}
      onClick={copy}
    >
      <Icon name={copied ? 'checkmark' : 'copy'} size={16} />
    </button>
    {/* 复制成功只靠图标变化的话，读屏用户什么也听不到。 */}
    <span className="session-copyable__status" role="status">{copied ? `已复制${label}` : ''}</span>
  </span>
}

/// 可空文本。缺数不是空字符串，也不是 0：没有取值就写一个破折号。
export function optionalCell(value: string | undefined) {
  return value === undefined || value === '' ? '—' : <TruncatedText>{value}</TruncatedText>
}

export function optionalCopyableCell(value: string | undefined, label: string, className?: string) {
  return value === undefined || value === '' ? '—' : <CopyableValue value={value} label={label} className={className} />
}

/// 表格里的时刻。完整的 `toLocaleString()` 要 125px 以上；格子里只写「月-日 时:分」，
/// 完整时刻（含年与秒）进 `title` —— 和长文本截断是同一个约定：显示收窄，信息不丢。
/// `hour12: false` 是为了不让 AM/PM 再多吃宽度。
export function timeCell(value: string | undefined) {
  if (value === undefined) return '—'
  const date = new Date(value)
  const compact = date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
  return <TruncatedText className="dbs-numeric" title={fullTimeLabel(value)}>{compact}</TruncatedText>
}

/// 只写时刻的单元格，完整时刻（含年月日与秒）进 `title`。
///
/// 当前会话快照说的全是「此刻还在跑的会话」，年月日对每一行都一样，写出来只是把
/// 十一列里最紧张的两列各吃掉 30px。日期没有丢 —— 它在悬停提示里。
export function clockCell(value: string | undefined) {
  if (value === undefined) return '—'
  const compact = new Date(value).toLocaleTimeString('zh-CN', { hour12: false })
  return <TruncatedText className="dbs-numeric" title={fullTimeLabel(value)}>{compact}</TruncatedText>
}

export function fullTimeLabel(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
}

/// 持续时间。毫秒以下保留毫秒，超过一秒换成秒 —— 迁移前就是这个口径，原样保留。
export function durationLabel(value: number | undefined): string {
  if (value === undefined) return '—'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(1)} s`
}

/// 等待事件由「类型 / 事件」两段拼成，两段都可能缺。
export function waitEventLabel(type: string | undefined, event: string | undefined): string {
  return [type, event].filter(Boolean).join(' / ') || '—'
}

export function blockingPidsLabel(values: number[]): string {
  return values.length > 0 ? values.map(String).join(', ') : '—'
}
