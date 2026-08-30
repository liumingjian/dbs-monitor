//
// 侧栏折叠状态的纯模块。
//
// 折叠是一个真实状态：它跨页面保持，所以要落到存储里。存储在两类环境下不可用 ——
// `file://` 源与禁用了站点数据的浏览器 —— 而且**取 `localStorage` 这个动作本身就会抛**，
// 不是等到 `getItem` 才抛。所以这里收的是「取存储的函数」而不是一个 `Storage`：
// 只有这样，读写两侧才都能把那次抛包住，降级成「不记忆」而不是把整个壳打崩。
//
// 这一层不认识 React、不认识路由、不碰 `window`，因此可以在纯函数测试层里覆盖。
//

/** 取存储的方式。浏览器侧是 `browserStorage`；测试里传一个假的，或传一个会抛的。 */
export type StorageAccess = () => Storage | null | undefined

const storageKey = 'dbs-monitor.nav-collapsed'
const collapsedValue = 'collapsed'
const expandedValue = 'expanded'

/// 读取上次的折叠状态。存储不可用、没写过、或写进去的是别的东西时，一律当作展开。
export function readNavCollapsed(access: StorageAccess): boolean {
  try {
    return access()?.getItem(storageKey) === collapsedValue
  } catch {
    return false
  }
}

/// 记住折叠状态。存储不可用时静默降级为不记忆 —— 抛出去会连带打掉整个页面外框。
export function writeNavCollapsed(access: StorageAccess, collapsed: boolean): void {
  try {
    access()?.setItem(storageKey, collapsed ? collapsedValue : expandedValue)
  } catch {
    // 不记忆就是这里唯一的降级方式：状态在本次会话内仍然正常工作。
  }
}

/// 切换控件的可访问名。说的是「按下去会发生什么」，不是「现在是什么」。
export function navToggleLabel(collapsed: boolean): string {
  return collapsed ? '展开导航' : '收起导航'
}

/// 角标文案。还没取到数（`undefined`）与 0 一样不显示角标 —— 缺数不是 0，
/// 但「不知道有多少」和「没有」在角标上的表现只能一样：不画。
/// 超过两位数收成 `99+`，否则窄轨上放不下。
export function unreadBadgeText(count: number | undefined): string {
  if (count === undefined || !Number.isFinite(count) || count <= 0) return ''
  return count > 99 ? '99+' : String(Math.floor(count))
}

/// 导航项的可访问名：折叠后图标独自承担信息，未读条数必须念得出来。
export function navItemLabel(label: string, count: number | undefined): string {
  const badge = unreadBadgeText(count)
  return badge === '' ? label : `${label}，${badge} 条未处置告警`
}

/// 浏览器侧的取存储方式。取 `localStorage` 这一步本身可能抛，所以它必须是函数体。
export function browserStorage(): Storage {
  return window.localStorage
}
