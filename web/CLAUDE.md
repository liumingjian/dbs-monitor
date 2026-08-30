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
`Panel` / `SkeletonBlock` / `Sparkline`（行内趋势缩略图，手写 SVG） / `StatusBadge` / `StatusDot` /
`TruncatedText`。
页面组**不得修改**这一层；缺件写进结题报告，由协调者派活，不要在别人脚下改共享件。

表格的三条硬规则写在 `DataGrid.tsx` 顶部：1280px 及以上不横向滚动也不丢列（fixed 布局 +
按列最小宽度分配的百分比列宽 + 省略号悬停提示），粘性表头与横向滚动容器不是同一个元素，
行高显式给死（标准 40px / 密集 32px）。页面只需给每列 `minWidth`，不要自己设 `overflow-x`。

@floating-ui/react 是 `Drawer` 的焦点陷阱用的直接依赖（它本来就在 Carbon 的依赖树里，
这里只是显式声明）。除抽屉外没有第二处用它，页面组也不得因此认为可以新增依赖。

### `forms/`

表单基础设施层，和 `api/` / `styles/` 一样是层而不是第三类共享件：**不放组件**，
不认识任何业务概念，只放把 zod 接到 react-hook-form 上的适配件（当前只有 `zodResolver.ts`）。
服务端字段错误的映射不在这里，在 API 层的 `web/src/api/errors.ts`（`applyApiFieldErrors`）。

表单的写法是定死的，页面不要各写一套：

- 表单状态与校验一律 react-hook-form + zod；不用组件库的表单 API（Carbon 只有字段级的
  `invalid` / `invalidText`，没有表单级 API），也不用受控 `useState` 手搓校验。
- schema 与 `web/src/api/schema.d.ts` 生成的类型对齐：字段取值清单写
  `as const satisfies readonly <生成的联合类型>[]`，schema 写
  `satisfies z.ZodType<生成的请求体类型>`，再由一个返回该请求体类型的函数把出参真的用出去。
  漂了就编译不过，这是相对旧的 `rules` 数组的净改进，别把它退化成注释。
- schema 里**不写** `transform` / `default`（`zodResolver` 的类型也不收）：表单值就是提交值，
  trim、空串转 undefined 这类归一化放在提交处。
- 校验错误一律显示在对应字段下方，用 `web/src/primitives/FormField.tsx` 的 `errorText`，
  **不做页面顶部的错误汇总**。整表单级的失败（没有字段信息的那种）才用错误条。
- 服务端字段错误用 `applyApiFieldErrors`；它返回空数组才退回整表单的错误条，两边不要都显示。
- 控件的 `labelText` 留空并 `hideLabel` / `noLabel`，标签由 `FormField` 出——两边都给会读两遍。
  已知缺口：Carbon 的 `TextArea` 与 `Select` 在自己算完 `aria-describedby` 后才落笔，会盖掉
  `FormField` 交过来的那个（`TextInput` 不会，它的 `...rest` 在最后）。照样把 `describedBy` 接上去，
  不要为此改 `primitives/`，也不要改用控件自带的 `invalidText` 去绕——错误文案只有一个出口。

## 先例

路由定义：`web/src/main.tsx`。
`validateSearch`：`web/src/routes/instances.$id/index.tsx`。
跨页继承 search params：`web/src/routes/instances/index.tsx`。
领域图表组件：`web/src/domain/MetricChart.tsx`。
展示组件基线：`web/src/primitives/`（面板 `Panel.tsx`、表格外壳 `DataGrid.tsx`、抽屉 `Drawer.tsx`）。
表单：`web/src/routes/instances.$id/alertEvidence.tsx` 的处置表单（客户端校验、服务端字段错误
回填与聚焦、重置、字段联动四件都在里面）。
应用外框（炭黑页头 + 可折叠侧栏）：`web/src/routes/root/index.tsx`；
折叠状态是纯模块 `web/src/routes/root/navCollapse.ts`，不要在组件里再写一份存储读写。
页面版式与列表页样板：`web/src/routes/instances/index.tsx`。

### 列表页的三段版式与密度切换

列表页一律三段：**页头**（`h1` + 该页唯一的主操作）、**工具条**（筛选控件 + 数据新鲜度）、
**一个 `flush` 的 `Panel` 包住 `DataGrid`，分页放进 `Panel` 的 footer**。面板标题栏右侧只放
「作用于这张表的视图开关」，主操作不放那里。列只给 `minWidth`；40px 的行放不下两行，
所以一格只写一个事实，别把四件事挤进一格。

**密度切换是产品级偏好，不是某张表的局部状态。** 读写只有一个去处：纯模块
`web/src/routes/root/tableDensity.ts`（`readTableDensity` / `writeTableDensity`，落 localStorage，
存储不可用就降级成不记忆）。页面拿它初始化 `useState`，在 `onChange` 里同时 set + write，
控件是一个两档的 Carbon `ContentSwitcher`（分段单选，不是开关）。样板在实例列表里，照抄十行，
不要各自再发明一套键名。**密集档（32px）不渲染趋势缩略图那一列** —— 规范是「丢掉缩略图而不是
压扁它」，留一列空格子只是白占宽度。

### 页签条是导航，不是受控状态

`/instances/$id/*` 与 `/alert-settings/*` 的页签条本身就是地址切换。写成
`<Tabs selectedIndex onChange={navigate}>` 能保住 `role="tab"` 与 `aria-selected`，但页签退化成
`<button>`：中键新开、复制链接、悬停预取全都没了，而规范不允许丢功能点。

**做法：页签一律 `<Tab as={链接组件}>`。** Carbon 的 `Tab` 收 `as`，渲染出来是真锚点，而
`role="tab"` / `aria-selected` / `aria-controls` / 方向键漫游仍由 Carbon 照常给（1.115.0 实测）。
`selectedIndex` 由当前路由算出；`TabList` 给 `activation="manual"` —— 自动激活会让方向键在不导航的
情况下改选中态，页签与地址就对不上了。

`as` 槽只收组件，不能顺带把路由属性一起交出去（`params` / `search` 的类型与 `to` 绑定，转一手就
退化成任意对象）。所以每个去处包成一个「已经知道自己去哪儿」的组件，并用 `useMemo` 固定它的身份 ——
身份一变锚点就重挂，键盘焦点会被甩掉。样板见 `web/src/routes/instances.$id/workbench.tsx`。

## 测试定位

测试只断言外部可观察的行为。定位元素按以下顺序，先能用哪个用哪个：

1. **无障碍角色 + 可访问名**：`getByRole('button', { name: '保存' })`。首选，因为它同时证明元素可达。
2. **稳定测试标识**：角色/名字表达不了时（计数一类节点、锁定某个区域），在实现上加
   `data-testid`，测试用 `getByTestId`。**唯一的标识约定就是 `data-testid`**，不新增别的属性名；
   Testing Library 与 Playwright 都默认认它。
3. 组件库生成的类名（`.ant-*`）、`data-row-key` 之类的组件库私有 DOM 属性、DOM 嵌套结构：
   **一律禁止**。它们在换库时全部失效，且失效不携带任何信息。

**表格的行只有一个钩子：`DataGrid` 的 `rowTestId`。** 行的外壳归共享组件，页面没有别的地方
能给行挂标识，所以定位行既不许回到组件库的 `data-row-key`，也不该退化成「含有某个单元格的行」
这类偶然写法。列表页一律传 `rowTestId="<实体>-row"`（`instance-row`、`alert-row`），
测试用 `getByTestId('instance-row')`；它只挂在数据行上，骨架行与空态行不带，
因此它的计数就等于数据行数。想定位某一行仍然用 `getByRole('row', { name: ... })`。

已有的 `data-overview-module` / `data-columns` / `data-fresh` / `data-loading` 是承载领域取值的
语义属性，不是测试标识，保留原样；只有纯粹为定位而存在的钩子才叫 `data-testid`。

角色是实现要守的契约，不是组件库顺手给的副产品。换组件库时必须保住：页签渲染
`role="tab"` 且带 `aria-selected`，开关渲染 `role="switch"`，数字输入渲染 `role="spinbutton"`，
可访问名一律由实现用 `aria-label` 显式给出。组件库的 props 允许时就把 `role` 显式写出来
（如 `InputNumber role="spinbutton"`）；不允许时（AntD 6 的 `Switch` 就不收 `role`），
换库那次必须自行验证新组件仍然渲染出同样的角色。
