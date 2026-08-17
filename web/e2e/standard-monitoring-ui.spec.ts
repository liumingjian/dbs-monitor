import { expect, test, type Locator, type Page } from '@playwright/test'
import { standardMonitoringMetricIDs } from '../src/routes/instances.$id/standardMonitoring'

const instanceID = '11111111-1111-4111-8111-111111111111'
const from = '2026-08-11T10:00:00.000Z'
const to = '2026-08-11T11:00:00.000Z'

test('22 connected charts remain usable on desktop and mobile', async ({ page }) => {
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
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({
    json: {
      from,
      to,
      step: '30s',
      metrics: standardMonitoringMetricIDs.map((metric, index) => ({
        metric,
        unit: unitForMetric(metric),
        unavailability: null,
        series: [{
          labels: {},
          points: [[1786442400, index + 1], [1786442430, index + 3], [1786442460, index + 2]],
        }],
      })),
    },
  }))

  await page.goto(`/instances/${instanceID}/monitoring?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&step=auto&columns=3&connect=true`)

  await expect(page.locator('.metric-card')).toHaveCount(22)
  await expect(page.getByRole('figure')).toHaveCount(22)
  await expect(page.getByText('实际粒度：30s')).toHaveCount(22)
  await expect(page.locator('canvas')).toHaveCount(22)
  await expect.poll(() => canvasHasPixels(page.locator('canvas').first())).toBe(true)
  await expect.poll(() => canvasHasPixels(page.locator('canvas').last())).toBe(true)
  expect(await hasPageOverflow(page)).toBe(false)

  await page.getByLabel('开始时间').fill('2026-08-11T09:00')
  await page.getByLabel('结束时间').fill('2026-08-11T12:00')
  await page.getByRole('button', { name: '应用时间范围' }).click()
  await expect(page).toHaveURL((url) => url.searchParams.get('from') === '2026-08-11T09:00:00.000Z' && url.searchParams.get('to') === '2026-08-11T12:00:00.000Z')
  await expect(page.locator('canvas')).toHaveCount(22)
  await expect.poll(() => canvasHasPixels(page.locator('canvas').last())).toBe(true)
  await page.screenshot({ path: '/tmp/issue-85-standard-monitoring-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.metric-grid').first()).toHaveCSS('grid-template-columns', /\d+px/)
  await expect.poll(() => hasPageOverflow(page)).toBe(false)
  await page.screenshot({ path: '/tmp/issue-85-standard-monitoring-mobile.png', fullPage: true })
  expect(await pageOverflowElements(page)).toEqual([])
})

function unitForMetric(metric: string): string {
  if (metric.includes('percent')) return '%'
  if (metric.includes('bytes')) return 'bytes'
  return 'count'
}

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

async function hasPageOverflow(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)
}

async function pageOverflowElements(page: Page): Promise<string[]> {
  return page.evaluate(() => [...document.querySelectorAll('body *')].flatMap((element) => {
    const bounds = element.getBoundingClientRect()
    if (bounds.left >= -1 && bounds.right <= document.documentElement.clientWidth + 1) return []
    // 被祖先容器横向裁剪的元素（如 AntD 页签导航的滚动区）不会造成页面横向滚动，不算溢出
    for (let ancestor = element.parentElement; ancestor; ancestor = ancestor.parentElement) {
      if (getComputedStyle(ancestor).overflowX !== 'visible') {
        const box = ancestor.getBoundingClientRect()
        if (box.left >= -1 && box.right <= document.documentElement.clientWidth + 1) return []
      }
    }
    return [`${element.tagName}.${element.className} left=${bounds.left} right=${bounds.right} width=${bounds.width}`]
  }))
}
