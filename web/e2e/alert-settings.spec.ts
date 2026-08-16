import { expect, test } from '@playwright/test'
import type { components } from '../src/api/schema'

type NotificationPolicy = components['schemas']['NotificationPolicy']

const defaultPolicy = {
  id: '00000000-0000-4000-8000-000000000044',
  name: '默认策略',
  is_default: true,
  contact_ids: [],
  contact_group_ids: [],
  channels: [{ channel: 'SMTP' }],
  severity_filter: ['critical', 'warning', 'info'],
  notify_on_fire: true,
  notify_on_recovery: true,
  repeat_interval: 3600,
  created_at: '2026-08-11T12:00:00Z',
  updated_at: '2026-08-11T12:00:00Z',
} satisfies NotificationPolicy

test('[AC-04-S3] keeps all four alert settings pages reachable and read-only controls explicit', async ({ page }) => {
  await page.route('**/api/v1/me', (route) => route.fulfill({ json: { username: 'readonly', role: 'READONLY' } }))
  await page.route('**/api/v1/notification-channels/smtp', (route) => route.fulfill({ json: { configured: false, auth_configured: false } }))
  await page.route('**/api/v1/notification-channels/webhooks', (route) => route.fulfill({ json: [{
    id: '00000000-0000-4000-8000-000000000080',
    name: 'On-call gateway',
    enabled: true,
    url: 'https://gateway.example.com/alerts',
    signing_configured: true,
    created_at: '2026-08-16T00:00:00Z',
    updated_at: '2026-08-16T00:00:00Z',
  }] }))
  await page.route('**/api/v1/notification-channels/failures', (route) => route.fulfill({ json: {
    has_failures: true,
    channels: [{
      channel: 'WEBHOOK',
      target_id: '00000000-0000-4000-8000-000000000080',
      target: 'https://gateway.example.com/alerts',
      recent_failure_count: 1,
      last_failure_reason: 'HTTP 503',
      last_failed_at: '2026-08-16T00:01:00Z',
      recent_failures: [{
        failed_at: '2026-08-16T00:01:00Z',
        target: 'https://gateway.example.com/alerts',
        reason: 'receiver unavailable',
        retry_count: 2,
      }],
    }],
  } }))
  await page.route('**/api/v1/notification-contacts', (route) => route.fulfill({ json: [] }))
  await page.route('**/api/v1/notification-contact-groups', (route) => route.fulfill({ json: [] }))
  await page.route('**/api/v1/notification-policies', (route) => route.fulfill({ json: [defaultPolicy] }))
  await page.route('**/api/v1/maintenance-windows', (route) => route.fulfill({ json: [] }))
  await page.route('**/api/v1/instances', (route) => route.fulfill({ json: [] }))

  await page.goto('/alert-settings/notifications')
  await expect(page.getByRole('tab', { name: '通知渠道' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tab', { name: '通知渠道' }).locator('.ant-badge-dot')).toBeVisible()
  await expect(page.getByText('需要告警管理员角色才能修改配置或发送测试通知')).toBeVisible()
  await expect(page.getByRole('button', { name: '保存' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '新建目标' })).toBeDisabled()
  await expect(page.getByText('签名已设置')).toBeVisible()
  await expect(page.getByText(/\*{8}/)).toHaveCount(0)
  await page.getByText('最近失败 1 次').click()
  await expect(page.getByText('receiver unavailable')).toBeVisible()

  await page.getByRole('link', { name: '联系人', exact: true }).click()
  await expect(page).toHaveURL(/\/alert-settings\/contacts$/)
  await expect(page.getByText('需要告警管理员角色才能修改联系人和联系人组')).toBeVisible()
  await expect(page.getByRole('button', { name: /新建联系人$/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: '新建联系人组' })).toBeDisabled()

  await page.getByRole('link', { name: '通知策略' }).click()
  await expect(page).toHaveURL(/\/alert-settings\/policies$/)
  await expect(page.getByText('需要告警管理员角色才能修改通知策略')).toBeVisible()
  await expect(page.getByRole('button', { name: '新建策略' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '删除 默认策略' })).toBeDisabled()

  await page.getByRole('link', { name: '维护窗口' }).click()
  await expect(page).toHaveURL(/\/alert-settings\/maintenance-windows$/)
  await expect(page.getByText('需要告警管理员角色才能管理维护窗口')).toBeVisible()
  await expect(page.getByRole('button', { name: '新建维护窗口' })).toBeDisabled()
  await expect(page.getByRole('tab', { name: '生效中 0' })).toBeVisible()
})
