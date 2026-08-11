import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { CredentialSummary } from './settings'

beforeAll(() => {
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
