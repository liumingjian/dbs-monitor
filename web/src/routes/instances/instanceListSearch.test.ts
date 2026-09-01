import { describe, expect, it } from 'vitest'
import {
  currentPage,
  currentPageSize,
  currentSort,
  defaultInstanceListSearch,
  hasInstanceFilters,
  instanceListQuery,
  parseInstanceListSearch,
  withInstanceFilters,
} from './instanceListSearch'

describe('instance list search params', () => {
  // 缺省视图就是一个空地址：默认值不写进地址栏，链接因此只带自己关心的筛选。
  it('keeps default page, page size and sort out of the address', () => {
    expect(parseInstanceListSearch({})).toEqual({})
    expect(parseInstanceListSearch({ page: '1', page_size: '50', sort: 'health' })).toEqual({})
    expect(defaultInstanceListSearch()).toEqual({})
    expect(currentPage({})).toBe(1)
    expect(currentPageSize({})).toBe(50)
    expect(currentSort({})).toBe('health')
  })

  it('accepts a repeated query parameter as one value or as many', () => {
    expect(parseInstanceListSearch({ status: 'CRITICAL' })).toEqual({ status: ['CRITICAL'] })
    expect(parseInstanceListSearch({ status: ['CRITICAL', 'WARNING'], flags: ['NO_DATA'], severity: ['critical'] })).toEqual({
      status: ['CRITICAL', 'WARNING'], flags: ['NO_DATA'], severity: ['critical'],
    })
  })

  it('reads page numbers back out of the address as numbers', () => {
    expect(parseInstanceListSearch({ page: '3', page_size: '25', q: 'db-7', sort: '-name' })).toEqual({
      page: 3, page_size: 25, q: 'db-7', sort: '-name',
    })
    expect(currentPage({ page: 3 })).toBe(3)
  })

  // 不认识的取值整体报错，不悄悄丢：悄悄丢等于同一个地址两个人看到两种结果，
  // 而这个地址存在的理由就是「发给同事」。
  it('rejects values it does not recognise instead of silently dropping them', () => {
    expect(parseInstanceListSearch({ status: ['CRITICAL', 'NOPE'] })).toEqual({ error: '健康档位筛选取值无效' })
    expect(parseInstanceListSearch({ flags: 'AGENT_OFFLINE' })).toEqual({ error: '标记筛选取值无效' })
    expect(parseInstanceListSearch({ severity: 'fatal' })).toEqual({ error: '告警级别筛选取值无效' })
    expect(parseInstanceListSearch({ engine: 'ORACLE' })).toEqual({ error: '引擎筛选取值无效' })
    expect(parseInstanceListSearch({ sort: 'name_asc' })).toEqual({ error: '排序取值无效' })
    expect(parseInstanceListSearch({ page: '0' })).toEqual({ error: '页码必须是正整数' })
    expect(parseInstanceListSearch({ page_size: '1000' })).toEqual({ error: '每页条数最多 500' })
  })

  // 请求里页码、页大小与排序永远是显式的（服务端不该去猜前端的默认值），
  // 而没设的筛选一项都不发。
  it('always sends paging and sort, and only the filters that are actually set', () => {
    expect(instanceListQuery({})).toEqual({ page: 1, page_size: 50, sort: 'health' })
    expect(instanceListQuery({ page: 2, page_size: 25 })).toEqual({ page: 2, page_size: 25, sort: 'health' })
    expect(instanceListQuery({ sort: 'stalest', q: 'db', status: ['CRITICAL'], severity: [] })).toEqual({
      page: 1, page_size: 50, sort: 'stalest', q: 'db', status: ['CRITICAL'],
    })
  })

  it('returns to the first page whenever the filters change', () => {
    const current = { page: 4, page_size: 25, status: ['CRITICAL' as const] }
    expect(withInstanceFilters(current, { severity: ['warning'] })).toEqual({
      page_size: 25, status: ['CRITICAL'], severity: ['warning'],
    })
    // 清空一个筛选就把它从地址里拿掉，而不是留下一个空数组。
    expect(withInstanceFilters(current, { status: [] })).toEqual({ page_size: 25 })
  })

  it('knows whether anything is filtered at all', () => {
    expect(hasInstanceFilters(defaultInstanceListSearch())).toBe(false)
    expect(hasInstanceFilters({ sort: 'name', q: 'db' })).toBe(true)
  })
})
