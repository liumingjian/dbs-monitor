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

  await expect(page.getByTestId('metric-card')).toHaveCount(22)
  await expect(page.getByRole('figure')).toHaveCount(22)
  await expect(page.getByText('实际粒度：30s')).toHaveCount(22)
  await expect(page.getByTestId('metric-chart')).toHaveCount(22)
  await expectChartHasData(page.getByTestId('metric-chart').first())
  await expectChartHasData(page.getByTestId('metric-chart').last())
  await expectMultiColumn(page)
  expect(await hasPageOverflow(page)).toBe(false)

  await page.getByLabel('开始时间').fill('2026-08-11T09:00')
  await page.getByLabel('结束时间').fill('2026-08-11T12:00')
  await page.getByRole('button', { name: '应用时间范围' }).click()
  await expect(page).toHaveURL((url) => url.searchParams.get('from') === '2026-08-11T09:00:00.000Z' && url.searchParams.get('to') === '2026-08-11T12:00:00.000Z')
  await expect(page.getByTestId('metric-chart')).toHaveCount(22)
  await expectChartHasData(page.getByTestId('metric-chart').last())
  await page.screenshot({ path: '/tmp/issue-85-standard-monitoring-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectSingleColumn(page)
  await expect.poll(() => hasPageOverflow(page)).toBe(false)
  await page.screenshot({ path: '/tmp/issue-85-standard-monitoring-mobile.png', fullPage: true })
  expect(await pageOverflowElements(page)).toEqual([])
})

function unitForMetric(metric: string): string {
  if (metric.includes('percent')) return '%'
  if (metric.includes('bytes')) return 'bytes'
  return 'count'
}

/**
 * 每张图下方都有一个可展开的无障碍数据表。表里有数据行，就说明这张图真的收到了数据——
 * 这正是原先读 canvas 像素想证明的事，而数据表与 canvas/SVG 哪种渲染技术都无关。
 */
async function chartDataRows(chart: Locator): Promise<string[][]> {
  const table = chart.getByRole('table')
  if (!(await table.isVisible())) await chart.getByText('查看数据表').click()
  const rows = await table.getByRole('row').all()
  const cells = await Promise.all(rows.map((row) => row.getByRole('cell').allInnerTexts()))
  // 表头行只有 columnheader、没有 cell，因此在这里被过滤掉。
  return cells.filter((row) => row.length > 0)
}

/** 桩数据给每个指标 3 个采样点，所以数据表应当有 3 行、且没有一行是"缺数"。 */
async function expectChartHasData(chart: Locator): Promise<void> {
  await expect.poll(async () => {
    const values = (await chartDataRows(chart)).map((cells) => cells.at(-1) ?? '')
    return { rows: values.length, gaps: values.filter((value) => value.includes('缺数')).length }
  }).toEqual({ rows: 3, gaps: 0 })
}

/** 相邻两张卡片的位置就是"几列"的可观察结果：同一行则顶边相同、左边不同。 */
async function adjacentCardOrigins(page: Page): Promise<{ left: number; top: number }[]> {
  const cards = page.getByTestId('metric-card')
  return Promise.all([0, 1].map(async (index) => {
    const box = await cards.nth(index).boundingBox()
    if (!box) throw new Error(`metric-card ${index} has no bounding box`)
    return { left: box.x, top: box.y }
  }))
}

async function expectMultiColumn(page: Page): Promise<void> {
  await expect.poll(async () => {
    const [first, second] = await adjacentCardOrigins(page)
    return { sameRow: Math.abs(second.top - first.top) < 1, sideBySide: second.left > first.left }
  }).toEqual({ sameRow: true, sideBySide: true })
}

async function expectSingleColumn(page: Page): Promise<void> {
  await expect.poll(async () => {
    const [first, second] = await adjacentCardOrigins(page)
    return { sameColumn: Math.abs(second.left - first.left) < 1, stacked: second.top > first.top }
  }).toEqual({ sameColumn: true, stacked: true })
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
