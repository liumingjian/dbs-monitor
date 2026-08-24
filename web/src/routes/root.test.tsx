import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { InstanceListLabel } from './root'

afterEach(cleanup)

describe('instance list navigation label', () => {
  it('carries no badge before the instance list has loaded', () => {
    const { container } = render(<InstanceListLabel pausedCount={undefined} />)
    expect(screen.getByText('实例列表')).toBeTruthy()
    expect(container.querySelector('.ant-badge-count')).toBeNull()
  })

  it('carries no badge while nothing is paused', () => {
    const { container } = render(<InstanceListLabel pausedCount={0} />)
    expect(container.querySelector('.ant-badge-count')).toBeNull()
  })

  it('counts the paused instances', () => {
    render(<InstanceListLabel pausedCount={3} />)
    expect(screen.getByTitle('3 个实例已暂停采集')).toBeTruthy()
    expect(screen.getByText('3')).toBeTruthy()
  })
})
