import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { components } from '../../api/schema'
import { AgentRegistrationPanel, AgentTokenModal, CredentialSummary, InstanceRemovalPanel, buildAgentInstallCommand } from './settings'

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
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

describe('credential summary', () => {
  it('shows a fixed mask without a stored-password reveal control', () => {
    render(<CredentialSummary username="monitor_user" />)

    expect(screen.getByText('monitor_user')).toBeTruthy()
    expect(screen.getByText('已设置')).toBeTruthy()
    const mask = screen.getByLabelText('密码状态') as HTMLInputElement
    expect(mask.value).toBe('************')
    expect(mask.type).toBe('password')
    expect(screen.queryByRole('button')).toBeNull()
  })
})

describe('instance removal', () => {
  it('requires the exact instance name before removal', () => {
    const onRemove = vi.fn()
    const view = render(<InstanceRemovalPanel instanceName="production-db" canRemove={false} actionPending={false} onRemove={onRemove} />)

    expect((screen.getByRole('button', { name: /移.*除.*实.*例/ }) as HTMLButtonElement).disabled).toBe(true)
    view.rerender(<InstanceRemovalPanel instanceName="production-db" canRemove actionPending={false} onRemove={onRemove} />)

    fireEvent.click(screen.getByRole('button', { name: /移.*除.*实.*例/ }))
    const confirm = screen.getByRole('button', { name: /确.*认.*移.*除/ }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)

    fireEvent.change(screen.getByLabelText('输入实例名确认移除'), { target: { value: 'production' } })
    expect(confirm.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('输入实例名确认移除'), { target: { value: 'production-db' } })
    expect(confirm.disabled).toBe(false)

    fireEvent.click(confirm)
    expect(onRemove).toHaveBeenCalledOnce()
  })
})

const registration: components['schemas']['AgentRegistration'] = {
  state: 'EXPECTED_ONLINE',
  agent_expected: true,
  installation: {
    ca_fingerprint_sha256: 'abc123',
    installer_path: '/api/agent/install/install.sh',
    authentication_path: '/etc/dbs-monitor-agent/token',
    file_mode: '0600',
    restart_command: 'systemctl restart dbs-monitor-agent.service',
  },
}

describe('Agent one-time token', () => {
  it('removes the token and installation command after the modal closes', () => {
    function Harness() {
      const [issued, setIssued] = useState<{ instanceId: string; token: string; registration: typeof registration } | null>({
        instanceId: '00000000-0000-0000-0000-000000000069', token: 'one-time-token', registration,
      })
      return <AgentTokenModal issued={issued} onClose={() => setIssued(null)} />
    }
    render(<Harness />)

    expect(screen.getByDisplayValue('one-time-token')).toBeTruthy()
    expect(screen.getByLabelText('Agent 安装命令')).toBeTruthy()
    // 名字写死成「关闭」而不是 /关.*闭/：对话框右上角的关闭按钮现在叫「关闭对话框」
    // （primitives/Modal 的中文默认值），正则会同时命中它和页脚的「关闭」。
    // 与 routes/users/users.test.tsx 里同一个模式的收窄一致。
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.queryByDisplayValue('one-time-token')).toBeNull()
    expect(screen.queryByLabelText('Agent 安装命令')).toBeNull()
  })

  it('builds a pinned TLS bootstrap command without an insecure fallback', () => {
    const command = buildAgentInstallCommand('https://monitor.example:8443', 'instance-id', 'one-time-token', registration)
    expect(command).toContain('abc123')
    expect(command).toContain('openssl s_client')
    expect(command).toContain('--cacert "$ca"')
    expect(command).toContain('one-time-token')
    expect(command).not.toContain('--insecure')
  })
})

describe('Agent registration lifecycle', () => {
  it.each([
    ['NEVER_REGISTERED', '从未登记', ['登记']],
    ['EXPECTED_ONLINE', '应在线', ['轮换', '吊销', '停用']],
    ['REVOKED', '已吊销', ['停用']],
    ['DISABLED', '已停用', ['重新启用']],
  ] as const)('shows %s with its valid actions', (state, label, actions) => {
    render(
      <AgentRegistrationPanel
        registration={{ ...registration, state, agent_expected: state === 'EXPECTED_ONLINE' || state === 'REVOKED' }}
        canManage
        actionPending={false}
        onRegister={() => undefined}
        onRotate={() => undefined}
        onRevoke={() => undefined}
        onDisable={() => undefined}
      />,
    )
    expect(screen.getByText(label)).toBeTruthy()
    for (const action of actions) {
      expect(screen.getByRole('button', { name: new RegExp(action.split('').join('.*')) })).toBeTruthy()
    }
  })
})
