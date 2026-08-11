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

  await page.getByRole('link', { name: '长查询采样记录' }).click()
  await expect(page).toHaveURL((url) => inheritedContext(url))
  await expect(page.getByText('标识具有时效性')).toBeVisible()
  await expect(page.getByText('4242', { exact: true })).toBeVisible()
  await expect(page.getByText('select * from')).toHaveCount(0)

  await page.getByRole('link', { name: '查询统计排行' }).click()
  await expect(page).toHaveURL((url) => inheritedContext(url))
  await expect(page.getByText('987654321', { exact: true })).toBeVisible()
  await expect(page.getByText('select * from')).toHaveCount(0)

  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: '/tmp/issue-86-query-statistics-mobile.png', fullPage: true })
})

async function mockAPIs(page: Page) {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'admin', role: 'PLATFORM_ADMIN' } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({
    json: {
      id: instanceID,
      name: '生产库 primary',
      host: '10.20.1.15',
      port: 5432,
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
