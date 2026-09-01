import type { components } from '../api/schema'
import type { StatusTone } from '../primitives/StatusBadge'
import { StatusDot } from '../primitives/StatusDot'
import { collectionPausePresentation } from './CollectionPausedTag'

export type HealthStatusValue = components['schemas']['HealthStatus']

export const HEALTH_STATUSES = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const satisfies readonly HealthStatusValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected health status: ${value}`)
}

/// 健康状态的文案。筛选下拉的选项标签与状态标记读的是同一份，两处永远说同一个词。
export function healthLabel(status: HealthStatusValue): string {
  switch (status) {
    case 'CRITICAL':
      return '严重'
    case 'WARNING':
      return '警告'
    case 'UNKNOWN':
      return '未知'
    case 'HEALTHY':
      return '正常'
    case 'PAUSED':
      return '已暂停'
    default:
      return assertNever(status)
  }
}

/// 健康状态 → 展示层的状态档位。业务枚举到视觉档位的映射留在 `domain/`：
/// `primitives/` 不认识 `CRITICAL` 是什么，这条边界是它能被别的产品直接搬走的原因。
export function healthTone(status: HealthStatusValue): StatusTone {
  switch (status) {
    case 'CRITICAL':
      return 'critical'
    case 'WARNING':
      return 'warning'
    case 'HEALTHY':
      return 'normal'
    case 'UNKNOWN':
      return 'unknown'
    case 'PAUSED':
      return 'unknown'
    default:
      return assertNever(status)
  }
}

/// 健康状态的**呈现**：文案 + 档位，只此一处。暂停且知道暂停时刻时，文案带上已暂停时长
/// （走 `collectionPausePresentation`，与采集暂停标记是同一句话），暂停过久按警告档呈现。
///
/// 实例列表与实例总览都从这里取值，因此两处不会各说各的 —— 端到端用例正是这样断言的。
export function healthStatusPresentation(
  status: HealthStatusValue,
  pausedAt?: string | Date,
  now: Date = new Date(),
): { label: string; tone: StatusTone } {
  if (status === 'PAUSED' && pausedAt !== undefined) {
    const pause = collectionPausePresentation(new Date(pausedAt), now)
    return { label: pause.label, tone: pause.warning ? 'warning' : healthTone(status) }
  }
  return { label: healthLabel(status), tone: healthTone(status) }
}

export function HealthStatus({
  status,
  pausedAt,
  now,
}: {
  status: HealthStatusValue
  pausedAt?: string
  now?: Date
}) {
  // data-testid 是本仓库唯一的稳定测试标识约定（见 web/CLAUDE.md）。
  // 健康状态的呈现形态会随视图层替换而变，标识挂在外层包裹元素上，不依赖标签组件的类名。
  const { label, tone } = healthStatusPresentation(status, pausedAt, now)
  return (
    <span data-testid="health-status">
      <StatusDot tone={tone}>{label}</StatusDot>
    </span>
  )
}
