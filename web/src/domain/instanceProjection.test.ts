import { describe, expect, it } from 'vitest'
import type { components } from '../api/schema'
import {
  agentFailureLabel,
  attributionLabel,
  collectionFreshnessLabel,
  collectionFreshnessTitle,
  connectionSaturationLabel,
  dataFreshnessLabel,
  instanceSlotEntry,
  latestValue,
  trendValues,
  usageTone,
} from './instanceProjection'

type Instance = components['schemas']['Instance']
type InstancesMetricSeriesResponse = components['schemas']['InstancesMetricSeriesResponse']

function instance(overrides: Partial<Instance> = {}): Instance {
  return {
    id: '00000000-0000-4000-8000-000000000001',
    name: 'one',
    host: '10.0.0.1',
    port: 5432,
    engine: 'POSTGRESQL',
    database: 'postgres',
    username: 'postgres',
    agent_metrics_enabled: false,
    alert_status: 'OK',
    agent_status: 'not_installed',
    collection_pause: { paused: false },
    health: {
      status: 'HEALTHY',
      counts: { critical: 0, warning: 0, info: 0 },
      flags: { no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0 },
    },
    ...overrides,
  }
}

const trends: InstancesMetricSeriesResponse = {
  from: '2026-08-11T11:00:00Z',
  to: '2026-08-11T12:00:00Z',
  step: '5m',
  instances: [
    {
      instance_id: '00000000-0000-4000-8000-000000000001',
      metrics: [
        {
          metric: 'pg.tps',
          slot: 'throughput',
          unit: 'tx/s',
          unavailability: null,
          series: [{ labels: {}, points: [[1, 12], [2, null], [3, 21]] }],
        },
        {
          metric: 'pg.connection.saturation_percent',
          slot: 'connection_saturation',
          unit: 'percent',
          unavailability: null,
          series: [{ labels: {}, points: [[1, 40], [2, 87.4], [3, null]] }],
        },
      ],
    },
    {
      instance_id: '00000000-0000-4000-8000-000000000002',
      metrics: [
        { metric: 'pg.tps', slot: 'throughput', unit: 'tx/s', unavailability: 'NO_SAMPLES_YET', series: [] },
        { metric: 'pg.connection.saturation_percent', slot: 'connection_saturation', unit: 'percent', unavailability: 'NO_SAMPLES_YET', series: [] },
      ],
    },
  ],
}

describe('instance list column projection', () => {
  it('folds the agent into collection freshness, and only when the agent is failing', () => {
    // 在线与未安装都不是新鲜度的问题：没装 Agent 的实例本来就靠服务端直采。
    expect(agentFailureLabel('online')).toBeUndefined()
    expect(agentFailureLabel('not_installed')).toBeUndefined()
    expect(agentFailureLabel('offline')).toBe('Agent 离线')

    expect(collectionFreshnessLabel(instance({ data_freshness_seconds: 42 }))).toBe('42 秒前')
    expect(collectionFreshnessLabel(instance({ data_freshness_seconds: 120, agent_status: 'offline' })))
      .toBe('2 分钟前 · Agent 离线')
    expect(collectionFreshnessLabel(instance({ agent_status: 'error' }))).toBe('未知 · Agent 异常')
  })

  it('keeps the absolute collection time in the hover text, not in the cell', () => {
    const never = instance()
    expect(collectionFreshnessTitle(never)).toBe('未知 · 尚无成功采集')
    expect(collectionFreshnessTitle(never)).toContain(collectionFreshnessLabel(never))
  })

  it('never turns a missing sample into zero', () => {
    expect(dataFreshnessLabel(undefined)).toBe('未知')
    expect(connectionSaturationLabel(null)).toBe('—')
    expect(usageTone(null)).toBeUndefined()
    expect(trendValues(undefined)).toEqual([])
    expect(latestValue(undefined)).toBeNull()
  })

  it('reads connection saturation as a rounded percentage with two escalation bands', () => {
    expect(connectionSaturationLabel(87.4)).toBe('87%')
    expect(connectionSaturationLabel(0)).toBe('0%')
    expect(usageTone(10)).toBeUndefined()
    expect(usageTone(75)).toBe('warning')
    expect(usageTone(90)).toBe('critical')
  })

  it('picks each instance out of one batched trend response, addressed by semantic slot', () => {
    const first = '00000000-0000-4000-8000-000000000001'
    const second = '00000000-0000-4000-8000-000000000002'
    // 折线保留缺口，不补零：补零会把「没采到」画成「掉到 0」。
    expect(trendValues(instanceSlotEntry(trends, first, 'throughput'))).toEqual([12, null, 21])
    // 饱和度取最后一个真正有值的点，末尾的缺数不会把它抹成空。
    expect(latestValue(instanceSlotEntry(trends, first, 'connection_saturation'))).toBe(87.4)
    expect(trendValues(instanceSlotEntry(trends, second, 'throughput'))).toEqual([])
    expect(instanceSlotEntry(trends, 'missing-instance', 'throughput')).toBeUndefined()
    expect(instanceSlotEntry(undefined, first, 'throughput')).toBeUndefined()
  })

  it('says so when there is nothing to attribute', () => {
    expect(attributionLabel(instance())).toBe('无未恢复告警')
    expect(attributionLabel(instance({
      health: { ...instance().health, attribution: { rule_name: '连接数过高', current_value: 91 } },
    }))).toBe('连接数过高 (91)')
  })
})
