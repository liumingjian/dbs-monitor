import { describe, expect, it } from 'vitest'
import { standardMonitoringGroups, standardMonitoringMetricIDs } from './standardMonitoring'

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
