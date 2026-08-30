import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { PasswordChangeModal } from '../root'
import { OneTimePasswordModal } from '.'

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
  // jsdom 没有 ResizeObserver，Carbon 的对话框会用到它。只是环境垫片，不动任何断言。
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
    // 名字写死成「关闭」而不是 /关.*闭/：对话框右上角的关闭按钮现在也叫「关闭对话框」
    // （组件库的默认名 `Close` 是英文，已在 `primitives/Modal` 里统一换掉），模糊匹配会同时
    // 命中两个按钮。点哪一个都能关，这里定位的是页脚那颗主按钮。
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
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
