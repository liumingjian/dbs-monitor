import type { components } from '../api/schema'
import { StatusBadge, type StatusTone } from '../primitives/StatusBadge'

export type AlertStatusValue = components['schemas']['AlertStatus']

export const ALERT_STATUSES = ['OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED'] as const satisfies readonly AlertStatusValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected alert status: ${value}`)
}

/// 告警状态的**呈现**：文案 + 档位，只此一处。文案永远在，颜色从来不是唯一信号。
///
/// `PENDING`（待持续）与 `NO_DATA`（无数据）走中性档：一个是还没到持续时长，一个是没有
/// 依据可判，都不是「出事了」的判断，不该抢走 `FIRING` 的注意力。迁移前 `PENDING` 用的是
/// 组件库的 processing 蓝 —— 蓝色只表示可交互，状态不许用蓝，因此改为中性档
/// （与上一轮「维护中」「已确认」是同一次调整）。
export function alertStatusPresentation(status: AlertStatusValue): { label: string; tone: StatusTone } {
  switch (status) {
    case 'OK':
      return { label: '正常', tone: 'normal' }
    case 'PENDING':
      return { label: '待持续', tone: 'unknown' }
    case 'FIRING':
      return { label: '告警中', tone: 'critical' }
    case 'NO_DATA':
      return { label: '无数据', tone: 'unknown' }
    case 'RECOVERED':
      return { label: '已恢复', tone: 'normal' }
    default:
      return assertNever(status)
  }
}

export function AlertStatus({ status }: { status: AlertStatusValue }) {
  const { label, tone } = alertStatusPresentation(status)
  return <StatusBadge tone={tone}>{label}</StatusBadge>
}
