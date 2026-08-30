import { Button, Modal } from '@carbon/react'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Icon } from '../../primitives/Icon'
import type { IconName } from '../../primitives/Icon'

export type ConfirmedActionProps = {
  /** 按钮的可访问名与悬停提示，例如「删除 On-call gateway」。图标按钮没有可见文案，这是它唯一的名字。 */
  name: string
  icon: IconName
  /** 破坏性动作：按钮走 Carbon 的 danger 变体，确认框的主按钮也是红的。 */
  destructive?: boolean
  /** 确认框标题，一句话说清将要发生什么。 */
  heading: string
  /** 确认框正文：影响与不可撤销性。 */
  description: ReactNode
  /** 确认按钮文案，用动词，不要「确定」。 */
  confirmLabel: string
  disabled?: boolean
  /**
   * 禁用原因。**禁用的控件必须说明为什么** —— 只读用户点下去得到一条错误提示是
   * 本次迁移要消灭的行为，理由要在点之前就看得到。
   */
  disabledReason?: string
  pending?: boolean
  onConfirm: () => void
}

/// 行内的破坏性操作：图标按钮 + 二次确认。
///
/// 确认框只在打开时挂载。Carbon 的 `Modal` 关着的时候 DOM 仍在，一张表里十行就是十个
/// 隐藏的「删除」按钮，按可访问名定位会一次命中一片。
export function ConfirmedAction({
  name,
  icon,
  destructive = false,
  heading,
  description,
  confirmLabel,
  disabled = false,
  disabledReason,
  pending = false,
  onConfirm,
}: ConfirmedActionProps) {
  const [open, setOpen] = useState(false)
  const ActionIcon = () => <Icon name={icon} />

  return (
    <>
      <span title={disabled ? disabledReason : name}>
        <Button
          kind={destructive ? 'danger--ghost' : 'ghost'}
          size="sm"
          hasIconOnly
          renderIcon={ActionIcon}
          iconDescription={name}
          aria-label={name}
          disabled={disabled}
          onClick={() => setOpen(true)}
        />
      </span>
      {open && (
        <Modal
          open
          danger={destructive}
          modalHeading={heading}
          primaryButtonText={confirmLabel}
          secondaryButtonText="取消"
          primaryButtonDisabled={pending}
          size="sm"
          onRequestSubmit={() => {
            setOpen(false)
            onConfirm()
          }}
          onRequestClose={() => setOpen(false)}
          onSecondarySubmit={() => setOpen(false)}
        >
          <p className="dbs-body">{description}</p>
        </Modal>
      )}
    </>
  )
}

/// 非破坏性的行内图标按钮（编辑一类）。与 `ConfirmedAction` 共用一套外形与禁用说明。
export function InlineAction({ name, icon, disabled = false, disabledReason, onClick }: {
  name: string
  icon: IconName
  disabled?: boolean
  disabledReason?: string
  onClick: () => void
}) {
  const ActionIcon = () => <Icon name={icon} />
  return (
    <span title={disabled ? disabledReason : name}>
      <Button
        kind="ghost"
        size="sm"
        hasIconOnly
        renderIcon={ActionIcon}
        iconDescription={name}
        aria-label={name}
        disabled={disabled}
        onClick={onClick}
      />
    </span>
  )
}
