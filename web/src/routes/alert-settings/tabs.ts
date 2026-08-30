/// 告警设置合并页的页签词汇与地址解析。
///
/// 四个原本各自独立的设置页（通知渠道 / 联系人 / 通知策略 / 维护窗口）合并成一个
/// 多标签页面之后，「当前在哪个标签」是**地址状态**（web/CLAUDE.md：URL 状态 → search params），
/// 不是组件局部状态 —— 页签条本身就是导航，刷新、收藏、复制链接都要落回同一个标签。
///
/// 纯模块，不认识 React 也不认识路由器：解析与词汇在这里定义一次，页面与重定向共用。

export type AlertSettingsTab = 'channels' | 'contacts' | 'policies' | 'maintenance'

/** 页签顺序即渲染顺序，`selectedIndex` 从这里算。 */
export const alertSettingsTabs = [
  'channels',
  'contacts',
  'policies',
  'maintenance',
] as const satisfies readonly AlertSettingsTab[]

export type AlertSettingsSearch = {
  tab: AlertSettingsTab
  /** 维护窗口快捷入口带来的实例，新建时预选。 */
  instance_id?: string
  /** 维护窗口快捷入口：进入时直接打开新建表单。 */
  new_window?: boolean
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert settings tab: ${String(value)}`)
}

export function alertSettingsTabLabel(tab: AlertSettingsTab): string {
  switch (tab) {
    case 'channels':
      return '通知渠道'
    case 'contacts':
      return '联系人'
    case 'policies':
      return '通知策略'
    case 'maintenance':
      return '维护窗口'
    default:
      return assertNever(tab)
  }
}

function isAlertSettingsTab(value: unknown): value is AlertSettingsTab {
  return typeof value === 'string' && (alertSettingsTabs as readonly string[]).includes(value)
}

/// 地址解析。认不出来的 `tab` 退回第一个标签而不是渲染空页 ——
/// 合并之前 `/alert-settings` 这个前缀下的每个地址都渲染得出东西，之后也得如此。
export function parseAlertSettingsSearch(search: Record<string, unknown>): AlertSettingsSearch {
  const parsed: AlertSettingsSearch = {
    tab: isAlertSettingsTab(search.tab) ? search.tab : 'channels',
  }
  if (typeof search.instance_id === 'string' && search.instance_id !== '') {
    parsed.instance_id = search.instance_id
  }
  if (search.new_window === true || search.new_window === 'true') {
    parsed.new_window = true
  }
  return parsed
}
