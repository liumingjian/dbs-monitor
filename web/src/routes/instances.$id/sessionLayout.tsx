import { ContentSwitcher, Switch, Tab, TabList, Tabs } from '@carbon/react'
import { Link } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel } from '../root/tableDensity'
import type { SessionSearch } from './sessionSearch'
import { sessionTabLabel, sessionTabQuery, sessionTabs, withSessionTab, type SessionTab } from './sessionTabs'

/// 三个标签内容共用的入参。数据各取各的，但版式的三个旋钮（实例、调查上下文、行高）
/// 由合并页统一持有 —— 密度是产品级偏好，切标签不该把它忘掉。
export type SessionTabPanelProps = {
  id: string
  search: SessionSearch
  density: TableDensity
  onDensityChange: (density: TableDensity) => void
  /** 改调查上下文（时间范围）就是换一个可分享的地址，由合并页统一导航。 */
  onSearchChange: (search: SessionSearch) => void
}

/// 密集模式切换。读写只有 `routes/root/tableDensity.ts` 一个去处，键名不要各自再发明。
/// 分段单选而不是开关：开关只说得出「密集模式：关」，说不出「标准行高」。
export function SessionDensitySwitcher({ density, onChange }: {
  density: TableDensity
  onChange: (density: TableDensity) => void
}) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return <ContentSwitcher
    size="sm"
    selectedIndex={densities.indexOf(density)}
    onChange={({ index }) => {
      // 组件库把选中下标标成可选；拿不到下标就是没换档，什么都不做，别兜底成第一档。
      const next = index === undefined ? undefined : densities[index]
      if (next !== undefined) onChange(next)
    }}
  >
    {densities.map((value) => <Switch key={value} name={value} text={densityLabel(value)} />)}
  </ContentSwitcher>
}

/// 会话合并页的二级页签条与地址构造。
///
/// **这是一次路由合并**（票 #200）：会话快照、长查询采样记录、查询统计排行三个地址收拢成
/// `/instances/$id/sessions` 一个地址，谁在前台由 search param `tab` 决定。
///
/// 页签条**就是**导航（每个页签是一个地址），所以每个 `Tab` 以 `as={链接组件}` 渲染成真锚点：
/// 中键新开、复制链接、悬停预取都还在，`role="tab"` / `aria-selected` / 方向键漫游由组件库
/// 照常给。`TabList` 必须 `activation="manual"` —— 自动激活会让方向键在不导航的情况下改选中态，
/// 页签与地址就对不上了。判定与理由见 `web/CLAUDE.md` 的先例一节，样板是 `workbench.tsx`。
export function SessionTabStrip({ id, search, active }: {
  id: string
  search: SessionSearch
  active: SessionTab
}) {
  // `as` 槽只收组件，不能顺带把路由属性交出去（`params` / `search` 的类型与 `to` 绑定，
  // 转一手就退化成任意对象）。所以每个去处包成一个「已经知道自己去哪儿」的组件，
  // 并用 useMemo 固定身份 —— 身份一变锚点重挂，键盘焦点会被甩掉。
  const links = useMemo(() => {
    const destination = (tab: SessionTab) => (props: object) => <Link
      {...props}
      to="/instances/$id/sessions"
      params={{ id }}
      search={withSessionTab(search, tab)}
    />
    return {
      current: destination('current'),
      samples: destination('long-query-samples'),
      statistics: destination('query-statistics'),
    }
  }, [id, search])

  return <Tabs selectedIndex={sessionTabs.indexOf(active)}>
    <TabList aria-label="会话与阻塞" activation="manual">
      <Tab as={links.current}>{sessionTabLabel('current')}</Tab>
      <Tab as={links.samples}>{sessionTabLabel('long-query-samples')}</Tab>
      <Tab as={links.statistics}>{sessionTabLabel('query-statistics')}</Tab>
    </TabList>
  </Tabs>
}

/// 三个标签的字符串地址。合并之后它们只差一个 `tab` 参数，但仍然是三个独立的、
/// 可收藏可分享的地址 —— 监控页的长查询下钻、不可用说明块的「回到本页」都从这里取。
export function sessionPageHref(id: string, search: SessionSearch): string {
  return tabHref(id, search, 'current')
}

export function longQuerySamplesPageHref(id: string, search: SessionSearch): string {
  return tabHref(id, search, 'long-query-samples')
}

export function queryStatisticsPageHref(id: string, search: SessionSearch): string {
  return tabHref(id, search, 'query-statistics')
}

function tabHref(id: string, search: SessionSearch, tab: SessionTab): string {
  const params = new URLSearchParams(sessionTabQuery(search, tab))
  return `/instances/${encodeURIComponent(id)}/sessions?${params.toString()}`
}
