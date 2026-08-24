---
status: partially-superseded
kind: execution-record
note: 矩阵骨架与判定规则在效；条目计数与产物名已被下游改写
---
# 20 · v1 验收矩阵的骨架与判定规则

> 出处：[v1 验收矩阵的骨架与判定规则 #111](https://github.com/liumingjian/dbs-monitor/issues/111)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：**骨架与判定规则**，不是验收内容本身。逐片的具体条目由下游四张票 [#118](https://github.com/liumingjian/dbs-monitor/issues/118) / [#119](https://github.com/liumingjian/dbs-monitor/issues/119) / [#120](https://github.com/liumingjian/dbs-monitor/issues/120) / [#121](https://github.com/liumingjian/dbs-monitor/issues/121) 填入。
> 输入边界（不重议）：[`docs/spec/mvp-master-spec.md`](../spec/mvp-master-spec.md) 十片总表与五条跨片原则、[T9](../design/10-ai-guardrails-and-verification.md) §3 的 A/B 两栏护栏登记表、[T6](../design/07-api-contract-and-codegen.md) 的封闭 13 码与 `x-required-role` 全覆盖、[T14](../design/14-platform-observability-and-diagnostics.md)「平台自身故障不进入目标告警或 `NO_DATA`」、[00 §4](../design/00-decision-index.md) 四条不变式、[18](../design/18-v1-delivery-boundary-bs-binary.md) 的 B/S 二进制交付边界、地图 #105 Notes 第 3 条（平台库是客户自备的外部前置）。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**矩阵沿 spec 十片切，每片一条成功路径 + 四类固定失败路径，横切组独立计分；API 层是默认断言层，浏览器只证「用户看到什么」；测试数据只许经业务 API 或真实采集管线产生——绕过业务 API 直插业务表即为假覆盖，一律禁止。**

---

## 1. D1 · 域切分：主轴是 spec 十片，页面树与 OpenAPI 是覆盖维

**切分主轴 = `docs/spec/mvp-master-spec.md` 的十片。**

理由：穷尽性与互斥性由 spec 目录本身背书（十片总表 + 依赖图已经是一次完整切分），且与已发布的 46 张实现票 #52–#97 天然对齐——换任何一根轴都要重新证明「覆盖全量」，且与实现票错位。

**页面树与 OpenAPI 不作切分轴，作两条覆盖维。** 每条矩阵条目登记它触及的 IA 页面节点（[`03` §3`](../design/03-monitor-platform-ia-draft.md) 首版页面树的路径）与 `operationId`。用途是**反查**：哪个页面节点、哪个 `operationId` 一条条目都没碰。这两维不参与「基线是否达标」的判定，只产出缺口清单。

**片⑩（发布收口）不出矩阵条目。** 它不是业务域，其内容已分给 [#110](https://github.com/liumingjian/dbs-monitor/issues/110)（交付物与候选留痕）与 [#114](https://github.com/liumingjian/dbs-monitor/issues/114)（质量门）。矩阵中只留一行指针。

因此矩阵的业务片是 **①–⑨ 共九片**。

---

## 2. D2 · 每片的通用路径规则：一条成功 + 四类失败

每片**必须**具备下列五条基线条目：

| 类 | 内容 | 判定要点 |
|---|---|---|
| `S1` | 端到端成功路径 | 配置 → 真实采集 / 真实评估 → 页面或 API 可见。中间不得跳步。 |
| `F1` | 权限拒绝 | 三档角色 × [`03` §8.2](../design/03-monitor-platform-ia-draft.md) 写权限矩阵；被拒绝方必须拿到规定的拒绝语义，而非 500 或静默成功。 |
| `F2` | 空状态 | 必须落在 `Unavailability` **封闭 13 码**之一，且**不得渲染成 0**（[T2](../design/04-metric-storage-model.md)「不补 0」+ B6）。 |
| `F3` | 目标库不可达 / 能力不足 | 降级且给出原因；能力四态可见；不得表现为「有数据但全零」。 |
| `F4` | 平台自身故障 | **不得**污染目标实例告警、**不得**计入 `NO_DATA`（T14 硬约束）。 |

某片若某一类确实不适用，条目仍必须存在，状态记 `n-a` 并写明理由。**留空 = 未覆盖**，不是「不适用」。

基线之外可加深（`S2`、`F5`…），加深条目的准入见 D6 的 `pending` 政策。

---

## 3. D3 · 三层断言的分工

- **API 层是默认主断言层。** 快、稳、可矩阵化，绝大多数条目落在这一层。
- **浏览器 E2E 只覆盖两类**：[`03` §6](../design/03-monitor-platform-ia-draft.md) 的五条关键用户路径，以及 [T7 D10.1](../design/08-frontend-stack-and-ui.md) 的 B6 前端必测四项（13 码全覆盖 / 缺数不是 0 / `dataUpdatedAt` / search params 往返）。
- **数据库层断言只读不写**，且仅用于 API 观测不到的效果：分区生命周期、加密列内无明文、审计留痕、迁移结果。**任何 DB 层的写操作都不属于断言，属于 D4 禁止的数据准备。**

**「允许只做 API 层」的判据（可检验）**：该断言点在 UI 上**没有独立语义**——UI 只是同一份 JSON 的转述。反过来，凡断言点是「用户看到什么」（空状态文案、归因行、降级原因、数据新鲜度），必须上浏览器。

---

## 4. D4 · 反假覆盖：数据来源禁令

现行 [`scripts/check-e2e.sh`](../../scripts/check-e2e.sh) 是本项要根除的样本：它起完 server 后直接 `psql INSERT` 造 `instance` / `metric_series` / `metric_sample`，再跑一条 Playwright smoke——业务 API、采集管线、告警评估全被绕过，绿灯不证明任何业务语义。矩阵落地时该段数据准备**删除**，不保留。

**禁令**：测试数据只许经**业务 API** 或**真实采集管线**产生；测试代码与 E2E 脚本**禁止对业务表写入**（`INSERT` / `UPDATE` / `DELETE`）。

**白名单**：唯一例外须逐条登记于 `test/acceptance/matrix.yaml` 的 `exceptions:` 段，写明「为什么没有 API 路径」。预期只有两类，且均应先尝试 D8 的替代手段：

1. 无法参数化的历史时间轴；
2. 无法用真实手段制造的故障态。

**结构守卫（B 栏新增，B11）**：扫描 `scripts/`、`web/e2e/`、`test/` 下对业务表的写语句，命中且未在白名单内即红。该守卫跑在 `make check` 里。

---

## 5. D5 · 横切组：四不变式 + 三内置规则独立计分

设一个不属于任何片的**横切断言组**：`INV-1..4`（[00 §4](../design/00-decision-index.md) 四条不变式）与 `BUILTIN-1..3`（[`02` §6.1](../design/02-alert-rule-model-draft.md) 三条内置采集状态规则）。

**每条横切条目必须同时具备**：

1. 一条 A 栏表驱动单测（复用 / 扩展 A1–A5 与 A9 golden）；
2. 一条端到端断言。

**只有单测 = 未覆盖。** 理由：这七条恰是「改坏了没人看得出来」那一类——A 栏证明纯函数正确，端到端证明它真的被接进了运行路径。

横切组**独立计分**：任一片全绿都不构成横切条目的覆盖；各片条目可以引用横切 ID，但引用不计入横切组的达标。

---

## 6. D6 · 载体、条目粒度、ID 与 `pending` 政策

### 6.1 两件产物

| 产物 | 内容 | 维护者 |
|---|---|---|
| 本文档 | 骨架与判定规则 | 决策层，推翻须新开记录 |
| `test/acceptance/matrix.yaml` | 机器可读条目清单 | 下游四票填入，此后随实现票更新 |

### 6.2 粒度

**一条条目 = 一条可判定路径**（一次执行 + 一组断言）。断言点是条目的**属性**，不是条目本身——否则条目数从数十膨胀到数百，维护即死。

### 6.3 ID 规范

- 片内：`AC-<片号两位>-<类><序号>`，如 `AC-05-S1`、`AC-05-F2`。
- 横切：`INV-1..4`、`BUILTIN-1..3`。
- **片号 `01`–`10` 永不复用、永不重编**（同「枚举码一经发布只增不改」的纪律）。条目作废写 `status: retired` 并留理由，不删行、不让出 ID。

### 6.4 条目字段

```yaml
- id: AC-05-F2            # 见 6.3
  slice: "05"             # 01..09（10 不出条目）
  kind: F2                # S1 | F1..F4 | 加深 S2+/F5+
  baseline: true          # true 的条目不许 pending
  title: <一句话>
  layer: api | browser | db   # 主断言层，见 D3
  asserts: [<断言点>, ...]
  pages: [<IA 页面树路径>, ...]      # 覆盖维，可空
  operations: [<operationId>, ...]  # 覆盖维，可空
  crosscut: [INV-2, ...]  # 引用的横切条目，可空
  status: covered | pending | n-a | retired
  test_ref: <测试标识>     # status=covered 时必填，见 6.6
  owner_issue: 118        # 归属的实现 / 条目票
  reason: <n-a 或 pending 的理由>
```

### 6.5 `pending` 政策

**基线不许 `pending`，加深可以。**

基线 = 九片 × 五条（`S1` + `F1..F4`）= 45 条，加七条横切 = **52 条硬底**。任一条不是 `covered` 或有理由的 `n-a`，v1 即未达标。

加深条目允许 `pending`，但每条必须具名 `owner_issue` 与 `reason`，且**总数与逐条清单进 Go/No-Go 报告**。阻断阈值本身归 [#114](https://github.com/liumingjian/dbs-monitor/issues/114)；本文只定「必须逐条具名、必须计数、不得静默省略」。

**`n-a` 的准入**：只有「该语义在本片不存在」才算不适用。「暂时测不了」是 `pending`，不是 `n-a`。人工判定项（如片⑤的 AntD 观感门）记 `n-a` 并写明「不可自动判定 + 人工判定的归属」，**不得伪装成 `covered`**。

### 6.6 覆盖漂移门（B 栏新增，B12）

`status: covered` 的每条条目，其 `test_ref` 必须能在测试代码中被检索到（Go 测试名、Playwright 测试标题或用例注解均可）；对不上即红。这道门保证 `covered` 是事实而不是声明。

---

## 7. D7 · 执行环境与 `make acceptance`

### 7.1 两个 PostgreSQL，一个真 Agent

`compose.yaml` 起**两个** PG 容器：**平台库**与**被监控目标库**分离。

理由不是洁癖：地图 #105 Notes 第 3 条把平台库定为**客户自备的外部前置**。同库合一会让一整类越界永远测不出来——平台代码误碰目标库、目标库权限不足被平台库权限掩盖、平台库故障与目标库故障混为一谈（正是 `F4` 要区分的东西）。

`cmd/monitor-agent` 以**真进程**运行，**走真实接入流程拿 token**（片⑧的接入 API），不预置任何行。Agent 具体接入步骤由 [#121](https://github.com/liumingjian/dbs-monitor/issues/121) 定，是其余三组的前置。

### 7.2 `make acceptance`

新增 `make acceptance` 作为矩阵专用目标：起平台库 + 目标库 + server + agent，跑矩阵，产出机器可读结果。

- `make check`（≤120 秒快层）**不含**它；
- `make check-full` **含**它。

E2E 因此变慢，这是**已接受的代价**：真实采集管线与真实接入是 D4 禁令的另一面，省掉它就回到假覆盖。

### 7.3 与下游票的界

| 本文（#111） | [#114](https://github.com/liumingjian/dbs-monitor/issues/114) | [#115](https://github.com/liumingjian/dbs-monitor/issues/115) |
|---|---|---|
| 判定规则 + 覆盖清单 + 结果格式 | 哪些门阻断、阈值多少、跑几轮 | 在哪台机器上跑才算数 |

### 7.4 结果格式

`make acceptance` 产出 `acceptance-result.json`：矩阵版本、被测提交 SHA、每条条目的 `id` / `status` / 实际结果 / 耗时，以及汇总（基线达标与否、`pending` 计数与清单、页面树与 `operationId` 的覆盖缺口）。该文件是 #114 的 Go/No-Go 报告与 #110 的候选留痕的**输入**，格式变更须同步这两票。

---

## 8. D8 · 时间不伪造，故障不模拟

### 8.1 时间参数化

采集周期、保留期、分区跨度、通知 repeat 与退避等时间常量**做成配置项**，验收用极短值跑**真实时间轴**。

**不注入假时钟到 server 进程**：[T5](../design/05-backend-code-structure.md) 的 `clock.Clock` 接缝止步 L2，对进程内测试有效；矩阵里的 server 是独立进程，注时钟既贵又失真（失真本身会掩盖问题）。

仅当某语义确实无法配置化时，才允许直写历史样本，且必须走 D4 白名单逐条登记。

### 8.2 故障用真实手段制造

停目标库容器（不可达）、kill agent（离线）、错误凭据（权限拒绝）、调低磁盘水位阈值（紧急拒写）、断开平台库（平台自身故障）。

**禁止 mock、禁止直改状态表**——与 D4 同源，也与根 `CLAUDE.md`「不造 mock」一致。

---

## 9. D9 · 角色 fixture

三档角色账号**一律经用户管理 API 创建**（[17](../design/17-user-role-and-instance-onboarding.md) 的角色模型），初始 admin 由 `INITIAL_ADMIN_PASSWORD` 引导，**不许直插 `user` 表**。

- `F1`（权限拒绝）在 **API 层**用三种角色的会话各跑一遍；
- **浏览器**只补一条 UI 断言：**可见性不收窄、写能力收窄**（不变式③里只有 UI 能证的那一面）。

---

## 10. D10 · 与 46 张实现票的对齐方向

对齐方向是**矩阵 → 票**：矩阵条目标注 `owner_issue`（归属片与责任票）。

**不**反向要求 46 张实现票各自认领条目 ID——票已发布且大部分在途，反向改动成本高、遗漏难查，而单向标注已足以回答「这条验收由谁负责」。

跨片原则第 1 条（「形状断言随片①各 Task 同步写，不得留到片⑩补」）不受影响：矩阵接管的是**验收面**，不是各片自己的单测责任。

---

## 11. 本文新增的两道守卫（登记进 [T9](../design/10-ai-guardrails-and-verification.md) B 栏）

| # | 内容 | 出处 | 兑现时机 |
|---|---|---|---|
| B11 | 测试与 E2E 脚本中禁止对业务表写入（白名单外命中即红） | 本文 D4 | 矩阵落地 |
| B12 | `covered` 条目的 `test_ref` 必须在测试代码中可检索（覆盖漂移门） | 本文 D6.6 | 矩阵落地 |

按 [T9](../design/10-ai-guardrails-and-verification.md) §3.4 的纪律，这两条一经登记即有约束力。

---

## 12. 已接受的代价

1. **`make check-full` 显著变慢**：两个 PG + 真 Agent + 真实时间轴。换 D4 禁令有意义——否则矩阵和 `check-e2e.sh` 一样只证明进程能起来。
2. **参数化时间轴不能覆盖真正的长周期语义**（如超长保留期的真实跨度）。极短参数证明的是**机制**，不是时长本身；真实时长归人工/发布期演练，不冒充自动判定。
3. **`n-a` 有被滥用的风险**：它是唯一能绕开基线的出口。对冲是 `reason` 必填 + 人工判定项必须写明归属，且 `n-a` 与 `pending` 一样进 Go/No-Go 报告的显式清单。
4. **单向对齐（D10）意味着实现票里看不到自己被哪条验收覆盖**，需要反查矩阵。接受，换 46 张票零改动。

---

## 13. 未决，交下游

- 逐片具体条目：[#118](https://github.com/liumingjian/dbs-monitor/issues/118)（片①⑦⑨）、[#119](https://github.com/liumingjian/dbs-monitor/issues/119)（片②③④）、[#120](https://github.com/liumingjian/dbs-monitor/issues/120)（片⑤⑥）、[#121](https://github.com/liumingjian/dbs-monitor/issues/121)（片⑧ + 横切组）。
- 通知在无外部依赖下的验收深度：归 [#119](https://github.com/liumingjian/dbs-monitor/issues/119)。
- 门禁与阈值：[#114](https://github.com/liumingjian/dbs-monitor/issues/114)；真实 Linux 的最终验收：[#115](https://github.com/liumingjian/dbs-monitor/issues/115)。
- 安全断言集与数据恢复门各自的条目并入矩阵的方式：[#112](https://github.com/liumingjian/dbs-monitor/issues/112) / [#113](https://github.com/liumingjian/dbs-monitor/issues/113) 产出后按本文 D6 字段登记。
