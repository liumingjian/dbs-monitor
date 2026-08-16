import { Tag } from 'antd'
import type { components } from '../api/schema'

export type AlertStatusValue = components['schemas']['AlertStatus']

export const ALERT_STATUSES = ['OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED'] as const satisfies readonly AlertStatusValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected alert status: ${value}`)
}

export function AlertStatus({ status }: { status: AlertStatusValue }) {
  switch (status) {
    case 'OK':
      return <Tag color="success">正常</Tag>
    case 'PENDING':
      return <Tag color="processing">待持续</Tag>
    case 'FIRING':
      return <Tag color="error">告警中</Tag>
    case 'NO_DATA':
      return <Tag>无数据</Tag>
    case 'RECOVERED':
      return <Tag color="success">已恢复</Tag>
    default:
      return assertNever(status)
  }
}
