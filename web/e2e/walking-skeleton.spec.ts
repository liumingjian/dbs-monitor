import { expect, test, type Locator } from '@playwright/test'

import { EMPTY_STORAGE_STATE } from './auth'

test.use({ storageState: EMPTY_STORAGE_STATE })

const instanceName = 'T11 smoke instance'

test('[AC-01-S1] [AC-05-S1] [AC-05-F5] instance overview and standard monitoring expose the real collection path', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('t11-playwright-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()

  await expect(page).toHaveURL(/\/instances$/)
  await expect(page.getByRole('heading', { name: 'PostgreSQL 实例' })).toBeVisible()
  const instanceRow = page.getByRole('row', { name: new RegExp(instanceName) })
  const listHealthText = await instanceRow.getByTestId('health-status').innerText()
  const listAttributionText = await instanceRow.getByTestId('instance-attribution').innerText()
  const listSeverityCounts = await Promise.all(
    ['C', 'W', 'I'].map((severity) =>
      instanceRow.getByText(new RegExp(`^${severity}\\d+$`)).innerText(),
    ),
  )
  await instanceRow.getByRole('link', { name: '总览' }).click()

  await expect(page.getByRole('tab', { name: '实例总览' })).toHaveAttribute('aria-selected', 'true')
  const overviewStatusSection = page.getByTestId('overview-status')
  await expect(overviewStatusSection.getByTestId('health-status')).toHaveText(listHealthText)
  await expect(overviewStatusSection.getByRole('heading')).toHaveText(listAttributionText)
  for (const count of listSeverityCounts) {
    await expect(overviewStatusSection.getByText(count, { exact: true })).toBeVisible()
  }

  await expect(page.locator('[data-overview-module]')).toHaveCount(7)
  await expect(page.getByTestId('overview-module-title')).toHaveText([
    '可用性与采集状态',
    '当前告警摘要',
    '核心资源',
    '数据库负载',
    '复制状态',
    '近期性能事件',
    '快速排障入口',
  ])
  await expect(page.getByText('近期没有性能事件')).toBeVisible()

  // 「快速排障入口」卡片与页签导航里有同名链接，按所属区域定位而不是靠图标混入的可访问名前缀
  const troubleshootingCard = page.locator('[data-overview-module="troubleshooting"]')
  const getLinkURL = async (scope: Locator, accessibleName: string) => {
    const href = await scope.getByRole('link', { name: accessibleName }).getAttribute('href')
    if (href === null) {
      throw new Error(`${accessibleName} link is missing an href`)
    }
    return new URL(href, page.url())
  }

  const overviewURL = new URL(page.url())
  const monitoringURL = await getLinkURL(troubleshootingCard, '标准监控')
  expect(monitoringURL.searchParams.get('from')).toBe(overviewURL.searchParams.get('from'))
  expect(monitoringURL.searchParams.get('to')).toBe(overviewURL.searchParams.get('to'))

  const sessionsURL = await getLinkURL(troubleshootingCard, '会话与阻塞')
  expect(sessionsURL.searchParams.get('from')).toBe(overviewURL.searchParams.get('from'))
  expect(sessionsURL.searchParams.get('to')).toBe(overviewURL.searchParams.get('to'))
  expect(sessionsURL.searchParams.get('filter')).toBe('lock_wait')

  const collectionURL = await getLinkURL(troubleshootingCard, '采集状态')
  expect(collectionURL.searchParams.get('metric')).toBeNull()

  const maintenanceURL = await getLinkURL(overviewStatusSection, '新建维护窗口')
  expect(maintenanceURL.searchParams.get('instance_id')).toBe(overviewURL.pathname.split('/').at(-1))

  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

  await page.getByRole('tab', { name: '监控与报警' }).click()

  await expect(page.getByRole('tab', { name: '标准监控' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('metric-card')).toHaveCount(22)
  await expect(page.getByRole('region', { name: '资源指标' }).getByTestId('metric-card')).toHaveCount(5)
  await expect(page.getByRole('region', { name: '数据库指标' }).getByTestId('metric-card')).toHaveCount(12)
  await expect(page.getByRole('region', { name: '复制指标' }).getByTestId('metric-card')).toHaveCount(5)

  const tpsCard = page.getByTestId('metric-card').filter({ has: page.getByText('TPS', { exact: true }) })
  const tpsChart = tpsCard.getByRole('figure', { name: 'TPS趋势' })
  await expect(tpsChart).toBeVisible()
  await tpsChart.getByText('查看数据表').click()
  await expect(tpsChart.locator('tbody tr').first()).toBeVisible()

  // check-e2e 不注册也不启动 monitor-agent（agent_expected=false），后端把 host.* 指标
  // 归为 NOT_APPLICABLE_ROLE（handler.go agentMetricUnavailability）；
  // 有数据的图表交互由上面的 TPS 卡（真实 SQL 采集路径）覆盖
  const cpuCard = page.getByTestId('metric-card').filter({ has: page.getByText('CPU', { exact: true }) })
  await expect(cpuCard.getByText('当前角色不适用')).toBeVisible()

  await cpuCard.getByRole('button', { name: /指标详情/ }).click()
  await expect(page.getByRole('dialog', { name: 'CPU' })).toContainText('host.cpu.usage_percent')
  await page.getByRole('button', { name: '关闭指标详情' }).click()

  const memoryCard = page.getByTestId('metric-card').filter({ has: page.getByText('内存', { exact: true }) })
  await expect(memoryCard.getByText('当前角色不适用')).toBeVisible()
  await expect(memoryCard.getByRole('link', { name: '查看实例角色' })).toHaveAttribute('href', /\/collection\?metric=host\.memory\.usage_percent/)
  await expect(memoryCard).not.toContainText('0')

  const slowQueryCard = page.getByTestId('metric-card').filter({ has: page.getByText('长查询数量', { exact: true }) })
  const drilldown = slowQueryCard.getByRole('link', { name: /查看采样记录/ })
  // 会话相关的三个视图合并成一个多标签页面（票 #200）之后，长查询下钻的规范地址是
  // `/sessions?…&tab=long-query-samples`。旧地址仍然重定向到同一个标签，那条由
  // `sessions-ui.spec.ts` 专门覆盖；这里断言的是「下钻直接落到采样记录上」这件事本身。
  await expect(drilldown).toHaveAttribute('href', /\/sessions\?.*tab=long-query-samples/)
  await expect(drilldown).toHaveAttribute('href', /metric=pg.query.long_running_count/)

  const stepRequest = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return url.pathname.endsWith('/metrics/series') && url.searchParams.get('step') === '1m'
  })
  // 角色 + 可访问名，不走 getByLabel：下拉的按钮与它展开的列表框指向同一个 <label>，
  // 两个元素因此都「被这个标签标注」，getByLabel 一次会命中两个。
  await page.getByRole('combobox', { name: '数据粒度' }).click()
  await page.getByRole('option', { name: '1 分钟' }).click()
  await stepRequest
  await expect(page).toHaveURL((url) => url.searchParams.get('step') === '1m')
  await expect(tpsChart.getByText('实际粒度：1m')).toBeVisible()

  await page.getByLabel('图表列数').getByText('3 列').click()
  await expect(page).toHaveURL((url) => url.searchParams.get('columns') === '3')
  // 直接点这个开关本体。原来这里点的是标签文字，因为组件库把真正的
  // `<button role="switch">` 做成 1px 的隐藏元素、可见的滑块画在标签里 —— 指针够不着控件。
  // `primitives/Toggle` 把 button 铺回控件自己的位置之后，「按角色与可访问名找到它再点」
  // 就是可行的，这也正是这条断言该证明的事。
  await page.getByRole('switch', { name: '光标联动' }).click()
  await expect(page).toHaveURL((url) => url.searchParams.get('connect') === 'false')

  const start = new Date(Date.now() - 30 * 60 * 1000)
  const end = new Date(Date.now() + 30 * 60 * 1000)
  start.setSeconds(0, 0)
  end.setSeconds(0, 0)
  const from = start.toISOString()
  const to = end.toISOString()
  const formatInput = (value: Date) => {
    // 浏览器上下文固定 UTC（playwright.config timezoneId），这里必须用 UTC 取值，与 Node 本机时区无关
    const part = (number: number) => String(number).padStart(2, '0')
    return `${value.getUTCFullYear()}-${part(value.getUTCMonth() + 1)}-${part(value.getUTCDate())}T${part(value.getUTCHours())}:${part(value.getUTCMinutes())}`
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
  await expect(tpsChart).toBeVisible()

  await page.goto(`/instances/not-used/monitoring?from=last-hour&to=bad&step=10m`)
  await expect(page.getByText('时间范围必须是绝对 RFC3339 时间')).toBeVisible()
  await expect(page.getByRole('button', { name: '使用最近一小时' })).toBeVisible()
})
