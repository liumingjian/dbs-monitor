import { Tag } from 'antd'
import type { components } from '../api/schema'
import { CollectionPausedTag } from './CollectionPausedTag'

export type HealthStatusValue = components['schemas']['HealthStatus']

export const HEALTH_STATUSES = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const satisfies readonly HealthStatusValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected health status: ${value}`)
}

export function HealthStatus({ status, pausedAt }: { status: HealthStatusValue; pausedAt?: string }) {
  switch (status) {
    case 'CRITICAL':
      return <Tag color="error">严重</Tag>
    case 'WARNING':
      return <Tag color="warning">警告</Tag>
    case 'UNKNOWN':
      return <Tag>未知</Tag>
    case 'HEALTHY':
      return <Tag color="success">正常</Tag>
    case 'PAUSED':
      return pausedAt ? <CollectionPausedTag pausedAt={pausedAt} /> : <Tag>已暂停</Tag>
    default:
      return assertNever(status)
  }
}
