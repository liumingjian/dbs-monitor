import {
  FloatingFocusManager,
  FloatingOverlay,
  FloatingPortal,
  useDismiss,
  useFloating,
  useInteractions,
  useRole,
} from '@floating-ui/react'
import { useId } from 'react'
import type { ReactNode } from 'react'
import { Icon } from './Icon'
import './Drawer.css'

export type DrawerProps = {
  /** 受控开合。关闭时整个抽屉从 DOM 移除，里面的表单状态随之丢弃。 */
  open: boolean
  /** Esc、点击遮罩、点关闭按钮都会调用它。 */
  onClose: () => void
  /** 抽屉标题，同时作为对话框的可访问名。 */
  title: ReactNode
  /** 标题下的一行说明。 */
  description?: ReactNode
  /** 底部操作条（保存 / 取消）。与内容之间隔一条发丝线，滚动时固定在底部。 */
  footer?: ReactNode
  /** 宽度档位：`md` 480px（规范值），`lg` 720px 给宽表单与并排字段用。 */
  size?: 'md' | 'lg'
  children: ReactNode
  'data-testid'?: string
}

/// 右侧抽屉。核心组件库没有抽屉，这一件是自持的。
///
/// 焦点管理用 `@floating-ui/react` 的 `FloatingFocusManager`：它本来就在
/// `@carbon/react` 的依赖里（Popover / Tooltip 都用它），所以这不是新引入的一棵依赖树，
/// 只是把一件本来就在的东西显式声明出来。另外两条路都更差：Carbon 的 `wrapFocus`
/// 是内部件、不对外导出，抄进仓库会在上游演进时静默腐坏；把 `ComposedModal`
/// 改造成右侧面板则要逐条推翻它的定位与动效样式。
///
/// 语义：`role="dialog"` + `aria-modal` + 指向标题的可访问名；Esc 关闭、点遮罩关闭、
/// 焦点困在抽屉内、关闭后焦点回到打开它的那个控件。
export function Drawer({
  open,
  onClose,
  title,
  description,
  footer,
  size = 'md',
  children,
  'data-testid': testId,
}: DrawerProps) {
  const titleId = useId()
  const { refs, context } = useFloating({
    open,
    onOpenChange: (next) => {
      if (!next) onClose()
    },
  })
  const dismiss = useDismiss(context, { outsidePressEvent: 'mousedown' })
  const role = useRole(context, { role: 'dialog' })
  const { getFloatingProps } = useInteractions([dismiss, role])

  if (!open) return null

  return (
    <FloatingPortal>
      <FloatingOverlay className="dbs-drawer__scrim" lockScroll>
        <FloatingFocusManager context={context} modal returnFocus>
          <aside
            ref={refs.setFloating}
            className="dbs-drawer"
            data-size={size}
            data-testid={testId}
            aria-modal="true"
            aria-labelledby={titleId}
            {...getFloatingProps()}
          >
            <header className="dbs-drawer__header">
              <div className="dbs-drawer__heading">
                <h2 className="dbs-drawer__title dbs-panel-title" id={titleId}>
                  {title}
                </h2>
                {description !== undefined && <p className="dbs-drawer__description dbs-caption">{description}</p>}
              </div>
              <button type="button" className="dbs-drawer__close" onClick={onClose} aria-label="关闭">
                <Icon name="close" size={20} />
              </button>
            </header>
            <div className="dbs-drawer__body">{children}</div>
            {footer !== undefined && <footer className="dbs-drawer__footer">{footer}</footer>}
          </aside>
        </FloatingFocusManager>
      </FloatingOverlay>
    </FloatingPortal>
  )
}
