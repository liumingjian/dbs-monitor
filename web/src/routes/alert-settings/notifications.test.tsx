import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { NotificationSettingsLabel } from '../root'
import { WebhookTargetsTable } from './notifications'

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  Object.defineProperty(globalThis, 'ResizeObserver', {
    writable: true,
    value: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  })
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

afterEach(cleanup)

describe('Webhook notification settings', () => {
  it('keeps targets and failure details visible while disabling read-only actions', () => {
    const actions = { edit: vi.fn(), remove: vi.fn(), test: vi.fn() }
    render(
      <WebhookTargetsTable
        targets={[{
          id: '00000000-0000-4000-8000-000000000080',
          name: 'On-call gateway',
          enabled: true,
          url: 'https://gateway.example.com/alerts',
          signing_configured: true,
          created_at: '2026-08-11T12:00:00Z',
          updated_at: '2026-08-11T12:00:00Z',
        }]}
        failures={[{
          channel: 'WEBHOOK',
          target_id: '00000000-0000-4000-8000-000000000080',
          target: 'https://gateway.example.com/alerts',
          recent_failure_count: 21,
          last_failure_reason: 'HTTP 503',
          last_failed_at: '2026-08-11T12:01:00Z',
          recent_failures: [{
            failed_at: '2026-08-11T12:01:00Z',
            target: 'https://gateway.example.com/alerts',
            reason: 'receiver unavailable',
            retry_count: 2,
          }],
        }]}
        canManage={false}
        onEdit={actions.edit}
        onDelete={actions.remove}
        onTest={actions.test}
      />,
    )

    expect(screen.getByText('On-call gateway')).toBeTruthy()
    expect(screen.getByText('签名已设置 ********')).toBeTruthy()
    expect(screen.getByText('最近失败 21 次')).toBeTruthy()
    for (const name of ['测试 On-call gateway', '编辑 On-call gateway', '删除 On-call gateway']) {
      expect(screen.getByRole('button', { name })).toHaveProperty('disabled', true)
    }

    fireEvent.click(screen.getByText('最近失败 21 次'))
    expect(screen.getByText('receiver unavailable')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
    expect(actions.edit).not.toHaveBeenCalled()
    expect(actions.remove).not.toHaveBeenCalled()
    expect(actions.test).not.toHaveBeenCalled()
  })

  it('shows and clears the navigation failure badge', () => {
    const view = render(<NotificationSettingsLabel hasFailures={false} />)
    expect(view.container.querySelector('.ant-badge-dot')).toBeNull()

    view.rerender(<NotificationSettingsLabel hasFailures />)
    expect(view.container.querySelector('.ant-badge-dot')).toBeTruthy()
  })
})
