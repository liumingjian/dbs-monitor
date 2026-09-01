import { Button, Tab, TabList, Tabs } from '@carbon/react'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import { Icon } from '../../primitives/Icon'
import type { MonitoringSearch } from './timeRange'
import './workbench.css'

type WorkbenchTab = 'overview' | 'monitoring' | 'sessions' | 'events' | 'alerts'

type WorkbenchHeaderProps = {
  id: string
  instanceName: string | undefined
  activeKey: WorkbenchTab
  search: MonitoringSearch
}

const tabOrder: WorkbenchTab[] = ['overview', 'monitoring', 'sessions', 'events', 'alerts']

/// 实例工作台的页头与页签条。
///
/// 页签条**就是**导航（每个页签是一个地址），所以每个 `Tab` 以 `as={Link}` 渲染成真链接：
/// 中键新开、复制链接、悬停预取都还在，`role="tab"` / `aria-selected` / 方向键漫游也都还在。
/// 判定与理由写在 `web/CLAUDE.md` 的先例一节，后续页面照抄，不要各自再定一次。
export function WorkbenchHeader({ id, instanceName, activeKey, search }: WorkbenchHeaderProps) {
  // Carbon 的 `as` 槽只收一个组件，不能顺带把路由属性也交给它（`params` / `search` 的类型
  // 与 `to` 绑定，转一手就丢了）。所以路由属性写在这里的闭包里，`as` 拿到的是一个已经
  // 知道自己去哪儿的组件；memo 是为了让它跨渲染保持同一个身份，否则每次渲染都会重挂锚点、
  // 把键盘焦点甩掉。
  const links = useMemo(() => {
    const timeRange = { from: search.from, to: search.to }
    return {
      collection: (props: object) => <Link {...props} to="/instances/$id/collection" params={{ id }} />,
      settings: (props: object) => <Link {...props} to="/instances/$id/settings" params={{ id }} />,
      overview: (props: object) => <Link {...props} to="/instances/$id" params={{ id }} search={search} />,
      monitoring: (props: object) => <Link {...props} to="/instances/$id/monitoring" params={{ id }} search={search} />,
      sessions: (props: object) => <Link {...props} to="/instances/$id/sessions" params={{ id }} search={timeRange} />,
      events: (props: object) => <Link
        {...props}
        to="/instances/$id/performance-events"
        params={{ id }}
        search={{ ...timeRange, tab: 'firing', disposition: 'ACKED', page: 1 }}
      />,
      alerts: (props: object) => <Link
        {...props}
        to="/instances/$id/alerts"
        params={{ id }}
        search={{ tab: 'current', include_paused: false }}
      />,
    }
  }, [id, search])

  return <div className="workbench-header">
    <Link to="/instances" className="cds--link workbench-header__back">← 返回实例列表</Link>
    <div className="workbench-header__heading">
      <div>
        <h1 className="dbs-page-title">{instanceName ?? '实例工作台'}</h1>
        <p className="dbs-caption">实例工作台</p>
      </div>
      <div className="workbench-header__actions">
        <Button as={links.collection} kind="tertiary" size="md" renderIcon={Icon.glyph.database}>采集管理</Button>
        <Button as={links.settings} kind="tertiary" size="md" renderIcon={Icon.glyph.settings}>接入设置</Button>
      </div>
    </div>
    <Tabs selectedIndex={tabOrder.indexOf(activeKey)}>
      <TabList aria-label="实例工作台" activation="manual">
        <Tab as={links.overview}>实例总览</Tab>
        <Tab as={links.monitoring}>监控与报警</Tab>
        <Tab as={links.sessions}>会话与阻塞</Tab>
        <Tab as={links.events}>性能事件</Tab>
        <Tab as={links.alerts}>告警</Tab>
      </TabList>
    </Tabs>
  </div>
}

