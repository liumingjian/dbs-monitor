import type { components } from '../../api/schema'

export type SessionSnapshotEntry = components['schemas']['SessionSnapshotEntry']

export function groupSessionSnapshot(items: SessionSnapshotEntry[]) {
  return {
    active: items.filter((item) => item.state === 'active'),
    longTransactions: items.filter((item) => item.transaction_duration_ms !== undefined && item.transaction_duration_ms >= 300_000),
    lockWaits: items.filter((item) => item.wait_event_type === 'Lock'),
    blockingChains: items.filter((item) => item.blocking_pids.length > 0),
    details: items,
  }
}
