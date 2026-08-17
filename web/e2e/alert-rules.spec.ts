import { expect, test } from '@playwright/test'
import type { components } from '../src/api/schema'

type AlertRule = components['schemas']['AlertRule']
type AlertRuleInput = components['schemas']['AlertRuleInput']
type AlertRuleTemplate = components['schemas']['AlertRuleTemplate']
type Instance = components['schemas']['Instance']

const instanceID = '11111111-1111-4111-8111-111111111111'
const builtinRuleID = '22222222-2222-4222-8222-222222222222'
const createdRuleID = '33333333-3333-4333-8333-333333333333'

const instance = {
  id: instanceID,
  name: '生产库 primary',
  host: '10.20.1.15',
  port: 5432,
  database: 'orders',
  username: 'monitor',
  agent_metrics_enabled: true,
  alert_status: 'OK',
  agent_status: 'online',
  health: {
    status: 'HEALTHY',
    counts: { critical: 0, warning: 0, info: 0 },
    flags: { no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0 },
  },
  collection_pause: { paused: false },
} satisfies Instance

const builtinRule = {
  id: builtinRuleID,
  name: '数据库不可达',
  metric_id: 'pg.availability.reachable',
  aggregation: 'latest',
  operator: '=',
  threshold: 0,
  recovery_operator: '=',
  recovery_threshold: 1,
  window_seconds: 30,
  consecutive_count: 3,
  recovery_consecutive_count: 3,
  severity: 'critical',
  no_data_policy: 'mark_no_data',
  scope: 'ALL',
  instance_ids: [],
  evaluation_interval_seconds: 30,
  enabled: true,
  is_builtin: true,
  effective_notification_policy_name: '默认策略（继承）',
  current_alert_count: 0,
  version: 1,
  created_at: '2026-08-11T12:00:00Z',
  updated_at: '2026-08-11T12:00:00Z',
} satisfies AlertRule

const template = {
  id: 'cpu_high',
  version: 1,
  name: 'CPU 使用率过高',
  metric_id: 'host.cpu.usage_percent',
  aggregation: 'avg',
  operator: '>',
  threshold: 80,
  recovery_operator: '<',
  recovery_threshold: 70,
  window_seconds: 300,
  consecutive_count: 5,
  recovery_consecutive_count: 5,
  severity: 'warning',
  no_data_policy: 'mark_no_data',
  evaluation_interval_seconds: 60,
} satisfies AlertRuleTemplate

test('creates alert rules and keeps built-in protections visible', async ({ page }) => {
  const rules: AlertRule[] = [{ ...builtinRule }]
  let submittedRule: AlertRuleInput | undefined

  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'alert-admin', role: 'ALERT_ADMIN' } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({ json: instance }))
  await page.route('**/api/v1/instances', (route) => route.fulfill({ json: [instance] }))
  await page.route(`**/api/v1/instances/${instanceID}/collection/tasks`, (route) => route.fulfill({ json: [] }))
  await page.route(`**/api/v1/instances/${instanceID}/collection/capabilities`, (route) => route.fulfill({ json: [] }))
  await page.route('**/api/v1/alert-rule-templates', (route) => route.fulfill({ json: [template] }))
  await page.route('**/api/v1/alert-rule-templates/*/alert-rules', (route) => {
    const fromTemplate = {
      ...builtinRule,
      ...template,
      id: '44444444-4444-4444-8444-444444444444',
      scope: 'ALL',
      instance_ids: [],
      enabled: true,
      is_builtin: false,
      effective_notification_policy_name: '默认策略（继承）',
      current_alert_count: 0,
      created_at: '2026-08-11T12:01:00Z',
      updated_at: '2026-08-11T12:01:00Z',
    } satisfies AlertRule
    rules.push(fromTemplate)
    return route.fulfill({ status: 201, json: fromTemplate })
  })
  await page.route('**/api/v1/alert-rules', async (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: rules })
    submittedRule = route.request().postDataJSON() as AlertRuleInput
    const created = {
      ...builtinRule,
      ...submittedRule,
      id: createdRuleID,
      is_builtin: false,
      effective_notification_policy_name: '默认策略（继承）',
      current_alert_count: 0,
      created_at: '2026-08-11T12:02:00Z',
      updated_at: '2026-08-11T12:02:00Z',
    } satisfies AlertRule
    rules.push(created)
    return route.fulfill({ status: 201, json: created })
  })

  await page.goto(`/instances/${instanceID}/alerts/rules`)

  await expect(page.getByText('数据库不可达')).toBeVisible()
  await expect(page.getByText('不可停用')).toBeVisible()
  await expect(page.getByText('默认策略（继承）')).toBeVisible()

  await page.getByRole('button', { name: '规则模板' }).click()
  await expect(page.getByText('CPU 使用率过高')).toBeVisible()
  await page.getByRole('button', { name: '一键创建' }).click()
  await expect(page.getByText('CPU 使用率过高')).toBeVisible()

  await page.getByRole('button', { name: '新建规则' }).click()
  await page.getByLabel('规则名称').fill('自定义连接告警')
  await expect(page.getByRole('dialog', { name: '新建告警规则' }).getByText('连续 3 次 × 30 秒 ≈ 1 分 30 秒')).toBeVisible()
  await page.getByRole('button', { name: /保\s*存/ }).click()

  await expect(page.getByText('自定义连接告警')).toBeVisible()
  expect(submittedRule).toMatchObject({
    name: '自定义连接告警',
    metric_id: 'pg.connection.total',
    consecutive_count: 3,
    evaluation_interval_seconds: 30,
  })
  await page.screenshot({ path: test.info().outputPath('alert-rules-desktop.png'), fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('alert-rules-mobile.png'), fullPage: true })
})
