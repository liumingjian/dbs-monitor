# 前端

TS + React + Vite 纯 SPA，AntD 6 + ECharts 6，TanStack Router + openapi-react-query。

## 样式与令牌

`web/src/styles/index.scss` 是**全应用唯一的 Sass 入口**，只在 `main.tsx` 里 import 一次。
组件级样式表**不得**是 `.scss`、**不得** `@use` Carbon：Sass 只允许模块在首次加载时被
`with (...)` 配置，Vite 又把每个 `.scss` 当独立编译单元，组件表里 @use 到 Carbon
会拿到一份没配过的 Carbon —— 中文回退栈与令牌覆盖静默失效，或者报一句
`This module was already loaded` 的无关错误。

组件侧写纯 `.css`：颜色 / 间距 / 圆角一律 `var(--cds-*)`（Carbon 自带）或
`var(--dbs-*)`（规范新增，见 `web/src/styles/_product-tokens.scss`），字体档位用
`web/src/styles/_type-classes.scss` 发射的 `.dbs-*` 类。**任何地方都不写字面量色值**；
唯一的例外是 `web/src/styles/_palette.scss`，那里是 DESIGN.md 令牌表的落地点。
JS 需要色值时走 `web/src/styles/tokens.ts`，不要在 TS 里写 hex。

### Carbon 组件子集

`index.scss` 里逐个 `@use '@carbon/react/scss/components/<name>'` 的那张清单**就是**本应用的
Carbon 组件面。整包引入（`@use` 整个 @carbon/react 包）会发射全部约 70 个组件的 CSS（实测 100 kB gzip，
规格估算的两倍），已经收窄掉。**用清单外的 Carbon 组件，它会渲染成没有样式的裸元素**——
不报错，只是难看。需要新组件就在 `index.scss` 加一行并注明哪个页面要它。
Carbon 的 16 栅格没有引入：版式用组件样式表里的原生 CSS grid，不要用 `<Grid>` / `<Column>`。

前端依赖装在 `make web-install` 下（或自带 `IBM_TELEMETRY_DISABLED=true`）：
Carbon 的 `postinstall` 会上报遥测，开关在根 Makefile 里显式关着。

## 状态只有三个桶，没有第四个

服务端状态 → TanStack Query；URL 状态 → search params；组件局部 → `useState`。
不引入 Redux / Zustand / Jotai / MobX / Valtio。
不用 `createContext` 手搓全局 store；`createContext` 白名单：无（新增须登记完整仓库相对路径）。
`step` 可进 URL，但渲染永远用响应回传的粒度值。

## 缺数不是 0

禁止 `?? 0` / `|| 0`。需要豁免时写明理由。
图表一律用 `web/src/domain/MetricChart.tsx`，其 `unavailability` 参数必填；不装 `echarts-for-react`。
轮询数据必须用 `dataUpdatedAt` 判新鲜度。

## 枚举

对枚举做映射必须用带 `assertNever` 的穷尽 `switch`。
禁止 `default:` 兜底成 fallback 文案。

## 目录

路由树即页面树；页面私有件不上浮。
共享件只有两层：`domain/`（带业务含义）与 `primitives/`（无业务含义的展示件）。
`web/src/styles/` 是令牌层，不是第三层共享件：只放样式与令牌读取口，不放组件。
不建 `components/` / `utils/` / `shared/` / `common/` —— 这些名字不表达取舍，什么都能塞。
`invalidateQueries` 只许出现在 `domain/<域>/mutations.ts`。

### `domain/` 仍是封闭清单

`AlertStatus` / `CollectionPausedTag` / `Freshness` / `HealthStatus` / `MetricChart` / `SuppressionTags` / `TimeRangePicker` / `UnavailabilityBlock`。
清单只增不改语义：新增项须带业务含义，且在本文件登记。通用面板、表格外壳、指标条不得进入。

### `primitives/`

只放展示件：面板、表格外壳、状态徽标、指标条、通知条、抽屉、表单字段外壳、内联图标集。
判定：**能否给它起一个不含本仓库任何术语的名字，并原样搬进一个与 PostgreSQL 监控无关的产品？**
能 → `primitives/`；名字或取值绕不开实例 / 告警 / 指标 / 采集 / 抑制 / 新鲜度等概念 → `domain/`。
约束：不认识任何业务概念（含枚举取值与其文案映射）；不取数（无 `useQuery` / 无 `openapi-react-query`）；
不写 `invalidateQueries`；不认识路由（无 `Link` / `useNavigate` / 无 search params）。
一件一文件，只导出组件；不放工具函数——`primitives/` 不是新的 `utils/`。

基线清单（页面从这里取件，不要自己再拼一套）：
`DataGrid`（数据表格外壳） / `Drawer` / `FormField` / `Icon` / `MetricBar` / `NotificationBar` /
`Panel` / `SkeletonBlock` / `StatusBadge` / `StatusDot` / `TruncatedText`。
页面组**不得修改**这一层；缺件写进结题报告，由协调者派活，不要在别人脚下改共享件。

表格的三条硬规则写在 `DataGrid.tsx` 顶部：1280px 及以上不横向滚动也不丢列（fixed 布局 +
按列最小宽度分配的百分比列宽 + 省略号悬停提示），粘性表头与横向滚动容器不是同一个元素，
行高显式给死（标准 40px / 密集 32px）。页面只需给每列 `minWidth`，不要自己设 `overflow-x`。

@floating-ui/react 是 `Drawer` 的焦点陷阱用的直接依赖（它本来就在 Carbon 的依赖树里，
这里只是显式声明）。除抽屉外没有第二处用它，页面组也不得因此认为可以新增依赖。

## 先例

路由定义：`web/src/main.tsx`。
`validateSearch`：`web/src/routes/instances.$id/index.tsx`。
跨页继承 search params：`web/src/routes/instances/index.tsx`。
领域图表组件：`web/src/domain/MetricChart.tsx`。
展示组件基线：`web/src/primitives/`（面板 `Panel.tsx`、表格外壳 `DataGrid.tsx`、抽屉 `Drawer.tsx`）。

## 测试定位

测试只断言外部可观察的行为。定位元素按以下顺序，先能用哪个用哪个：

1. **无障碍角色 + 可访问名**：`getByRole('button', { name: '保存' })`。首选，因为它同时证明元素可达。
2. **稳定测试标识**：角色/名字表达不了时（计数一类节点、锁定某个区域），在实现上加
   `data-testid`，测试用 `getByTestId`。**唯一的标识约定就是 `data-testid`**，不新增别的属性名；
   Testing Library 与 Playwright 都默认认它。
3. 组件库生成的类名（`.ant-*`）、`data-row-key` 之类的组件库私有 DOM 属性、DOM 嵌套结构：
   **一律禁止**。它们在换库时全部失效，且失效不携带任何信息。

已有的 `data-overview-module` / `data-columns` / `data-fresh` / `data-loading` 是承载领域取值的
语义属性，不是测试标识，保留原样；只有纯粹为定位而存在的钩子才叫 `data-testid`。

角色是实现要守的契约，不是组件库顺手给的副产品。换组件库时必须保住：页签渲染
`role="tab"` 且带 `aria-selected`，开关渲染 `role="switch"`，数字输入渲染 `role="spinbutton"`，
可访问名一律由实现用 `aria-label` 显式给出。组件库的 props 允许时就把 `role` 显式写出来
（如 `InputNumber role="spinbutton"`）；不允许时（AntD 6 的 `Switch` 就不收 `role`），
换库那次必须自行验证新组件仍然渲染出同样的角色。
