import { expect, test } from '@playwright/test'

const instanceID = '10000000-0000-4000-8000-000000000001'
const activeAlertID = '20000000-0000-4000-8000-000000000001'
const pausedAlertID = '20000000-0000-4000-8000-000000000002'

function alertObservation(id: string, paused: boolean) {
  return {
    id,
    instance_id: instanceID,
    instance_name: 'payments-primary',
    rule_id: '30000000-0000-4000-8000-000000000001',
    rule_name: paused ? 'Paused connection pressure' : 'Connection pressure',
    rule_version: 2,
    rule_snapshot: { name: 'Connection pressure', threshold: 80 },
    metric_id: 'pg.connection.total',
    status: 'FIRING',
    severity: 'critical',
    disposition: 'ACKED',
    paused,
    paused_at: paused ? '2026-08-01T00:00:00Z' : undefined,
    current_value: 96,
    threshold: 80,
    first_triggered_at: '2026-08-11T10:15:00Z',
    updated_at: '2026-08-11T10:45:00Z',
    duration_ms: 1_800_000,
  }
}

test('global alert lists reuse detail and preserve trigger context into monitoring', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'reader', role: 'READONLY' } }))
  await page.route('**/api/v1/alerts/current*', (route) => {
    const includePaused = new URL(route.request().url()).searchParams.get('include_paused') === 'true'
    const items = [alertObservation(activeAlertID, false)]
    if (includePaused) items.push(alertObservation(pausedAlertID, true))
    return route.fulfill({ json: { total: items.length, items } })
  })
  await page.route('**/api/v1/alerts/history*', (route) => route.fulfill({ json: { total: 0, items: [] } }))
  await page.route(`**/api/v1/alert-instances/${activeAlertID}`, (route) => route.fulfill({
    json: {
      ...alertObservation(activeAlertID, false),
      rule_version_history: [{ version: 1, snapshot: { threshold: 75 }, evaluated_at: '2026-08-11T10:15:00Z' }],
      notification_results: [],
    },
  }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({
    json: {
      id: instanceID,
      name: 'payments-primary',
      host: '127.0.0.1',
      port: 5432,
      database: 'payments',
      username: 'monitor',
      agent_metrics_enabled: true,
      alert_status: 'FIRING',
      health: {
        status: 'CRITICAL',
        counts: { critical: 1, warning: 0, info: 0 },
        flags: { no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0 },
      },
      agent_status: 'online',
      last_collected_at: '2026-08-11T10:45:00Z',
      collection_pause: { paused: false },
    },
  }))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({
    json: {
      from: '2026-08-11T10:15:00Z',
      to: '2026-08-11T10:45:00Z',
      step: '1m',
      metrics: [{
        metric: 'pg.connection.total',
        unit: 'count',
        unavailability: null,
        series: [{ labels: {}, points: [[1786443300, 96]] }],
      }],
    },
  }))

  await page.goto('/alerts')
  await expect(page.getByRole('heading', { name: '全局告警' })).toBeVisible()
  await expect(page.getByTestId('alert-row')).toHaveCount(1)
  await expect(page.getByText('Paused connection pressure')).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('global-alerts-desktop.png'), fullPage: true })

  await page.getByLabel('包含已暂停冻结告警').click()
  await expect(page).toHaveURL((url) => url.searchParams.get('include_paused') === 'true')
  await expect(page.getByTestId('alert-row')).toHaveCount(2)
  await expect(page.getByText('Paused connection pressure')).toBeVisible()

  await page.getByRole('row', { name: /Connection pressure/ }).getByRole('link', { name: '详情' }).click()
  await expect(page).toHaveURL(`/instances/${instanceID}/alerts/${activeAlertID}`)
  await expect(page.getByRole('heading', { name: '触发指标' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '规则快照' })).toBeVisible()
  await expect(page.getByText('暂无通知结果')).toBeVisible()

  const monitoring = page.getByRole('link', { name: /查看标准监控/ })
  await expect(monitoring).toHaveAttribute('href', new RegExp(`/instances/${instanceID}/monitoring\\?`))
  const href = await monitoring.getAttribute('href')
  const destination = new URL(href!, 'https://example.test')
  expect(destination.searchParams.get('metric')).toBe('pg.connection.total')
  expect(destination.searchParams.get('from')).toBe('2026-08-11T10:15:00.000Z')
  expect(destination.searchParams.get('to')).toBe('2026-08-11T10:45:00.000Z')

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: '触发指标' })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('alert-detail-mobile.png'), fullPage: true })
})
