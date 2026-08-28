import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { AlertUnavailabilityReason, compareAlertUrgency, durationLabel, isUnresolved, optionalNumber, sortCoversEveryRow } from './index'

afterEach(cleanup)

describe('current alert unavailability reason', () => {
  it('renders domain copy and a metric-scoped collection link', () => {
    const view = render(
      <AlertUnavailabilityReason alert={{
        instance_id: '10000000-0000-4000-8000-000000000001',
        metric_id: 'pg.connection.total',
        unavailability: 'PERMISSION_DENIED',
      }} />,
    )

    expect(view.getByText('权限不足')).toBeTruthy()
    expect(view.getByRole('link', { name: '补齐监控权限' }).getAttribute('href')).toBe(
      '/instances/10000000-0000-4000-8000-000000000001/collection?metric=pg.connection.total',
    )
  })

  it('renders a placeholder when no reason is available', () => {
    const view = render(
      <AlertUnavailabilityReason alert={{
        instance_id: '10000000-0000-4000-8000-000000000001',
        metric_id: 'pg.connection.total',
      }} />,
    )

    expect(view.getByText('—')).toBeTruthy()
    expect(view.queryByRole('link')).toBeNull()
  })
})

describe('current alert ordering and formatting', () => {
  const base = {
    id: 'a', instance_id: 'i', instance_name: 'pg', rule_id: 'rid', rule_name: 'r',
    rule_snapshot: {}, metric_id: 'm', rule_version: 1, duration_ms: 0,
    in_maintenance: false, paused: false, disposition: 'NONE' as const,
    updated_at: '2026-08-01T00:00:00Z',
  }

  it('ranks what is burning above rules that merely evaluated OK', () => {
    const rows = [
      { ...base, id: 'ok', status: 'OK' as const, severity: 'critical' as const },
      { ...base, id: 'pending', status: 'PENDING' as const, severity: 'warning' as const },
      { ...base, id: 'firing', status: 'FIRING' as const, severity: 'warning' as const },
      { ...base, id: 'nodata', status: 'NO_DATA' as const, severity: 'critical' as const },
    ]
    expect(rows.sort(compareAlertUrgency).map((row) => row.id)).toEqual(['firing', 'nodata', 'pending', 'ok'])
  })

  it('breaks status ties by severity, then by how long it has been burning', () => {
    const rows = [
      { ...base, id: 'short', status: 'FIRING' as const, severity: 'critical' as const, duration_ms: 1000 },
      { ...base, id: 'long', status: 'FIRING' as const, severity: 'critical' as const, duration_ms: 9000 },
      { ...base, id: 'warn', status: 'FIRING' as const, severity: 'warning' as const, duration_ms: 99_000 },
    ]
    expect(rows.sort(compareAlertUrgency).map((row) => row.id)).toEqual(['long', 'short', 'warn'])
  })

  it('treats only unresolved statuses as worth a severity badge', () => {
    expect(isUnresolved('FIRING')).toBe(true)
    expect(isUnresolved('NO_DATA')).toBe(true)
    expect(isUnresolved('PENDING')).toBe(true)
    expect(isUnresolved('OK')).toBe(false)
    expect(isUnresolved('RECOVERED')).toBe(false)
  })
})

describe('current alert page coverage', () => {
  it('admits that the sort only ranks the rows it was given', () => {
    expect(sortCoversEveryRow(undefined)).toBe(true)
    expect(sortCoversEveryRow(50)).toBe(true)
    expect(sortCoversEveryRow(51)).toBe(false)
  })
})

describe('current alert cell formatting', () => {
  it('does not print float noise into a 100px column', () => {
    expect(optionalNumber(8.242500000000001)).toBe('8.243')
    expect(optionalNumber(undefined)).toBe('—')
    expect(optionalNumber(0)).toBe('0')
  })

  it('keeps a threshold readable back rather than compacting it away', () => {
    expect(optionalNumber(104_900_000)).toBe('104,900,000')
  })

  it('reports a sub-minute alert in seconds instead of "0 分钟"', () => {
    expect(durationLabel(42_000)).toBe('42 秒')
    expect(durationLabel(0)).toBe('0 秒')
    expect(durationLabel(90_000)).toBe('1 分钟')
    expect(durationLabel(7_200_000)).toBe('2 小时')
  })
})
