import { describe, expect, it } from 'vitest'
import { standardMonitoringGroups, standardMonitoringMetricIDs } from './standardMonitoring'
import { instanceThresholds } from './monitoring'

const expectedMetricIDs = [
  'host.cpu.usage_percent',
  'host.memory.usage_percent',
  'host.disk.usage_percent',
  'host.disk.free_bytes',
  'host.disk.iops',
  'host.disk.throughput_bytes_per_sec',
  'host.network.bytes_per_sec',
  'pg.connection.total',
  'pg.connection.active',
  'pg.connection.idle_in_transaction',
  'pg.tps',
  'pg.xact.commit_per_sec',
  'pg.xact.rollback_per_sec',
  'pg.tuples.read_per_sec',
  'pg.tuples.write_per_sec',
  'pg.temp.files_per_sec',
  'pg.temp.bytes_per_sec',
  'pg.transaction.long_count',
  'pg.transaction.max_duration_sec',
  'pg.lock.waiting_count',
  'pg.session.blocked_count',
  'pg.query.long_running_count',
  'pg.prepared_xacts.count',
  'pg.replication.role',
  'pg.replication.connection_state',
  'pg.replication.wal_lag_bytes',
  'pg.replication.replay_lag_ms',
  'pg.replication_slot.retained_wal_bytes',
]

describe('standard monitoring definition', () => {
  it('lays out exactly 5 resource, 12 database, and 5 replication charts', () => {
    expect(standardMonitoringGroups.map((group) => [group.title, group.charts.length])).toEqual([
      ['资源指标', 5],
      ['数据库指标', 12],
      ['复制指标', 5],
    ])
  })

  it('requests every standard monitoring metric exactly once', () => {
    expect(standardMonitoringMetricIDs).toHaveLength(28)
    expect(new Set(standardMonitoringMetricIDs).size).toBe(28)
    expect(standardMonitoringMetricIDs).toEqual(expect.arrayContaining(expectedMetricIDs))
  })

  it('provides details for every chart and a sample drilldown for long queries', () => {
    const charts = standardMonitoringGroups.flatMap((group) => group.charts)
    expect(charts.every((chart) => chart.description.length > 0)).toBe(true)
    expect(charts.find((chart) => chart.metrics.includes('pg.query.long_running_count'))?.drilldown).toBe('long-query-samples')
  })
})

describe('instanceThresholds', () => {
  const rule = {
    id: 'r1', name: '连接数高于 5', metric_id: 'pg.connection.total',
    aggregation: 'avg' as const, operator: '>' as const, threshold: 5,
    recovery_operator: '<=' as const, recovery_threshold: 4,
    current_alert_count: 0, version: 1,
    created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z',
    window_seconds: 30, consecutive_count: 3, recovery_consecutive_count: 3,
    severity: 'critical' as const, no_data_policy: 'ignore' as const,
    scope: 'ALL' as const, instance_ids: [], evaluation_interval_seconds: 30,
    enabled: true, is_builtin: false, effective_notification_policy_name: '默认',
  }
  const metrics = [{ metric: 'pg.connection.total', unit: 'count', unavailability: null, series: [] }]

  it('takes the unit from the series so the line lands on the right axis', () => {
    expect(instanceThresholds([rule], 'i1', ['pg.connection.total'], metrics)).toEqual([
      { label: '连接数高于 5', unit: 'count', value: 5, severity: 'critical' },
    ])
  })

  it('ignores disabled rules and rules scoped to other instances', () => {
    expect(instanceThresholds([{ ...rule, enabled: false }], 'i1', ['pg.connection.total'], metrics)).toEqual([])
    expect(instanceThresholds(
      [{ ...rule, scope: 'INSTANCES' as const, instance_ids: ['other'] }],
      'i1', ['pg.connection.total'], metrics,
    )).toEqual([])
  })

  it('draws nothing before the rules have loaded', () => {
    expect(instanceThresholds(undefined, 'i1', ['pg.connection.total'], metrics)).toEqual([])
  })

  it('drops a threshold whose unit is unknown rather than guessing one', () => {
    expect(instanceThresholds([rule], 'i1', ['pg.connection.total'], undefined)).toEqual([])
    expect(instanceThresholds([rule], 'i1', ['pg.connection.total'], [])).toEqual([])
  })
})
