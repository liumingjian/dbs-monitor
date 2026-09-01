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
    engine: 'POSTGRESQL',
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

/// 服务端筛选、排序与分页的替身：真实后端做什么，这里就做什么。
/// 页面把条件写进地址栏、随请求发给服务端，这段替身按同一套语义答复 ——
/// 断言因此仍然是「页面把条件发对了、把回来的那一页画对了」。
function respondToInstanceList(url: URL) {
  const params = url.searchParams
  const statuses = params.getAll('status')
  const flags = params.getAll('flags')
  const severities = params.getAll('severity')
  const search = params.get('q')
  const matched = Array.from({ length: 50 }, (_, index) => mockInstance(index)).filter((instance) => {
    if (search !== null && !`${instance.name} ${instance.host}:${instance.port}`.toLowerCase().includes(search.toLowerCase())) return false
    if (statuses.length > 0 && !statuses.includes(instance.health.status)) return false
    if (flags.some((flag) => flag === 'NO_DATA' && !instance.health.flags.no_data)) return false
    if (severities.length > 0 && !severities.some((severity) => instance.health.counts[severity as 'critical' | 'warning' | 'info'] > 0)) return false
    return true
  })
  const page = Number(params.get('page') ?? '1')
  const pageSize = Number(params.get('page_size') ?? '50')
  return { items: matched.slice((page - 1) * pageSize, page * pageSize), total: matched.length }
}

test('instance list renders eight server-filtered columns for 50 instances', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: {
    id: '00000000-0000-4000-8000-000000000001',
    username: 'gate-reader',
    role: 'READONLY',
    enabled: true,
    created_at: '2026-08-11T12:00:00Z',
  } }))
  // 分页化之后地址上带着 query，所以按正则匹配；返回的是当页与总数，不是裸数组。
  await page.route(/\/api\/v1\/instances(\?|$)/, (route) => route.fulfill({
    json: respondToInstanceList(new URL(route.request().url())),
  }))
  // 趋势与连接饱和度现在是**一次**批量请求：50 行不再各自打一次服务端。
  // 而且列表按**语义位**要数（ADR-0001），位到指标的解析在服务端完成 —— 替身照做：
  // 请求里给的每个位都答一条，条上同时带着位与它在这台实例的引擎上解析出来的指标 ID。
  const slotAnswers: Record<string, { metric: string; unit: string; points: (number | null)[][] }> = {
    throughput: {
      metric: 'pg.tps',
      unit: 'tx/s',
      points: [[1770806400, 12], [1770806700, 18], [1770807000, null], [1770807300, 21]],
    },
    connection_saturation: {
      metric: 'pg.connection.saturation_percent',
      unit: 'percent',
      points: [[1770806400, 40], [1770807300, 87.4]],
    },
  }
  await page.route(/\/api\/v1\/instances\/metrics\/series/, (route) => {
    const url = new URL(route.request().url())
    const slots = url.searchParams.getAll('slot')
    return route.fulfill({ json: {
      from: '2026-08-11T11:00:00Z',
      to: '2026-08-11T12:00:00Z',
      step: '5m',
      instances: url.searchParams.getAll('instance_id').map((id) => ({
        instance_id: id,
        metrics: slots.map((slot) => ({
          slot,
          metric: slotAnswers[slot].metric,
          unit: slotAnswers[slot].unit,
          unavailability: null,
          series: [{ labels: {}, points: slotAnswers[slot].points }],
        })),
      })),
    } })
  })

  // 行的外壳是共享的表格组件（primitives/DataGrid），它提供 `rowTestId` 这一个行钩子：
  // 只挂在数据行上，骨架行与空态行不带，所以这个计数就是数据行数。
  const instanceRows = page.getByTestId('instance-row')

  const startedAt = Date.now()
  await page.goto('/instances')
  await expect(instanceRows).toHaveCount(50)
  test.info().annotations.push({ type: 'render-ms', description: String(Date.now() - startedAt) })
  await expect(page.getByText('gate-instance-050')).toBeVisible()

  // 八列，一列不多一列不少。地址与 Agent 已经不在这里：地址进详情页与搜索索引，
  // Agent 并进采集新鲜度。
  const headerCells = page.getByRole('table', { name: '实例列表' }).getByRole('columnheader')
  await expect(headerCells).toHaveText(['实例', '引擎', '健康', '告警', '告警归因', '连接饱和度', '吞吐趋势', '采集新鲜度'])

  // 趋势画的是吞吐；饱和度是一个百分比，来自同一次批量请求。
  await expect(page.getByRole('img', { name: /吞吐趋势$/ })).toHaveCount(50)
  await expect(instanceRows.first().getByText('87%')).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('instance-list-desktop.png'), fullPage: true })

  // 筛选由服务端完成，并且写进地址栏——这个地址原样打开就是同一个视图。
  await page.getByRole('combobox', { name: '主状态' }).click()
  await page.getByTestId('health-status-option-CRITICAL').click()
  await expect(instanceRows).toHaveCount(10)
  await expect(page).toHaveURL(/status=CRITICAL/)
  await page.mouse.click(8, 500)
  await expect(page.getByTestId('health-status-option-CRITICAL')).toBeHidden()

  await page.reload()
  await expect(instanceRows).toHaveCount(10)

  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(instanceRows).toHaveCount(50)
  await expect(page).not.toHaveURL(/status=/)

  // 搜索命中地址：地址整列去掉了，但仍然找得到那台机器。
  await page.getByLabel('搜索').fill('10.0.0.7')
  await expect(page).toHaveURL(/q=10.0.0.7/)
  await expect(instanceRows).toHaveCount(1)
  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(instanceRows).toHaveCount(50)

  // 最小支持宽度：不横向滚动，也不少一列。列数在两个宽度上比对，
  // 「窄下来就把列藏掉」会在这里当场被抓住。
  const wideColumns = await headerCells.allInnerTexts()
  await page.setViewportSize({ width: 1280, height: 900 })
  await expect(headerCells).toHaveText(wideColumns)
  expect(await pageOverflows(page)).toBe(false)
  await page.screenshot({ path: test.info().outputPath('instance-list-1280.png') })

  // 密集档：行高收到 32px，**八列一列不少**（丢列在任何档位下都是禁止的），
  // 而且这个选择跨刷新保持。
  const rowHeight = async () => (await instanceRows.first().boundingBox())?.height
  expect(await rowHeight()).toBeCloseTo(40, 0)
  await page.getByRole('tab', { name: '紧凑行高' }).click()
  await expect(headerCells).toHaveText(wideColumns)
  await expect(page.getByRole('img', { name: /吞吐趋势$/ })).toHaveCount(50)
  expect(await rowHeight()).toBeCloseTo(32, 0)
  expect(await pageOverflows(page)).toBe(false)
  await page.reload()
  await expect(page.getByRole('tab', { name: '紧凑行高' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('tab', { name: '标准行高' }).click()
  await expect(page.getByRole('img', { name: /吞吐趋势$/ })).toHaveCount(50)

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
  await page.getByLabel('Bootstrap 数据库').fill('postgres')
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
