import { expect, test } from '@playwright/test'

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
  await page.getByRole('link', { name: '监控与报警' }).click()
  await expect(page.getByRole('tab', { name: '标准监控' })).toHaveAttribute('aria-selected', 'true')

  const chart = page.locator('.metric-card canvas').first()
  await expect(chart).toBeVisible()
  const chartPixels = await chart.evaluate((canvas) => {
    const chartCanvas = canvas as HTMLCanvasElement
    const context = chartCanvas.getContext('2d')
    if (!context) return 0
    const pixels = context.getImageData(0, 0, chartCanvas.width, chartCanvas.height).data
    let painted = 0
    for (let index = 3; index < pixels.length; index += 4) {
      if (pixels[index] !== 0) {
        painted++
      }
    }
    return painted
  })
  expect(chartPixels).toBeGreaterThan(0)

  const tabStyle = await page.getByRole('tab', { name: '标准监控' }).evaluate((element) => {
    const style = getComputedStyle(element)
    return { display: style.display, fontFamily: style.fontFamily }
  })
  expect(tabStyle.display).not.toBe('none')
  expect(tabStyle.fontFamily).not.toBe('')
  expect(violations).toEqual([])
})
