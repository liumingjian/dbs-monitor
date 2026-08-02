# 前端 UI 体系、图表库与数据获取层调研

> 本文件为 **RT-E 调研产出**，服务于 [issue #18](https://github.com/liumingjian/dbs-monitor/issues/18)，**不构成决策**。决策在 T7。
> 调研日期：2026-08-02。版本与许可证取自 npm registry 元数据与官方仓库原文，链接见各节。

---

## 0. 输入边界（不在本票重议）

- 技术栈已定死：**TypeScript + React + Vite 纯 SPA**；否决 Next.js（R2 地图 Notes 第 1 条）。
- 前端编译为静态资源 **`go:embed` 进 Go 主二进制** → 产物必须是纯静态文件、无 Node 运行时、体积是硬指标。
- R1 硬约束：**缺数不是 0，不得把缺数画成 0**（`01` 缺数语义、`03` §1.4 / §7.2）。
- 设计原则：**阿里云控制台优先，差异驱动澄清**（`03` §1.1）。
- 状态模型：告警五档 + 正交标记（`maintenance` / `acked` / `paused`）；`03` §7.2 的 12 种数据状态必须可区分。

## 0.1 版本与许可证核对（2026-08-02，来源：npm registry `dist-tags.latest` + `time`）

| 包 | 最新版本 | 发布日期 | 许可证 |
|---|---|---|---|
| `antd` | 6.5.3 | 2026-07-31 | MIT |
| `@mantine/core` | 9.5.0 | 2026-07-27 | MIT |
| `tailwindcss` | 4.3.3 | 2026-07-16 | MIT |
| `lucide-react`（shadcn 默认图标） | 1.28.0 | 2026-07-30 | ISC |
| `echarts` | 6.1.0 | 2026-05-19 | Apache-2.0 |
| `echarts-for-react` | 3.0.6 | 2026-01-21 | MIT |
| `recharts` | 3.10.1 | 2026-07-25 | MIT |
| `uplot` | 1.6.32 | 2025-03-14 | MIT |
| `@visx/visx` | 4.0.0 | 2026-06-11 | MIT |
| `@tanstack/react-query` | 5.101.4 | 2026-07-21 | MIT |
| `@tanstack/react-router` | 1.170.18 | 2026-07-13 | MIT |
| `react-router-dom` | 7.18.2 | 2026-07-28 | MIT |
| `openapi-typescript` / `openapi-fetch` | 7.13.0 / 0.17.0 | 2026-02-11 | MIT |
| `@hey-api/openapi-ts` | 0.99.0 | 2026-06-22 | MIT |
| `orval` | 8.23.0 | 2026-07-25 | MIT |

`shadcn/ui` 本身不是 npm 运行时依赖，仓库许可证为 **MIT**（<https://github.com/shadcn-ui/ui/blob/main/LICENSE.md>）。

**维护状态**：除 `uplot`（最近发布 2025-03-14，节奏慢但仍是单人维护的稳定库）外，其余均在近 3 个月内有发布。

---

## 1. UI 组件体系

### 1.1 对比表

| 维度 | Ant Design 6 | shadcn/ui + Tailwind 4 | Mantine 9 |
|---|---|---|---|
| 分发方式 | npm 依赖，组件在 `node_modules` | **组件源码复制进仓库**（官方原话：*"This is not a component library. It is how you build your component library."*，<https://ui.shadcn.com/docs>） | npm 依赖 |
| 设计语言 | **就是阿里云控制台的设计语言**（AntD 出自阿里，正是 `03` §1.1 的参照对象） | 无自带设计语言，需自建 | 自有设计语言，偏 SaaS 风格 |
| 密集数据表格 | `Table` 覆盖固定列 / 虚拟滚动 / 多选筛选 / 展开行，开箱最全 | 表格是 TanStack Table 无头方案 + 自写样式，功能要自己攒 | `Table` 较基础，复杂表格同样常配 TanStack Table |
| 抽屉详情 | `Drawer` 一等组件 | 基于 Radix Dialog 的 `Sheet` | `Drawer` 一等组件 |
| 表单密集配置（告警规则） | `Form` 含校验、联动、`Form.List` 动态项，AntD 最成熟的部分之一 | 需 react-hook-form + zod 自行搭 | `@mantine/form` 自带 |
| 状态标记体系 | `Tag` / `Badge` / `Space`，配色 token 化，`03` 的「色块+文字、色盲可辨」易落 | 需自建 Badge variant（但正因此完全可控） | `Badge` / `Pill` 可用 |
| 依赖树 | **重**：`antd@6.5.3` 直接依赖 40+ 个 `@rc-component/*` 包，包体解包 ~48.9 MB（npm `dist.unpackedSize`） | 极浅：Radix primitives 按需 + Tailwind（构建期） | 中：`@mantine/core` 5 个直接依赖，解包 ~9.1 MB |
| Tree-shaking | v6 支持 ESM 按需引入；`6.5.0` release notes 明确列出 "bundle size optimizations"（<https://github.com/ant-design/ant-design/releases>）。**注意 `dayjs` 与 `@ant-design/icons` 是运行时依赖，会进产物** | Tailwind 4 只输出用到的 class；组件是自己的源码，不用的根本不存在 | CSS Modules（v7+ 起弃用 emotion），CSS 按需 |
| 样式运行时 | CSS-in-JS（`@ant-design/cssinjs`），样式运行时生成 → 有运行时开销 | 构建期生成 CSS，零运行时 | CSS Modules，零运行时 |
| AI agent 友好性 | 改行为要「覆盖/包一层」：模型无法编辑 `node_modules`，只能靠 props + ConfigProvider token + CSS 覆盖 | **组件源码在仓库内，模型可直接编辑**（官方 "Open Code" 原则，同页） | 与 AntD 同类，靠 props + CSS variables |
| 升级破坏性 | 大版本（4→5→6）有迁移成本；小版本稳定 | 无「升级」概念：复制进来的代码不会被动变；代价是官方修复不会自动流入 | 大版本（6→7 换样式方案）也有过破坏性变更 |
| 许可证 | MIT | MIT | MIT |

### 1.2 推荐

**Ant Design 6 为主，在其上手写少量领域组件（状态标记、归因行、空状态壳）。**

理由（按 R1 约束权重排序）：

1. `03` §1.1 把「阿里云控制台优先」写成第一条设计原则，而 AntD 就是该控制台的设计语言 —— 选 AntD 时「差异驱动澄清」的差异面积最小；选 shadcn 等于把整套视觉语言重新推导一遍，且推导结果需要人来判定「像不像阿里云」，这是最贵的一类反馈。
2. R1 的页面形态高度集中在 AntD 最强的三块：密集表格（`03` §4.1 三层信息契约）、抽屉详情、表单密集的告警规则配置（`02`）。shadcn 在这三块都要自攒等价物。
3. MVP 是私有化内网控制台，**首屏体积不是用户可感知瓶颈**；`go:embed` 关心的是二进制大小而非网络传输，几 MB 量级的 gzip 产物对交付形态无实质影响。

**shadcn 的真实优势（AI agent 可直接改源码）在本项目被削弱**：Claude Code 会话面对的是「实现 R1 已冻结的规格」，需要的是**确定的反馈闭环**（TS 类型 + 编译器），不是无限的样式自由度。AntD 的 TS 类型质量高、API 面稳定，对模型反而是更强的约束。

### 1.3 被推翻的条件

- 若 T7 决定**放弃「像阿里云」**，改走自有视觉语言 → shadcn/ui + Tailwind 立刻反超。
- 若实测 `go:embed` 后主二进制体积越过交付红线（红线需先定），且 AntD 按需引入后仍占大头 → 重新评估 Mantine（依赖树浅一个数量级、零样式运行时）。
- 若 AntD 6 的 CSS-in-JS 运行时在 50 实例列表 + 多图页面上出现可测的交互卡顿 → 转 Mantine。
- 若团队已有 Tailwind 深度积累 → shadcn 的学习成本归零，权重变化。

### 1.4 未覆盖 / 无一手数据

- **三者的实际 gzip 产物体积未自测**。官方均未给出可直接引用的「典型控制台应用打包后体积」数字，本文拒绝引用二手 benchmark。
  自测方法：`vite build` 三个最小骨架（一张密集表格 + 一个抽屉 + 一个多字段表单），`ls -l dist/assets/*.js` 与 `gzip -c | wc -c` 对比，再各自 `go:embed` 后比较主二进制大小。
- AntD v6 相对 v5 的体积优化幅度，官方 release notes 只写 "bundle size optimizations"，无量化数字。

---

## 2. 图表库

### 2.1 缺数可表达性（R1 硬约束，一票否决维度）

| 库 | 缺数表达 | 一手证据 |
|---|---|---|
| **ECharts** | `null` 数据点断线；`series-line.connectNulls` **默认 `false`** | 官方 option 文档源文件 `en/option/series/line.md`：`## connectNulls(boolean) = false` — "Whether to connect the line across null points."；源码 `src/chart/line/LineSeries.ts` 默认 `connectNulls: false` |
| **Recharts** | 同样 `connectNulls` **默认 `false`** | 源码 `src/cartesian/Line.tsx`、`src/cartesian/Area.tsx` defaults `connectNulls: false`；注释原文："Line coordinates can have gaps in them. We have `connectNulls` prop that allows to connect those gaps anyway." |
| **uPlot** | README feature list 明确列 "Support for missing data" 并附 gap 演示 | <https://github.com/leeoniya/uPlot> |
| **visx** | 无内置概念，取决于自己写的 `defined()` 谓词（d3-shape 语义），完全可控但要自己写对 | — |

**结论：四者都能画断线，前三家默认即为「不连」。这条硬约束不构成区分度。**

**真正的风险不在图表库，在数据层**：后端返回的序列必须把缺数表达为 `null` 而非省略点或 0，前端不得用 `?? 0` 兜底。建议 T7 把这条落成 API 契约层的约定 + 一条 lint/测试护栏，而不是寄希望于图表库默认值。

另注：`03` §1.4 要求区分 12 种空状态 —— **任何图表库都表达不了「为什么没数据」**，这必须由图表外的空状态壳组件承担（图表区域整体替换为带原因的说明块）。选型对它无影响。

### 2.2 对比表

| 维度 | ECharts 6 | Recharts 3 | uPlot | visx 4 |
|---|---|---|---|---|
| 渲染 | Canvas（可选 SVG） | **SVG**（每点一个 DOM 节点） | Canvas | SVG（d3 无头封装） |
| 大点数时序 | 有 `large` / `sampling` / `dataZoom` 等一等能力 | SVG 节点数随点数线性增长，30 天原始数据下钻是已知风险区 | 作者 README 声称 166,650 点冷启动 25ms、约 10 万点/ms，并给出与 Chart.js/ECharts/Plotly 的对比表（**作者自测，非独立第三方 benchmark，本文不背书**） | 取决于自己实现 |
| 多图时间范围联动 | **一等能力**：`echarts.connect(group)`，官方 API 文档 `en/api/echarts.md`："connect(Function) — Connects interaction of multiple chart series" | 需自行提升共享状态（受控 `Brush` / `syncId`） | 需自行同步 scale / cursor（有 cursor sync API，需自己接） | 全自研 |
| TS 类型 | 官方随包，且提供 `ComposeOption<>` 按需组合出**更严格**的 option 类型（官方 handbook `basics/import`） | v3 已是 TS 重写，类型随包 | 随包 `dist/uPlot.d.ts`（README） | 每包独立 TS，类型质量高 |
| Tree-shaking | 官方支持 `echarts/core` + `echarts/charts` + `echarts/renderers` 按需 `use()`（handbook 同页），**不按需引入则打包全量** | ESM，按需 import | 单文件，README 称 min 后 ~50 KB（对比表中 47.9 KB） | 极细粒度按需 |
| React 集成 | 需 `echarts-for-react`（第三方，MIT，2026-01 有发布）或自写 ~30 行 wrapper | **原生 React 组件** | 需自写 wrapper | 原生 React |
| 心智模型 | option 对象（命令式 `setOption`） | JSX 声明式，最贴 React | 命令式 | JSX |
| 许可证 | Apache-2.0 | MIT | MIT | MIT |

### 2.3 推荐

**ECharts 6（`echarts/core` 按需引入 + 自写薄 React wrapper）。**

1. **多图时间范围联动**是 `03` §4.3 标准监控页刚需（多图共享时间范围与游标），ECharts 的 `echarts.connect` 是官方一等能力，其余三家都要自己实现同步 —— 这是典型「看起来简单、边界条件很多」的活。
2. **30 天原始数据下钻**下 Recharts 的 SVG 模型是结构性风险（DOM 节点数 ∝ 点数）；ECharts Canvas + `dataZoom` + `sampling` 正是为此设计。
3. **设计语言一致性**：ECharts 同出阿里，与 AntD 视觉体系天然协调，符合「阿里云优先」。

uPlot 在纯性能维度可能最强，但它是**低层绘图器**：图例、tooltip、联动、空状态壳全要自建。**留作后备**：若 ECharts 下钻实测不达标，uPlot 是明确的升级路径而非平级替换。

### 2.4 被推翻的条件

- 若实测 ECharts 在「30 天 × 35 序列」下钻场景交互卡顿（拖 dataZoom 掉帧）→ 转 uPlot，接受自建图例/联动成本。
- 若 T7 决定后端一律返回降采样序列（点数上限锁在数千级）→ 性能维度失效，Recharts 的「声明式 + 原生 React + 类型好」权重上升。**这与 R2 地图第 5 条「不做降采样长期留存」不矛盾**（那是存储侧；查询侧是否降采样属地图「Not yet specified」的开放项）。
- 若 `echarts-for-react` 被判定为不可接受的第三方依赖 → 自写 wrapper（约 30 行：`init`/`setOption`/`resize`/`dispose`），不影响选型结论。

### 2.5 未覆盖 / 无一手数据

- **无任何独立第三方 benchmark 被引用**。uPlot 的数字来自其作者 README；ECharts / Recharts 官方均未发布可比的时序渲染 benchmark。
  自测方法：造 35 序列 × 30 天 × 60s 采样（≈43,200 点/序列）的固定数据集，同一页面同一浏览器下测「首次渲染耗时」「dataZoom 拖动帧率」「内存占用」，用 `performance.mark` + DevTools Performance 取数。此自测应作为 R2 walking skeleton 的一部分（骨架本就是选型的验证手段）。
- 各库在本项目实际配置下的 gzip 体积未自测。

---

## 3. 数据获取与服务端状态

### 3.1 TanStack Query 与 OpenAPI 生成客户端的配合

R2 已定「契约优先，OpenAPI 为单一事实源，生成 Go 服务端接口 + TS 客户端与类型」。三条主流路线：

| 路线 | 形态 | 说明 |
|---|---|---|
| **`openapi-typescript` + `openapi-fetch` + `openapi-react-query`** | **只生成类型，不生成代码** | 官方文档原话：openapi-react-query 是 *"a type-safe tiny wrapper (1 kb) around @tanstack/react-query"*；`openapi-typescript` 由 schema 生成纯 `types.d.ts`（零运行时），`openapi-fetch` 按 schema 做路径/参数/响应的编译期校验。<https://openapi-ts.dev/openapi-react-query/> |
| **`@hey-api/openapi-ts`** | 生成 SDK 函数 + 可选 TanStack Query options | 生成物进仓库，需随 schema 重新生成 |
| **`orval`** | 直接生成 `useXxxQuery` hooks | 生成量最大，定制靠配置 |

**推荐：`openapi-typescript` + `openapi-fetch` + `openapi-react-query`。**

- **生成物是类型而非代码**，仓库里没有几千行机器生成的 hooks 要 review/diff —— 对「几十个 Claude Code 会话读同一个仓库」是决定性的：生成代码越少，模型能读懂的信噪比越高。
- 契约漂移仍被**编译器**拦下（R2 地图第 6 条要的正是这个），而非被生成器的运行时行为拦下。
- 运行时开销约 1 kb（官方自述），对 `go:embed` 产物无影响。

**被推翻条件**：若 T7 希望「每个接口一个现成 hook、模型完全不用写 queryKey」，则 orval / hey-api 更省事，代价是仓库里多出大量生成代码与一层生成器配置。

### 3.2 轮询 / 自动刷新的官方惯例

官方机制是 **`refetchInterval`**（TanStack Query 官方 Important Defaults：*"Queries can optionally be configured with a `refetchInterval` to trigger refetches periodically"*，<https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults>）。同页相关默认值：

- `staleTime: 0` —— 数据默认立即过期；stale query 在**组件挂载 / 窗口重新聚焦 / 网络重连**时自动后台重取。
- `gcTime: 5 分钟` —— 无观察者的查询缓存保留时长。
- 失败默认重试 3 次，指数退避。

对监控页面的直接含义：

1. 窗口聚焦即刷新（`refetchOnWindowFocus` 默认开）恰好匹配「运维切回浏览器就想看最新状态」，无需自建。
2. 轮询间隔应与后端采集周期同阶（R1 采样 10s–60s），比采集更快的轮询只是白烧 CPU。
3. **与 R1 硬约束有一处必须小心**：后台轮询失败或返回缺数时，TanStack Query 会保留上一次成功的数据（`isFetching` 为真但 `data` 仍在）。UI **不得**把这个陈旧值当作当前值展示 —— 必须用 `dataUpdatedAt` 判断新鲜度并落到 `03` §7.2 的「数据过期」状态。建议 T7 写进前端护栏。

---

## 4. 路由（issue #18 第 4 项）

| 维度 | React Router 7 | TanStack Router 1 |
|---|---|---|
| 最新版本 | `react-router-dom@7.18.2`（2026-07-28，MIT） | `@tanstack/react-router@1.170.18`（2026-07-13，MIT） |
| 类型安全 | 通过 typegen 提供路由类型；params/search 的端到端推断弱于 TanStack | 以「100% 类型安全路由」为核心卖点，params 与 **search params** 均有类型推断与校验 |
| 生态成熟度 | 事实标准，资料最多 → 模型先验知识最强 | 较新，模型先验知识较弱 |

**推荐：倾向 React Router 7** —— 页面树（`03` §3）结构简单、层级浅、几乎没有复杂 search params 状态机；模型的先验知识密度在这里是实打实的收益。

**被推翻条件**：若时间范围、筛选条件（`03` §4.1 多选筛选 + 正交标记筛选）决定全部落进 URL search params 并跨页继承（`03` §5.2 上下文继承原则），TanStack Router 的 search params 类型化与校验带来实质收益 → 反转。

**未覆盖（明确标注）**：本项**未做一手文档深挖**，时间预算用在了 UI 与图表两项上；上述对比基于版本元数据与两者公开定位。**T7 若认为路由是关键分歧点，应补一轮专项调研。**

---

## 5. 一页纸总结

| 项 | 推荐 | 最强的被推翻条件 |
|---|---|---|
| UI 组件体系 | **Ant Design 6** | 放弃「像阿里云」；或实测体积 / CSS-in-JS 运行时越红线 |
| 图表库 | **ECharts 6**（按需引入） | 下钻场景实测卡顿 → uPlot；或查询侧统一降采样 → Recharts |
| 数据获取 | **TanStack Query + openapi-typescript / -fetch / -react-query** | 想要开箱即用的 per-endpoint hooks → orval / hey-api |
| 路由 | React Router 7（低置信，调研深度不足） | search params 成为一等状态载体 → TanStack Router |

**跨项的两条护栏建议（比选型本身更重要）**：

1. 「缺数不是 0」的执行点在 **API 契约与前端数据层**，不在图表库 —— 缺数必须以 `null` 穿透到图表，禁止 `?? 0`。
2. 「空状态必须解释原因」的执行点在**图表外的空状态壳组件**，任何图表库都无法承担 —— 这是需要在骨架里就立好先例的自研组件。

---

## 6. 一手来源

- Ant Design releases：<https://github.com/ant-design/ant-design/releases>
- Mantine releases：<https://github.com/mantinedev/mantine/releases>
- shadcn/ui 文档：<https://ui.shadcn.com/docs> ／ 许可证：<https://github.com/shadcn-ui/ui/blob/main/LICENSE.md>
- ECharts 按需引入与 TS 类型（官方 handbook）：<https://github.com/apache/echarts-handbook/blob/master/contents/en/basics/import.md>
- ECharts `connectNulls` 默认值：<https://github.com/apache/echarts-doc/blob/master/en/option/series/line.md> ／ <https://github.com/apache/echarts/blob/master/src/chart/line/LineSeries.ts>
- ECharts `connect` API：<https://github.com/apache/echarts-doc/blob/master/en/api/echarts.md>
- Recharts `connectNulls` 默认值：<https://github.com/recharts/recharts/blob/main/src/cartesian/Line.tsx>
- uPlot README（体积、缺数、作者自测 benchmark）：<https://github.com/leeoniya/uPlot>
- openapi-react-query 文档：<https://openapi-ts.dev/openapi-react-query/>
- TanStack Query Important Defaults：<https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults>
- 版本与许可证：npm registry（`https://registry.npmjs.org/<pkg>`）2026-08-02 快照
