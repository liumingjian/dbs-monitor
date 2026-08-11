import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { components } from '../../api/schema'
import {
  CollectionConfiguration,
  CollectionManagementView,
  ConfigurationTodo,
} from './collection'

type Capability = components['schemas']['CapabilitySnapshotEntry']
type Task = components['schemas']['CollectionTaskState']

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(cleanup)

const baseTask: Task = {
  task_id: 'pg.stat_activity',
  kind: 'sql',
  interval_seconds: 5,
  consecutive_failures: 0,
  metric_ids: ['pg.connection.active'],
  required_capabilities: ['role.pg_monitor'],
}

describe('configuration todo', () => {
  it('puts UNKNOWN before fixable missing items and links affected metrics', () => {
    const capabilities: Capability[] = [
      {
        capability_id: 'role.pg_monitor',
        class: 'fixable',
        status: 'MISSING',
        affected_metric_count: 1,
        fix_hint: '将监控账号加入 pg_monitor 角色。',
      },
      {
        capability_id: 'topo.has_slot',
        class: 'structural',
        status: 'UNKNOWN',
        affected_metric_count: 0,
      },
    ]

    render(<ConfigurationTodo capabilities={capabilities} tasks={[baseTask]} />)

    const todo = screen.getByRole('region', { name: '配置缺失待办' })
    expect(todo.textContent?.indexOf('无法检查采集能力')).toBeLessThan(todo.textContent?.indexOf('缺少 pg_monitor 角色') ?? -1)
    expect(within(todo).getByText('以下 1 项状态未知')).toBeTruthy()
    expect(within(todo).getByText('影响 1 项指标')).toBeTruthy()
    fireEvent.click(within(todo).getByText('缺少 pg_monitor 角色'))
    expect(within(todo).getByRole('link', { name: 'pg.connection.active' }).getAttribute('href')).toBe('#metric-pg.connection.active')
    expect(within(todo).getByText('将监控账号加入 pg_monitor 角色。')).toBeTruthy()
  })

  it('shows the latest observation time when every fixable capability is ready', () => {
    render(<ConfigurationTodo capabilities={[{
      capability_id: 'role.pg_monitor',
      class: 'fixable',
      status: 'PRESENT',
      observed_at: '2026-08-11T12:00:00Z',
      affected_metric_count: 1,
    }]} tasks={[baseTask]} />)

    expect(screen.getByRole('region', { name: '配置缺失待办' })).toBeTruthy()
    expect(screen.getByText(/无待办.*所有可修复的采集能力均已就绪/)).toBeTruthy()
    expect(screen.getByText(/最近检查.*2026/)).toBeTruthy()
  })

  it('never hides the module or reports ready when the snapshot is absent', () => {
    render(<ConfigurationTodo capabilities={[]} tasks={[]} />)

    expect(screen.getByRole('region', { name: '配置缺失待办' })).toBeTruthy()
    expect(screen.getByText(/无法检查采集能力/)).toBeTruthy()
    expect(screen.queryByText(/所有可修复的采集能力均已就绪/)).toBeNull()
  })

  it('keeps structural NOT_APPLICABLE facts out of the todo', () => {
    render(<ConfigurationTodo capabilities={[
      {
        capability_id: 'role.pg_monitor',
        class: 'fixable',
        status: 'PRESENT',
        observed_at: '2026-08-11T12:00:00Z',
        affected_metric_count: 1,
      },
      {
        capability_id: 'topo.has_slot',
        class: 'structural',
        status: 'NOT_APPLICABLE',
        observed_at: '2026-08-11T12:00:00Z',
        na_reason: '本实例没有 replication slot。',
        affected_metric_count: 0,
      },
    ]} tasks={[baseTask]} />)

    const todo = screen.getByRole('region', { name: '配置缺失待办' })
    expect(within(todo).queryByText(/replication slot/)).toBeNull()
    expect(within(todo).getByText(/所有可修复的采集能力均已就绪/)).toBeTruthy()
  })
})

describe('collection management modules', () => {
  it('renders all six modules and metric diagnostics from task state', () => {
    render(<CollectionManagementView
      instanceName="生产库"
      capabilities={[]}
      tasks={[{
        ...baseTask,
        last_success_at: '2026-08-11T12:00:00Z',
        last_result: 'FAILED',
        last_error_message: 'permission denied for pg_stat_activity',
        consecutive_failures: 2,
      }]}
      registration={{ state: 'EXPECTED_ONLINE', agent_expected: true, installation: installation(), agent_version: '1.2.3', last_reported_at: '2026-08-11T12:00:00Z' }}
      pause={{ paused: false }}
      agentMetricsEnabled
      canManage
      intervalPending={false}
      pausePending={false}
      error=""
      onIntervalChange={() => undefined}
      onPauseChange={() => undefined}
    />)

    for (const heading of ['采集总状态', 'Agent 状态', '数据库连接与权限检查', '扩展与插件能力', '指标采集状态', '采集配置']) {
      expect(screen.getByRole('heading', { name: heading })).toBeTruthy()
    }
    const metricRow = screen.getByRole('row', { name: /pg.connection.active/ })
    expect(within(metricRow).getByText('2026-08-11 12:00:00')).toBeTruthy()
    expect(within(metricRow).getByText('失败')).toBeTruthy()
    expect(within(metricRow).getByText('permission denied for pg_stat_activity')).toBeTruthy()
    expect(within(metricRow).getByText('role.pg_monitor')).toBeTruthy()
    expect(screen.getByText('1.2.3')).toBeTruthy()
    expect(screen.getAllByText('2026-08-11 12:00:00').length).toBeGreaterThan(1)
  })
})

describe('collection configuration', () => {
  it('submits the configured interval and pause state for platform admins', () => {
    const onIntervalChange = vi.fn()
    const onPauseChange = vi.fn()
    render(<CollectionConfiguration
      tasks={[baseTask]}
      pause={{ paused: false }}
      agentMetricsEnabled
      canManage
      intervalPending={false}
      pausePending={false}
      onIntervalChange={onIntervalChange}
      onPauseChange={onPauseChange}
    />)

    fireEvent.change(screen.getByRole('spinbutton', { name: 'pg.stat_activity 采样周期' }), { target: { value: '7' } })
    fireEvent.click(screen.getByRole('button', { name: /保存 pg.stat_activity 采样周期/ }))
    expect(onIntervalChange).toHaveBeenCalledWith('pg.stat_activity', 7)

    fireEvent.change(screen.getByLabelText('暂停原因'), { target: { value: '计划停机' } })
    fireEvent.click(screen.getByRole('switch', { name: '暂停采集' }))
    expect(onPauseChange).toHaveBeenCalledWith(true, '计划停机')
  })

  it('keeps controls visible but disabled for non-platform-admin users', () => {
    render(<CollectionConfiguration
      tasks={[baseTask]}
      pause={{ paused: false }}
      agentMetricsEnabled
      canManage={false}
      intervalPending={false}
      pausePending={false}
      onIntervalChange={() => undefined}
      onPauseChange={() => undefined}
    />)

    expect((screen.getByRole('spinbutton', { name: 'pg.stat_activity 采样周期' }) as HTMLInputElement).disabled).toBe(true)
    expect((screen.getByRole('switch', { name: '暂停采集' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('需要平台管理员角色')).toBeTruthy()
  })
})

function installation(): components['schemas']['AgentInstallation'] {
  return {
    ca_fingerprint_sha256: 'a'.repeat(64),
    installer_path: '/install.sh',
    authentication_path: '/token',
    file_mode: '0600',
    restart_command: 'systemctl restart dbs-monitor-agent',
  }
}
