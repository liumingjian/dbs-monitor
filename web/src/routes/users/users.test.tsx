import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { PasswordChangeModal } from '../root'
import { OneTimePasswordModal } from '.'

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
})

afterEach(cleanup)

describe('one-time password dialog', () => {
  it.each([
    ['初始口令', 'initial-password-value'],
    ['重置口令', 'reset-password-value'],
  ])('forgets the %s after it closes', (title, password) => {
    function Harness() {
      const [issued, setIssued] = useState<{ title: string; password: string } | null>({ title, password })
      return <OneTimePasswordModal issued={issued} onClose={() => setIssued(null)} />
    }

    render(<Harness />)
    expect(screen.getByDisplayValue(password)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /关.*闭/ }))
    expect(screen.queryByDisplayValue(password)).toBeNull()
  })
})

describe('password change dialog', () => {
  it('requires the old password and rejects a new password shorter than 12 characters', async () => {
    const submit = vi.fn()
    render(<PasswordChangeModal open pending={false} error="" onClose={() => undefined} onSubmit={submit} />)

    fireEvent.change(screen.getByLabelText('旧口令'), { target: { value: 'known old password' } })
    fireEvent.change(screen.getByLabelText('新口令'), { target: { value: 'short' } })
    fireEvent.click(screen.getByRole('button', { name: /保.*存/ }))
    await waitFor(() => expect(screen.getByText('新口令至少 12 个字符')).toBeTruthy())
    expect(submit).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('新口令'), { target: { value: 'long enough password' } })
    fireEvent.click(screen.getByRole('button', { name: /保.*存/ }))
    await waitFor(() => expect(submit).toHaveBeenCalledWith({
      old_password: 'known old password',
      new_password: 'long enough password',
    }))
  })
})
