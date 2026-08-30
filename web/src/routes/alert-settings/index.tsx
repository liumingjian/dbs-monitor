import { createRoute, redirect } from '@tanstack/react-router'
import { useCallback, useState } from 'react'
import { $api } from '../../api/client'
import { NotificationBar } from '../../primitives/NotificationBar'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import { rootRoute } from '../root'
import { ContactsPanel } from './contacts'
import { AlertSettingsHeader } from './header'
import { MaintenancePanel, parseMaintenanceSearch } from './maintenance'
import { NotificationChannelsPanel } from './notifications'
import { PoliciesPanel } from './policies'
import { readOnlyReason } from './shared'
import { parseAlertSettingsSearch } from './tabs'
import type { AlertSettingsTab } from './tabs'
import './alertSettings.css'

/// 告警设置：一个多标签页面。
///
/// **这是一次路由合并。** 通知渠道 / 联系人 / 通知策略 / 维护窗口原本是四个地址、四个页面，
/// 各自顶着一条长得像页签、其实是四组独立链接的条。现在收拢成 `/alert-settings` 一个地址，
/// 「在哪个标签」是 search param（web/CLAUDE.md：URL 状态 → search params）。
///
/// 四个旧地址一个都没删，全部改为重定向（见本文件末尾），因为它们散落在实例总览、
/// 应用外框和用户的书签里 —— 合并不该让任何一条既有链接失效。
export const alertSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings',
  validateSearch: parseAlertSettingsSearch,
  component: AlertSettingsPage,
})

function AlertSettingsPage() {
  const search = alertSettingsRoute.useSearch()
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const role = currentUserQuery.data?.role
  const canManage = role === 'ALERT_ADMIN' || role === 'PLATFORM_ADMIN'

  // 快捷入口只在进入这一次打开新建表单。关掉之后再切回来不该又弹一次。
  const [shortcutSpent, setShortcutSpent] = useState(false)
  const spendShortcut = useCallback(() => setShortcutSpent(true), [])

  return (
    <div className="alert-settings-page">
      <AlertSettingsHeader active={search.tab} />
      {currentUserQuery.isPending
        ? <SkeletonBlock lines={4} />
        : (
          <>
            {!canManage && (
              <NotificationBar tone="info" title="只读模式">
                {readOnlyReason[readOnlyReasonKey(search.tab)]}
              </NotificationBar>
            )}
            <AlertSettingsTabContent
              tab={search.tab}
              canManage={canManage}
              initialInstanceID={search.instance_id}
              openMaintenanceEditor={search.new_window === true && !shortcutSpent && canManage}
              onMaintenanceEditorClosed={spendShortcut}
            />
          </>
        )}
    </div>
  )
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert settings tab: ${String(value)}`)
}

function readOnlyReasonKey(tab: AlertSettingsTab): keyof typeof readOnlyReason {
  switch (tab) {
    case 'channels':
      return 'channels'
    case 'contacts':
      return 'contacts'
    case 'policies':
      return 'policies'
    case 'maintenance':
      return 'maintenance'
    default:
      return assertNever(tab)
  }
}

/// 当前标签的内容。四个标签各自取自己的数据，**只渲染选中的那个** ——
/// 四份轮询同时跑起来是合并带来的唯一新成本，不接。
function AlertSettingsTabContent({ tab, canManage, initialInstanceID, openMaintenanceEditor, onMaintenanceEditorClosed }: {
  tab: AlertSettingsTab
  canManage: boolean
  initialInstanceID?: string
  openMaintenanceEditor: boolean
  onMaintenanceEditorClosed: () => void
}) {
  switch (tab) {
    case 'channels':
      return <NotificationChannelsPanel canManage={canManage} />
    case 'contacts':
      return <ContactsPanel canManage={canManage} />
    case 'policies':
      return <PoliciesPanel canManage={canManage} />
    case 'maintenance':
      return <MaintenancePanel
        canManage={canManage}
        initialInstanceID={initialInstanceID}
        openInitially={openMaintenanceEditor}
        onEditorOpened={onMaintenanceEditorClosed}
      />
    default:
      return assertNever(tab)
  }
}

// ---------------------------------------------------------------------------
// 旧地址 → 新标签
//
// 合并前的四个地址（外加维护窗口的快捷入口）全部保留为重定向路由。它们出现在实例总览的
// 链接、应用外框的告警设置入口、e2e 与用户的书签里，删掉就是丢功能点。
// 映射记录在票 #203 上。
// ---------------------------------------------------------------------------

function redirectToTab(tab: AlertSettingsTab) {
  throw redirect({ to: '/alert-settings', search: { tab }, replace: true })
}

export const notificationSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/notifications',
  beforeLoad: () => redirectToTab('channels'),
})

export const contactSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/contacts',
  beforeLoad: () => redirectToTab('contacts'),
})

export const policySettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/policies',
  beforeLoad: () => redirectToTab('policies'),
})

export const maintenanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/maintenance-windows',
  beforeLoad: () => redirectToTab('maintenance'),
})

/// 实例总览的「进入维护」快捷入口：带着实例落到维护窗口标签，并直接打开新建表单。
export const maintenanceNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/maintenance-windows/new',
  validateSearch: parseMaintenanceSearch,
  beforeLoad: ({ search }) => {
    throw redirect({
      to: '/alert-settings',
      search: { tab: 'maintenance' as const, instance_id: search.instance_id, new_window: true },
      replace: true,
    })
  },
})
