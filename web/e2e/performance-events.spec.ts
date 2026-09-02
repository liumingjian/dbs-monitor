import { expect, test } from '@playwright/test'

const instanceID = '10000000-0000-4000-8000-000000000001'
const alertID = '20000000-0000-4000-8000-000000000001'
const eventID = '30000000-0000-4000-8000-000000000001'
const actorID = '40000000-0000-4000-8000-000000000001'

const eventRange = {
  from: '2026-08-11T10:00:00.000Z',
  to: '2026-08-11T11:00:00.000Z',
}

function performanceEvent(disposition: 'NONE' | 'ACKED' = 'NONE') {
  return {
    id: eventID,
    instance_id: instanceID,
    alert_instance_id: alertID,
    event_type: 'LOCK_BLOCKING',
    alert_status: 'FIRING',
    severity: 'critical',
    disposition,
    derived_at: '2026-08-11T10:15:00Z',
    updated_at: '2026-08-11T10:45:00Z',
    duration_ms: 1_800_000,
    metric_id: 'pg.lock.waiting_count',
    threshold: 5,
    trigger_value: 12,
    cause_summary: '锁等待 12 已超过阈值 5，等待会话可能被长事务阻塞。',
    suggested_action: '检查阻塞链顶端会话，并与业务方确认后处理阻塞事务。',
    trigger_snapshot_result: 'SUCCESS',
  }
}

function alertDetail(disposition: 'NONE' | 'ACKED') {
  return {
    id: alertID,
    instance_id: instanceID,
    instance_name: 'payments-primary',
    rule_id: '50000000-0000-4000-8000-000000000001',
    rule_name: 'Lock waits high',
    rule_version: 1,
    rule_snapshot: { threshold: 5 },
    metric_id: 'pg.lock.waiting_count',
    status: 'FIRING',
    severity: 'critical',
    disposition,
    paused: false,
    current_value: 12,
    threshold: 5,
    first_triggered_at: '2026-08-11T10:15:00Z',
    updated_at: '2026-08-11T10:45:00Z',
    duration_ms: 1_800_000,
    rule_version_history: [],
    notification_results: [],
  }
}

function instance() {
  return {
    id: instanceID,
    name: 'payments-primary',
    host: '127.0.0.1',
    port: 5432,
    engine: 'POSTGRESQL',
    database: 'payments',
    username: 'monitor',
    agent_metrics_enabled: true,
    alert_status: 'FIRING',
    health: {
      status: 'CRITICAL',
      counts: { critical: 1, warning: 0, info: 0 },
      flags: { no_data: false, in_maintenance: false, recently_recovered: false, ignored: 0, configuration_missing: 0 },
    },
    agent_status: 'online',
    last_collected_at: '2026-08-11T10:45:00Z',
    collection_pause: { paused: false },
  }
}

test('alert-derived event writes disposition back and preserves trigger evidence', async ({ page }) => {
  let disposition: 'NONE' | 'ACKED' = 'NONE'
  let snapshotResult: 'SUCCESS' | 'FAILED' | 'NOT_APPLICABLE' = 'SUCCESS'
  let submittedDisposition: unknown

  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'alert-admin', role: 'ALERT_ADMIN' } }))
  await page.route('**/api/v1/notification-channels/failures', (route) => route.fulfill({ json: { has_failures: false, channels: [] } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({ json: instance() }))
  await page.route('**/api/v1/instances/*/performance-events*', (route) => {
    const url = new URL(route.request().url())
    expect(url.searchParams.get('recovered')).toBe('false')
    return route.fulfill({ json: { total: 1, items: [performanceEvent(disposition)] } })
  })
  await page.route(`**/api/v1/performance-events/${eventID}`, (route) => route.fulfill({ json: performanceEvent(disposition) }))
  await page.route(`**/api/v1/alert-instances/${alertID}/disposition`, async (route) => {
    if (route.request().method() === 'PUT') {
      submittedDisposition = route.request().postDataJSON()
      disposition = 'ACKED'
    }
    return route.fulfill({
      json: {
        alert_instance_id: alertID,
        disposition,
        disposition_by: disposition === 'ACKED' ? actorID : undefined,
        disposition_at: disposition === 'ACKED' ? '2026-08-11T10:50:00Z' : undefined,
        note: disposition === 'ACKED' ? '正在处理阻塞事务' : undefined,
        stops_repeat_notifications: disposition === 'ACKED',
        excluded_from_health_rollup: false,
        history: disposition === 'ACKED' ? [{
          kind: 'ACKED',
          from_disposition: 'NONE',
          to_disposition: 'ACKED',
          actor_id: actorID,
          note: '正在处理阻塞事务',
          rule_version: 1,
          current_value: 12,
          rule_snapshot: { threshold: 5 },
          evaluated_at: '2026-08-11T10:45:00Z',
          acted_at: '2026-08-11T10:50:00Z',
        }] : [],
      },
    })
  })
  await page.route(`**/api/v1/alert-instances/${alertID}/trigger-snapshot`, (route) => {
    if (snapshotResult === 'FAILED') {
      return route.fulfill({ json: {
        result: 'FAILED',
        metric_id: 'pg.lock.waiting_count',
        captured_at: '2026-08-11T10:15:01Z',
        original_match_count: 0,
        truncated: false,
        failure_reason: 'statement timeout',
        sessions: [],
      } })
    }
    if (snapshotResult === 'NOT_APPLICABLE') {
      return route.fulfill({ json: {
        result: 'NOT_APPLICABLE',
        metric_id: 'pg.replication.wal_lag_bytes',
        original_match_count: 0,
        truncated: false,
        sessions: [],
      } })
    }
    return route.fulfill({ json: {
      result: 'SUCCESS',
      metric_id: 'pg.lock.waiting_count',
      captured_at: '2026-08-11T10:15:01Z',
      original_match_count: 2,
      truncated: false,
      sessions: [
        { pid: 8121, username: 'app', database_name: 'payments', state: 'active', wait_event_type: 'Lock', wait_event: 'transactionid', blocking_pids: [8110] },
        { pid: 8110, username: 'worker', database_name: 'payments', state: 'idle in transaction', blocking_pids: [] },
      ],
    } })
  })
  await page.route(`**/api/v1/alert-instances/${alertID}`, (route) => route.fulfill({ json: alertDetail(disposition) }))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({ json: {
    from: eventRange.from,
    to: eventRange.to,
    step: '1m',
    metrics: [{ metric: 'pg.lock.waiting_count', unit: 'count', unavailability: null, series: [{ labels: {}, points: [[1786443300, 12]] }] }],
  } }))

  await page.goto(`/instances/${instanceID}/performance-events?from=${encodeURIComponent(eventRange.from)}&to=${encodeURIComponent(eventRange.to)}&tab=firing&disposition=ACKED&page=1`)
  await expect(page.getByRole('tab', { name: /性能事件/ })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('row', { name: /锁等待 \/ 阻塞/ })).toBeVisible()
  await page.getByRole('row', { name: /锁等待 \/ 阻塞/ }).getByRole('link', { name: '详情' }).click()

  await expect(page.getByRole('heading', { name: '锁等待 / 阻塞' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '关联指标图' })).toBeVisible()
  await expect(page.getByLabel('锁等待 / 阻塞趋势')).toBeVisible()
  await expect(page.getByRole('heading', { name: '原因摘要' })).toBeVisible()
  await expect(page.getByText('检查阻塞链顶端会话，并与业务方确认后处理阻塞事务。')).toBeVisible()
  await expect(page.getByRole('heading', { name: '告警触发时现场' })).toBeVisible()
  await expect(page.getByText('以下证据捕获于关联告警触发时，不代表当前状态')).toBeVisible()
  await expect(page.getByText('被 PID 8110 阻塞')).toBeVisible()

  const monitoringHref = await page.getByRole('link', { name: /查看标准监控/ }).getAttribute('href')
  if (monitoringHref === null) throw new Error('standard monitoring link is missing an href')
  const monitoring = new URL(monitoringHref, 'https://example.test')
  expect(monitoring.searchParams.get('metric')).toBe('pg.lock.waiting_count')
  expect(monitoring.searchParams.get('from')).toBe('2026-08-11T10:15:00.000Z')
  expect(monitoring.searchParams.get('to')).toBe('2026-08-11T10:45:00.000Z')

  await page.getByRole('button', { name: '确认' }).click()
  await page.getByLabel('备注').fill('正在处理阻塞事务')
  await page.getByRole('button', { name: /提\s*交/ }).click()
  await expect.poll(() => submittedDisposition).toEqual({ disposition: 'ACKED', note: '正在处理阻塞事务' })
  await expect(page.getByText('正在处理阻塞事务').first()).toBeVisible()

  await page.getByRole('link', { name: /查看告警详情/ }).click()
  for (const heading of ['触发指标', '规则快照', 'No Data 原因', '采集状态', '通知结果', '处置记录', '触发现场快照', '关联性能事件']) {
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  }
  await expect(page.getByText('正在处理阻塞事务').first()).toBeVisible()
  await expect(page.getByText('被 PID 8110 阻塞')).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('performance-event-alert-detail-desktop.png'), fullPage: true })

  snapshotResult = 'FAILED'
  await page.reload()
  await expect(page.getByText('现场快照采集失败')).toBeVisible()
  await expect(page.getByText('statement timeout')).toBeVisible()

  snapshotResult = 'NOT_APPLICABLE'
  await page.reload()
  await expect(page.getByText('该类型不采集现场快照')).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: '触发现场快照' })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('performance-event-alert-detail-mobile.png'), fullPage: true })
})

// #197 的表单层验收：客户端校验、字段联动、服务端字段错误回填与聚焦、重置。
// 处置表单是唯一四件齐全的表单，所以整条链在这里证明一次。
test('disposition form validates, links fields, and lands server field errors on the offending input', async ({ page }) => {
  let dispositionFailure: { field: string; message: string } | null = null
  let submitted: unknown

  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'alert-admin', role: 'ALERT_ADMIN' } }))
  await page.route('**/api/v1/notification-channels/failures', (route) => route.fulfill({ json: { has_failures: false, channels: [] } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({ json: instance() }))
  await page.route(`**/api/v1/performance-events/${eventID}`, (route) => route.fulfill({ json: performanceEvent() }))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({ json: {
    from: eventRange.from,
    to: eventRange.to,
    step: '1m',
    metrics: [{ metric: 'pg.lock.waiting_count', unit: 'count', unavailability: null, series: [{ labels: {}, points: [[1786443300, 12]] }] }],
  } }))
  await page.route(`**/api/v1/alert-instances/${alertID}/trigger-snapshot`, (route) => route.fulfill({ json: {
    result: 'NOT_APPLICABLE',
    metric_id: 'pg.replication.wal_lag_bytes',
    original_match_count: 0,
    truncated: false,
    sessions: [],
  } }))
  await page.route(`**/api/v1/alert-instances/${alertID}/disposition`, (route) => {
    if (route.request().method() === 'PUT') {
      submitted = route.request().postDataJSON()
      if (dispositionFailure) {
        return route.fulfill({
          status: 400,
          json: { error: { code: 'VALIDATION_FAILED', message: 'alert disposition validation failed', field_errors: [dispositionFailure] } },
        })
      }
    }
    return route.fulfill({ json: {
      alert_instance_id: alertID,
      disposition: 'NONE',
      stops_repeat_notifications: false,
      excluded_from_health_rollup: false,
      history: [],
    } })
  })

  const search = `from=${encodeURIComponent(eventRange.from)}&to=${encodeURIComponent(eventRange.to)}&tab=firing&disposition=ACKED&page=1`
  await page.goto(`/instances/${instanceID}/performance-events/${eventID}?${search}`)
  await page.getByRole('button', { name: '忽略' }).click()

  // 客户端校验：错误出现在字段下方，不是页面顶部的汇总条。
  await page.getByRole('button', { name: /提\s*交/ }).click()
  await expect(page.getByText('请选择忽略原因')).toBeVisible()
  expect(submitted).toBeUndefined()

  // 字段联动：只有「其他」才要求补充说明，字段本身也只在这时出现。
  await expect(page.getByLabel('补充说明')).toHaveCount(0)
  await page.getByLabel(/忽略原因/).selectOption('OTHER')
  await expect(page.getByLabel(/补充说明/)).toBeVisible()
  await page.getByRole('button', { name: /提\s*交/ }).click()
  await expect(page.getByText('请输入补充说明')).toBeVisible()
  expect(submitted).toBeUndefined()

  // 服务端字段错误：落到对应输入框，并把焦点放上去。
  dispositionFailure = { field: 'ignore_reason_detail', message: '补充说明与已有记录冲突' }
  await page.getByLabel(/补充说明/).fill('等待业务方确认')
  await page.getByRole('button', { name: /提\s*交/ }).click()
  await expect.poll(() => submitted).toEqual({
    disposition: 'IGNORED',
    ignore_reason_code: 'OTHER',
    ignore_reason_detail: '等待业务方确认',
  })
  await expect(page.getByText('补充说明与已有记录冲突')).toBeVisible()
  await expect(page.getByLabel(/补充说明/)).toBeFocused()

  // 重置：关掉再打开，值、联动出来的字段与错误全部回到初始状态。
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '忽略' }).click()
  await expect(page.getByText('补充说明与已有记录冲突')).toHaveCount(0)
  await expect(page.getByText('请输入补充说明')).toHaveCount(0)
  await expect(page.getByLabel(/补充说明/)).toHaveCount(0)
  await expect(page.getByLabel(/忽略原因/)).toHaveValue('')
})
