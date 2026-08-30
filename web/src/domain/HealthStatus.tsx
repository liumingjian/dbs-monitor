import { Tag } from 'antd'
import type { components } from '../api/schema'
import { CollectionPausedTag } from './CollectionPausedTag'

export type HealthStatusValue = components['schemas']['HealthStatus']

export const HEALTH_STATUSES = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const satisfies readonly HealthStatusValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected health status: ${value}`)
}

export function HealthStatus({ status, pausedAt }: { status: HealthStatusValue; pausedAt?: string }) {
  // data-testid 是本仓库唯一的稳定测试标识约定（见 web/CLAUDE.md）。
  // 健康状态的呈现形态会随视图层替换而变，标识挂在外层包裹元素上，不依赖标签组件的类名。
  return <span data-testid="health-status">{healthStatusTag(status, pausedAt)}</span>
}

function healthStatusTag(status: HealthStatusValue, pausedAt: string | undefined) {
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
