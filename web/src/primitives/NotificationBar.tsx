import { ActionableNotification, InlineNotification } from '@carbon/react'
import type { ReactNode } from 'react'
import './NotificationBar.css'

/**
 * 通知条的语气。比状态语汇多一档 `info`：中性的说明不是状态，
 * 它用的是灰蓝的低对比底色而非「蓝色 = 可交互」的那支蓝。
 */
export type NotificationTone = 'critical' | 'warning' | 'normal' | 'info'

export type NotificationBarProps = {
  tone: NotificationTone
  /** 一句话说清发生了什么。Carbon 这一档只吃字符串。 */
  title: string
  /**
   * 补充说明，通常是原因与下一步。
   * **不要往里放可交互元素**：通知条的正文区被组件库限定为非交互内容，
   * 按钮走 `action`，链接另起一行放在通知外面。
   */
  children?: ReactNode
  /** 行内主操作。给了就渲染成带操作按钮的通知（role="alertdialog"）。 */
  action?: { label: string; onClick: () => void }
  /** 可关闭时给它；不给就没有关闭按钮。 */
  onClose?: () => void
  className?: string
  'data-testid'?: string
}

function assertNever(value: never): never {
  throw new Error(`unhandled notification tone: ${String(value)}`)
}

function carbonKind(tone: NotificationTone) {
  switch (tone) {
    case 'critical':
      return 'error' as const
    case 'warning':
      return 'warning' as const
    case 'normal':
      return 'success' as const
    case 'info':
      return 'info' as const
    default:
      return assertNever(tone)
  }
}

/// 通知条。整条一律低对比档（浅底 + 状态色左边条），
/// 因为规范禁止把状态色铺成大面积背景 —— 高对比档正是那样。
export function NotificationBar({
  tone,
  title,
  children,
  action,
  onClose,
  className,
  'data-testid': testId,
}: NotificationBarProps) {
  const shared = {
    kind: carbonKind(tone),
    lowContrast: true,
    title,
    children,
    hideCloseButton: onClose === undefined,
    onClose: onClose === undefined ? undefined : () => onClose(),
    className: ['dbs-notification', className].filter(Boolean).join(' '),
    'data-testid': testId,
  }

  return action === undefined ? (
    <InlineNotification {...shared} />
  ) : (
    <ActionableNotification {...shared} inline actionButtonLabel={action.label} onActionButtonClick={action.onClick} />
  )
}
