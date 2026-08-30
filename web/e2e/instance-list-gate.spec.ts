import { expect, test } from '@playwright/test'

const healthStatuses = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const

function mockInstance(index: number) {
  const status = healthStatuses[index % healthStatuses.length]
  const sequence = String(index + 1).padStart(3, '0')
  return {
    id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, '0')}`,
    name: `gate-instance-${sequence}`,
    host: `10.0.0.${index + 1}`,
    port: 5432,
    database: 'postgres',
    username: 'monitor',
    agent_metrics_enabled: true,
    alert_status: status === 'CRITICAL' || status === 'WARNING' ? 'FIRING' : 'OK',
    agent_status: index % 2 === 0 ? 'online' : 'not_installed',
    last_collected_at: '2026-08-11T12:00:00Z',
    data_freshness_seconds: index + 1,
    collection_pause: status === 'PAUSED'
      ? { paused: true, updated_at: '2026-08-01T12:00:00Z' }
      : { paused: false },
    health: {
      status,
      attribution: status === 'CRITICAL' || status === 'WARNING'
        ? { rule_name: `gate rule ${sequence}`, current_value: 80 + index }
        : undefined,
      counts: {
        critical: status === 'CRITICAL' ? 1 : 0,
        warning: status === 'WARNING' ? 1 : 0,
        info: index % 7 === 0 ? 1 : 0,
      },
      flags: {
        no_data: index % 11 === 0,
        in_maintenance: index % 13 === 0,
        recently_recovered: index % 17 === 0,
        ignored: index % 19 === 0 ? 1 : 0,
        configuration_missing: index % 23 === 0 ? 1 : 0,
      },
    },
  }
}

test('AntD gate renders and filters 50 instance projections', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: {
    id: '00000000-0000-4000-8000-000000000001',
    username: 'gate-reader',
    role: 'READONLY',
    enabled: true,
    created_at: '2026-08-11T12:00:00Z',
  } }))
  await page.route('**/api/v1/instances', (route) => route.fulfill({ json: Array.from({ length: 50 }, (_, index) => mockInstance(index)) }))

  const startedAt = Date.now()
  await page.goto('/instances')
  await expect(page.getByTestId('instance-row')).toHaveCount(50)
  test.info().annotations.push({ type: 'render-ms', description: String(Date.now() - startedAt) })
  await expect(page.getByText('gate-instance-050')).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('instance-list-desktop.png'), fullPage: true })

  await page.getByLabel('主状态').click()
  await page.getByTestId('health-status-option-CRITICAL').click()
  await expect(page.getByTestId('instance-row')).toHaveCount(10)
  await page.mouse.click(8, 500)
  await expect(page.getByTestId('health-status-option-CRITICAL')).toBeHidden()

  await page.getByRole('button', { name: '清除筛选' }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByTestId('instance-row')).toHaveCount(50)
  await page.screenshot({ path: test.info().outputPath('instance-list-mobile.png'), fullPage: true })
})
