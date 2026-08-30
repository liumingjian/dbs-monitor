import type { components } from '../api/schema'
import { StatusBadge } from '../primitives/StatusBadge'

const hourMilliseconds = 60 * 60 * 1000
const dayMilliseconds = 24 * hourMilliseconds
const warningThreshold = 7 * dayMilliseconds
type CollectionPauseAwareInstance = Pick<components['schemas']['Instance'], 'collection_pause'>

export function collectionPausePresentation(pausedAt: Date, now: Date): { label: string; warning: boolean } {
  const elapsed = Math.max(0, now.getTime() - pausedAt.getTime())
  const days = Math.floor(elapsed / dayMilliseconds)
  if (days > 0) {
    return { label: `已暂停 ${days} 天`, warning: elapsed > warningThreshold }
  }

  const hours = Math.floor(elapsed / hourMilliseconds)
  if (hours > 0) {
    return { label: `已暂停 ${hours} 小时`, warning: false }
  }
  return { label: '已暂停 不到 1 小时', warning: false }
}

export function pausedInstanceCount(instances: ReadonlyArray<CollectionPauseAwareInstance>): number {
  return instances.filter((instance) => instance.collection_pause.paused).length
}

export function CollectionPausedTag({ pausedAt, now = new Date() }: { pausedAt: string | Date; now?: Date }) {
  const presentation = collectionPausePresentation(new Date(pausedAt), now)
  // 暂停过久是要处理的事，走警告档；其余是中性事实，不给状态色。文案始终带时长，
  // 颜色只是让「停太久了」在扫视时先跳出来，从来不是唯一信号。
  return <StatusBadge tone={presentation.warning ? 'warning' : 'unknown'}>{presentation.label}</StatusBadge>
}
