import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AlertUnavailabilityReason } from './index'

describe('current alert unavailability reason', () => {
  it('renders domain copy and a metric-scoped collection link', () => {
    const view = render(<AlertUnavailabilityReason alert={{
      instance_id: '10000000-0000-4000-8000-000000000001',
      metric_id: 'pg.connection.total',
      unavailability: 'PERMISSION_DENIED',
    }} />)

    expect(view.getByText('权限不足')).toBeTruthy()
    expect(view.getByRole('link', { name: '补齐监控权限' }).getAttribute('href')).toBe(
      '/instances/10000000-0000-4000-8000-000000000001/collection?metric=pg.connection.total',
    )
  })
})
