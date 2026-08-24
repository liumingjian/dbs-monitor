import { expect, test } from '@playwright/test'
import { standardMonitoringGroups } from '../src/routes/instances.$id/standardMonitoring'

// 现场由 test/acceptance/collection_browser_test.go::TestAcceptance_AC_01_F2 真实搭起来：
// 监控账号没有 pg_monitor，所以 pg.connection.total 一路空到图上，pg.replication.role 有真实出点。
const instanceID = process.env.AC01F2_INSTANCE_ID ?? ''
const blockedMetric = 'pg.connection.total'
// pg.role 任务没有能力前置，所以这条指标在没有 pg_monitor 的实例上照样出真实点。
const sampledMetric = 'pg.replication.role'
const charts = standardMonitoringGroups.flatMap((group) => group.charts)
const blockedChart = charts.find((chart) => chart.metrics[0] === blockedMetric)
const sampledChart = charts.find((chart) => chart.metrics[0] === sampledMetric)

test('[AC-01-F2] empty chart names its reason and walks through to the affected metric row', async ({ page }) => {
  test.skip(instanceID === '', '验收专用：现场由 Go harness 搭起来，普通 e2e 轮次里跳过')
  expect(blockedChart, `no standard-monitoring chart leads with ${blockedMetric}`).toBeDefined()
  expect(sampledChart, `no standard-monitoring chart leads with ${sampledMetric}`).toBeDefined()

  const to = new Date(Date.now() + 60_000)
  const from = new Date(to.getTime() - 30 * 60_000)
  const monitoring = `/instances/${instanceID}/monitoring?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`

  const seriesResponse = page.waitForResponse((response) => response.url().includes('/metrics/series'))
  await page.goto(monitoring)
  const payload = await (await seriesResponse).json()

  // B6：空图表渲染 13 码的原因文案，绝不渲染成 0 —— 连画布都不该出现。
  const blockedCard = page.locator('.metric-card').filter({ has: page.getByText(blockedChart!.title, { exact: true }) })
  await expect(blockedCard.getByText('权限不足', { exact: true })).toBeVisible()
  await expect(blockedCard.locator('canvas')).toHaveCount(0)
  await expect(blockedCard.getByText('0', { exact: true })).toHaveCount(0)

  // 缺桶不补 0：请求的是 30 分钟窗口，实例刚刚才建起来，返回的点因此只落在窗口末尾；
  // 补 0 的实现会从窗口起点就开始给桶，最早一个点会顶到 from。
  const sampled = payload.metrics.find((item: { metric: string }) => item.metric === sampledMetric)
  expect(sampled.unavailability).toBeNull()
  const timestamps = sampled.series.flatMap((item: { points: [number, number | null][] }) =>
    item.points.map((point) => point[0] * 1000))
  expect(timestamps.length).toBeGreaterThan(0)
  expect(Math.min(...timestamps)).toBeGreaterThan(from.getTime() + 15 * 60_000)

  // B6：数据新鲜度可见。
  await expect(page.getByLabel(/^数据新鲜$|^数据已过期$/).first()).toBeVisible()

  // B6：时间范围经 search params 往返。
  await page.getByLabel('开始时间').fill('2026-08-11T09:00')
  await page.getByLabel('结束时间').fill('2026-08-11T12:00')
  await page.getByRole('button', { name: '应用时间范围' }).click()
  await expect(page).toHaveURL((url) =>
    url.searchParams.get('from') === '2026-08-11T09:00:00.000Z' &&
    url.searchParams.get('to') === '2026-08-11T12:00:00.000Z')

  // IA §6.5 全路径：空图表 → 采集管理 → 受影响指标行。
  await page.goto(monitoring)
  const action = blockedCard.getByRole('link', { name: '补齐监控权限' })
  await expect(action).toHaveAttribute('href', `/instances/${instanceID}/collection?metric=${encodeURIComponent(blockedMetric)}`)
  await action.click()
  await expect(page).toHaveURL(new RegExp(`/instances/${instanceID}/collection`))
  await expect(page.locator(`#metric-${blockedMetric.replaceAll('.', '\\.')}`)).toBeVisible()
})
