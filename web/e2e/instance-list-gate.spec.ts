import { expect, test, type Page } from '@playwright/test'

const healthStatuses = ['CRITICAL', 'WARNING', 'UNKNOWN', 'HEALTHY', 'PAUSED'] as const

function mockInstance(index: number) {
  const status = healthStatuses[index % healthStatuses.length]
  const sequence = String(index + 1).padStart(3, '0')
  return {
    id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, '0')}`,
    name: `gate-instance-${sequence}`,
    host: `10.0.0.${index + 1}`,
    port: 5432,
    database: 'postgres',
    username: 'monitor',
    agent_metrics_enabled: true,
    alert_status: status === 'CRITICAL' || status === 'WARNING' ? 'FIRING' : 'OK',
    agent_status: index % 2 === 0 ? 'online' : 'not_installed',
    last_collected_at: '2026-08-11T12:00:00Z',
    data_freshness_seconds: index + 1,
    collection_pause: status === 'PAUSED'
      ? { paused: true, updated_at: '2026-08-01T12:00:00Z' }
      : { paused: false },
    health: {
      status,
      attribution: status === 'CRITICAL' || status === 'WARNING'
        ? { rule_name: `gate rule ${sequence}`, current_value: 80 + index }
        : undefined,
      counts: {
        critical: status === 'CRITICAL' ? 1 : 0,
        warning: status === 'WARNING' ? 1 : 0,
        info: index % 7 === 0 ? 1 : 0,
      },
      flags: {
        no_data: index % 11 === 0,
        in_maintenance: index % 13 === 0,
        recently_recovered: index % 17 === 0,
        ignored: index % 19 === 0 ? 1 : 0,
        configuration_missing: index % 23 === 0 ? 1 : 0,
      },
    },
  }
}

test('instance list renders and filters 50 instance projections', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: {
    id: '00000000-0000-4000-8000-000000000001',
    username: 'gate-reader',
    role: 'READONLY',
    enabled: true,
    created_at: '2026-08-11T12:00:00Z',
  } }))
  await page.route('**/api/v1/instances', (route) => route.fulfill({ json: Array.from({ length: 50 }, (_, index) => mockInstance(index)) }))
  // 行内趋势缩略图一行一个请求，实例是假的，所以这里也给一份假的时间序列：
  // 不给的话 50 行会各自去打真实的服务端，测的就不是列表本身了。
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({ json: {
    from: '2026-08-11T11:00:00Z',
    to: '2026-08-11T12:00:00Z',
    step: '5m',
    metrics: [{
      metric: 'pg.connection.total',
      unit: 'count',
      unavailability: null,
      series: [{ labels: {}, points: [[1770806400, 12], [1770806700, 18], [1770807000, null], [1770807300, 21]] }],
    }],
  } }))

  // 行的外壳是共享的表格组件（primitives/DataGrid），它提供 `rowTestId` 这一个行钩子：
  // 只挂在数据行上，骨架行与空态行不带，所以这个计数就是数据行数。
  const instanceRows = page.getByTestId('instance-row')

  const startedAt = Date.now()
  await page.goto('/instances')
  await expect(instanceRows).toHaveCount(50)
  test.info().annotations.push({ type: 'render-ms', description: String(Date.now() - startedAt) })
  await expect(page.getByText('gate-instance-050')).toBeVisible()
  // 每一行都画出了趋势缩略图；它是图片角色，可访问名带实例名。
  await expect(page.getByRole('img', { name: /趋势$/ })).toHaveCount(50)
  await page.screenshot({ path: test.info().outputPath('instance-list-desktop.png'), fullPage: true })

  // 下拉是 combobox 角色；按角色 + 可访问名定位，避免同名的浮层列表也被匹配上。
  await page.getByRole('combobox', { name: '主状态' }).click()
  await page.getByTestId('health-status-option-CRITICAL').click()
  await expect(instanceRows).toHaveCount(10)
  await page.mouse.click(8, 500)
  await expect(page.getByTestId('health-status-option-CRITICAL')).toBeHidden()

  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(instanceRows).toHaveCount(50)

  // 最小支持宽度：不横向滚动，也不少一列。列数在两个宽度上比对，
  // 「窄下来就把列藏掉」会在这里当场被抓住。
  const headerCells = page.getByRole('table', { name: '实例列表' }).getByRole('columnheader')
  const wideColumns = await headerCells.allInnerTexts()
  await page.setViewportSize({ width: 1280, height: 900 })
  await expect(headerCells).toHaveText(wideColumns)
  expect(await pageOverflows(page)).toBe(false)
  await page.screenshot({ path: test.info().outputPath('instance-list-1280.png') })

  // 密集档：行高收到 32px，趋势缩略图整列让出去（规范是「丢掉缩略图而不是压扁它」），
  // 而且这个选择跨刷新保持。
  const rowHeight = async () => (await instanceRows.first().boundingBox())?.height
  expect(await rowHeight()).toBeCloseTo(40, 0)
  await page.getByRole('tab', { name: '紧凑行高' }).click()
  await expect(page.getByRole('img', { name: /趋势$/ })).toHaveCount(0)
  expect(await rowHeight()).toBeCloseTo(32, 0)
  await page.reload()
  await expect(page.getByRole('tab', { name: '紧凑行高' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '标准行高' }).click()
  await expect(page.getByRole('img', { name: /趋势$/ })).toHaveCount(50)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(instanceRows).toHaveCount(50)
  await page.screenshot({ path: test.info().outputPath('instance-list-mobile.png'), fullPage: true })
})

async function pageOverflows(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)
}

test('new-instance modal validates in the browser and reports a failed connection test', async ({ page }) => {
  await page.goto('/instances')
  await page.getByRole('button', { name: '新建实例' }).click()

  // 客户端校验：错误落在对应字段下方，不做页面顶部的汇总。
  await page.getByRole('button', { name: '连接测试并创建' }).click()
  await expect(page.getByText('请输入实例名称')).toBeVisible()
  await expect(page.getByText('请输入主机地址')).toBeVisible()
  await expect(page.getByText('请输入密码')).toBeVisible()

  const unreachable = `e2e-unreachable-${Date.now()}`
  await page.getByLabel('名称').fill(unreachable)
  await page.getByLabel('主机').fill('127.0.0.1')
  await page.getByLabel('端口').fill('1')
  await page.getByLabel('数据库').fill('postgres')
  await page.getByLabel('用户名').fill('monitor')
  await page.getByLabel('密码').fill('monitor')
  await expect(page.getByText('请输入实例名称')).toBeHidden()

  // 连接测试失败没有携带字段级信息，所以退回整表单的错误条；弹窗留在原地，填过的值还在。
  await page.getByRole('button', { name: '连接测试并创建' }).click()
  await expect(page.getByText('无法连接目标 PostgreSQL')).toBeVisible()
  await expect(page.getByLabel('名称')).toHaveValue(unreachable)
  await page.screenshot({ path: test.info().outputPath('create-instance-modal.png') })

  // 登录页也在本组里，顺手留一张：它在应用外框之外，只有这条用例走得到未登录状态。
  await page.context().clearCookies()
  await page.goto('/login')
  await expect(page.getByRole('button', { name: /登\s*录/ })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('login.png') })
})
