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
共享件只有两层：`domain/`（带业务含义）与 `primitives/`（无业务含义的展示件）。
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

## 先例

路由定义：`web/src/main.tsx`。
`validateSearch`：`web/src/routes/instances.$id/index.tsx`。
跨页继承 search params：`web/src/routes/instances/index.tsx`。
领域图表组件：`web/src/domain/MetricChart.tsx`。
