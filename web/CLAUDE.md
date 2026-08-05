# 前端

TS + React + Vite 纯 SPA，AntD 6 + ECharts 6，TanStack Router + openapi-react-query。
理由与目录结构：`docs/design/08-frontend-stack-and-ui.md`。

## 状态只有三个桶，没有第四个

服务端状态 → TanStack Query；URL 状态 → search params；组件局部 → `useState`。
不引入 Redux / Zustand / Jotai / MobX / Valtio。
不用 `createContext` 手搓全局 store；当前没有 `createContext` 白名单文件。
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
`domain/` 封闭清单：`Freshness` / `MetricChart` / `UnavailabilityBlock`。
不建 `components/` / `utils/` / `shared/` / `common/`。
`invalidateQueries` 只许出现在 `domain/<域>/mutations.ts`。

## 先例

路由定义：`web/src/main.tsx`。
`validateSearch`：`web/src/routes/instances.$id/index.tsx`。
跨页继承 search params：`web/src/routes/instances/index.tsx`。
领域图表组件：`web/src/domain/MetricChart.tsx`。
