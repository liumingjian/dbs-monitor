import { describe, expect, it } from 'vitest'
import type { components } from '../../api/schema'
import { groupMaintenanceWindows, parseMaintenanceSearch } from './maintenance'

type MaintenanceWindow = components['schemas']['MaintenanceWindow']

const base = {
  id: '00000000-0000-4000-8000-000000000082',
  instance_ids: ['00000000-0000-4000-8000-000000000001'],
  starts_at: '2026-08-11T12:00:00Z', ends_at: '2026-08-11T13:00:00Z', reason: 'restart',
  created_by: '00000000-0000-4000-8000-000000000002', created_at: '2026-08-11T11:00:00Z', updated_at: '2026-08-11T11:00:00Z',
} satisfies Omit<MaintenanceWindow, 'status'>

describe('maintenance window page mapping', () => {
  it('groups every window by its projected status', () => {
    const grouped = groupMaintenanceWindows([
      { ...base, status: 'ENDED' },
      { ...base, id: '00000000-0000-4000-8000-000000000083', status: 'ACTIVE' },
      { ...base, id: '00000000-0000-4000-8000-000000000084', status: 'SCHEDULED' },
    ])
    expect(grouped.ACTIVE).toHaveLength(1)
    expect(grouped.SCHEDULED).toHaveLength(1)
    expect(grouped.ENDED).toHaveLength(1)
  })

  it('retains only a usable instance shortcut value', () => {
    expect(parseMaintenanceSearch({ instance_id: 'instance-1', ignored: true })).toEqual({ instance_id: 'instance-1' })
    expect(parseMaintenanceSearch({ instance_id: 42 })).toEqual({})
  })
})
