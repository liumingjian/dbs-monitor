import { expect, test, type Page } from '@playwright/test'

const instanceID = '11111111-1111-4111-8111-111111111111'
const from = '2026-08-11T10:00:00.000Z'
const to = '2026-08-11T11:00:00.000Z'
const sampledAt = '2026-08-11T10:55:00.000Z'

test('sessions, long-query samples, and query statistics preserve investigation context', async ({ page }) => {
  await mockAPIs(page)
  const context = `from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&metric=pg.query.long_running_count&sampled_at=${encodeURIComponent(sampledAt)}`

  await page.goto(`/instances/${instanceID}/sessions?${context}`)
  await expect(page.getByText('采集时间：')).toContainText('2026')
  await expect(page.getByLabel('数据新鲜')).toBeVisible()
  await expect(page.getByRole('tab', { name: /活跃会话 1/ })).toBeVisible()
  await expect(page.getByText('快照已截断')).toBeVisible()
  await expect(page.getByText('select * from')).toHaveCount(0)

  // 三个视图合并成一个多标签页面之后，页签是 `role="tab"` 的真锚点而不是 `role="link"`
  // （web/CLAUDE.md 的先例：`<Tab as={链接组件}>`）。定位方式跟着角色走，断言的行为没变：
  // 点它会导航，并且调查上下文一个参数都不丢。
  await page.getByRole('tab', { name: '长查询采样记录' }).click()
  await expect(page).toHaveURL((url) => inheritedContext(url) && url.searchParams.get('tab') === 'long-query-samples')
  await expect(page.getByText('标识具有时效性')).toBeVisible()
  await expect(page.getByText('4242', { exact: true })).toBeVisible()
  await expect(page.getByText('select * from')).toHaveCount(0)

  await page.getByRole('tab', { name: '查询统计排行' }).click()
  await expect(page).toHaveURL((url) => inheritedContext(url) && url.searchParams.get('tab') === 'query-statistics')
  await expect(page.getByText('987654321', { exact: true })).toBeVisible()
  await expect(page.getByText('select * from')).toHaveCount(0)

  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: '/tmp/issue-86-query-statistics-mobile.png', fullPage: true })
})

/// 合并没有让任何一条旧地址失效：两个旧子地址都重定向到对应标签，调查上下文原样带过去。
test('the merged sessions page keeps every old address working', async ({ page }) => {
  await mockAPIs(page)
  const context = `from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&metric=pg.query.long_running_count&sampled_at=${encodeURIComponent(sampledAt)}`

  for (const [oldPath, tab, tabName] of [
    ['long-query-samples', 'long-query-samples', '长查询采样记录'],
    ['query-statistics', 'query-statistics', '查询统计排行'],
  ] as const) {
    await page.goto(`/instances/${instanceID}/sessions/${oldPath}?${context}`)
    await expect(page).toHaveURL((url) =>
      url.pathname === `/instances/${instanceID}/sessions` && inheritedContext(url) && url.searchParams.get('tab') === tab)
    await expect(page.getByRole('tab', { name: tabName })).toHaveAttribute('aria-selected', 'true')
  }

  // 会话快照那一档的地址从来没有变过，实例总览的锁等待下钻照旧落在对应切面上。
  await page.goto(`/instances/${instanceID}/sessions?${context}&filter=lock_wait`)
  await expect(page.getByRole('tab', { name: '当前会话' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tab', { name: /锁等待 1/ })).toHaveAttribute('aria-selected', 'true')
})

async function mockAPIs(page: Page) {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'admin', role: 'PLATFORM_ADMIN' } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({
    json: {
      id: instanceID,
      name: '生产库 primary',
      host: '10.20.1.15',
      port: 5432,
      engine: 'POSTGRESQL',
      database: 'orders',
      username: 'monitor',
      alert_status: 'OK',
      agent_metrics_enabled: true,
    },
  }))
  await page.route(`**/api/v1/instances/${instanceID}/sessions`, (route) => route.fulfill({
    json: {
      sampled_at: sampledAt,
      original_count: 620,
      truncated: true,
      items: [{
        pid: 4242,
        username: 'app_user',
        database_name: 'orders',
        client_address: '10.20.2.8',
        state: 'active',
        query_started_at: '2026-08-11T10:54:30.000Z',
        transaction_started_at: '2026-08-11T10:49:00.000Z',
        query_duration_ms: 30_000,
        transaction_duration_ms: 360_000,
        wait_event_type: 'Lock',
        wait_event: 'transactionid',
        blocking_pids: [4100],
      }],
    },
  }))
  await page.route('**/api/v1/instances/*/long-query-samples*', (route) => route.fulfill({
    json: {
      total: 1,
      items: [{
        sampled_at: sampledAt,
        pid: 4242,
        username: 'app_user',
        database_name: 'orders',
        state: 'active',
        query_started_at: '2026-08-11T10:54:30.000Z',
        query_duration_ms: 30_000,
        blocking_pids: [],
        snapshot_original_count: 1,
        snapshot_truncated: false,
      }],
    },
  }))
  await page.route(`**/api/v1/instances/${instanceID}/query-stats`, (route) => route.fulfill({
    json: {
      sampled_at: sampledAt,
      items: [{ queryid: '987654321', database_oid: 16384, user_oid: 16385, calls: 82, total_exec_time_ms: 912.4 }],
    },
  }))
}

function inheritedContext(url: URL): boolean {
  return url.searchParams.get('from') === from &&
    url.searchParams.get('to') === to &&
    url.searchParams.get('metric') === 'pg.query.long_running_count' &&
    url.searchParams.get('sampled_at') === sampledAt
}
