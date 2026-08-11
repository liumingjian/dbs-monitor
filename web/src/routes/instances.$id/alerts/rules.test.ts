import { describe, expect, it } from 'vitest'
import type { components } from '../../../api/schema'
import { capabilityFit, consecutiveDurationLabel, formatRuleDuration } from './rules'

type CollectionTask = components['schemas']['CollectionTaskState']
type Capability = components['schemas']['CapabilitySnapshotEntry']
type Instance = components['schemas']['Instance']

const task = {
  metric_ids: ['pg.connection.total'],
  required_capabilities: ['role.pg_monitor'],
} as CollectionTask

const instance = {
  agent_metrics_enabled: true,
  agent_status: 'online',
} as Instance

describe('alert rule timing', () => {
  it.each([
    [30, '30 秒'],
    [90, '1 分 30 秒'],
    [3600, '1 小时'],
    [3665, '1 小时 1 分 5 秒'],
  ])('formats %i seconds as %s', (seconds, expected) => {
    expect(formatRuleDuration(seconds)).toBe(expected)
  })

  it('shows consecutive evaluations and their approximate duration', () => {
    expect(consecutiveDurationLabel(3, 30)).toBe('连续 3 次 × 30 秒 ≈ 1 分 30 秒')
  })
})

describe('alert rule capability fit', () => {
  it('reports satisfied when every declared capability is present', () => {
    const capabilities = [{ capability_id: 'role.pg_monitor', status: 'PRESENT' }] as Capability[]
    expect(capabilityFit('pg.connection.total', [task], capabilities, instance)).toBe('SATISFIED')
  })

  it.each(['MISSING', 'NOT_APPLICABLE'] as const)('reports unsatisfied for %s requirements', (status) => {
    const capabilities = [{ capability_id: 'role.pg_monitor', status }] as Capability[]
    expect(capabilityFit('pg.connection.total', [task], capabilities, instance)).toBe('UNSATISFIED')
  })

  it('reports unknown for absent or unknown capability observations', () => {
    expect(capabilityFit('pg.connection.total', [task], [], instance)).toBe('UNKNOWN')
  })

  it('uses the instance Agent state for host metrics', () => {
    expect(capabilityFit('host.cpu.usage_percent', [], [], { ...instance, agent_status: 'offline' })).toBe('UNSATISFIED')
  })
})
