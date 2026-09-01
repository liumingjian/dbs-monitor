import { describe, expect, it } from 'vitest'
import { parseSearch, stringifySearch } from './searchParams'

describe('searchParams', () => {
  // 多选筛选写成重复键：这是能整条发给同事的写法，也是服务端读同一批参数的写法。
  it('writes a repeated key per list value', () => {
    expect(stringifySearch({ status: ['CRITICAL', 'WARNING'] })).toBe('?status=CRITICAL&status=WARNING')
    expect(stringifySearch({ q: '10.0.0.7', page: 2, connect: true })).toBe('?q=10.0.0.7&page=2&connect=true')
    expect(stringifySearch({ page: undefined })).toBe('')
  })

  it('reads repeated keys back as a list and single keys as a scalar', () => {
    expect(parseSearch('?status=CRITICAL&status=WARNING')).toEqual({ status: ['CRITICAL', 'WARNING'] })
    expect(parseSearch('?status=CRITICAL')).toEqual({ status: 'CRITICAL' })
    expect(parseSearch('?page=2&connect=true&from=2026-08-11T10:00:00.000Z')).toEqual({
      page: 2,
      connect: true,
      from: '2026-08-11T10:00:00.000Z',
    })
    expect(parseSearch('')).toEqual({})
  })

  it('round-trips every value it writes', () => {
    const search = { q: 'orders', page: 3, connect: false, status: ['CRITICAL', 'PAUSED'] }
    expect(parseSearch(stringifySearch(search))).toEqual(search)
  })

  // 改编码之前发出去的地址还在别人的聊天记录里，仍然要打开成同一个视图。
  it('still understands the JSON list encoding written by older links', () => {
    expect(parseSearch('?status=%5B%22CRITICAL%22%5D')).toEqual({ status: ['CRITICAL'] })
  })
})
