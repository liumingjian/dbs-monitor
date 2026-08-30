// 令牌层的 JS 读取口。
//
// 存在的理由：`@carbon/charts` 的分类色板只接受 JS 选项（`options.color.scale`，
// 按系列名给色），既没有 Sass 入口，也不读 Carbon 的主题令牌。而规范要求
// 「所有颜色以语义令牌引用，不出现字面量色值」。所以真值只有一份 ——
// `web/src/styles/_palette.scss` 发布出来的 `--dbs-*` 自定义属性 —— 由这里读出来。
//
// 这个文件里**不允许出现任何色值字面量**。取不到就返回空，让调用方自己决定，
// 而不是在这里编一个兜底色。

/// DESIGN.md 的数据可视化色板令牌名，按序。刻意不含红色系。
export const vizColorTokens = [
  '--dbs-viz-1',
  '--dbs-viz-2',
  '--dbs-viz-3',
  '--dbs-viz-4',
  '--dbs-viz-5',
  '--dbs-viz-6',
] as const

/// 读一个令牌的计算值。样式表未加载时（例如 jsdom 里没有真实 CSS）返回空串。
export function tokenValue(name: string, element?: Element): string {
  const target = element ?? document.documentElement
  return getComputedStyle(target).getPropertyValue(name).trim()
}

/// 数据可视化色板的实际色值，按序。
export function vizPalette(element?: Element): string[] {
  return vizColorTokens.map((token) => tokenValue(token, element))
}
