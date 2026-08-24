---
status: partially-superseded
kind: execution-record
note: 条目内容在效；硬底计数已被后续组累加
---
# 21 · v1 验收矩阵条目 A 组 · 片①⑦⑨

> 出处：[v1 验收矩阵条目 A 组 · 片①⑦⑨（采集 / 暂停 / 平台可观测性）#118](https://github.com/liumingjian/dbs-monitor/issues/118)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：**A 组条目的定稿与三处对骨架的增补**。骨架与判定规则见 [20](20-v1-acceptance-matrix.md)（决策票 [#111](https://github.com/liumingjian/dbs-monitor/issues/111)），**本文不原地改写 20 号任何一条**；本文只在 20 号留下的填空位上定稿，并把三处必要增补显式记在这里。
> 输入边界（不重议）：[`docs/spec/mvp-master-spec.md`](../spec/mvp-master-spec.md) 片①（#41）、片⑦（#47）、片⑨（#49）全文与其 S1 拍板验收判据；[20](20-v1-acceptance-matrix.md) 全部 D1–D10；[T14](../design/14-platform-observability-and-diagnostics.md)、[T12](../design/12-collection-concurrency-timeouts-and-backpressure.md)、[`06`](../design/06-metric-dictionary-and-collection-plan.md)、[`03`](../design/03-monitor-platform-ia-draft.md) §4.8、[`00`](../design/00-decision-index.md) ADR-08/ADR-10。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。
>
> **编号约定**：兄弟票各开一份平行记录 —— [#119](https://github.com/liumingjian/dbs-monitor/issues/119)（片②③④）取 `22-`、[#120](https://github.com/liumingjian/dbs-monitor/issues/120)（片⑤⑥）取 `23-`、[#121](https://github.com/liumingjian/dbs-monitor/issues/121)（片⑧+横切）取 `24-`。四组并行会话不得往同一份文档里写。

---

## 0. 一句话结论

**A 组出 23 条：片① 8 条、片⑦ 7 条、片⑨ 8 条；其中 `n-a` 1 条、加深基线 8 条、**允许 `pending` 的普通加深 0 条**（23 条的 `status` 目前统一为实现未落地的占位，见 §11）。S1 亲口拍板的验收判据一律落成 `baseline: true` 的加深条目而非可 `pending` 的加深，全局硬底因此由 52 升到 60；四条上浏览器，其余 API 层，一条含 DB 只读前后对比。**

---

## 1. D1 · S1 原文验收判据落成「加深基线」，不落成普通加深

片①的 S1 判据原文是「字典全部指标经真实 Task 产出样本或显式降级」，片⑦是「待办三态不报假绿、永不隐藏、正向空状态带检查时间」，片⑨是「磁盘紧急拒写可证且不删旧数据」。这三条**既不是「一条成功路径」，也不属 `F1..F4` 任何一类**——20 号的五条基线格式装不下它们。

**决定**：设成加深条目（`AC-01-S2`、`AC-07-S2`、`AC-09-F5`），但字段打 `baseline: true`。

否决的两条：

- **塞进 `S1` 的 `asserts`**：全字典对账是独立的一次执行（遍历字典逐项判定），塞进 `S1` 会让 `S1` 变成两次执行，违反 20 号 D6.2「一条条目 = 一次执行 + 一组断言」。而它本身完全符合条目定义，没理由寄生。
- **设成普通加深（允许 `pending`）**：S1 判据可以合法地不达标，等于没判据。

**后果（对 20 号 D6.5 的增补）**：硬底算式由「45 基线 + 7 横切 = 52」变为「45 + 7 + 逐组定稿的加深基线」。A 组定稿 8 条，硬底 = **60**。B/C/D 组若再定加深基线，同样只增不减，各自在 `22-`/`23-`/`24-` 里交代。**这是本文对 20 号唯一一处算式性增补，20 号原文不动。**

---

## 2. D2 · 能力四态要三条路径，`F3` 一条装不下

`F3` 的字面是「目标库不可达 / 能力不足」，但这在片①是**两种真实故障手段、两条执行路径**：停目标库容器 vs 回收 `pg_monitor` 角色。加上结构性不适用与 `UNKNOWN`，能力四态需三条：

| 条目 | 态 | 真实手段 | 核心判据 |
|---|---|---|---|
| `AC-01-F3` | `MISSING`(fixable) | 真实 SQL 回收 `pg_monitor` | `PERMISSION_DENIED` + FixHint + 「影响 N 项指标」沿 `Task.Requires` 反查；不得表现为「有数据但全零」 |
| `AC-01-F5` | `UNKNOWN` | 真停目标库容器 | `DB_UNREACHABLE`；能力快照**整份** `UNKNOWN`，绝不反推 `MISSING`、绝不呈现空清单 |
| `AC-01-F6` | `NOT_APPLICABLE` | 主库实例 / 无 slot 实例 | `NOT_APPLICABLE_ROLE` + NAReason；「不适用」与「真实积压 0 字节」严格可区分 |

`F5`/`F6` 打 `baseline: true`：「查不到不得冒充具备」「不适用不得冒充 0」是 spec 里最贵的两条语义，落成可 `pending` 的加深等于没落。

---

## 3. D3 · 浏览器四条，IA §6.5 由 `AC-01-F2` 一条承载整条路径

按 20 号 D3 判据（「断言点是用户看到什么」才上浏览器），A 组四条上浏览器：

| 条目 | 为什么必须是浏览器 |
|---|---|
| `AC-01-F2` | 13 码降级文案 + 缺数不是 0 + `dataUpdatedAt` + search params 往返（B6 四项） |
| `AC-07-S2` | 待办三态是纯呈现语义：`UNKNOWN` 置顶、正向空状态带检查时间、模块永不隐藏 |
| `AC-07-F2` | 「已暂停」不得渲染成「采集失败」或「数据库不可达」 |
| `AC-09-F4` | 平台自身故障页 —— 不白屏、不 502、不一片「暂无数据」是 `09` D6 的核心 UI 语义 |

**IA §6.5「数据缺失或能力不可用排查」是 A 组唯一命中 §6 五条关键路径的一条**，且横跨上表前三条。**决定**：由 `AC-01-F2` 从空图表一路走完整条路径（空图表 → 看不可用原因 → 跳采集管理 → 看受影响指标），`AC-07-S2`/`F2` 只断言采集管理页**内部**语义。否决「另设一条 §6.5 专用条目」：那会造出第四次浏览器执行跑同一批页面，而 §6.5 是**路径**不是断言点，天然该挂在起点条目上。

**D9 的那条 UI 断言**（可见性不收窄、写能力收窄）并入 `AC-07-S2` 的同一次浏览器执行（采集配置模块对非平台管理员置灰并说明所需角色，入口不隐藏），不另起执行、不另设条目。

---

## 4. D4 · 暂停冻结语义归片⑦，不放进片②

冻结不转 `RECOVERED`、解冻不回放（条件仍满足延续同一实例不新建）、暂停期历史保留——语义跨到告警域，但**归属采集域**，落成 `AC-07-S3`（`baseline: true`）。

否决「归片②的条目」：片②只有五条基线，被片⑦的语义挤占后自己的规则与评估语义就没位置了；且 [#119](https://github.com/liumingjian/dbs-monitor/issues/119) 那边看不到这条的来龙去脉。暂停期历史保留并入 `AC-07-S3` 的断言点（解冻后查暂停前区间仍有数据、空窗缺桶不补 0），不单设条目——它是保留模型的自然后果，没有独立的执行路径。

---

## 5. D5 · journal 载体不在 `make acceptance` 内验证（对 20 号 D3 的一处让步，显式记账）

[T14](../design/14-platform-observability-and-diagnostics.md) 把「结构化 systemd journal」定为平台自身故障的三出口之一，但 `make acceptance` 的 server 是 compose 里的**裸进程**，没有 systemd，`journalctl` 不可用。

**决定**：server 的结构化事件写 stdout，矩阵断言**结构化事件本身**（字段形状 + 秘密扫描）；**journal 这个载体的真实性明确移交片⑩整机演练**。

否决：

- **acceptance 环境引入 systemd 容器**：为一条断言把整个验收环境复杂度抬一个量级，且 systemd-in-docker 的失真不比 stdout 少。
- **不断言 journal，只断言诊断 API**：会漏掉秘密禁区扫描，那是 `14` D5 的硬约束，必须进自动化。

**这处让步必须写在矩阵里而不是藏起来**：`AC-09-S1` 的 `reason` 指向本节。说白了就是——**journal 事件的内容已验证，journal 这个载体没验证**。

---

## 6. D6 · 片⑨的 `F2` 记 `n-a`，并由 `F6` 补回让出的保护

片⑨不产出封闭 13 码中任何一码：诊断 API 不是指标序列端点，事实源取不到状态时投影的是平台四态的 `UNKNOWN`，不是 `Unavailability`。这正是 20 号 D6.5 里 `n-a` 被设计出来处理的情形（「该语义在本片不存在」），不是「暂时测不了」。

**决定**：`AC-09-F2` 记 `n-a`；同时设 `AC-09-F6`（`baseline: true`）断言「任一事实源查不到 → 该源 `UNKNOWN` 且总态按归并序绝不为 `OK`」。`n-a` 让出多少保护，`F6` 补回多少。

否决「硬凑一条 13 码条目」：把 `n-a` 的正当用法浪费掉，再造一条假条目。

另：`14` D6 的四类故障注入必须各有归宿——平台 PG 不可达（`F4`）、磁盘越级（`F5`）、采集池饱和（`F3`）、证书过期（`F7`）。`F7` 是本票补的第八条，否则第四类注入无处安放。

---

## 7. D7 · 时间参数化取值表

20 号 D8 定了「参数化而非伪造时间」，本文定数：

| 参数 | 产品默认 | 验收值 | 说明 |
|---|---|---|---|
| 任务采样周期 | 5s / 30–60s / 5m | **5s 全线** | 5s 是产品硬下限，不再降 |
| 能力探测循环 / 快照有效期 | 5m / 5m | **10s / 20s** | 证明四态刷新与过期投影 `UNKNOWN` |
| 分区跨度 / 预建余量 / 保留期 | 1 天 / 7 天 / 30 天 | **1min / 3 个 / 5 个** | 证明滚动与 `DROP TABLE` 丢弃 |
| `STALE` 判定阈值 | 随周期 | **15s** | |
| Agent 补报内存窗口 | 5m | **30s** | 证明超窗丢弃 |
| 退避封顶 | 60s | **5s** | 证明封顶，不证明时长 |
| 平台健康 60 秒汇总节拍 | 60s | **5s** | 快照重算同节拍 |
| 证书过期预警窗 | 30 天 | **不参数化** | 改签一张 20 秒后过期的测试证书 |

**单轮 `make acceptance` 目标 ≤ 10 分钟**，分区滚动是长杆（约 5 分钟）。

两处需要点名：

1. **分区跨度参数化到 1 分钟是本文外溢到实现的唯一硬要求**：分区维护循环必须能按分钟建分区，「按天 UTC 分区」的命名逻辑要跟着参数走。这是真实代码路径的参数化，不是伪造时间。
2. **证书选「真签短命证书」而非改预警窗**：改窗口只证明 `DEGRADED` 的显隐，不证明过期后进 `FAILED`。

---

## 8. D8 · `test_ref` 命名规范（对 20 号 D6.6 的落法细化）

20 号 D6.6 要求 `covered` 条目的 `test_ref` 在测试代码中可检索，但没定形式；Go 测试名、Playwright 标题、用例注解三种载体语法各异，B12 若解析三种语言会很脆。

**决定**：**`test_ref` 的值必须内含条目 ID 的字面量。**

- Go：`TestAcceptance_AC_01_S1`（下划线形）
- Playwright：`[AC-07-F2] 暂停期图表显式已暂停`（方括号形）

B12 因此退化成「拿条目 ID 的两种字面形去 `test/`、`web/e2e/` 里 grep，命中即通过」——一条规则、零语言解析。代价是测试命名被矩阵绑定，改 ID 要同步改测试名；但 ID 本就「永不复用、永不重编」，绑定安全。

---

## 9. D9 · A 组的 `exceptions:` 白名单保持为空

逐条核过真实手段：平台库不可达 = 真停容器；采集池饱和 = 真并发压；证书过期 = 真签 20 秒短命证书；能力不足 = 真回收 `pg_monitor`；目标库不可达 = 真停容器。

唯一有疑问的是磁盘紧急拒写——真把盘写到 95% 不现实。**定性：调低磁盘水位阈值算真实手段，不进白名单。** 依据是 20 号 D8.2 原文已把它列为真实故障手段：阈值本来就是部署期配置项，把紧急线调到当前使用率之下走的是**与生产完全相同的判定代码路径**，无 mock、无直改状态表、无伪造事实。

**A 组 `exceptions: []`。**

---

## 10. D10 · `operations` 覆盖维预先写下尚不存在的 `operationId`

事实：仓库现存 `operationId` 只有 8 个（`createSession`、`listInstances`、`createInstance`、`getInstance`、`updateInstance`、`deleteInstance`、`getMetricSeries`、`reportAgentMetrics`）。A 组 23 条里只有片①两条能引用现存的 `getMetricSeries`，其余引用的端点在 spec 里承诺、在 OpenAPI 里**一个都还不存在**。

**决定**：按 spec 承诺**预先写下规划中的 `operationId`**：

`listInstanceCapabilities`、`listInstanceCollectionTasks`、`updateCollectionTaskInterval`、`pauseInstanceCollection`、`resumeInstanceCollection`、`getPlatformHealth`、`getDiskWatermark`、`getSchedulerSummary`、`getPartitionMaintenance`、`getCertificateStatus`、`getKeyringStatus`、`getPlatformVersion`。

矩阵因此对 OpenAPI 构成**正向约束**：覆盖缺口反查会直接报「这些 `operationId` 尚不存在」——正是要的信号。

否决「留空待实现后回填」：覆盖维在整个实现期都是空的、缺口报告全绿，恰好把「还没做」伪装成「没缺口」，与 D4 反假覆盖同一类错误。

漂移的处理照 20 号 D10 的单向对齐：**以 OpenAPI 为准回填矩阵**，名字不符时改矩阵不改契约。**本票不动 `api/*.yaml`**（那会触发 `make gen` 义务，超出本票范围）。

---

## 11. 条目清单（详情见 `test/acceptance/matrix.yaml`）

| ID | kind | layer | baseline | status | 一句话 |
|---|---|---|---|---|---|
| `AC-01-S1` | S1 | api | ✓ | pending | 建实例 → 真实 Task → 序列出点，水位推进 |
| `AC-01-S2` | S2 | api | ✓ | pending | 全字典对账：有样本 or 有码，无第三种结局 |
| `AC-01-F1` | F1 | api | ✓ | pending | 三档角色 × 任务周期写端点 |
| `AC-01-F2` | F2 | browser | ✓ | pending | IA §6.5 全路径 + B6 四项 |
| `AC-01-F3` | F3 | api | ✓ | pending | 回收 `pg_monitor` → MISSING + 影响 N 项 |
| `AC-01-F4` | F4 | api | ✓ | pending | 平台库不可达期间目标侧零污染 |
| `AC-01-F5` | F5 | api | ✓ | pending | 目标库不可达 → 能力整份 UNKNOWN |
| `AC-01-F6` | F6 | api | ✓ | pending | 主库复制延迟 / 无 slot → NOT_APPLICABLE |
| `AC-07-S1` | S1 | api | ✓ | pending | 暂停 → 停调度 → 水位不推进 → `COLLECTION_PAUSED` → 恢复不补跑 |
| `AC-07-S2` | S2 | browser | ✓ | pending | 待办三态四情形 + 模块永不隐藏 + D9 的 UI 断言 |
| `AC-07-S3` | S3 | api | ✓ | pending | 冻结不转 RECOVERED、解冻不回放、延续同一实例 |
| `AC-07-F1` | F1 | api | ✓ | pending | 暂停/恢复端点仅 PLATFORM_ADMIN |
| `AC-07-F2` | F2 | browser | ✓ | pending | 图表显式「已暂停」，不冒充采集失败 |
| `AC-07-F3` | F3 | api | ✓ | pending | 暂停叠加目标库不可达：已暂停优先 |
| `AC-07-F4` | F4 | api | ✓ | pending | 平台故障后暂停状态不丢不误恢复 |
| `AC-09-S1` | S1 | api | ✓ | pending | 七事实源齐全的 OK 快照 + 60s 汇总 + 秘密扫描 |
| `AC-09-F1` | F1 | api | ✓ | pending | 诊断 API 仅 PLATFORM_ADMIN，不伪装空数据 |
| `AC-09-F2` | F2 | api | ✓ | **n-a** | 本片不产出 13 码（保护由 F6 补回） |
| `AC-09-F3` | F3 | api | ✓ | pending | 池饱和 → DEGRADED + 细因计数 |
| `AC-09-F4` | F4 | browser | ✓ | pending | 平台库不可达 → FAILED + 故障页 + 目标侧零污染 |
| `AC-09-F5` | F5 | db | ✓ | pending | 紧急拒写可证 + 分区与保留期前后一致 |
| `AC-09-F6` | F6 | api | ✓ | pending | 事实源查不到 → UNKNOWN，总态绝不 OK |
| `AC-09-F7` | F7 | api | ✓ | pending | 短命证书 → DEGRADED → FAILED |

全部 `status: pending` 是**实现未落地的占位**：条目内容已定稿，`pending` 由片①⑦⑨ 的实现票逐条转 `covered` 并填 `test_ref`。按 D1，v1 判定时这些 `baseline: true` 条目仍为 `pending` 即未达标。

---

## 12. 已接受的代价

1. **硬底从 52 涨到 60**（+15%），验收面变宽。换 S1 原文判据不可 `pending`，值。
2. **journal 载体未验证**（D5）。矩阵显式记账而不是假装覆盖，剩余风险由片⑩整机演练承接。
3. **`operations` 引用了尚不存在的 `operationId`**（D10）。实现期缺口报告会一直红——这是特性不是缺陷，但要求读报告的人理解「红 = 还没做」而非「坏了」。
4. **分区跨度参数化外溢到实现**（D7）。分区维护循环必须支持分钟级跨度，这是本票对片⑨实现的一条硬要求。
5. **`AC-01-F2` 比其他浏览器条目重**（承载整条 §6.5）。它一旦不稳，影响面比单点断言大。

---

## 13. 未决，交下游

- 片②③④、⑤⑥、⑧+横切的条目：[#119](https://github.com/liumingjian/dbs-monitor/issues/119) / [#120](https://github.com/liumingjian/dbs-monitor/issues/120) / [#121](https://github.com/liumingjian/dbs-monitor/issues/121)，各取 `22-`/`23-`/`24-`。
- 加深基线在**其余三组**是否也出现、硬底最终值：随各组定稿累加。
- `pending` 阻断阈值与 Go/No-Go 门禁：[#114](https://github.com/liumingjian/dbs-monitor/issues/114)。
- 真实 Linux 上的最终验收：[#115](https://github.com/liumingjian/dbs-monitor/issues/115)。
- 本文 D7 的参数化取值需在实现期以真实跑批校准（尤其分区滚动的 5 分钟长杆），偏差回写本文的 supersede 记录。
