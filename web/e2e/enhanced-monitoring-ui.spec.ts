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
  await expect(page.getByTestId('enhanced-metric-card')).toHaveCount(27)
  await expect(page.getByRole('figure', { name: 'CPU 使用率趋势' })).toBeVisible()
  await expect(page.getByText('实际粒度：raw')).toBeVisible()
  await expect(page.getByText(/平台自我保护：最近一次采集因背压被跳过/)).toBeVisible()
  // 唯一有数据的指标（CPU 使用率）真的把桩里的 42/47/44 画了出来。
  await expect.poll(() => chartValues(page.getByRole('figure', { name: 'CPU 使用率趋势' }))).toEqual(['42%', '47%', '44%'])
})

/**
 * 每张图下方都有一个可展开的无障碍数据表。表里的数值就是这张图收到的数据——
 * 这正是原先读 canvas 像素想证明的事，而数据表与 canvas/SVG 哪种渲染技术都无关。
 */
async function chartValues(chart: Locator): Promise<string[]> {
  const table = chart.getByRole('table')
  if (!(await table.isVisible())) await chart.getByText('查看数据表').click()
  const rows = await table.getByRole('row').all()
  const cells = await Promise.all(rows.map((row) => row.getByRole('cell').allInnerTexts()))
  // 表头行只有 columnheader、没有 cell，因此在这里被过滤掉；每行最后一列是数值。
  return cells.filter((row) => row.length > 0).map((row) => row[row.length - 1])
}
