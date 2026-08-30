import {
  Header,
  HeaderGlobalAction,
  HeaderGlobalBar,
  HeaderMenu,
  HeaderMenuButton,
  HeaderMenuItem,
  HeaderName,
  HeaderNavigation,
  Modal,
  SideNav,
  SideNavDivider,
  SideNavItems,
  SideNavLink,
  TextInput,
} from '@carbon/react'
import { Link, Outlet, createRootRoute, useLocation, useNavigate } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import type { ComponentType, ReactNode } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { FormField } from '../../primitives/FormField'
import { Icon } from '../../primitives/Icon'
import type { IconName } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import {
  browserStorage,
  navItemLabel,
  navToggleLabel,
  readNavCollapsed,
  unreadBadgeText,
  writeNavCollapsed,
} from './navCollapse'
import './shell.css'

type PasswordChangeInput = components['schemas']['PasswordChangeInput']

type PasswordChangeModalProps = {
  open: boolean
  pending: boolean
  error: string
  onClose: () => void
  onSubmit: (values: PasswordChangeInput) => void
}

export const rootRoute = createRootRoute({
  component: RootLayout,
})

/// 应用外框：48px 炭黑页头 + 三组侧栏 + 内容区。
///
/// 侧栏用 Carbon 的 `SideNav`，但**必须**关掉它自带的鼠标与焦点监听：Carbon 的 `isRail`
/// 语义是「悬停即展开」，这里要的是门闩式折叠 —— 点一下收起，跨页面保持，不受指针位置影响。
/// 关掉两个监听之后 `isRail` 只剩「48px 窄轨」这一个作用，宽度完全由受控的 `expanded` 决定。
///
/// 另一件实测出来的事（浏览器里量的，不是读文档）：`<SideNav expanded={false}>` 在默认的
/// `isChildOfHeader` 下渲染出来**仍然是 256px** —— `cds--side-nav--ux` 把宽度写死成 256，
/// 折叠等于没折。窄轨只能由 `isRail` 给。
function RootLayout() {
  const location = useLocation()
  const inShell = location.pathname !== '/login'
  const [collapsed, setCollapsed] = useState(() => readNavCollapsed(browserStorage))

  function toggleNav() {
    const next = !collapsed
    setCollapsed(next)
    writeNavCollapsed(browserStorage, next)
  }

  return (
    <div className="dbs-shell" data-nav={inShell ? (collapsed ? 'rail' : 'expanded') : 'none'}>
      <Header aria-label="DBS Monitor">
        {inShell && (
          <HeaderMenuButton
            aria-label={navToggleLabel(collapsed)}
            aria-expanded={!collapsed}
            isCollapsible
            isActive={!collapsed}
            onClick={toggleNav}
          />
        )}
        <HeaderName as={Link} to="/instances" prefix="">DBS Monitor</HeaderName>
        {inShell && <ShellGlobalBar />}
      </Header>
      {inShell && <ShellSideNav collapsed={collapsed} />}
      <main className="dbs-shell__content">
        <Outlet />
      </main>
    </div>
  )
}

/// 页头右侧：全局操作与用户菜单。
///
/// 全局操作只有一个：告警设置，带通知渠道失败指示。它是快捷方式而不是唯一入口 ——
/// 同一个去处在侧栏「管理」组里是一条真链接，所以这里用按钮不丢任何能力。
function ShellGlobalBar() {
  const navigate = useNavigate()
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const failureQuery = $api.useQuery(
    'get',
    '/api/v1/notification-channels/failures',
    {},
    { refetchInterval: 15_000 },
  )
  const changePasswordMutation = $api.useMutation('put', '/api/v1/password')
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [error, setError] = useState('')
  const hasFailures = failureQuery.data?.has_failures === true

  function changeOwnPassword(values: PasswordChangeInput) {
    setError('')
    changePasswordMutation.mutate({ body: values }, {
      onSuccess: () => setPasswordOpen(false),
      onError: (failure) => setError(apiErrorMessage(failure, '修改口令失败，请重试')),
    })
  }

  function closePasswordModal() {
    setPasswordOpen(false)
    setError('')
  }

  return (
    <>
      <HeaderGlobalBar>
        <HeaderGlobalAction
          aria-label={hasFailures ? '告警设置，通知渠道存在未清除失败' : '告警设置'}
          tooltipAlignment="end"
          onClick={() => void navigate({ to: '/alert-settings/notifications' })}
        >
          <span className="dbs-shell-badge-host">
            <Icon name="settings" size={20} />
            <NotificationSettingsLabel hasFailures={hasFailures} />
          </span>
        </HeaderGlobalAction>
        <HeaderNavigation aria-label="当前用户">
          <HeaderMenu aria-label="用户菜单" menuLinkName={currentUserQuery.data?.username ?? '当前用户'}>
            <HeaderMenuItem onClick={() => setPasswordOpen(true)}>修改口令</HeaderMenuItem>
          </HeaderMenu>
        </HeaderNavigation>
      </HeaderGlobalBar>
      <PasswordChangeModal
        open={passwordOpen}
        pending={changePasswordMutation.isPending}
        error={error}
        onClose={closePasswordModal}
        onSubmit={changeOwnPassword}
      />
    </>
  )
}

/// 侧栏。三组：监控 / 告警 / 管理。
///
/// 折叠时**不渲染**标签与分组标题（而不是把它们淡出）：淡出会在每个标题原来的位置留下一条
/// 空带，48px 的窄轨一条都留不起。名称改由 `title` 悬停提示与 `aria-label` 承担，
/// 未处置告警数收成图标角上的角标。
function ShellSideNav({ collapsed }: { collapsed: boolean }) {
  const location = useLocation()
  const currentAlerts = $api.useQuery(
    'get',
    '/api/v1/alerts/current',
    { params: { query: { include_paused: false, limit: 1, offset: 0 } } },
    { refetchInterval: 30_000 },
  )
  const unread = currentAlerts.data?.total
  const at = (prefix: string) => location.pathname.startsWith(prefix)

  // Carbon 的 `as` 槽只收一个组件，路由属性没法跟着一起传（`search` 的类型与 `to` 绑定，
  // 转一手就退化成任意对象）。所以每条链接的去处写死在这里的闭包里，`as` 拿到的是一个
  // 已经知道自己去哪儿的组件；没有依赖，跨渲染是同一个身份，不会重挂锚点。
  const links = useMemo(() => ({
    instances: (props: object) => <Link {...props} to="/instances" />,
    alerts: (props: object) => <Link {...props} to="/alerts" search={{ tab: 'current', include_paused: false }} />,
    users: (props: object) => <Link {...props} to="/users" />,
    alertSettings: (props: object) => <Link {...props} to="/alert-settings/notifications" />,
  }), [])

  return (
    <SideNav
      aria-label="主导航"
      className="dbs-shell-nav"
      expanded={!collapsed}
      isRail
      isPersistent
      addMouseListeners={false}
      addFocusListeners={false}
    >
      <SideNavItems>
        <ShellNavGroup heading="监控" collapsed={collapsed} first>
          <SideNavLink
            as={links.instances}
            {...navLinkProps({ label: '实例列表', icon: 'database', collapsed, active: at('/instances') })}
          />
        </ShellNavGroup>
        <ShellNavGroup heading="告警" collapsed={collapsed}>
          <SideNavLink
            as={links.alerts}
            {...navLinkProps({ label: '全局告警', icon: 'notification', collapsed, active: at('/alerts'), count: unread })}
          />
        </ShellNavGroup>
        <ShellNavGroup heading="管理" collapsed={collapsed}>
          <SideNavLink
            as={links.users}
            {...navLinkProps({ label: '用户管理', icon: 'userAvatar', collapsed, active: at('/users') })}
          />
          <SideNavLink
            as={links.alertSettings}
            {...navLinkProps({ label: '告警设置', icon: 'settings', collapsed, active: at('/alert-settings') })}
          />
        </ShellNavGroup>
      </SideNavItems>
    </SideNav>
  )
}

/// 一个导航分组。展开时是标题 + 列表；折叠时标题整个不渲染，改用一条分隔线表达分组。
function ShellNavGroup({ heading, collapsed, first = false, children }: {
  heading: string
  collapsed: boolean
  first?: boolean
  children: ReactNode
}) {
  return (
    <li className="dbs-shell-nav__group">
      {collapsed
        ? !first && <SideNavDivider />
        : <h2 className="dbs-shell-nav__heading dbs-caption">{heading}</h2>}
      <ul className="dbs-shell-nav__list" aria-label={heading}>{children}</ul>
    </li>
  )
}

/// 一条导航项上除去「去哪儿」之外的全部：图标、角标、选中态、可访问名与悬停提示。
function navLinkProps({ label, icon, collapsed, active, count }: {
  label: string
  icon: IconName
  collapsed: boolean
  active: boolean
  /** 未处置告警数。`undefined` 是「还没取到」，与 0 一样不画角标。 */
  count?: number
}) {
  const badge = unreadBadgeText(count)
  const accessibleName = navItemLabel(label, count)
  const NavIcon: ComponentType = () => (
    <span className="dbs-shell-badge-host">
      <Icon name={icon} size={16} />
      {collapsed && badge !== '' && <span className="dbs-shell-badge" aria-hidden="true">{badge}</span>}
    </span>
  )

  return {
    renderIcon: NavIcon,
    isActive: active,
    'aria-current': active ? ('page' as const) : undefined,
    'aria-label': accessibleName,
    title: accessibleName,
    children: collapsed ? null : (
      <span className="dbs-shell-nav__label">
        {label}
        {badge !== '' && <span className="dbs-shell-nav__count" aria-hidden="true">{badge}</span>}
      </span>
    ),
  }
}

/// 通知渠道失败指示。颜色不是唯一信号：调用方同时改写按钮的可访问名。
export function NotificationSettingsLabel({ hasFailures }: { hasFailures: boolean }) {
  if (!hasFailures) return null
  return (
    <span
      className="dbs-shell-badge dbs-shell-badge--dot"
      data-testid="notification-failure-badge"
      title="通知渠道存在未清除失败"
    />
  )
}

export function PasswordChangeModal({ open, pending, error, onClose, onSubmit }: PasswordChangeModalProps) {
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [failures, setFailures] = useState<{ old?: string; next?: string }>({})

  function submit() {
    const next: { old?: string; next?: string } = {}
    if (oldPassword === '') next.old = '请输入旧口令'
    if (newPassword === '') next.next = '请输入新口令'
    else if (newPassword.length < 12) next.next = '新口令至少 12 个字符'
    setFailures(next)
    if (next.old !== undefined || next.next !== undefined) return
    onSubmit({ old_password: oldPassword, new_password: newPassword })
  }

  function close() {
    setOldPassword('')
    setNewPassword('')
    setFailures({})
    onClose()
  }

  return (
    <Modal
      open={open}
      modalHeading="修改口令"
      primaryButtonText="保存"
      secondaryButtonText="取消"
      primaryButtonDisabled={pending}
      onRequestSubmit={submit}
      onRequestClose={close}
      onSecondarySubmit={close}
      size="sm"
    >
      <div className="dbs-shell-password-form">
        {error !== '' && <NotificationBar tone="critical" title={error} />}
        <FormField errorText={failures.old} required>
          {(control) => (
            <TextInput
              id={control.id}
              type="password"
              labelText="旧口令"
              autoComplete="current-password"
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              value={oldPassword}
              onChange={(event) => setOldPassword(event.target.value)}
            />
          )}
        </FormField>
        <FormField errorText={failures.next} required>
          {(control) => (
            <TextInput
              id={control.id}
              type="password"
              labelText="新口令"
              autoComplete="new-password"
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
          )}
        </FormField>
      </div>
    </Modal>
  )
}
