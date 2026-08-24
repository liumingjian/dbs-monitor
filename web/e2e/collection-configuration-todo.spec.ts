import { expect, test } from '@playwright/test'
import { EMPTY_STORAGE_STATE } from './auth'

// 三个实例的现场由 test/acceptance/collection_browser_test.go::TestAcceptance_AC_07_S2 真实搭起来。
const readyID = process.env.AC07S2_READY_ID ?? ''
const missingID = process.env.AC07S2_MISSING_ID ?? ''
const unknownID = process.env.AC07S2_UNKNOWN_ID ?? ''
const viewerUsername = process.env.AC07S2_VIEWER_USERNAME ?? ''
const viewerPassword = process.env.AC07S2_VIEWER_PASSWORD ?? ''

const readyCopy = '无待办——所有可修复的采集能力均已就绪'
const unknownCopy = '无法检查采集能力'

test('[AC-07-S2] the configuration todo tells all three states apart across four situations', async ({ page }) => {
  test.skip(readyID === '', '验收专用：现场由 Go harness 搭起来，普通 e2e 轮次里跳过')

  // 情形一 · 可修复缺失：出条目，带影响指标计数与文字修复指引，且没有一键修复按钮。
  await page.goto(`/instances/${missingID}/collection`)
  const missingTodo = page.getByRole('region', { name: '配置缺失待办' })
  const item = missingTodo.locator('.collection-todo-item')
  await expect(item).toHaveCount(1)
  await expect(item.getByText('缺少 pg_monitor 角色')).toBeVisible()
  await expect(item.getByText(/影响 \d+ 项指标/)).toBeVisible()
  await item.locator('summary').click()
  await expect(item.getByText('将监控账号加入 pg_monitor 角色。')).toBeVisible()
  await expect(missingTodo.getByRole('button', { name: /修复/ })).toHaveCount(0)
  await expect(missingTodo.getByText(readyCopy)).toHaveCount(0)

  // 情形二 · 结构性不适用：不进清单，只在能力模块显示「不适用」+ NAReason，措辞与「缺失」不同。
  await page.goto(`/instances/${readyID}/collection`)
  const readyTodo = page.getByRole('region', { name: '配置缺失待办' })
  await expect(readyTodo.locator('.collection-todo-item')).toHaveCount(0)
  const databaseModule = page.getByRole('region', { name: '数据库连接与权限检查' })
  const slotRow = databaseModule.locator('tbody tr[data-row-key="topo.has_slot"]')
  await expect(slotRow.getByText('不适用', { exact: true })).toBeVisible()
  await expect(slotRow.getByText('本实例没有 replication slot。')).toBeVisible()
  await expect(slotRow.getByText('缺失', { exact: true })).toHaveCount(0)

  // 情形四 · 全部就绪：显式正向空状态，且带能力快照的最近检查时间。
  await expect(readyTodo.getByText(readyCopy)).toBeVisible()
  await expect(readyTodo.getByText(/最近检查 \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/)).toBeVisible()
  // 模块在四种情形下都不隐藏。
  await expect(page.getByRole('region', { name: '扩展与插件能力' })).toBeVisible()

  // 情形三 · 查不到：进清单且排最前，绝不降级为「就绪」。
  await page.goto(`/instances/${unknownID}/collection`)
  const unknownTodo = page.getByRole('region', { name: '配置缺失待办' })
  await expect(unknownTodo.getByText(unknownCopy)).toBeVisible()
  await expect(unknownTodo.getByText(readyCopy)).toHaveCount(0)
  const firstAlertTitle = unknownTodo.locator('.ant-alert').first()
  await expect(firstAlertTitle).toContainText(unknownCopy)
  await expect(page.getByRole('region', { name: '数据库连接与权限检查' })).toBeVisible()
})

test.describe('read-only operator', () => {
  test.use({ storageState: EMPTY_STORAGE_STATE })

  // D9 的那条 UI 断言并入本次执行：可见性不收窄，写能力收窄。
  test('[AC-07-S2] collection configuration stays visible but disabled for a read-only operator', async ({ page }) => {
    test.skip(readyID === '', '验收专用：现场由 Go harness 搭起来，普通 e2e 轮次里跳过')
    const login = await page.request.post('/api/v1/login', {
      data: { username: viewerUsername, password: viewerPassword },
    })
    expect(login.ok(), `read-only login failed: ${login.status()}`).toBeTruthy()

    await page.goto(`/instances/${readyID}/collection`)
    const configuration = page.getByRole('region', { name: '采集配置' })
    await expect(configuration).toBeVisible()
    await expect(configuration.getByText('需要平台管理员角色')).toBeVisible()
    await expect(configuration.getByLabel('暂停采集')).toBeDisabled()
    await expect(configuration.getByLabel('暂停原因')).toBeDisabled()
    await expect(configuration.getByRole('spinbutton', { name: 'pg.probe 采样周期', exact: true })).toBeDisabled()
    await expect(configuration.getByRole('button', { name: '保存 pg.probe 采样周期' })).toBeDisabled()
  })
})
