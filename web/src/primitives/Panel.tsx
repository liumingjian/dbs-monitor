import { useId } from 'react'
import type { HTMLAttributes, ReactNode } from 'react'
import { SkeletonBlock } from './SkeletonBlock'
import './Panel.css'

export type PanelProps = {
  /** 面板标题。给了就渲染成标题栏，并把 `<section>` 的可访问名指向它。 */
  title?: ReactNode
  /** 标题的标题层级，默认 `h2`。页面主标题是 `h1`，面板从 `h2` 起。 */
  headingLevel?: 2 | 3 | 4
  /** 标题下方的一行说明。 */
  description?: ReactNode
  /** 标题栏右侧的操作区（按钮、切换器）。 */
  actions?: ReactNode
  /** 底部条，与内容之间隔一条发丝线。 */
  footer?: ReactNode
  /** 面板底色：`canvas` 白底（默认），`raised` 浅灰底，用于嵌套或次级面板。 */
  surface?: 'canvas' | 'raised'
  /** 去掉内容区内边距。表格、图表这类自带边距的内容用它，避免双重留白。 */
  flush?: boolean
  /** 载入中：内容区换成骨架占位。 */
  loading?: boolean
  children?: ReactNode
} & Omit<HTMLAttributes<HTMLElement>, 'title'>

/// 面板容器：白底 + 1px 发丝线 + 直角，没有投影。
///
/// 层级由「底色变化 + 发丝线」表达。DESIGN.md 明确禁止用投影做层级，
/// 所以这里没有任何 `box-shadow`，也不该有人加。
export function Panel({
  title,
  headingLevel = 2,
  description,
  actions,
  footer,
  surface = 'canvas',
  flush = false,
  loading = false,
  children,
  className,
  ...rest
}: PanelProps) {
  const headingId = useId()
  const Heading = `h${headingLevel}` as const

  return (
    <section
      {...rest}
      className={['dbs-panel', className].filter(Boolean).join(' ')}
      data-surface={surface}
      aria-labelledby={title === undefined ? undefined : headingId}
    >
      {(title !== undefined || actions !== undefined) && (
        <header className="dbs-panel__header">
          <div className="dbs-panel__heading">
            {title !== undefined && (
              <Heading className="dbs-panel__title dbs-panel-title" id={headingId}>
                {title}
              </Heading>
            )}
            {description !== undefined && <p className="dbs-panel__description dbs-caption">{description}</p>}
          </div>
          {actions !== undefined && <div className="dbs-panel__actions">{actions}</div>}
        </header>
      )}
      <div className="dbs-panel__body" data-flush={flush ? 'true' : undefined}>
        {loading ? <SkeletonBlock lines={3} /> : children}
      </div>
      {footer !== undefined && <footer className="dbs-panel__footer">{footer}</footer>}
    </section>
  )
}
