// QA-only：在 web/ 下跑（Playwright 装在那儿）：`cd web && node ../scripts/qa-shots.mjs`。
//
// 人工比对用的截图流水，覆盖合并之后的**全部**路由。
//
// 这些截图**不是自动断言**：不进 Playwright 用例，不进任何快照基线。视觉大改期间
// 维护像素基线只会天天变红；它们的用途是让人一页一页看过去。所以这个文件刻意留在
// `scripts/` 而不是 `web/e2e/`，`make check` 不碰它。
//
// 两档宽度：常规审阅宽度（1600）与最小支持宽度（1280，DESIGN.md 的
// `structure.min-supported-width`）。移动档 390 只覆盖告警流——规范里唯一要求窄屏
// 收成单列的页面。
//
// 环境变量：QA_BASE_URL / QA_ADMIN_PASSWORD / QA_SHOT_DIR / QA_INSTANCE_ID。
// `QA_INSTANCE_ID` 可以不给：不给就从实例列表页自己认出第一台实例。
import { mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'

// ESM 的裸名解析是按**脚本自己的位置**走的，而这个脚本住在 `scripts/`，Playwright 装在
// `web/node_modules`。所以从当前工作目录起解析一次 —— 这样「在 web/ 下跑」才真的成立，
// 不必把脚本复制进 web/，也不必指望 ESM 认 NODE_PATH（它不认）。
const { chromium } = createRequire(`${process.cwd()}/`)('@playwright/test')

const base = process.env.QA_BASE_URL ?? 'https://127.0.0.1:18443'
const out = process.env.QA_SHOT_DIR ?? '/tmp/qa-shots'
const password = process.env.QA_ADMIN_PASSWORD ?? 'qa-admin-password'

/// 常规审阅宽度与最小支持宽度。移动档单列在 `mobileRoutes` 里单独走。
const viewports = [
  { name: 'w1600', width: 1600, height: 1200 },
  { name: 'w1280', width: 1280, height: 900 },
]

const hour = 3600000
const range = { from: new Date(Date.now() - hour).toISOString(), to: new Date().toISOString() }
const timeQuery = `from=${encodeURIComponent(range.from)}&to=${encodeURIComponent(range.to)}`

/// 合并之后的全部路由。实例 / 告警 / 性能事件的标识在登录之后现场认领；
/// 认不到就跳过那一张并在日志里说清楚——空库不该让整条流水失败。
function desktopRoutes({ instanceID, alertPath, eventPath }) {
  const routes = [
    ['02-instances', '/instances'],
    ['03-instance-overview', `/instances/${instanceID}?${timeQuery}`],
    ['04-monitoring', `/instances/${instanceID}/monitoring?${timeQuery}`, { settle: 4000 }],
    // 会话与阻塞：三个视图合并成一个地址的三个页签（#200），每个页签都是一张。
    ['05-sessions-current', `/instances/${instanceID}/sessions?${timeQuery}&tab=current`],
    ['06-sessions-long-query', `/instances/${instanceID}/sessions?${timeQuery}&tab=long-query-samples`],
    ['07-sessions-query-statistics', `/instances/${instanceID}/sessions?${timeQuery}&tab=query-statistics`],
    ['08-performance-events-firing', `/instances/${instanceID}/performance-events?${timeQuery}&tab=firing`],
    ['09-performance-events-recovered', `/instances/${instanceID}/performance-events?${timeQuery}&tab=recovered`],
    ['10-performance-events-disposed', `/instances/${instanceID}/performance-events?${timeQuery}&tab=disposed`],
    ['12-instance-alerts', `/instances/${instanceID}/alerts?tab=current&include_paused=false`],
    ['14-alert-rules', `/instances/${instanceID}/alerts/rules`],
    ['15-collection', `/instances/${instanceID}/collection`],
    ['16-instance-settings', `/instances/${instanceID}/settings`],
    ['17-alerts-current', '/alerts?tab=current&include_paused=false'],
    ['18-alerts-history', '/alerts?tab=history&include_paused=false'],
    // 告警设置：四个设置页合并成一个地址的四个页签（#203）。
    ['19-alert-settings-channels', '/alert-settings?tab=channels'],
    ['20-alert-settings-contacts', '/alert-settings?tab=contacts'],
    ['21-alert-settings-policies', '/alert-settings?tab=policies'],
    ['22-alert-settings-maintenance', '/alert-settings?tab=maintenance'],
    ['23-users', '/users'],
  ]
  if (eventPath) routes.push(['11-performance-event-detail', eventPath, { settle: 4000 }])
  if (alertPath) routes.push(['13-alert-detail', alertPath, { settle: 4000 }])
  return routes.sort((a, b) => a[0].localeCompare(b[0]))
}

/// 移动档只有告警流：规范中唯一要求窄屏收成单列的页面。
const mobileRoutes = [['24-alerts-mobile', '/alerts?tab=current&include_paused=false']]

const browser = await chromium.launch()

async function shot(page, dir, name, url, { full = true, settle = 2500 } = {}) {
  mkdirSync(`${out}/${dir}`, { recursive: true })
  await page.goto(`${base}${url}`, { waitUntil: 'networkidle' }).catch(() => {})
  await page.waitForTimeout(settle)
  await page.screenshot({ path: `${out}/${dir}/${name}.png`, fullPage: full })
  console.log(`  ${dir}/${name} <- ${url}`)
  // 横向溢出量一遍。整页截图的宽度永远等于视口宽度，溢出在图上看不出来，
  // 只能从文档量：`scrollWidth > innerWidth` 就是 1280 下出了横向滚动条。
  // 这**不是断言** —— 它只打印，让人去看那一页；判断仍然是人做的。
  const overflow = await page.evaluate(() => {
    if (document.documentElement.scrollWidth <= window.innerWidth) return null
    const describe = (el) => {
      const name = typeof el.className === 'string' ? el.className : (el.className?.baseVal ?? '')
      return `${el.tagName.toLowerCase()}${name ? `.${name.split(/\s+/).filter(Boolean)[0]}` : ''}`
    }
    const widest = Array.from(document.querySelectorAll('body *'))
      .filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1)
      .slice(0, 5)
      .map(describe)
    return { scrollWidth: document.documentElement.scrollWidth, innerWidth: window.innerWidth, widest }
  })
  if (overflow) {
    console.log(`  !! 横向溢出 ${overflow.scrollWidth} > ${overflow.innerWidth}：${overflow.widest.join(' / ')}`)
  }
}

async function signIn(page) {
  await page.goto(`${base}/login`, { waitUntil: 'networkidle' }).catch(() => {})
  await page.fill('#username, input[autocomplete="username"]', 'admin')
  await page.fill('input[autocomplete="current-password"]', password)
  await page.click('button[type="submit"]')
  await page.waitForURL('**/instances', { timeout: 15000 })
}

/// 从当前页面里认第一条匹配的链接。列表页给得出详情地址，就不必去猜 API 的形状。
/// 轮询而不是取一次：列表是取完数才渲染的，`networkidle` 之后第一帧里往往还只有骨架行。
async function firstHref(page, pattern, { attempts = 10 } = {}) {
  for (let i = 0; i < attempts; i += 1) {
    const hrefs = await page.$$eval('a[href]', (nodes) => nodes.map((n) => n.getAttribute('href')))
    const found = hrefs.find((href) => href && pattern.test(href))
    if (found) return found
    await page.waitForTimeout(1000)
  }
  return undefined
}

async function run({ name, width, height }, routesFor, { detail }) {
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width, height },
    deviceScaleFactor: 1,
  })
  const page = await context.newPage()
  console.log(`viewport ${name} (${width}x${height})`)

  await shot(page, name, '01-login', '/login', { full: false })
  await signIn(page)

  let instanceID = process.env.QA_INSTANCE_ID
  if (!instanceID) {
    // 列表里的实例链接带着继承下来的时间范围查询串，先切掉 `?` 再取路径段 ——
    // 不切的话取到的是「uuid?from=…」，后面每个地址都会被拼成一个还能打开、
    // 但打开的是总览页的怪地址，而截图会看起来「只是内容不太对」。
    const href = await firstHref(page, /^\/instances\/[0-9a-f-]{36}/)
    instanceID = href?.split('?')[0].split('/')[2]
  }
  if (!instanceID) throw new Error('no instance found: run scripts/qa-up.sh first')

  let alertPath
  let eventPath
  if (detail) {
    await page.goto(`${base}/alerts?tab=current&include_paused=false`, { waitUntil: 'networkidle' }).catch(() => {})
    await page.waitForTimeout(2000)
    alertPath = await firstHref(page, /^\/instances\/[0-9a-f-]{36}\/alerts\/[0-9a-f-]{36}/, { attempts: 3 })
    if (!alertPath) console.log('  (no alert instance yet — skipping 13-alert-detail)')

    const eventsURL = `/instances/${instanceID}/performance-events?${timeQuery}&tab=firing`
    await page.goto(`${base}${eventsURL}`, { waitUntil: 'networkidle' }).catch(() => {})
    await page.waitForTimeout(2000)
    eventPath = await firstHref(page, /^\/instances\/[0-9a-f-]{36}\/performance-events\/[^/]+/, { attempts: 3 })
    if (!eventPath) console.log('  (no performance event yet — skipping 11-performance-event-detail)')
  }

  for (const [shotName, url, options] of routesFor({ instanceID, alertPath, eventPath })) {
    await shot(page, name, shotName, url, options)
  }
  await context.close()
}

for (const viewport of viewports) {
  await run(viewport, desktopRoutes, { detail: true })
}
await run({ name: 'w390', width: 390, height: 844 }, () => mobileRoutes, { detail: false })

await browser.close()
console.log(`\nshots in ${out} — 人工比对用，不作自动断言`)
