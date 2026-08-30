//
// 表格密度偏好的纯模块。
//
// 密度是一个**跨页面**的偏好，不是某张表的局部状态：一个把行高调紧的运维，在实例列表、
// 会话列表、告警流上要的是同一个行高。所以它和侧栏折叠一样落到存储里，并且和
// `navCollapse.ts` 放在一起 —— 应用级偏好只有这一个去处，别在页面里再写一份存储读写。
//
// 页面侧只做两件事：`useState(() => readTableDensity(browserStorage))`，以及在切换控件的
// `onChange` 里同时 `setDensity` + `writeTableDensity`。控件本身由页面渲染（一个
// Carbon `ContentSwitcher`，样板见 `web/src/routes/instances/index.tsx`），因为它属于
// 那张表的工具条，而不属于一个共享组件。
//
// 这一层不认识 React、不认识路由、不碰 `window`，因此可以在纯函数测试层里覆盖。
//

import type { StorageAccess } from './navCollapse'

/** 行高档位。与 `primitives/DataGrid` 的 `density` 同型：标准 40px / 密集 32px。 */
export type TableDensity = 'standard' | 'dense'

const storageKey = 'dbs-monitor.table-density'

/// 读取上次的密度偏好。存储不可用、没写过、或写进去的是别的东西时，一律当作标准行高。
export function readTableDensity(access: StorageAccess): TableDensity {
  try {
    return access()?.getItem(storageKey) === 'dense' ? 'dense' : 'standard'
  } catch {
    return 'standard'
  }
}

/// 记住密度偏好。存储不可用时静默降级为不记忆 —— 抛出去会连带打掉整个页面。
export function writeTableDensity(access: StorageAccess, density: TableDensity): void {
  try {
    access()?.setItem(storageKey, density)
  } catch {
    // 不记忆就是这里唯一的降级方式：偏好在本次会话内仍然正常工作。
  }
}

/// 切换控件的可访问名。说的是这一档是什么，不是按下去会发生什么 ——
/// 它是一组分段单选（当前档位由 `aria-selected` 表达），不是一个开关。
export function densityLabel(density: TableDensity): string {
  switch (density) {
    case 'standard':
      return '标准行高'
    case 'dense':
      return '紧凑行高'
    default:
      return assertNever(density)
  }
}

function assertNever(value: never): never {
  throw new Error(`unexpected table density: ${String(value)}`)
}
