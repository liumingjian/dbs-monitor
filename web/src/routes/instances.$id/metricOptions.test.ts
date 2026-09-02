import { describe, expect, it } from 'vitest'
import type { components } from '../../api/schema'
import { allMetricIDs, isEnhancedCandidate, isMetricID, metricCatalogFrom } from './metricOptions'

type MetricCatalogEntry = components['schemas']['MetricCatalogEntry']

/// 第二个引擎只存在于这个用例里：生成的 InstanceEngine 今天只有 PostgreSQL 一项，
/// 而跨引擎的可选性正是要在「有两个引擎」的前提下才看得见。
const otherEngine = 'ENGINE_UNDER_TEST' as MetricCatalogEntry['engine']

function entry(id: string, engine: MetricCatalogEntry['engine'], slot: MetricCatalogEntry['semantic_slot']): MetricCatalogEntry {
  return { metric_id: id, engine, unit: 'count', display_name: id, semantic_slot: slot, level: 'INSTANCE', aggregation: 'NONE' }
}

describe('metric IDs', () => {
  it('exposes every R1 P0 metric exactly once', () => {
    expect(allMetricIDs).toHaveLength(38)
    expect(new Set(allMetricIDs).size).toBe(38)
  })

  it('exposes every pg_stat_activity task metric', () => {
    expect(allMetricIDs).toEqual(expect.arrayContaining([
      'pg.connection.total',
      'pg.connection.active',
      'pg.connection.idle_in_transaction',
      'pg.transaction.long_count',
      'pg.transaction.max_duration_sec',
      'pg.lock.waiting_count',
      'pg.session.blocked_count',
      'pg.query.long_running_count',
    ]))
  })

  it('recognises catalogued metric IDs and rejects everything else', () => {
    expect(isMetricID('pg.tps')).toBe(true)
    expect(isMetricID('mysql.qps')).toBe(false)
    expect(isMetricID(undefined)).toBe(false)
  })

  it('keeps control-plane metrics out of the enhanced chart picker', () => {
    expect(isEnhancedCandidate('agent.status')).toBe(false)
    expect(isEnhancedCandidate('pg.tps')).toBe(true)
  })
})

describe('metric applicability across engines', () => {
  const slots = [
    { slot_id: 'connections' as const, display_name: '连接数' },
    { slot_id: 'throughput' as const, display_name: '吞吐' },
  ]

  it('lets an engine-agnostic metric apply anywhere', () => {
    const catalog = metricCatalogFrom([entry('host.cpu.usage_percent', 'AGNOSTIC', null)], slots)
    expect(catalog.appliesToEngine('host.cpu.usage_percent', 'POSTGRESQL')).toEqual({ applicable: true })
  })

  it('lets a metric apply on its own engine', () => {
    const catalog = metricCatalogFrom([entry('pg.replication_slot.retained_wal_bytes', 'POSTGRESQL', null)], slots)
    expect(catalog.appliesToEngine('pg.replication_slot.retained_wal_bytes', 'POSTGRESQL')).toEqual({ applicable: true })
  })

  // 一份两用：另一个引擎的指标只要填的是同一个语义位，而这个位在本引擎上有绑定，就能用。
  it('follows the semantic slot onto another engine', () => {
    const catalog = metricCatalogFrom([
      entry('other.connection.total', otherEngine, 'connections'),
      entry('pg.connection.total', 'POSTGRESQL', 'connections'),
    ], slots)
    expect(catalog.appliesToEngine('other.connection.total' as never, 'POSTGRESQL')).toEqual({ applicable: true })
  })

  // 位在本引擎上没有绑定时不可选，而且理由要说出是哪个引擎缺哪个位——界面就靠这句话解释
  // 为什么这台实例选不了。
  it('refuses a slot the engine does not bind, and says which slot', () => {
    const catalog = metricCatalogFrom([entry('other.tps', otherEngine, 'throughput')], slots)
    expect(catalog.appliesToEngine('other.tps' as never, 'POSTGRESQL')).toEqual({
      applicable: false,
      reason: 'PostgreSQL 还没有「吞吐」这个指标',
    })
  })

  it('does not block on a catalogue it has not received yet', () => {
    expect(metricCatalogFrom([], slots).appliesToEngine('pg.tps', 'POSTGRESQL')).toEqual({ applicable: true })
  })
})
