import { expect, test } from '@playwright/test'
import { overviewMetricIDs } from '../src/routes/instances.$id/overview'

const instanceID = '11111111-1111-4111-8111-111111111111'
const from = '2026-08-11T10:00:00.000Z'
const to = '2026-08-11T11:00:00.000Z'

const instance = {
  id: instanceID,
  name: '生产库 primary',
  host: '10.20.1.15',
  port: 5432,
  database: 'orders',
  username: 'monitor',
  alert_status: 'FIRING',
  agent_metrics_enabled: true,
  agent_status: 'online',
  last_collected_at: '2026-08-11T10:59:30.000Z',
  data_freshness_seconds: 30,
  collection_pause: { paused: false },
  health: {
    status: 'CRITICAL',
    attribution: { rule_name: '复制延迟超阈值', current_value: 88 },
    counts: { critical: 1, warning: 2, info: 1 },
    flags: {
      no_data: false,
      in_maintenance: true,
      recently_recovered: false,
      ignored: 1,
      configuration_missing: 0,
    },
  },
}

test('overview reuses the list projection and provides seven contextual modules', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'reader', role: 'READONLY' } }))
  await page.route('**/api/v1/instances', (route) => route.fulfill({ json: [instance] }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({ json: instance }))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({
    json: {
      from,
      to,
      step: '30s',
      metrics: overviewMetricIDs.map((metric, index) => ({
        metric,
        unit: metric.includes('percent') ? '%' : 'count',
        unavailability: metric === 'pg.replication_slot.retained_wal_bytes' ? 'NOT_APPLICABLE_ROLE' : null,
        series: metric === 'pg.replication_slot.retained_wal_bytes' ? [] : [{ labels: {}, points: [[1786445970, index]] }],
      })),
    },
  }))
  await page.route('**/api/v1/instances/*/performance-events*', (route) => route.fulfill({ json: { total: 0, items: [] } }))

  await page.goto('/instances')
  const row = page.getByRole('row', { name: /生产库 primary/ })
  await expect(row).toContainText('严重')
  await expect(row).toContainText('复制延迟超阈值 (88)')
  await row.getByRole('link', { name: '总览' }).click()

  await expect(page).toHaveURL((url) => url.pathname === `/instances/${instanceID}` && url.searchParams.has('from') && url.searchParams.has('to'))
  await expect(page.locator('[data-overview-module]')).toHaveCount(7)
  await expect(page.getByRole('heading', { name: '复制延迟超阈值 (88)' })).toBeVisible()
  await expect(page.getByText('近期没有性能事件')).toBeVisible()
  await expect(page.getByText('当前角色不适用')).toBeVisible()
  await page.screenshot({ path: '/tmp/issue-88-overview-desktop.png', fullPage: true })

  const monitoring = new URL(await page.getByRole('link', { name: '标准监控' }).getAttribute('href') ?? '', 'https://dbs.test')
  expect(monitoring.pathname).toBe(`/instances/${instanceID}/monitoring`)
  expect(monitoring.searchParams.get('from')).toBeTruthy()
  expect(monitoring.searchParams.get('to')).toBeTruthy()

  const sessions = new URL(await page.getByRole('link', { name: '会话与阻塞' }).getAttribute('href') ?? '', 'https://dbs.test')
  expect(sessions.searchParams.get('filter')).toBe('lock_wait')
  expect(sessions.searchParams.get('from')).toBe(monitoring.searchParams.get('from'))
  expect(sessions.searchParams.get('to')).toBe(monitoring.searchParams.get('to'))

  const collection = new URL(await page.getByRole('link', { name: '采集状态' }).getAttribute('href') ?? '', 'https://dbs.test')
  expect(collection.pathname).toBe(`/instances/${instanceID}/collection`)

  const maintenance = new URL(await page.getByRole('link', { name: '新建维护窗口' }).getAttribute('href') ?? '', 'https://dbs.test')
  expect(maintenance.searchParams.get('instance_id')).toBe(instanceID)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/issue-88-overview-mobile.png', fullPage: true })
})
