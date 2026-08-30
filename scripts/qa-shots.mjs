// QA-only: run from web/ (needs its node_modules): node ../scripts/qa-shots.mjs — resolve via NODE_PATH or copy into web/.
import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const base = process.env.QA_BASE_URL ?? 'https://127.0.0.1:18443'
const id = process.env.QA_INSTANCE_ID
const out = process.env.QA_SHOT_DIR ?? '/tmp/qa-shots'
mkdirSync(out, { recursive: true })

const browser = await chromium.launch()
const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1600, height: 1200 }, deviceScaleFactor: 1 })
const page = await context.newPage()

async function shot(name, url, { full = true, settle = 2500 } = {}) {
  await page.goto(`${base}${url}`, { waitUntil: 'networkidle' }).catch(() => {})
  await page.waitForTimeout(settle)
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: full })
  console.log(`shot ${name} <- ${url}`)
}

await shot('01-login', '/login', { full: false })
await page.fill('#username, input[autocomplete="username"]', 'admin')
await page.fill('input[autocomplete="current-password"]', process.env.QA_ADMIN_PASSWORD ?? 'qa-admin-password')
await page.click('button[type="submit"]')
await page.waitForURL('**/instances', { timeout: 15000 })

await shot('02-instances', '/instances')
const range = `from=${new Date(Date.now() - 3600000).toISOString()}&to=${new Date().toISOString()}`
await shot('03-overview', `/instances/${id}?${range}`)
await shot('04-monitoring', `/instances/${id}/monitoring?${range}`, { settle: 4000 })
await shot('05-alerts', '/alerts?tab=current&include_paused=false')
await shot('06-rules', `/instances/${id}/alerts/rules`)
await shot('07-collection', `/instances/${id}/collection`)
await shot('08-notifications', '/alert-settings/notifications')
await shot('09-root-route', '/')

await page.setViewportSize({ width: 390, height: 844 })
await shot('10-instances-mobile', '/instances')
await shot('11-monitoring-mobile', `/instances/${id}/monitoring?${range}`, { settle: 4000 })

await browser.close()
