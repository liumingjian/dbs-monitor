import type { components } from '../api/schema'
import { StatusBadge } from '../primitives/StatusBadge'
import { CollectionPausedTag } from './CollectionPausedTag'
import './SuppressionTags.css'

type HealthFlags = components['schemas']['HealthFlags']
export type SuppressionTagValue = 'NO_DATA_MARKER' | 'MAINTENANCE_MARKER' | 'RECENT_RECOVERY_MARKER' | 'IGNORED_MARKER' | 'CONFIGURATION_MISSING_MARKER'

export const SUPPRESSION_TAGS = [
  'NO_DATA_MARKER',
  'MAINTENANCE_MARKER',
  'RECENT_RECOVERY_MARKER',
  'IGNORED_MARKER',
  'CONFIGURATION_MISSING_MARKER',
] as const satisfies readonly SuppressionTagValue[]

function assertNever(value: never): never {
  throw new Error(`unexpected suppression tag: ${value}`)
}

/// 各标记的档位。这些标记大多是**中性事实**（无数据、维护中、已忽略），走中性档；
/// 只有「配置缺失」是要人去处理的，才给警告档。文案永远在，颜色从来不是唯一信号。
function tagLabel(tag: SuppressionTagValue, flags: HealthFlags) {
  switch (tag) {
    case 'NO_DATA_MARKER':
      return flags.no_data ? <StatusBadge key={tag} tone="unknown">无数据</StatusBadge> : null
    case 'MAINTENANCE_MARKER':
      return flags.in_maintenance ? <StatusBadge key={tag} tone="unknown">维护中</StatusBadge> : null
    case 'RECENT_RECOVERY_MARKER':
      return flags.recently_recovered ? <StatusBadge key={tag} tone="normal">近期恢复</StatusBadge> : null
    case 'IGNORED_MARKER':
      return flags.ignored > 0 ? <StatusBadge key={tag} tone="unknown">{`已忽略 ${flags.ignored}`}</StatusBadge> : null
    case 'CONFIGURATION_MISSING_MARKER':
      return flags.configuration_missing > 0
        ? <StatusBadge key={tag} tone="warning">{`配置缺失 ${flags.configuration_missing}`}</StatusBadge>
        : null
    default:
      return assertNever(tag)
  }
}

export function SuppressionTags({ flags, className }: { flags: HealthFlags; className?: string }) {
  return (
    <span className={['dbs-marker-list', className].filter(Boolean).join(' ')}>
      {SUPPRESSION_TAGS.map((tag) => tagLabel(tag, flags))}
    </span>
  )
}

export function AlertSuppressionTags({
  inMaintenance,
  disposition,
  pausedAt,
  paused = pausedAt !== undefined,
  now,
  className,
}: {
  inMaintenance?: boolean | null
  disposition: components['schemas']['AlertDisposition']
  paused?: boolean
  pausedAt?: string
  now?: Date
  className?: string
}) {
  return <span className={['dbs-marker-list', className].filter(Boolean).join(' ')}>
    {inMaintenance === true && <StatusBadge tone="unknown">维护中</StatusBadge>}
    {dispositionTag(disposition)}
    {paused && (pausedAt
      ? <CollectionPausedTag pausedAt={pausedAt} now={now} />
      : <StatusBadge tone="unknown">已暂停</StatusBadge>)}
  </span>
}

function dispositionTag(disposition: components['schemas']['AlertDisposition']) {
  switch (disposition) {
    case 'NONE':
      return null
    case 'ACKED':
      return <StatusBadge tone="unknown">已确认</StatusBadge>
    case 'IGNORED':
      return <StatusBadge tone="unknown">已忽略</StatusBadge>
    default:
      return assertNever(disposition)
  }
}
