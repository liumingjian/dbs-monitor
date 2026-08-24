import { expect, test } from '@playwright/test'

// 现场由 test/acceptance/collection_browser_test.go::TestAcceptance_AC_07_F2 真实搭起来：
// 实例经 updateCollectionPause 真的暂停了，页面上不该出现任何别的 12 码文案。
const instanceID = process.env.AC07F2_INSTANCE_ID ?? ''
const instanceName = process.env.AC07F2_INSTANCE_NAME ?? ''

const otherUnavailabilityCopy = [
  '等待首个样本',
  '所选范围没有数据',
  '数据已过期',
  '采集失败',
  '数据库不可达',
  'Agent 离线',
  '权限不足',
  '扩展未安装',
  '功能未启用',
  '版本不支持',
  '当前角色不适用',
  '计数器已重置',
]

test('[AC-07-F2] a paused instance says paused and never impersonates a failure', async ({ page }) => {
  test.skip(instanceID === '', '验收专用：现场由 Go harness 搭起来，普通 e2e 轮次里跳过')

  const to = new Date(Date.now() + 60_000)
  const from = new Date(to.getTime() - 30 * 60_000)
  await page.goto(`/instances/${instanceID}/monitoring?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`)

  const cards = page.locator('.metric-card')
  await expect(cards.first().getByText('采集已暂停', { exact: true })).toBeVisible()
  const cardCount = await cards.count()
  await expect(page.getByText('采集已暂停', { exact: true })).toHaveCount(cardCount)
  for (const copy of otherUnavailabilityCopy) {
    await expect(page.getByText(copy, { exact: true })).toHaveCount(0)
  }
  // B6：绝不渲染成 0 —— 暂停期一张画布都不该画出来。
  await expect(page.locator('canvas')).toHaveCount(0)
  await expect(page.getByText('0', { exact: true })).toHaveCount(0)

  await page.goto('/instances')
  const row = page.locator('tbody tr[data-row-key]').filter({ hasText: instanceName })
  // 「已暂停」标记带时长；超 7 天转警示色的阈值由 collectionPausePresentation 的表驱动单测守。
  await expect(row.getByText(/^已暂停 /)).toBeVisible()
  // 导航角标计已暂停实例数。
  await expect(page.getByTitle(/^\d+ 个实例已暂停采集$/)).toBeVisible()
})
