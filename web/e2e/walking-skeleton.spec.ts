import { expect, test } from '@playwright/test'

const instanceName = 'T11 smoke instance'

test('standard monitoring workbench renders 22 charts and keeps controls in URL state', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('t11-playwright-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()

  await expect(page).toHaveURL(/\/instances$/)
  await expect(page.getByRole('heading', { name: 'PostgreSQL 实例' })).toBeVisible()
  await page.getByRole('row', { name: new RegExp(instanceName) }).getByRole('link', { name: '总览' }).click()

  await expect(page.getByRole('tab', { name: '实例总览' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('link', { name: '监控与报警' }).click()

  await expect(page.getByRole('tab', { name: '标准监控' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('.metric-card')).toHaveCount(22)
  await expect(page.getByRole('region', { name: '资源指标' }).locator('.metric-card')).toHaveCount(5)
  await expect(page.getByRole('region', { name: '数据库指标' }).locator('.metric-card')).toHaveCount(12)
  await expect(page.getByRole('region', { name: '复制指标' }).locator('.metric-card')).toHaveCount(5)

  const cpuCard = page.locator('.metric-card').filter({ has: page.getByText('CPU', { exact: true }) })
  const cpuChart = cpuCard.getByRole('figure', { name: 'CPU趋势' })
  await expect(cpuChart).toBeVisible()
  await cpuChart.getByText('查看数据表').click()
  await expect(cpuChart.locator('tbody tr').first()).toBeVisible()

  await cpuCard.getByRole('button', { name: /指标详情/ }).click()
  await expect(page.getByRole('dialog', { name: 'CPU' })).toContainText('host.cpu.usage_percent')
  await page.getByRole('button', { name: 'Close' }).click()

  const memoryCard = page.locator('.metric-card').filter({ has: page.getByText('内存', { exact: true }) })
  await expect(memoryCard.getByText('等待首个样本')).toBeVisible()

  const slowQueryCard = page.locator('.metric-card').filter({ has: page.getByText('长查询数量', { exact: true }) })
  const drilldown = slowQueryCard.getByRole('link', { name: /查看采样记录/ })
  await expect(drilldown).toHaveAttribute('href', /\/sessions\/long-query-samples\?/)
  await expect(drilldown).toHaveAttribute('href', /metric=pg.query.long_running_count/)

  const stepRequest = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/metrics/series') && url.searchParams.get('step') === '1m'
  })
  await page.getByLabel('数据粒度').click()
  await page.getByText('1 分钟', { exact: true }).click()
  await stepRequest
  await expect(page).toHaveURL((url) => url.searchParams.get('step') === '1m')
  await expect(cpuChart.getByText('实际粒度：1m')).toBeVisible()

  await page.getByLabel('图表列数').getByText('3 列').click()
  await expect(page).toHaveURL((url) => url.searchParams.get('columns') === '3')
  await page.getByLabel('光标联动').click()
  await expect(page).toHaveURL((url) => url.searchParams.get('connect') === 'false')

  const start = new Date(Date.now() - 30 * 60 * 1000)
  const end = new Date(Date.now() + 30 * 60 * 1000)
  start.setSeconds(0, 0)
  end.setSeconds(0, 0)
  const from = start.toISOString()
  const to = end.toISOString()
  const formatInput = (value: Date) => {
    const part = (number: number) => String(number).padStart(2, '0')
    return `${value.getFullYear()}-${part(value.getMonth() + 1)}-${part(value.getDate())}T${part(value.getHours())}:${part(value.getMinutes())}`
  }
  const rangeRequest = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/metrics/series') && url.searchParams.get('from') === from && url.searchParams.get('to') === to
  })
  await page.getByLabel('开始时间').fill(formatInput(start))
  await page.getByLabel('结束时间').fill(formatInput(end))
  await page.getByRole('button', { name: '应用时间范围' }).click()
  await rangeRequest
  await expect(page).toHaveURL((url) => url.searchParams.get('from') === from && url.searchParams.get('to') === to)
  await expect(cpuChart).toBeVisible()

  await page.goto(`/instances/not-used/monitoring?from=last-hour&to=bad&step=10m`)
  await expect(page.getByText('时间范围必须是绝对 RFC3339 时间')).toBeVisible()
  await expect(page.getByRole('button', { name: '使用最近一小时' })).toBeVisible()
})
