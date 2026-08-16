import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import {
  PerformanceEventSeverityTag,
  PerformanceEventMaintenanceTag,
  performanceEventDispositionLabel,
  performanceEventDurationLabel,
  performanceEventSeverityPresentation,
  performanceEventTypeLabel,
} from './performanceEventPresentation'

afterEach(cleanup)

describe('performance event presentation', () => {
  it.each([
    ['LOCK_BLOCKING', '锁等待 / 阻塞'],
    ['LONG_TRANSACTION', '长事务'],
    ['IDLE_IN_TRANSACTION', 'idle in transaction'],
    ['ACTIVE_SESSIONS_HIGH', '活跃会话过高'],
    ['REPLICATION_LAG', '复制延迟'],
    ['TEMP_FILES_SURGE', '临时文件突增'],
  ] as const)('labels %s events', (eventType, label) => {
    expect(performanceEventTypeLabel(eventType)).toBe(label)
  })

  it.each([
    ['critical', '严重', 'error'],
    ['warning', '警告', 'warning'],
    ['info', 'Info', 'processing'],
  ] as const)('presents %s severity', (severity, label, color) => {
    expect(performanceEventSeverityPresentation(severity)).toEqual({ label, color })
    render(<PerformanceEventSeverityTag severity={severity} />)
    expect(screen.getByText(label)).toBeTruthy()
  })

  it.each([
    ['NONE', '未处置'],
    ['ACKED', '已确认'],
    ['IGNORED', '已忽略'],
  ] as const)('labels %s disposition', (disposition, label) => {
    expect(performanceEventDispositionLabel(disposition)).toBe(label)
  })

  it.each([
    [59_999, '0 分钟'],
    [60 * 60_000, '1 小时'],
    [24 * 60 * 60_000, '1 天'],
  ])('formats a %i ms duration as %s', (milliseconds, label) => {
    expect(performanceEventDurationLabel(milliseconds)).toBe(label)
  })

  it('shows the maintenance marker only for attributed events', () => {
    const { rerender } = render(<PerformanceEventMaintenanceTag inMaintenance />)
    expect(screen.getByText('维护中')).toBeTruthy()
    rerender(<PerformanceEventMaintenanceTag inMaintenance={false} />)
    expect(screen.queryByText('维护中')).toBeNull()
  })
})
