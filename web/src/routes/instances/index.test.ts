import { describe, expect, it } from 'vitest'
import type { components } from '../../api/schema'
import { filterAndSortInstances } from './index'

type Instance = components['schemas']['Instance']

function instance(name: string, status: Instance['health']['status'], counts = { critical: 0, warning: 0, info: 0 }, flags = {
  no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0,
}): Instance {
  return {
    id: `${name}0000-0000-4000-8000-000000000000`.slice(0, 36),
    name,
    host: 'localhost',
    port: 5432,
    database: 'postgres',
    username: 'postgres',
    agent_metrics_enabled: false,
    alert_status: 'OK',
    agent_status: 'not_installed',
    collection_pause: { paused: status === 'PAUSED' },
    health: { status, counts, flags },
  }
}

const instances = [
  instance('healthy', 'HEALTHY', { critical: 0, warning: 0, info: 1 }),
  instance('paused', 'PAUSED'),
  instance('critical', 'CRITICAL', { critical: 1, warning: 0, info: 0 }, { no_data: true, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0 }),
  instance('unknown', 'UNKNOWN', { critical: 0, warning: 0, info: 0 }, { no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 2 }),
  instance('warning', 'WARNING', { critical: 0, warning: 1, info: 0 }, { no_data: false, in_maintenance: true, recently_recovered: false, ignored: 1, configuration_missing: 0 }),
]

describe('instance list projection', () => {
  it('sorts by health severity without hiding any instance by default', () => {
    expect(filterAndSortInstances(instances, {}).map((item) => item.name)).toEqual([
      'critical', 'warning', 'unknown', 'healthy', 'paused',
    ])
  })

  it('combines health status and orthogonal marker filters', () => {
    expect(filterAndSortInstances(instances, { statuses: ['CRITICAL'], flags: ['NO_DATA'] }).map((item) => item.name)).toEqual(['critical'])
    expect(filterAndSortInstances(instances, { flags: ['MAINTENANCE', 'IGNORED'] }).map((item) => item.name)).toEqual(['warning'])
  })

  it('filters alert levels info and configuration gaps independently', () => {
    expect(filterAndSortInstances(instances, { alertSeverity: 'warning' }).map((item) => item.name)).toEqual(['warning'])
    expect(filterAndSortInstances(instances, { hasInfo: true }).map((item) => item.name)).toEqual(['healthy'])
    expect(filterAndSortInstances(instances, { hasConfigurationMissing: true }).map((item) => item.name)).toEqual(['unknown'])
  })
})
