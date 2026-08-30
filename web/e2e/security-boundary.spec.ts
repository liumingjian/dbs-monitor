import { expect, test } from '@playwright/test'

import { EMPTY_STORAGE_STATE } from './auth'

test.use({ storageState: EMPTY_STORAGE_STATE })

test('[SEC-10] production CSP keeps the login-to-chart path functional', async ({ page }) => {
  const violations: string[] = []
  const violationPattern = /content security policy|violates.*(?:policy|directive)|refused to.*(?:policy|directive)/i
  page.on('console', (message) => {
    if (violationPattern.test(message.text())) {
      violations.push(message.text())
    }
  })
  page.on('pageerror', (error) => {
    if (violationPattern.test(error.message)) {
      violations.push(error.message)
    }
  })

  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env.SECURITY_E2E_USERNAME ?? 'admin')
  await page.getByLabel('密码').fill(process.env.SECURITY_E2E_PASSWORD ?? '')
  await page.getByRole('button', { name: /登\s*录/ }).click()

  await expect(page).toHaveURL(/\/instances$/)
  const instanceName = process.env.SECURITY_E2E_INSTANCE ?? 'SEC-10 browser target'
  const row = page.getByRole('row', { name: new RegExp(instanceName) })
  await expect(row).toBeVisible()
  await row.getByRole('link', { name: '总览' }).click()
  await expect(page.getByRole('tab', { name: '实例总览' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '监控与报警' }).click()
  await expect(page.getByRole('tab', { name: '标准监控' })).toHaveAttribute('aria-selected', 'true')

  const chart = page.getByTestId('metric-card').getByTestId('metric-chart').first()
  await expect(chart).toBeVisible()
  // 图下方的无障碍数据表里存在真实数值，说明这位有权限的用户确实拿到并渲染了图表数据。
  // 原先靠读 canvas 像素证明同一件事；数据表与 canvas/SVG 哪种渲染技术都无关。
  await expect.poll(async () => {
    const table = chart.getByRole('table')
    if (!(await table.isVisible())) await chart.getByText('查看数据表').click()
    const rows = await table.getByRole('row').all()
    const cells = await Promise.all(rows.map((row) => row.getByRole('cell').allInnerTexts()))
    const values = cells.filter((row) => row.length > 0).map((row) => row[row.length - 1])
    return values.filter((value) => value !== '' && !value.includes('缺数')).length
  }).toBeGreaterThan(0)

  const tabStyle = await page.getByRole('tab', { name: '标准监控' }).evaluate((element) => {
    const style = getComputedStyle(element)
    return { display: style.display, fontFamily: style.fontFamily }
  })
  expect(tabStyle.display).not.toBe('none')
  expect(tabStyle.fontFamily).not.toBe('')
  expect(violations).toEqual([])
})
