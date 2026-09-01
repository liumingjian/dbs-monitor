import { expect, test } from '@playwright/test'
import { standardMonitoringGroups, standardMonitoringMetricIDs } from '../src/routes/instances.$id/standardMonitoring'

const instanceID = '11111111-1111-4111-8111-111111111111'
const from = '2026-08-11T10:00:00.000Z'
const to = '2026-08-11T11:00:00.000Z'
const unavailabilityCases = [
  { code: 'NO_SAMPLES_YET', title: '等待首个样本', action: '稍后刷新', destination: 'current' },
  { code: 'NO_DATA_IN_RANGE', title: '所选范围没有数据', action: '扩大时间范围', destination: 'current' },
  { code: 'STALE', title: '数据已过期', action: '检查采集状态', destination: 'collection' },
  { code: 'COLLECTION_PAUSED', title: '采集已暂停', action: '查看采集设置', destination: 'collection' },
  { code: 'COLLECTION_FAILED', title: '采集失败', action: '查看失败原因', destination: 'collection' },
  { code: 'DB_UNREACHABLE', title: '数据库不可达', action: '检查网络与连接信息', destination: 'collection' },
  { code: 'AGENT_OFFLINE', title: 'Agent 离线', action: '检查 Agent 服务', destination: 'collection' },
  { code: 'PERMISSION_DENIED', title: '权限不足', action: '补齐监控权限', destination: 'collection' },
  { code: 'EXTENSION_MISSING', title: '扩展未安装', action: '安装所需扩展', destination: 'collection' },
  { code: 'FEATURE_DISABLED', title: '功能未启用', action: '启用数据库功能', destination: 'collection' },
  { code: 'VERSION_UNSUPPORTED', title: '版本不支持', action: '查看支持矩阵', destination: 'collection' },
  { code: 'NOT_APPLICABLE_ROLE', title: '当前角色不适用', action: '查看实例角色', destination: 'collection' },
  { code: 'COUNTER_RESET', title: '计数器已重置', action: '等待下一个采集周期', destination: 'current' },
] as const

test('all 13 backend codes render copy and canonical destinations without blank links', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'reader', role: 'READONLY' } }))
  await page.route(`**/api/v1/instances/${instanceID}`, (route) => route.fulfill({
    json: {
      id: instanceID,
      name: '生产库 primary',
      host: '10.20.1.15',
      port: 5432,
      engine: 'POSTGRESQL',
      database: 'orders',
      username: 'monitor',
      alert_status: 'OK',
      agent_metrics_enabled: true,
    },
  }))

  const charts = standardMonitoringGroups.flatMap((group) => group.charts)
  const codeByMetric = new Map(charts.slice(0, unavailabilityCases.length).flatMap((chart, index) =>
    chart.metrics.map((metric) => [metric, unavailabilityCases[index].code] as const),
  ))
  await page.route('**/api/v1/instances/*/metrics/series*', (route) => route.fulfill({
    json: {
      from,
      to,
      step: '30s',
      metrics: standardMonitoringMetricIDs.map((metric, index) => {
        const unavailability = codeByMetric.get(metric)
        return {
          metric,
          unit: 'count',
          unavailability: unavailability ?? null,
          series: unavailability ? [] : [{ labels: {}, points: [[1786442400, index + 1]] }],
        }
      }),
    },
  }))

  await page.goto(`/instances/${instanceID}/monitoring?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)

  for (const [index, item] of unavailabilityCases.entries()) {
    const chart = charts[index]
    const card = page.getByTestId('metric-card').filter({ has: page.getByText(chart.title, { exact: true }) })
    await expect(card.getByText(item.title, { exact: true })).toBeVisible()
    const link = card.getByRole('link', { name: item.action })
    const expected = item.destination === 'current'
      ? '#monitoring-controls'
      : `/instances/${instanceID}/collection?metric=${encodeURIComponent(chart.metrics[0])}`
    await expect(link).toHaveAttribute('href', expected)
  }

  await page.goto(`/instances/${instanceID}/monitoring?from=not-a-time&to=also-not-a-time`)
  await expect(page.getByText('时间范围必须是绝对 RFC3339 时间')).toBeVisible()
  await expect(page.getByRole('button', { name: '使用最近一小时' })).toBeVisible()
  await expect(page.locator('#root')).not.toBeEmpty()
})
