import { Tag } from 'antd'
import type { components } from '../api/schema'

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
  return <Tag color={presentation.warning ? 'warning' : undefined}>{presentation.label}</Tag>
}
