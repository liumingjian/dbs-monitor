import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ALERT_STATUSES, AlertStatus } from './AlertStatus'
import { HEALTH_STATUSES, HealthStatus } from './HealthStatus'
import { AlertSuppressionTags, SUPPRESSION_TAGS, SuppressionTags } from './SuppressionTags'

afterEach(cleanup)

describe('status vocabularies', () => {
  it('stay disjoint', () => {
    const all = [...HEALTH_STATUSES, ...ALERT_STATUSES, ...SUPPRESSION_TAGS]
    expect(new Set(all).size).toBe(all.length)
  })

  it('renders every health and alert state through exhaustive components', () => {
    const health = render(<>{HEALTH_STATUSES.map((status) => <HealthStatus key={status} status={status} />)}</>)
    for (const label of ['严重', '警告', '未知', '正常', '已暂停']) expect(screen.getByText(label)).toBeTruthy()
    health.unmount()

    render(<>{ALERT_STATUSES.map((status) => <AlertStatus key={status} status={status} />)}</>)
    for (const label of ['正常', '待持续', '告警中', '无数据', '已恢复']) expect(screen.getByText(label)).toBeTruthy()
  })

  it('keeps zero counts from hiding other orthogonal markers', () => {
    render(<SuppressionTags flags={{
      no_data: true,
      in_maintenance: true,
      recently_recovered: true,
      ignored: 0,
      configuration_missing: 0,
    }} />)
    expect(screen.getByText('无数据')).toBeTruthy()
    expect(screen.getByText('维护中')).toBeTruthy()
    expect(screen.getByText('近期恢复')).toBeTruthy()
    expect(screen.queryByText(/已忽略/)).toBeNull()
  })

  it('renders current-alert markers as independent facts', () => {
    render(<AlertSuppressionTags
      inMaintenance
      disposition="ACKED"
      pausedAt="2026-08-01T00:00:00Z"
      now={new Date('2026-08-09T00:00:00Z')}
    />)
    expect(screen.getByText('维护中')).toBeTruthy()
    expect(screen.getByText('已确认')).toBeTruthy()
    expect(screen.getByText('已暂停 8 天')).toBeTruthy()
  })
})
