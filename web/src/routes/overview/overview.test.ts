import { describe, expect, it } from 'vitest'
import { parseInstanceListSearch } from '../../domain/instanceListSearch'
import { usageTone } from '../../domain/instanceProjection'
import type { TopSqlEntry } from '../../domain/topSql'
import type { FleetCollectionHealth, FleetHealthCounts } from './overview'
import {
  collectionCountTiles,
  healthCountTiles,
  storageRatio,
  topSqlSummaries,
  usagePercentLabel,
} from './overview'

const counts: FleetHealthCounts = { critical: 3, warning: 5, unknown: 1, healthy: 40, paused: 2 }
const collection: FleetCollectionHealth = { stale_data: 7, agent_offline: 2, paused: 2 }

describe('fleet overview projections', () => {
  it('keeps the five health tiers complete, ordered and labelled', () => {
    expect(healthCountTiles(counts).map((tile) => [tile.label, tile.count])).toEqual([
      ['严重', 3],
      ['警告', 5],
      ['未知', 1],
      ['正常', 40],
      ['已暂停', 2],
    ])
  })

  it('shows a zero tier rather than hiding it', () => {
    const quiet = healthCountTiles({ critical: 0, warning: 0, unknown: 0, healthy: 12, paused: 0 })
    expect(quiet).toHaveLength(5)
    expect(quiet[0]).toMatchObject({ label: '严重', count: 0 })
  })

  it('drills each health tier into the list filtered by that tier', () => {
    expect(healthCountTiles(counts).map((tile) => tile.search)).toEqual([
      { status: ['CRITICAL'] },
      { status: ['WARNING'] },
      { status: ['UNKNOWN'] },
      { status: ['HEALTHY'] },
      { status: ['PAUSED'] },
    ])
  })

  it('drills the three collection numbers into the list', () => {
    expect(collectionCountTiles(collection)).toEqual([
      { key: 'stale_data', label: '数据不新鲜', count: 7, tone: 'warning', search: { flags: ['STALE_DATA'] } },
      { key: 'agent_offline', label: 'Agent 离线', count: 2, tone: 'warning', search: { flags: ['AGENT_OFFLINE'] } },
      { key: 'collection_paused', label: '采集暂停', count: 2, tone: 'unknown', search: { status: ['PAUSED'] } },
    ])
  })

  // 下钻地址必须是实例列表自己认得的地址：它的 validateSearch 用的就是这个解析函数，
  // 解析不出同一个对象就说明总览发出了一个列表看不懂的链接。
  it('produces addresses the instance list accepts unchanged', () => {
    for (const tile of [...healthCountTiles(counts), ...collectionCountTiles(collection)]) {
      expect(parseInstanceListSearch({ ...tile.search })).toEqual(tile.search)
    }
  })

  // 取默认值的字段不进地址栏：`/instances?status=CRITICAL` 与
  // `?page=1&page_size=50&sort=health&status=CRITICAL` 是同一个视图，短的那个才是可以发给同事的。
  it('leaves defaulted fields out of the address', () => {
    for (const tile of healthCountTiles(counts)) {
      expect(Object.keys(tile.search)).toEqual(['status'])
    }
  })

  it('reads the top five statements as text, elapsed time and a share of the worst one', () => {
    const entries: TopSqlEntry[] = [
      {
        instance_id: '11111111-1111-4111-8111-111111111111',
        instance_name: '订单库主库',
        queryid: '42',
        query_text: 'UPDATE orders SET state = $1 WHERE id = $2',
        calls: 900,
        total_exec_time_ms: 120_000,
      },
      {
        instance_id: '22222222-2222-4222-8222-222222222222',
        instance_name: '账务库',
        queryid: '-7',
        calls: 12,
        total_exec_time_ms: 30_000,
      },
    ]
    expect(topSqlSummaries(entries)).toEqual([
      {
        key: '11111111-1111-4111-8111-111111111111:42',
        statement: 'UPDATE orders SET state = $1 WHERE id = $2',
        elapsed: '2.0 min',
        caption: '订单库主库 · 900 次调用',
        ratio: 1,
      },
      {
        key: '22222222-2222-4222-8222-222222222222:-7',
        statement: 'queryid -7（未采到 SQL 文本）',
        elapsed: '30.0 s',
        caption: '账务库 · 12 次调用',
        ratio: 0.25,
      },
    ])
  })

  // 榜首耗时为 0（统计刚被重置）时比例条是空的，而不是除出一个 NaN 宽度。
  it('draws empty bars rather than NaN when nothing has cost any time yet', () => {
    const entry: TopSqlEntry = {
      instance_id: '33333333-3333-4333-8333-333333333333',
      instance_name: '只读库',
      queryid: '1',
      query_text: 'SELECT 1',
      calls: 0,
      total_exec_time_ms: 0,
    }
    expect(topSqlSummaries([entry])[0]).toMatchObject({ ratio: 0, elapsed: '0.0 ms' })
  })

  it('reads storage usage as a whole percent and only colours the top two bands', () => {
    expect(usagePercentLabel(91.4)).toBe('91%')
    expect(usageTone(91)).toBe('critical')
    expect(usageTone(75)).toBe('warning')
    expect(usageTone(74.9)).toBeUndefined()
    expect(storageRatio(40)).toBeCloseTo(0.4)
  })
})
