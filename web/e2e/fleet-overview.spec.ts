import { expect, test } from '@playwright/test'

import { EMPTY_STORAGE_STATE } from './auth'

// 总览页的两条动线：登录落地到总览，以及从健康计数下钻到已经筛好的实例列表。
// 第二条是这一页存在的理由——一个不能点开的数字只是墙上的装饰。

test.describe('登录后的落地页', () => {
  test.use({ storageState: EMPTY_STORAGE_STATE })

  test('登录直接落到机群总览，四块都在', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('用户名').fill('admin')
    await page.getByLabel('密码').fill('t11-playwright-password')
    await page.getByRole('button', { name: /登\s*录/ }).click()

    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('heading', { name: '机群总览' })).toBeVisible()
    for (const title of ['机群健康', '采集自监控', '需要关注的实例', '容量水位']) {
      await expect(page.getByRole('heading', { name: title })).toBeVisible()
    }
    // 第五块 Top SQL 是下一张票的，这里不该有它的空壳。
    await expect(page.getByRole('heading', { name: 'Top SQL' })).toHaveCount(0)

    // 侧栏六项三组；SQL 洞察先占位，不是链接（点开是 404 的入口比没有入口更糟）。
    const nav = page.getByRole('navigation', { name: '主导航' })
    for (const [group, items] of [
      ['监控', ['总览', '实例列表']],
      ['告警', ['全局告警', '告警设置']],
      ['系统', ['用户管理']],
    ] as const) {
      for (const item of items) {
        await expect(nav.getByRole('list', { name: group }).getByRole('link', { name: new RegExp(item) })).toBeVisible()
      }
    }
    await expect(nav.getByTitle('SQL 洞察（即将上线）')).toBeVisible()
    await expect(nav.getByRole('link', { name: /SQL 洞察/ })).toHaveCount(0)

    // 旧的落地地址仍然可用：它被人存过书签，也被发给过同事。
    await page.goto('/instances')
    await expect(page.getByRole('heading', { name: 'PostgreSQL 实例' })).toBeVisible()
  })
})

test('健康计数点开落到筛好的实例列表', async ({ page }) => {
  await page.goto('/')
  // 只看健康计数那一组：采集自监控用的是同一种可点计数，整页找会一次命中八个。
  const tiles = page.getByRole('list', { name: '机群健康计数' }).getByTestId('overview-count')
  await expect(tiles).toHaveCount(5)

  // 挑一个非零的档位来点：它的行数与计数必须逐个对得上。全零时退回第一个，
  // 断言仍然成立（筛出来的就是一页空的）。
  let chosen = tiles.first()
  let expectedRows = 0
  for (let index = 0; index < 5; index += 1) {
    const tile = tiles.nth(index)
    const text = await tile.innerText()
    const parsed = Number(text.replace(/\D+/g, ''))
    if (parsed > 0) {
      chosen = tile
      expectedRows = parsed
      break
    }
  }

  const href = await chosen.getAttribute('href')
  const status = new URL(href === null ? '' : href, page.url()).searchParams.get('status')
  expect(status).not.toBeNull()

  await chosen.click()

  await expect(page).toHaveURL((url) => url.pathname === '/instances' && url.searchParams.get('status') === status)
  await expect(page.getByRole('heading', { name: 'PostgreSQL 实例' })).toBeVisible()
  await expect(page.getByTestId('instance-row')).toHaveCount(expectedRows)
  // 筛选条件真的落在控件上，不只是留在地址栏里：这个视图整条发给同事时，
  // 对方看到的是同一个已经筛好的列表。「清除筛选」只有在真的筛了东西时才可用。
  await expect(page.getByRole('button', { name: '清除筛选' })).toBeEnabled()
})
