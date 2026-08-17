import { expect, test, type Locator } from '@playwright/test'
import { enhancedMonitoringMetricIDs } from '../src/routes/instances.$id/enhancedMonitoring'

const instanceID = '11111111-1111-4111-8111-111111111111'
const from = '2026-08-11T10:30:00.000Z'
const to = '2026-08-11T11:00:00.000Z'

test('enhanced monitoring opens raw curves and isolates protected collection gaps', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'readonly', role: 'READONLY' } }))
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
  await page.route(`**/api/v1/instances/${instanceID}/collection/tasks`, (route) => route.fulfill({
    json: [{
      task_id: 'pg.stat_database',
      kind: 'sql',
      interval_seconds: 5,
      last_result: 'SKIPPED_BACKPRESSURE',
      consecutive_failures: 1,
      metric_ids: ['pg.tps'],
      required_capabilities: ['role.pg_monitor'],
    }],
  }))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => {
    const url = new URL(route.request().url())
    expect(url.searchParams.get('step')).toBe('raw')
    expect(url.searchParams.getAll('metric')).toEqual(enhancedMonitoringMetricIDs)
    return route.fulfill({
      json: {
        from,
        to,
        step: 'raw',
        metrics: enhancedMonitoringMetricIDs.map((metric) => {
          if (metric === 'host.cpu.usage_percent') {
            return {
              metric,
              unit: 'percent',
              unavailability: null,
              series: [{ labels: { node: 'db-01' }, points: [[1786444200, 42], [1786444205, 47], [1786444210, 44]] }],
            }
          }
          return {
            metric,
            unit: 'count',
            unavailability: metric === 'pg.tps' ? 'COLLECTION_FAILED' : 'NO_DATA_IN_RANGE',
            series: [],
          }
        }),
      },
    })
  })

  await page.goto(`/instances/${instanceID}/monitoring?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&monitoring=enhanced&step=raw`)

  await expect(page.getByRole('tab', { name: '增强监控' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('.enhanced-metric-card')).toHaveCount(27)
  await expect(page.getByRole('figure', { name: 'CPU 使用率趋势' })).toBeVisible()
  await expect(page.getByText('实际粒度：raw')).toBeVisible()
  await expect(page.getByText(/平台自我保护：最近一次采集因背压被跳过/)).toBeVisible()
  await expect.poll(() => canvasHasPixels(page.locator('canvas').first())).toBe(true)
})

async function canvasHasPixels(canvas: Locator): Promise<boolean> {
  return canvas.evaluate((element) => {
    if (!(element instanceof HTMLCanvasElement)) return false
    const context = element.getContext('2d')
    if (!context || element.width === 0 || element.height === 0) return false
    const pixels = context.getImageData(0, 0, element.width, element.height).data
    for (let index = 3; index < pixels.length; index += 4) {
      if (pixels[index] > 0) return true
    }
    return false
  })
}
