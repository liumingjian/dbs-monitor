import { expect, test } from '@playwright/test'

const instanceName = 'T11 smoke instance'

test('[AC-01-S1] real pg_stat_database samples reach a chart and keep URL and query time ranges in sync', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('t11-playwright-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()

  await expect(page).toHaveURL(/\/instances$/)
  await expect(page.getByRole('heading', { name: 'PostgreSQL 实例' })).toBeVisible()
  await page.getByRole('row', { name: new RegExp(instanceName) }).getByRole('link', { name: /监控$/ }).click()

  const metricSelect = page.getByLabel('指标')
  await metricSelect.click()
  // TPS follows the three connection metrics in the non-searchable select.
  await metricSelect.press('ArrowDown')
  await metricSelect.press('ArrowDown')
  await metricSelect.press('ArrowDown')
  await metricSelect.press('Enter')
  await expect(page).toHaveURL((url) => url.searchParams.get('metric') === 'pg.tps')

  const chart = page.getByRole('figure', { name: 'TPS趋势' })
  await expect(chart).toBeVisible()
  await chart.getByText('查看数据表').click()
  await expect(chart.locator('tbody tr').first()).toBeVisible()

  const start = new Date(Date.now() - 30 * 60 * 1000)
  const end = new Date(Date.now() + 30 * 60 * 1000)
  start.setMilliseconds(0)
  end.setMilliseconds(0)
  const from = start.toISOString()
  const to = end.toISOString()
  const formatInput = (value: Date) => {
    const part = (number: number) => String(number).padStart(2, '0')
    return `${value.getFullYear()}-${part(value.getMonth() + 1)}-${part(value.getDate())} ${part(value.getHours())}:${part(value.getMinutes())}:${part(value.getSeconds())}`
  }
  const requestPromise = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/metrics/series') && url.searchParams.get('from') === from && url.searchParams.get('to') === to
  })
  await page.getByRole('textbox', { name: '开始时间' }).fill(formatInput(start).replace(' ', 'T'))
  await page.getByRole('textbox', { name: '结束时间' }).fill(formatInput(end).replace(' ', 'T'))
  await page.getByRole('button', { name: '应用时间范围' }).click()
  await requestPromise

  await expect(page).toHaveURL((url) => url.searchParams.get('from') === from && url.searchParams.get('to') === to)
  await expect(chart).toBeVisible()
  await chart.getByText('查看数据表').click()
  await expect(chart.locator('tbody tr').first()).toBeVisible()
})
