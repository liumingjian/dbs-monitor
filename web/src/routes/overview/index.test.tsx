import { cleanup, render, screen } from '@testing-library/react'
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { parseInstanceListSearch } from '../instances/instanceListSearch'
import { parseSearch, stringifySearch } from '../searchParams'
import { CountTiles, StorageWatermarks } from './index'
import { collectionCountTiles, healthCountTiles } from './overview'

afterEach(cleanup)

/// 页面组件这一层只测一件事：**数字可点，而且链接带对了查询参数**。
/// 数字本身对不对是投影函数的用例（overview.test.ts）与接口的集成测试的事。
///
/// 断言的是解析后的 query 参数，不是拼出来的字符串——字段顺序变了不该让用例变红。
async function renderInRouter(content: ReactNode) {
  const testRootRoute = createRootRoute({ component: () => <Outlet /> })
  const pageRoute = createRoute({ getParentRoute: () => testRootRoute, path: '/', component: () => <>{content}</> })
  const listRoute = createRoute({
    getParentRoute: () => testRootRoute,
    path: '/instances',
    validateSearch: parseInstanceListSearch,
    component: () => null,
  })
  const detailRoute = createRoute({
    getParentRoute: () => testRootRoute,
    path: '/instances/$id',
    validateSearch: (search: Record<string, unknown>) => search,
    component: () => null,
  })
  const router = createRouter({
    routeTree: testRootRoute.addChildren([pageRoute, listRoute, detailRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
    // 真实应用的编解码，否则这里量到的地址不是使用者会看到的那个。
    parseSearch,
    stringifySearch,
  })
  // 路由器先把当前地址加载完再渲染：`RouterProvider` 在加载完成前渲染的是空的，
  // 用例会看到一个没有任何链接的页面。
  await router.load()
  render(<RouterProvider router={router as never} />)
}

function destination(name: RegExp): URL {
  const href = screen.getByRole('link', { name }).getAttribute('href')
  return new URL(href === null ? '' : href, 'https://dbs-monitor.test')
}

/// 一个可重复筛选参数在地址里的取值。
///
/// 清单在地址里是重复键（`?status=CRITICAL&status=WARNING`）；旧链接里的 JSON 写法
/// 也仍然认。断言的是解码之后的取值，「地址里带着 CRITICAL 这个筛选」才是
/// 外部可观察的行为。
function filterValues(url: URL, key: string): string[] {
  return url.searchParams.getAll(key).flatMap((raw) => {
    if (!raw.startsWith('[')) return [raw]
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.map(String) : [raw]
  })
}

describe('overview numbers', () => {
  it('links every health count to the list already filtered by that tier', async () => {
    await renderInRouter(
      <CountTiles
        label="机群健康计数"
        tiles={healthCountTiles({ critical: 3, warning: 5, unknown: 1, healthy: 40, paused: 2 })}
      />,
    )
    expect(screen.getAllByTestId('overview-count')).toHaveLength(5)

    const critical = destination(/严重/)
    expect(critical.pathname).toBe('/instances')
    expect(filterValues(critical, 'status')).toEqual(['CRITICAL'])

    const paused = destination(/已暂停/)
    expect(filterValues(paused, 'status')).toEqual(['PAUSED'])
    // 取默认值的字段不写进地址：这个链接是可以整条发给同事的那种地址。
    expect(paused.searchParams.get('page')).toBeNull()
    expect(paused.searchParams.get('sort')).toBeNull()
  })

  it('keeps a zero count clickable — the answer to "is anything critical" is a view, not a dead number', async () => {
    await renderInRouter(
      <CountTiles
        label="机群健康计数"
        tiles={healthCountTiles({ critical: 0, warning: 0, unknown: 0, healthy: 12, paused: 0 })}
      />,
    )
    expect(filterValues(destination(/严重/), 'status')).toEqual(['CRITICAL'])
  })

  it('links the three collection numbers to their list filters', async () => {
    await renderInRouter(
      <CountTiles label="采集自监控计数" tiles={collectionCountTiles({ stale_data: 7, agent_offline: 2, paused: 2 })} />,
    )
    expect(filterValues(destination(/数据不新鲜/), 'flags')).toEqual(['STALE_DATA'])
    expect(filterValues(destination(/Agent 离线/), 'flags')).toEqual(['AGENT_OFFLINE'])
    expect(filterValues(destination(/采集暂停/), 'status')).toEqual(['PAUSED'])
  })

  it('links each storage watermark to that instance', async () => {
    await renderInRouter(
      <StorageWatermarks
        entries={[
          {
            instance_id: '11111111-1111-4111-8111-111111111111',
            instance_name: 'pg-orders',
            usage_percent: 91.4,
            sampled_at: '2026-09-01T00:00:00Z',
          },
        ]}
      />,
    )
    expect(destination(/pg-orders/).pathname).toBe('/instances/11111111-1111-4111-8111-111111111111')
    expect(screen.getByText('91%')).toBeTruthy()
  })

  it('says the watermark is unmeasured instead of showing an empty list of zeroes', async () => {
    await renderInRouter(<StorageWatermarks entries={[]} />)
    expect(screen.getByText(/还没有磁盘水位读数/)).toBeTruthy()
  })
})
