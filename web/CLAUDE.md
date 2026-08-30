# 前端

TS + React + Vite 纯 SPA，AntD 6 + ECharts 6，TanStack Router + openapi-react-query。

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
`domain/` 封闭清单：`AlertStatus` / `CollectionPausedTag` / `Freshness` / `HealthStatus` / `MetricChart` / `SuppressionTags` / `TimeRangePicker` / `UnavailabilityBlock`。
不建 `components/` / `utils/` / `shared/` / `common/`。
`invalidateQueries` 只许出现在 `domain/<域>/mutations.ts`。

## 先例

路由定义：`web/src/main.tsx`。
`validateSearch`：`web/src/routes/instances.$id/index.tsx`。
跨页继承 search params：`web/src/routes/instances/index.tsx`。
领域图表组件：`web/src/domain/MetricChart.tsx`。

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
