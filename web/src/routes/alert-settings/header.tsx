import { Tab, TabList, Tabs } from '@carbon/react'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { $api } from '../../api/client'
import { alertSettingsTabs } from './tabs'
import type { AlertSettingsTab } from './tabs'

/// 告警设置合并页的页头与页签条。
///
/// 四个设置页合并成一个多标签页面之后，页签条**就是**导航（每个页签是一个地址），
/// 所以每个 `Tab` 以 `as={链接组件}` 渲染成真锚点：中键新开、复制链接、悬停预取都还在，
/// `role="tab"` / `aria-selected` / 方向键漫游由组件库照常给。判定与理由见
/// `web/CLAUDE.md` 的先例一节，样板是 `routes/instances.$id/workbench.tsx`。
export function AlertSettingsHeader({ active }: { active: AlertSettingsTab }) {
  const failureQuery = $api.useQuery(
    'get',
    '/api/v1/notification-channels/failures',
    {},
    { refetchInterval: 15_000 },
  )

  // `as` 槽只收组件，不能顺带把路由属性交出去，所以每个去处包成一个「已经知道自己去哪儿」
  // 的组件，并用 useMemo 固定身份 —— 身份一变锚点重挂，键盘焦点会被甩掉。
  const links = useMemo(() => ({
    channels: (props: object) => <Link {...props} to="/alert-settings" search={{ tab: 'channels' as const }} />,
    contacts: (props: object) => <Link {...props} to="/alert-settings" search={{ tab: 'contacts' as const }} />,
    policies: (props: object) => <Link {...props} to="/alert-settings" search={{ tab: 'policies' as const }} />,
    maintenance: (props: object) => <Link {...props} to="/alert-settings" search={{ tab: 'maintenance' as const }} />,
  }), [])

  return (
    <div className="alert-settings-header">
      <h1 className="dbs-page-title">告警设置</h1>
      <p className="dbs-caption alert-settings-header__lede">
        通知渠道、联系人、通知策略与维护窗口现在收拢在同一页；旧地址会落到对应标签。
      </p>
      <Tabs selectedIndex={alertSettingsTabs.indexOf(active)}>
        <TabList aria-label="告警设置" activation="manual">
          <Tab as={links.channels}>
            <NotificationChannelsLabel hasFailures={failureQuery.data?.has_failures === true} />
          </Tab>
          <Tab as={links.contacts}>联系人</Tab>
          <Tab as={links.policies}>通知策略</Tab>
          <Tab as={links.maintenance}>维护窗口</Tab>
        </TabList>
      </Tabs>
    </div>
  )
}

/// 通知渠道页签上的失败角标。颜色不是唯一信号：角标带 `title`，读屏与悬停都能取到理由。
/// `data-testid` 是 e2e 依赖的定位钩子，换实现时必须原样保留。
export function NotificationChannelsLabel({ hasFailures }: { hasFailures: boolean }) {
  return (
    <span className="alert-settings-tab-label">
      通知渠道
      {hasFailures && (
        <span
          className="alert-settings-tab-label__dot"
          data-testid="notification-failure-badge"
          title="通知渠道存在未清除失败"
        />
      )}
    </span>
  )
}
