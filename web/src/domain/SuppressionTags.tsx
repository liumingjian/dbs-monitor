import { Tag } from 'antd'
import type { components } from '../api/schema'

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

function tagLabel(tag: SuppressionTagValue, flags: HealthFlags) {
  switch (tag) {
    case 'NO_DATA_MARKER':
      return flags.no_data ? <Tag key={tag}>无数据</Tag> : null
    case 'MAINTENANCE_MARKER':
      return flags.in_maintenance ? <Tag key={tag} color="processing">维护中</Tag> : null
    case 'RECENT_RECOVERY_MARKER':
      return flags.recently_recovered ? <Tag key={tag} color="success">近期恢复</Tag> : null
    case 'IGNORED_MARKER':
      return flags.ignored > 0 ? <Tag key={tag}>已忽略 {flags.ignored}</Tag> : null
    case 'CONFIGURATION_MISSING_MARKER':
      return flags.configuration_missing > 0 ? <Tag key={tag}>配置缺失 {flags.configuration_missing}</Tag> : null
    default:
      return assertNever(tag)
  }
}

export function SuppressionTags({ flags }: { flags: HealthFlags }) {
  return <span>{SUPPRESSION_TAGS.map((tag) => tagLabel(tag, flags))}</span>
}
