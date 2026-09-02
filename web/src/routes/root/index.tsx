import {
  Header,
  HeaderGlobalAction,
  HeaderGlobalBar,
  HeaderMenu,
  HeaderMenuButton,
  HeaderMenuItem,
  HeaderName,
  HeaderNavigation,
  PasswordInput,
  SideNav,
  SideNavDivider,
  SideNavItems,
  SideNavLink,
  Theme,
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
import { Modal } from '../../primitives/Modal'
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
  /** 当前用户名。作为弹窗的眉批，说明这次改的是谁的口令。 */
  username: string | undefined
  /** 请求进行到哪一步。`finished` 停在勾上，由 `onDone` 决定什么时候收起弹窗。 */
  status: 'inactive' | 'active' | 'finished'
  error: string
  onClose: () => void
  onDone: () => void
  onSubmit: (values: PasswordChangeInput) => void
}

/// 服务端的下限：`ChangeOwnPassword` 用 `utf8.RuneCountInString` 数**字符**。
/// 前端跟着数字符（`[...value].length`）而不是 `value.length` —— 后者数的是 UTF-16 码元，
/// 表情符号一个顶两个，会放行一条服务端要退回的口令。
const passwordMinLength = 12

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
  // 口令弹窗由外框持有，而不是由页头里的用户菜单持有：它必须落在 `g100` zone 之外，
  // 落在里面会跟着页头一起变成深色底。
  const [passwordOpen, setPasswordOpen] = useState(false)

  function toggleNav() {
    const next = !collapsed
    setCollapsed(next)
    writeNavCollapsed(browserStorage, next)
  }

  return (
    <div className="dbs-shell" data-nav={inShell ? (collapsed ? 'rail' : 'expanded') : 'none'}>
      {/* 炭黑页头是 DESIGN.md 里产品唯一一处刻意偏离白色版式的地方（`components.shell-header`
          = `colors.inverse-canvas`）。Carbon 的 ui-shell 自己不带颜色，它取的是**环境主题**的
          `$background` —— 页头落在浅色 `:root` 下就是白的。所以这里显式套一层 `g100` zone，
          页头连同它的菜单、浮层与图标按钮一起换到深色令牌上。
          zone 只包住 `<Header>`：侧栏按 DESIGN.md 是 `colors.canvas` 白底，口令弹窗也是白底，
          两者都在这层之外。 */}
      <Theme theme="g100">
        <Header aria-label="DBS Monitor">
          {inShell && (
            <HeaderMenuButton
              aria-label={navToggleLabel(collapsed)}
              aria-expanded={!collapsed}
              isCollapsible
              isActive={!collapsed}
              // 折叠与展开用**同一个**字形。Carbon 的默认行为是 `isActive ? Close : Menu`，
              // 也就是展开时把汉堡换成一个叉 —— 那个叉在 ui-shell 的语义里是「关掉浮层导航」，
              // 而这里的侧栏是常驻的门闩，按下去只是把它收成窄轨，什么都没有被关掉。
              // 图标跟着状态跳变还会让读者以为按钮换了功能。状态由按钮的高亮底色与
              // `aria-expanded` 表达，字形保持不动。
              //
              // 换字形而不是干脆去掉 `isCollapsible`：那个 prop 不只管图标，它一撤，
              // Carbon 会给按钮挂上 `--header__menu-toggle__hidden`，1056px 以上整个开关消失。
              renderCloseIcon={<Icon name="menu" size={20} />}
              onClick={toggleNav}
            />
          )}
          <HeaderName as={Link} to="/" prefix="">DBS Monitor</HeaderName>
          {inShell && <ShellGlobalBar onOpenPassword={() => setPasswordOpen(true)} />}
        </Header>
      </Theme>
      {inShell && <ShellSideNav collapsed={collapsed} />}
      <main className="dbs-shell__content">
        <Outlet />
      </main>
      {inShell && <PasswordChangeGate open={passwordOpen} onClose={() => setPasswordOpen(false)} />}
    </div>
  )
}

/// 页头右侧：全局操作与用户菜单。
///
/// 全局操作只有一个：告警设置，带通知渠道失败指示。它是快捷方式而不是唯一入口 ——
/// 同一个去处在侧栏「管理」组里是一条真链接，所以这里用按钮不丢任何能力。
///
/// 用户菜单挂在 `HeaderNavigation` 上；Carbon 在 1056px 以下把整条 `.cds--header__nav`
/// 藏起来，所以窄屏下的可见性由 `shell.css` 里的一条覆盖负责，见那里的说明。
function ShellGlobalBar({ onOpenPassword }: { onOpenPassword: () => void }) {
  const navigate = useNavigate()
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const failureQuery = $api.useQuery(
    'get',
    '/api/v1/notification-channels/failures',
    {},
    { refetchInterval: 15_000 },
  )
  const hasFailures = failureQuery.data?.has_failures === true
  const logout = useLogout()

  return (
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
      <HeaderNavigation aria-label="当前用户" className="dbs-shell-user">
        <HeaderMenu aria-label="用户菜单" menuLinkName={currentUserQuery.data?.username ?? '当前用户'}>
          {/* `as="button"` 不是装饰。`HeaderMenuItem` 默认渲染成一个**没有 href 的 `<a>`**：
              Carbon 给它接上了 `tabIndex`，所以焦点停得住，但锚点没有 href 就不会响应回车 ——
              键盘用户能选中这一项，按下去什么都不发生。换成真按钮，回车与空格都由浏览器负责。 */}
          <HeaderMenuItem as="button" type="button" onClick={onOpenPassword}>修改口令</HeaderMenuItem>
          <HeaderMenuItem as="button" type="button" onClick={logout.run} disabled={logout.pending}>
            退出登录
          </HeaderMenuItem>
        </HeaderMenu>
      </HeaderNavigation>
    </HeaderGlobalBar>
  )
}

/// 退出登录。
///
/// 请求成功与否都走同一条出路：跳到登录页。`POST /api/v1/logout` 会作废服务端会话并回一个
/// 过期的 Set-Cookie；它失败的方式只有两种，401（会话本来就没了）和网络不通，
/// 两种情况下把人留在一个已经登不上的界面里都毫无意义。
///
/// 用整页跳转而不是路由跳转：会话一没，`$api` 的查询缓存里全是上一个人的数据，
/// 而缓存的清理没有第二个人负责。整页重载是唯一一次把它清干净的机会。
function useLogout() {
  const logoutMutation = $api.useMutation('post', '/api/v1/logout')

  return {
    pending: logoutMutation.isPending,
    run: () => {
      const leave = () => window.location.assign('/login')
      logoutMutation.mutate({ body: {} }, { onSuccess: leave, onError: leave })
    },
  }
}

/// 口令修改。弹窗与它的请求一起住在这里，位置在页头的 `g100` zone 之外 ——
/// 落在 zone 里面它会跟着页头一起变成深色底。
function PasswordChangeGate({ open, onClose }: { open: boolean; onClose: () => void }) {
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const changePasswordMutation = $api.useMutation('put', '/api/v1/password')
  const [error, setError] = useState('')

  function changeOwnPassword(values: PasswordChangeInput) {
    setError('')
    changePasswordMutation.mutate({ body: values }, {
      onError: (failure) => setError(apiErrorMessage(failure, '修改口令失败，请重试')),
    })
  }

  function close() {
    setError('')
    changePasswordMutation.reset()
    onClose()
  }

  // 成功后不是立刻消失：主按钮先停在勾上一拍，再由 `onLoadingSuccess` 把弹窗收起来。
  // 接口回的是 204，没有别的地方能确认「这次真的改成了」—— 弹窗一闪而过等于没有回执。
  const status = changePasswordMutation.isPending
    ? 'active'
    : changePasswordMutation.isSuccess ? 'finished' : 'inactive'

  return (
    <PasswordChangeModal
      open={open}
      username={currentUserQuery.data?.username}
      status={status}
      error={error}
      onClose={close}
      onDone={close}
      onSubmit={changeOwnPassword}
    />
  )
}

/// 侧栏。三组六项：监控（总览 · 实例列表 · SQL 洞察）/ 告警（全局告警 · 告警设置）/
/// 系统（用户管理）。分组按「这一项回答的是哪一类问题」，不按「谁有权限改它」。
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
  // 总览的地址是 `/`，`startsWith('/')` 对每一条路径都成立，所以它只能精确匹配。
  const atOverview = location.pathname === '/'

  // Carbon 的 `as` 槽只收一个组件，路由属性没法跟着一起传（`search` 的类型与 `to` 绑定，
  // 转一手就退化成任意对象）。所以每条链接的去处写死在这里的闭包里，`as` 拿到的是一个
  // 已经知道自己去哪儿的组件；没有依赖，跨渲染是同一个身份，不会重挂锚点。
  const links = useMemo(() => ({
    overview: (props: object) => <Link {...props} to="/" />,
    instances: (props: object) => <Link {...props} to="/instances" />,
    sqlInsight: (props: object) => <Link {...props} to="/sql-insight" />,
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
            as={links.overview}
            {...navLinkProps({ label: '总览', icon: 'dashboard', collapsed, active: atOverview })}
          />
          <SideNavLink
            as={links.instances}
            {...navLinkProps({ label: '实例列表', icon: 'database', collapsed, active: at('/instances') })}
          />
          <SideNavLink
            as={links.sqlInsight}
            {...navLinkProps({ label: 'SQL 洞察', icon: 'chartColumn', collapsed, active: at('/sql-insight') })}
          />
        </ShellNavGroup>
        <ShellNavGroup heading="告警" collapsed={collapsed}>
          <SideNavLink
            as={links.alerts}
            {...navLinkProps({ label: '全局告警', icon: 'notification', collapsed, active: at('/alerts'), count: unread })}
          />
          <SideNavLink
            as={links.alertSettings}
            {...navLinkProps({ label: '告警设置', icon: 'settings', collapsed, active: at('/alert-settings') })}
          />
        </ShellNavGroup>
        <ShellNavGroup heading="系统" collapsed={collapsed}>
          <SideNavLink
            as={links.users}
            {...navLinkProps({ label: '用户管理', icon: 'userAvatar', collapsed, active: at('/users') })}
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
  // 这是 `renderIcon` 槽里**唯一**一个还留着的包装件（其余一律直接给 `Icon.glyph.*`，
  // 见 `primitives/Icon.tsx`）：折叠态的未处置计数要压在图标角上，字形本身塞不下它。
  // 既然是包装件，就必须把组件库交过来的 props 原样透传 —— Carbon 正是靠这个
  // className 给图标定位的，丢了不报错，只是位置不对。
  const NavIcon: ComponentType<{ className?: string }> = ({ className }) => (
    <span className={['dbs-shell-badge-host', className].filter(Boolean).join(' ')}>
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

/// 修改口令弹窗。
///
/// 三件事按 DESIGN.md 的表单版式来，都不是装饰：
///
/// - **口令输入用 Carbon 的 `PasswordInput`**，自带可见性切换。看不见自己敲了什么是
///   「确认新口令」这一栏存在的全部理由，能看见就少一次输错。切换按钮的两句可访问名
///   默认是英文，显式给中文（判断标准见 web/CLAUDE.md：读屏用户会不会听见英文）。
/// - **规则写在提交之前，不是提交之后。** 12 个字符的下限是服务端的硬规则，
///   过去只在输错之后才作为红字出现；现在它常驻在字段下方的提示里。
/// - **「其他设备会被登出」写在弹窗里。** 服务端的 `ChangeOwnPassword` 在改完口令之后调
///   `DeleteOtherUserSessions`，这是一个用户看不见、也无法撤销的副作用，界面有义务先说。
///
/// 校验一律落在对应字段下方（`FormField` 的 `errorText`），只有整表单级的失败
/// —— 服务端退回的「旧口令不正确」这类 —— 才上错误条。
export function PasswordChangeModal({ open, username, status, error, onClose, onDone, onSubmit }: PasswordChangeModalProps) {
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [failures, setFailures] = useState<{ old?: string; next?: string; confirm?: string }>({})

  function submit() {
    const next: { old?: string; next?: string; confirm?: string } = {}
    if (oldPassword === '') next.old = '请输入当前口令'
    if (newPassword === '') next.next = '请输入新口令'
    else if ([...newPassword].length < passwordMinLength) next.next = `新口令至少 ${passwordMinLength} 个字符`
    else if (newPassword === oldPassword) next.next = '新口令不能与当前口令相同'
    if (confirmPassword === '') next.confirm = '请再输入一次新口令'
    else if (newPassword !== '' && confirmPassword !== newPassword) next.confirm = '两次输入的新口令不一致'
    setFailures(next)
    if (next.old !== undefined || next.next !== undefined || next.confirm !== undefined) return
    onSubmit({ old_password: oldPassword, new_password: newPassword })
  }

  function reset() {
    setOldPassword('')
    setNewPassword('')
    setConfirmPassword('')
    setFailures({})
  }

  function close() {
    reset()
    onClose()
  }

  function done() {
    reset()
    onDone()
  }

  return (
    <Modal
      open={open}
      modalLabel={username ?? '当前用户'}
      modalHeading="修改口令"
      primaryButtonText="保存"
      secondaryButtonText="取消"
      primaryButtonDisabled={status !== 'inactive'}
      loadingStatus={status}
      loadingDescription="正在保存"
      loadingIconDescription="正在保存"
      onLoadingSuccess={done}
      onRequestSubmit={submit}
      onRequestClose={close}
      onSecondarySubmit={close}
      size="sm"
    >
      <div className="dbs-shell-password-form">
        {error !== '' && <NotificationBar tone="critical" title={error} />}
        <p className="dbs-shell-password-form__note dbs-caption">
          保存后，你在其他设备上的登录会被退出；当前这台不受影响。
        </p>
        <FormField errorText={failures.old} required>
          {(control) => (
            <PasswordInput
              id={control.id}
              labelText="当前口令"
              autoComplete="current-password"
              showPasswordLabel="显示口令"
              hidePasswordLabel="隐藏口令"
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              value={oldPassword}
              onChange={(event) => setOldPassword(event.target.value)}
            />
          )}
        </FormField>
        {/* 出错时不再重复常态提示：这一栏的错误文案本身就把规则写全了（「新口令至少 12 个
            字符」），提示与错误一上一下摞着说同一件事，只是把这一栏撑高两行。 */}
        <FormField
          errorText={failures.next}
          helperText={failures.next === undefined ? `至少 ${passwordMinLength} 个字符` : undefined}
          required
        >
          {(control) => (
            <PasswordInput
              id={control.id}
              labelText="新口令"
              autoComplete="new-password"
              showPasswordLabel="显示口令"
              hidePasswordLabel="隐藏口令"
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
          )}
        </FormField>
        <FormField errorText={failures.confirm} required>
          {(control) => (
            <PasswordInput
              id={control.id}
              labelText="确认新口令"
              autoComplete="new-password"
              showPasswordLabel="显示口令"
              hidePasswordLabel="隐藏口令"
              invalid={control.invalid}
              aria-describedby={control.describedBy}
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
            />
          )}
        </FormField>
      </div>
    </Modal>
  )
}
