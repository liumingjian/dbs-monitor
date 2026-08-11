# 前端技术栈与 UI 体系 v1.0

> 目标：定死前端的 UI 组件体系、图表库、数据获取层、路由、状态归属规则、目录结构、状态标记的视觉词汇与测试策略。
> 适用范围：`web/` 下的全部前端代码，及其经 `go:embed` 进入 `monitor-server` 的静态产物。
> 决策票：[T7 · 前端技术栈与 UI 体系](https://github.com/liumingjian/dbs-monitor/issues/25)。
> 输入边界（不重议）：地图 Notes 第 1 条（TypeScript + React + Vite 纯 SPA，否决 Next.js）与第 2 条（整包自带、不依赖客户环境）、[T2 · 时序存储选型与指标数据模型](04-metric-storage-model.md)、[T6 · API 契约组织与代码生成流水线](07-api-contract-and-codegen.md)、[RT-E · 前端 UI 体系、图表库与数据获取层](https://github.com/liumingjian/dbs-monitor/issues/18)（findings 在分支 `research/rt-e`）。
> 上游规格：[`03-monitor-platform-ia-draft.md`](03-monitor-platform-ia-draft.md)（§1.1 阿里云优先、§1.4 空状态、§3 页面树、§4.1 三层信息契约、§4.3 标准监控、§5.2 上下文继承、§7 状态模型）、[`00-decision-index.md`](00-decision-index.md) §4（四条不变式）。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。

---

## 0. 一句话结论

**AntD 6 + ECharts 6（自写 wrapper）+ TanStack Query（`openapi-react-query`）+ TanStack Router；路由树即页面树，`domain/` 是封闭清单；三个状态桶、明令禁止第四个；三套状态词汇三个组件、禁止通用 Badge。**

贯穿全票的取向与 [T5](05-backend-code-structure.md) / [T6](07-api-contract-and-codegen.md) 一致，并且是同一个动作的第五次执行：**把规范做成结构，而不是做成约定。** T5 让「实例列表自己算健康」写不出来（层序偏序），T6 让「第六档状态」和「明文回显」写不出来（类型分家、双 schema）；本票让下列四件事写不出来——

- 画了张指标图却没处理空状态（D4：`unavailability` 是必填入参）；
- 各写各的 `queryKey`（D5：key 由 method + path + params 派生）；
- 把状态藏进一个全局 store，蚀掉可分享链接（D7：没有第四个桶）；
- 把「已暂停」当成一枚正交标记渲染（D9：三套词汇三个不相交的类型）。

---

## 1. D1 · UI 组件体系：Ant Design 6

**结论：Ant Design 6。「阿里云优先」这条 R1 原则管到视觉语言层。**

IA §1.1 的原则清单（功能范围、信息架构、页面划分、指标组织、控件与交互）字面上不含「视觉设计」，因此存在两种读法。本票取**读法 A**：控件与交互既然要复刻阿里云控制台，用该控制台的设计语言本身（AntD 出自阿里）就是成本最低的路径。

**理由**

1. **反馈闭环的确定性是 R2 整条路线的中心命题。** 视觉「像不像阿里云」是无法编译、无法测试的判据，唯一能把它降到近零的办法就是直接采用那套组件库。选 shadcn/ui 等于把整套视觉语言重新推导一遍，而推导结果只能靠人肉判定——这是本项目最贵的一类反馈（后续几十个 Claude Code 会话没有设计师可问）。
2. **R1 的页面形态集中在 AntD 最强的三块**：密集表格（IA §4.1 三层信息契约）、抽屉详情、表单密集的告警规则配置（`02`）。
3. **shadcn 的核心优势（模型可直接编辑组件源码）在本项目被削弱。** 会话面对的是「实现 R1 已冻结的规格」，需要的是强约束而非样式自由度。AntD 那 40+ 个 `@rc-component/*` 依赖**不可编辑**——这在别处是缺点，在这里是护栏。

**显式接受的代价**：产物体积与 CSS-in-JS 运行时开销**均无一手实测数字**（RT-E §1.4 明确标注）。`antd@6.5.3` 解包约 48.9 MB，`dayjs` 与 `@ant-design/icons` 是必进产物的运行时依赖。此风险由骨架实测兜底（见 D2），不在纸面上假装已解决。

---

## 2. D2 · AntD 的推翻条件：只留交互卡顿，删掉体积红线

**结论：AntD 6 的唯一推翻条件 = 实测交互卡顿。体积不作为推翻条件。**

判据具体化为两处（均落在骨架可触及的范围内）：50 行实例列表的首次渲染；标准监控多图页切换时间范围的响应。

**理由**

1. **体积在本交付形态下不构成约束。** 地图 Notes 第 2 条定死整包自带、离线交付。无论 [T8](https://github.com/liumingjian/dbs-monitor/issues/26) 选哪条打包路线，包里都躺着一整个 PostgreSQL——自带 PG 的体积比前端产物大一到两个数量级。在这个背景下为几 MB 的 JS 设红线是拿错了尺子；私有化内网首屏体积也不是用户可感知瓶颈。
2. **「等 T8 定红线」是一条永远不会被执行的条件。** T8 resolve 时不会有人回头翻 T7 的推翻条件，T11 实现会话更不会。留着它只让文档看起来严谨。
3. **真正该测的是交互，不是字节数。**

**回改路径**：若 T8 后续发现交付介质存在硬性体积上限，由 T8 回改本条，而非在此留悬空条件。

---

## 3. D3 · 图表库：ECharts 6

**结论：ECharts 6，`echarts/core` 按需 `use()` 引入。**

### 3.1 必须先记一笔：RT-E 的性能论据已失效

RT-E §2.4 写明的推翻条件之一是「若后端一律返回降采样序列（点数上限锁在数千级）→ 性能维度失效」。**该条件已经成立**，且不是 T7 造成的，是上游两票：

- [T2](04-metric-storage-model.md) §7.2：粒度由后端算，前端不能直接传任意粒度；`granularity=raw` 逃生舱硬上限 ≤6 小时。
- [T6](07-api-contract-and-codegen.md) D6：「粒度由后端收敛到**图宽量级（数百点 / series）**，`raw` 逃生舱 ≤6h 亦仅数千点」。

因此 RT-E §2.3 的第 2 条主论据（30 天原始数据下钻、Recharts 的 DOM 节点数 ∝ 点数）**已死**——前端永远拿不到 43,200 点的序列。**本票明确废弃该论据，不以它作为选型依据。**

### 3.2 仍然成立的两条论据

1. **光标联动是 IA §4.3 点名的核心模块**，不是可选优化（§4.3「核心模块」清单原文即含「光标联动」）。`echarts.connect(group)` 是官方一等能力；Recharts 需靠受控状态自行同步游标与时间范围——典型「看起来简单、边界条件很多」的活，且要被几十个会话反复触碰。
2. **性能压力换了个轴，仍然存在。** IA §4.3 的指标分组共 **22 个指标**（资源 5 + 数据库 12 + 复制 5），即一页二十来张图全部游标联动。点数虽被后端锁在数百，但 22 张 SVG 图 × 数百点 ≈ 上万 DOM 节点，外加 22 套联动订阅。这是「多图」轴而非「多点」轴的压力，Canvas 在此仍占优。
3. 附带：ECharts 同出阿里，与 AntD 视觉体系天然协调，符合 D1。

**后备路径**：若骨架实测 ECharts 在 22 图同页 + 联动下卡顿，uPlot 是明确的升级路径（代价是图例、tooltip、联动、空状态壳全部自建），而非平级替换。

### 3.3 回写 T6 的一处越界

[T6](07-api-contract-and-codegen.md) D6 在论证 `points` 用 `[ts, value]` pair 时写了理由「pair 是 **ECharts** 原生入参」——当时 T7 尚未选定图表库，该理由越界。

**处理**：`[ts, value]` pair 这个选择本身仍然成立（对 Recharts 亦够用，只需一次 `map`），**T6 结论不改**；此处显式记一笔，说明该句理由在当时不成立，避免后续把「T6 已经选了 ECharts」当作既成事实引用。

---

## 4. D4 · 图表的对外形态：领域组件，不是通用 wrapper

**结论：不装 `echarts-for-react`。自写约 30 行 wrapper（`init` / `setOption` / `resize` / `dispose` / `connect`），且该 wrapper 不对外暴露——对外只有领域组件。**

```ts
// domain/MetricChart.tsx —— unavailability 是必填入参
type MetricChartProps = {
  series: MetricSeries[];
  unavailability: Unavailability | null;  // 必填：null 才画图
  step: Step;                             // 后端回传的实际粒度
  ...
};
```

拿到 `unavailability` 码就渲染带原因的说明块（`UnavailabilityBlock`），拿到数据才画图。

**理由**

不是「少一个依赖」，而是**约定 vs 结构**。RT-E §2.1 已查明：任何图表库都表达不了「为什么没数据」，IA §1.4 的空状态必须由图表**外面**的壳承担。若装了 `echarts-for-react`，仓库里就存在一个**可以合法直接使用的裸图表组件**——会话会用它，不是因为偷懒，而是因为它就在那儿、名字还很正当。把 `unavailability` 做成必填入参后，**「画了张图却没处理空状态」是 TS 编译错误**，落回 T6「漂移必然被编译器拦下」的同一条逻辑。

**附带收益**：`echarts/core` 的按需 `use()` 引入点收敛到一个文件（否则 22 张图各自 import，tree-shaking 靠自觉）；少一个发布节奏慢于 ECharts 本体的第三方依赖。

**代价**：`resize` 观察、`dispose` 时机、`option` 深比较这些边界要自己写对一次——只写一次，且骨架就会碰到。

---

## 5. D5 · 数据获取层：`openapi-typescript` + `openapi-fetch` + `openapi-react-query`

**输入边界**：[T6](07-api-contract-and-codegen.md) 已定死 TS 侧**只生成类型**（否决 orval / hey-api），也定死了轮询、MVP 内禁 WS/SSE、必须用 `dataUpdatedAt` 判新鲜度。因此本项唯一的真问题是：**`queryKey` 的规范从哪来。**

**结论：`openapi-react-query`——`queryKey` 由 method + path + params 结构化派生，会话不手写 key。**

**理由**

监控平台里缓存失效会出真错：改告警规则要让规则列表、当前告警、实例健康三处一起失效；暂停采集要让实例列表与工作台一起失效。若几十个会话各自手写 `['alerts', instanceId, filters]`，形状必然分叉，失效必然漏——而**漏失效的表现是「页面显示旧数据」，正是 IA §7.2 要求必须被正确表达、绝不能静悄悄发生的那件事**。

自建 queryKey factory（路 B）解决同一问题，但那是一份靠 review 维持的**文档约定**；派生 key 是**结构**：`/api/v1/instances/{id}/alerts` 这个路径本身就是 key 的一部分，OpenAPI 一改它就跟着改。且它仍符合 T6「生成物是类型不是代码」的立场——仓库里不多出一行生成的 hooks，运行时开销约 1 kb。

**两条附带条款**

1. **失效关系必须显式登记。** 本项解决的是「key 不分叉」，**不解决「改了 A 要失效 B」**——那是领域知识。规则：所有 `invalidateQueries` 只能出现在**变更所在域的 mutation hook 里**，不许散落在组件中。能否落成 lint 或目录约定，交 [T9](https://github.com/liumingjian/dbs-monitor/issues/27)。
2. **一个未核实事实（本项唯一软肋）。** RT-E §0.1 的版本表列了 `openapi-typescript` 7.13.0 与 `openapi-fetch` 0.17.0，但**未列 `openapi-react-query` 的版本、许可证与维护状态**——它是三者中最小众的一个。核实工作交 [T11](https://github.com/liumingjian/dbs-monitor/issues/29)；**若已停更或许可证不符，降级为路 B（裸 TanStack Query + 自建 queryKey factory），本票其余结论不受影响。**

### 5.1 各页面轮询周期值表

（收口增补 2026-08-05，承 [T6](07-api-contract-and-codegen.md) §11 D11 移交「轮询周期按页面定、集中声明，具体值归 T7」，本票 v1.0 漏接。页面名对齐 IA §3 页面树。）

| 页面 / 模块 | 周期 | 依据 |
|---|---|---|
| 实例列表 | **30s** | 数据新鲜度以采集周期（10–60s）为底，更快是空转 |
| 实例总览 | **30s** | 摘要页，与实例列表同频 |
| 标准监控 | **30s** | 图表粒度由后端收敛，30s 内新增不足一个像素列 |
| 增强监控 | **5s** | 对齐增强监控 5s 采样（地图约束 5） |
| 当前告警（全局与实例级）、性能事件「触发中」、导航角标 | **15s** | 告警评估与采集同频错相位，15s 足以在一个评估周期内跟上 |
| 会话与阻塞（活跃会话 / 长事务 / 锁等待 / 阻塞链） | **10s** | 实时快照类端点（T6 D7.d），排障时人在盯着看 |
| 采集管理 | **30s** | 状态页，与实例列表同频 |
| 告警历史、性能事件「已恢复」、告警详情、告警设置四页 | **不轮询** | 历史与配置数据不会自己变，手动刷新 |

两条硬约定不变：**按页面定、集中声明于一处不散落**（T6 D11 原话）；**渲染必须用 `dataUpdatedAt` 判新鲜度**。骨架现状：`web/src/routes/` 内联的三处 `refetchInterval: 30_000` 与本表一致，R3 收敛为单点声明；声明落点若进 `domain/`，须按 §8.2 登记。

---

## 6. D6 · 路由：TanStack Router 1

**结论：TanStack Router 1。本条是全票置信度最低的决策。**

### 6.1 RT-E 的路由推荐同样已被上游触发推翻条件

RT-E §4 推荐 React Router 7，但写明推翻条件：「若时间范围、筛选条件决定全部落进 URL search params 并跨页继承（IA §5.2），TanStack Router 的 search params 类型化与校验带来实质收益 → 反转」；同时**主动声明该项调研深度不足**（「未做一手文档深挖…T7 若认为路由是关键分歧点，应补一轮专项调研」）。

该条件已被两处定死：

- **IA §5.2** 列了七项必须跨页继承的上下文：实例、**时间范围**、指标、告警实例、性能事件、会话快照时间、**过滤条件**；并写明「否则用户需要在每个页面重新定位问题，排障路径会中断」。
- **[T6](07-api-contract-and-codegen.md) D7**：时间**一律绝对 RFC3339**、**无 `?last=`**，理由是「可分享链接不能指向会变的窗口」。

即：**URL 是不是一等状态载体，已经不是待定问题**。

### 6.2 理由

TanStack Router 把本项目唯一真正复杂的路由问题——**带校验的 search params + 跨页继承**——做成类型与运行时校验（`validateSearch`）。React Router 7 的 `useSearchParams` 只给字符串：`from` / `to` / 筛选数组的解析、校验、序列化，以及**每个 `<Link>` 都得记得把它们带上**，全是手写约定；一个漏带 search params 的跳转正是 §5.2 所说的「排障路径中断」，而它不会报任何错。

同 D4、D5：**能做成结构就不做成约定。**

### 6.3 显式记录的反面（本条为何置信度最低）

1. **反对票的核心是先验知识。** React Router 是事实标准，几十个 Claude Code 会话对它的先验密度实打实高于 TanStack Router。本路线的中心命题就是「让后续会话少犯错」，选一个模型更陌生的库是在这条命题上**反向下注**。**补偿措施**：由 [T9](https://github.com/liumingjian/dbs-monitor/issues/27) 在 `CLAUDE.md` 中补足 TanStack Router 的用法先例（路由定义、`validateSearch`、跨页继承写法各一个）。
2. **RT-E 该项未做一手调研。** 基于浅调研翻掉推荐，比接受推荐更需要理由——理由即 §6.1 的两处硬约束。
3. **被推翻条件**：骨架实测中若 TanStack Router 的先验知识缺口造成实际阻塞（会话反复写错路由定义），回改为 React Router 7 + 自写 `useTimeRange()` + 强制继承 search 的 `<AppLink>`。

---

## 7. D7 · 前端状态边界：三个桶，没有第四个

**结论：三个桶，并明令禁止第四个。**

| 桶 | 装什么 | 判据 |
|---|---|---|
| **服务端状态**（TanStack Query） | 一切来自 API 的数据 | **唯一来源**：禁止 `useState` 拷贝一份、禁止 `useEffect` 同步进本地 state。需要派生就在渲染时算 |
| **URL search params**（TanStack Router，带 `validateSearch`） | 实例、时间范围（绝对 RFC3339）、`step`、选中指标、筛选条件、页签、分页 | **「刷新后必须还在」或「贴给同事必须看到同一屏」**。IA §5.2 的七项上下文全部落这里 |
| **组件本地**（`useState`） | 抽屉开合、表单未提交草稿、hover / focus、展开行 | **纯交互瞬态，丢了不影响排障路径** |

### 7.1 禁止第四个桶（Redux / Zustand / 全局 Context store）

**本项目根本没有全局客户端状态：**

- 登录用户与角色？[T6](07-api-contract-and-codegen.md) D10 定了**服务端会话 cookie**，「我是谁、我什么角色」是一次 API 查询 ⇒ 服务端状态桶。
- 主题、布局偏好？IA §4.3 明写 **MVP 不做用户自定义布局的持久化**；列数切换属「刷新后应该还在」⇒ URL 桶。
- 跨页共享的时间范围？IA §5.2 要求继承 ⇒ URL 桶，这正是 D6 的理由。

一个空的全局 store 是最危险的容器：**它一旦存在，会话就会往里塞本该在 Query 或 URL 里的东西，而塞进去的那一刻，「可分享链接」与「缓存失效」两条保证同时失效。** 因此这不是「MVP 先不上」，是写进 `CLAUDE.md` 的禁令：需要第四个桶时先改本决策，不许就地引入。

### 7.2 `step` 的处理

IA §4.3 把「数据粒度选择」列为核心模块（用户可选），而 T2 §7.2 写「前端不能直接传粒度」。二者由 [T6](07-api-contract-and-codegen.md) 的签名调和：`step=auto | 15s | 1m | 5m | … | raw`。

执行口径：**`step` 进 URL 桶；前端永远以响应回传的 `step` 渲染，不以自己请求的那个为准**（后端有权降级并回传实际采用的粒度）。

---

## 8. D8 · 目录结构：路由树即页面树，`domain/` 是封闭清单

**结论：目录结构是 IA §3 页面树的直译，不是按技术类型的横切。**

```text
web/src/
  routes/                    # TanStack Router 路由树 = IA §3 页面树，逐节点对应
    instances/               # 实例列表（平台首页）
    alerts/                  # 全局告警
    instances.$id/           # 实例工作台
      overview/
      monitoring/{standard,enhanced}/
      sessions/
      events/
      alerts/
      collection/
    settings/                # 告警设置（通知渠道 / 联系人 / 通知策略 / 维护窗口）
  domain/                    # 封闭清单，见 §8.2
  api/                       # openapi-typescript 生成的 types.d.ts + openapi-fetch client + $api
```

**页面私有的组件、hooks 与本地类型就放在该页面目录里，不上浮。**

**理由**：承 T5「按领域垂直切包，否决水平三层」的同轴执行，理由也一样——**locality**。会话接到「标准监控页要加一个复制指标区」，路径机械可推；而 `components/` + `hooks/` 的切法下，同一个改动散在三四个目录里。子问题原文要求「使后续会话能从页面名直接定位文件」，这是唯一满足它的切法。

### 8.1 一处刻意的轴不对称（不处理，仅记录）

后端按**领域**切（T5）、OpenAPI 也按域拆（T6），而前端按**页面**切。这不是错配：一个页面本来就要消费多个域（实例工作台首屏同时读 instance、alerting、collect）。生成的类型按域组织、页面按页面树组织，交汇点在 `api/`。

### 8.2 `domain/` 封闭清单

`domain/` 是本结构唯一的垃圾桶风险点，且比后端更难防：T5 能宣布「共享面只有生成物，人写不进去」，但下列组件恰恰是**人写的、必须共享的**。

**因此照抄 T5 对付新包的那一招：`domain/` 是一份封闭清单，新增一项必须先在 `CLAUDE.md` 登记，默认拒绝。**

| 组件 | 承载的 R1 语义 |
|---|---|
| `MetricChart` | D4 的领域组件，`unavailability` 必填 |
| `UnavailabilityBlock` | T6 的 13 个码 → 说明文案 + 去向链接（IA §7.2 已给去向） |
| `HealthStatus` | 四档归并 + 已暂停 override + 归因行（IA §4.1 三层信息契约、§7.1） |
| `SuppressionTags` | 正交标记：无数据 / 维护中 / 近期恢复(24h) / 已忽略 N / 配置缺失 N |
| `AlertStatus` | 告警五档（IA §7.3） |
| `Freshness` | `dataUpdatedAt` → 新鲜度判定（T6 定死必须用它） |
| `TimeRangePicker` | 写 URL 桶（D7） |

清单的判据是客观的：**「它是不是在呈现 R1 定死的语义？」**——不是「反正好几个页面都用到」。

> **收口增补 2026-08-05：骨架偏离登记。** T11 当前把时间范围的 parse / serialize 逻辑内联在 `web/src/routes/instances.$id/timeRange.ts`，没有独立的 `TimeRangePicker` 组件；待 R5 提取组件时再登记回本清单。
>
> **R5 回写 2026-08-11（issue #85）：** `TimeRangePicker` 已提取至 `web/src/domain/TimeRangePicker.tsx` 并登记进封闭清单，骨架内联偏离关闭。

**并且明令禁止 `src/components/`、`src/utils/`、`src/shared/`、`src/common/`**（承 T5 同一条禁令）。

---

## 9. D9 · 状态标记的视觉词汇

前端有**三套语义不同的状态词汇**同屏出现：

| 词汇 | 值域 | 语义 |
|---|---|---|
| **实例健康主状态** | 严重 / 警告 / 未知 / 正常 + 已暂停(override) | 「这台多该看」，决定排序与默认筛选 |
| **告警状态** | `OK` / `PENDING` / `FIRING` / `NO_DATA` / `RECOVERED` | 单条告警的生命周期 |
| **正交标记** | 无数据 / 维护中 / 近期恢复 / 已忽略 N / 配置缺失 N / 已暂停 | 「为什么」，不改变主状态 |

**结论：三套词汇 = 三个独立组件（`HealthStatus` / `AlertStatus` / `SuppressionTags`），明令禁止合并成通用 `<StatusBadge type="…" />`。**

**理由**：通用 Badge 会让 R1 明令禁止的东西变得可写。`<StatusBadge value="已暂停" />` 无法阻止会话把「已暂停」渲染成一枚正交标记——而 §7.1 说它是 **override**，压过一切归并结果；同样无法阻止把 `NO_DATA`（告警状态的一档）与 `无数据`（正交标记，含义是「该实例存在处于 `NO_DATA` 的未恢复告警」）用同一视觉呈现，而二者在 R1 里是不同层的东西。三个组件、三个**不相交的 TS 联合类型**，混用即编译错误。

**三条约定**

1. **颜色永不单独承载信息。** 每个色块必须同时带文字标签（IA §4.1「色块 + 文字，色盲可辨」的直译）。推论：**禁止只靠 `<Badge status="error" />` 这类纯色点**；也禁止用颜色区分正交标记之间的差别。
2. **色板只在一处定义**，由 AntD token 派生（`colorError` / `colorWarning` / `colorSuccess` / `colorTextDisabled`），页面不许自选颜色值。严重级别 → 颜色的映射是**一张表**，因为它同时服务健康主状态、告警级别、C/W/I 计数三处；三处必须同色，才能兑现 §7.1 的「同词呈现使单一来源模型在 UI 上自证」。
3. **绿色永不等于「什么都没有」**（§7.1 防滥用护栏原话）。落成组件契约：`SuppressionTags` 在主状态为「正常」时**仍必须渲染** `已忽略 N` 与 `配置缺失 N`，不做「为 0 就整块隐藏」的顺手优化。

**两条按 IA 直接执行的细则**：`已暂停` 标记带时长且 **7 天转警示色**（§4.1）；归因行同级并列时取**首次触发时间最早者**，不堆叠多行（§4.1）。

---

## 10. D10 · 前端测试策略

**结论：只测「纯映射逻辑」，不测渲染细节，E2E 不在本票定。**

### 10.1 必测（表驱动，四项）

1. **13 个 `Unavailability` 码 → 空状态文案 + 去向链接，全覆盖。** 最重要的一条：漏一个码的表现是**页面显示空图表**，正是 IA §1.4「空状态必须解释原因」这条立身原则的反面。T6 已把码表封闭在 OpenAPI 里，测试可直接对着生成的类型穷尽——**新增一个码而没写文案，测试就红**。
2. **「缺数不是 0」的数据层断言。** 喂入 `null`（采到但不可计算）与缺桶（没采到）两种输入，断言到达图表的 series 仍是 `null` / 断点。RT-E §2.1 已查明该风险**完全在数据层、不在图表库**；T2 §7.3「纪律一」点名真风险是有人加 `COALESCE(…, 0)`，前端这一侧的对应物就是 `?? 0`。
3. **新鲜度判定函数**：`dataUpdatedAt` + 采集周期 → 是否「数据过期」。RT-E §3.2 指出 TanStack Query 轮询失败时**会保留上次成功的数据**（`isFetching` 为真但 `data` 仍在），把陈旧值当当前值展示是这里唯一的真事故。
4. **URL search params 的 parse / serialize 往返 + 非法值处理。** D6 选 TanStack Router 就是为了这个；须覆盖「同事贴来一个手改坏的链接」不得白屏，而是落到一个可解释的状态。

### 10.2 明确不测

AntD 组件自身行为；视觉快照（样式微调下只产生噪声 diff，且没人真的 review）；hover / 抽屉开合这类瞬态交互。

### 10.3 明确不在本票定

**E2E / 浏览器级验收归 [T10](https://github.com/liumingjian/dbs-monitor/issues/28)**（「多少秒后前端出图」本就是它的第 4 项）。T7 若在此定一套 E2E 框架，是替 T10 做决定。

---

## 11. 交给下游的四笔

| 去向 | 内容 |
|---|---|
| [T8 · 打包、部署与运行形态](https://github.com/liumingjian/dbs-monitor/issues/26) | 若交付介质存在硬性体积上限，由 T8 回改 D2；本票不留悬空条件 |
| [T9 · AI 开发护栏与验证闭环](https://github.com/liumingjian/dbs-monitor/issues/27) | ① `?? 0` 能否落成 lint；② D7 禁令（第四个桶）与 D8 目录禁令能否机械检查；③ `invalidateQueries` 只许出现在对应域 mutation hook 的约束；④ 在 `CLAUDE.md` 补 TanStack Router 用法先例（D6 的补偿措施） |
| [T10 · Walking skeleton 切片定义](https://github.com/liumingjian/dbs-monitor/issues/28) | E2E / 浏览器级验收标准 |
| [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29) | ① 核实 `openapi-react-query` 版本 / 许可证 / 维护状态，停更则降级路 B；② D2 的交互卡顿实测；③ D3 的 22 图同页 + 联动实测 |

---

## 12. 未决事实（显式记录，不假装已解决）

| 事实 | 状态 |
|---|---|
| AntD 6 / ECharts 6 的实际 gzip 产物体积与 `go:embed` 后二进制增量 | 无一手数据（RT-E §1.4 / §2.5 已标注）。**按 D2，不作为决策依据** |
| AntD 6 CSS-in-JS 运行时在 50 行列表 + 22 图页的实际开销 | 无一手数据。D2 的唯一推翻条件，骨架实测 |
| ECharts 在 22 图同页 + `connect` 联动下的渲染与交互表现 | 无独立第三方 benchmark（RT-E §2.5）。骨架实测 |
| `openapi-react-query` 的版本、许可证与维护状态 | **已核实**（2026-08-05 收口，npm registry 一手数据）：仓库钉版的 **0.5.4 即 npm `latest`**（2026-02-11 发布），**MIT** 许可证，由官方 `openapi-ts/openapi-typescript` monorepo 维护，与 `openapi-typescript` / `openapi-fetch` 同源。**不触发降级路 B**。注：registry 存在 2025-10-14 发布的 1.0.0，但 `latest` 标签仍指 0.5.4，升级与否留给 R5 常规依赖管理 |
| TanStack Router 的一手文档深度 | RT-E §4 自述调研不足。D6 的置信度来源于 IA §5.2 + T6 的硬约束，而非该库的调研深度 |

---

## 13. 否决记录汇总

| 被否决 | 出处 | 为什么 |
|---|---|---|
| shadcn/ui + Tailwind | D1 | 视觉语言需重新推导，判据不可编译不可测；其「模型可改源码」优势在「实现已冻结规格」的场景下被削弱 |
| Mantine 9 | D1 | 依赖树浅是真优势，但自有设计语言与「阿里云优先」不同源，同样要人肉判定相似度 |
| 体积红线作为推翻条件 | D2 | 整包已含自带 PG，量级不成比例；且「等 T8 定红线」是永远不会被执行的悬空条件 |
| Recharts 3 | D3 | 光标联动需自建（IA §4.3 核心模块）；22 图同页的 SVG 节点数压力 |
| uPlot | D3 | 低层绘图器，图例 / tooltip / 联动 / 空状态壳全要自建。**留作后备**：ECharts 实测不达标时的升级路径 |
| 「30 天原始点下钻」作为 ECharts 的论据 | D3 | 已被 T2 §7.2 与 T6 D6 的后端分桶杀掉，本票显式废弃 |
| `echarts-for-react` | D4 | 会在仓库里留下一个可以合法直接使用的裸图表组件，使「画了图却没处理空状态」重新变得可写 |
| 裸 TanStack Query + 自建 queryKey factory | D5 | 是文档约定而非结构；key 形状会随会话分叉，漏失效表现为「页面显示旧数据」。**留作 D5 的降级路径** |
| React Router 7 | D6 | search params 已由 IA §5.2 + T6 立为一等状态载体，而它只给字符串；漏带参数的跳转不报任何错 |
| 新开 RT-F 路由专项调研 | D6 | 分歧点已被上游硬约束消解，调研只能确认已知；代价是 T7 不完整结题而 T9/T10 均 blocked by T7 |
| Redux / Zustand / 全局 Context store | D7 | 本项目没有全局客户端状态；空 store 一存在，「可分享链接」与「缓存失效」两条保证即被蚕食 |
| 按技术类型切目录（`components/` `hooks/` `pages/`） | D8 | 毁 locality；同一改动散在三四个目录，「从页面名定位文件」不成立 |
| `src/utils/` `src/shared/` `src/common/` | D8 | 承 T5 同一条禁令 |
| 通用 `<StatusBadge type="…" />` | D9 | 使「已暂停当作正交标记」「`NO_DATA` 与 `无数据` 混同」重新变得可写 |
| 纯色点（无文字标签）表达状态 | D9 | 违反 IA §4.1「色块 + 文字，色盲可辨」 |
| 「计数为 0 就隐藏正交标记」 | D9 | 违反 §7.1「绿色永不等于什么都没有」 |
| 视觉快照测试 | D10 | 样式微调下只产生噪声 diff，且没人真的 review |
| 在 T7 定 E2E 框架 | D10 | 越界替 T10 决定骨架验收标准 |
