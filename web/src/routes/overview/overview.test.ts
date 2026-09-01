import { describe, expect, it } from 'vitest'
import { parseInstanceListSearch } from '../instances/instanceListSearch'
import type { FleetCollectionHealth, FleetHealthCounts } from './overview'
import { collectionCountTiles, healthCountTiles, storageRatio, storageTone, usagePercentLabel } from './overview'

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

  it('reads a watermark as a whole percent and only colours the top two bands', () => {
    expect(usagePercentLabel(91.4)).toBe('91%')
    expect(storageTone(91)).toBe('critical')
    expect(storageTone(75)).toBe('warning')
    expect(storageTone(74.9)).toBeUndefined()
    expect(storageRatio(40)).toBeCloseTo(0.4)
  })
})
