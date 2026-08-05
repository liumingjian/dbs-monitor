# AI 开发护栏与验证闭环 v1.0

> 目标：定死「下一个会话打开这个仓库，凭什么能自己判断自己写对了」——验证闭环的层次与内容、本地开发环境、强制测试清单、`CLAUDE.md` 的边界与体裁、不变式的可执行化、强制点与工作方式。
> 适用范围：`Makefile`、`compose.yaml`、GitHub Actions workflow、两份 `CLAUDE.md`、全部守卫测试。
> 决策票：[T9 · AI 开发护栏与验证闭环](https://github.com/liumingjian/dbs-monitor/issues/27)。
> 输入边界（不重议）：[T5 · 后端代码结构与模块边界](05-backend-code-structure.md)（四层偏序 + `arch_test.go`、五个接缝、单测依赖真库、否决 golangci-lint）、[T6 · API 契约组织与代码生成流水线](07-api-contract-and-codegen.md)（`make gen` 唯一入口 + 漂移门、三条机器守卫、`assertNever`、`dataUpdatedAt`）、[T7 · 前端技术栈与 UI 体系](08-frontend-stack-and-ui.md)（三个状态桶、`domain/` 封闭清单、前端必测四项）、[T2 · 时序存储选型与指标数据模型](04-metric-storage-model.md)（三条查询纪律）、[T4 · 指标字典载体与采集计划](06-metric-dictionary-and-collection-plan.md)（两条强制测试、三条禁止线、PG13–17 矩阵）、[T8 · 打包、部署与运行形态](09-packaging-and-deployment.md)（`migrations/` 只写 up、安装/升级脚本）。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。
> **本票只产决策文档。** 两份 `CLAUDE.md`、`Makefile`、compose、GitHub Actions workflow、B 栏各条守卫测试随 [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29) 落地（理由见 §12）。

---

## 0. 一句话结论

**两层闭环——`make check`（≤90 秒，每次改动跑）与 `make check-full`（CI / 发版跑）；开发环境用 Docker Compose 起真 PG；强制测试劈成「语义表驱动单测 9 条」与「结构守卫 6 类」两栏并配准入判据；`CLAUDE.md` 只收「闭环不会红」的禁令，祈使句、≤150 行、分根与 `web/` 两份；先例是指向活代码的指针不是贴片；不装 git hook，「完成」直接定义为 `make check` 全绿。**

贯穿全票的取向与 [T5](05-backend-code-structure.md) / [T6](07-api-contract-and-codegen.md) / [T8](09-packaging-and-deployment.md) 一致：**把规范做成结构，而不是做成约定。** 落到本票有一条额外的自省——

> **护栏本身也服从这条原则。** 因此 `CLAUDE.md` 里凡是机器已经守住的规则一律删掉：一份复述机器守卫的文档不增加任何保证，只消耗每个会话的上下文，并让真正只有它能守的那几条淹没在噪声里。**`CLAUDE.md` 的价值与它的长度成反比。**

---

## 1. D1 · 验证闭环切两层，不切三层

**结论**

| 层 | 命令 | 内容 | 谁跑 |
|---|---|---|---|
| 快 | `make check` | ① `make gen && git diff --exit-code`（生成物漂移门，[T6 D3](07-api-contract-and-codegen.md)）② `go vet ./...` ③ `go test ./...`（含全部 B 栏 Go 侧守卫、迁移可用性，需真 PG）④ `tsc --noEmit` ⑤ `eslint`（仅 `web/`）⑥ `vitest run` | **每次改动，会话自己跑** |
| 慢 | `make check-full` | 快层全部 + `vite build` + `go:embed` 真构建 + arm64 + [T11](https://github.com/liumingjian/dbs-monitor/issues/29) 的容量/延迟门槛；**R3 发布门槛**再加入 PG13–17 采集矩阵与打包/安装/升级脚本冒烟 | CI / 发版 |

**预算：`make check` ≤ 90 秒**（复用已起的开发 PG，不含容器冷启动）。超预算的东西往 `check-full` 挪，**不许加第三层**。

> **实测回写与预算修订（收口增补 2026-08-05，兑现 §14 交 T11 的第②笔）**：T11 原生 Linux amd64 验证实测 `make check` = **114 秒**（`docs/validation/t11-linux-amd64-progress.md`；测量宿主根分区近满、依赖缓存被挪往 `/tmp`，数字偏悲观）。预算击穿 27%。处置：**预算修订为 `make check` ≤ 120 秒**，暂不执行 §15 预写的「把 PG 相关测试挪进 `check-full`」，理由：
> 1. 快层 `go test` 依赖真 PG 是 [T5 §5.1](05-backend-code-structure.md)「不造 mock」被接受的代价（本节「代价」段已写明）：A 栏语义测试与 B 栏 Go 侧守卫大量触库，「挪走 PG 相关测试」在本仓库的实际含义接近把快层掏空成 vet + tsc + lint，守卫红灯整体推迟到 CI，D4「新会话靠撞红灯知道守卫存在」的机制随之失效。
> 2. §15 预写该处置时假设击穿来自可识别的重量级测试；实测验证记录**没有分包耗时数据**，定位不出可挪的「大户」，盲挪与「完成 = `make check` 全绿」的确定性直接冲突。
> 3. 90 秒是「行为阈值不是性能指标」（理由 2）：其论证目标是「会话愿意在中途跑它」。T11 验证会话在 114 秒下全程照跑、未跳过闭环；两分钟以内该论证仍成立。
> 4. 取 120 而非「实测值加一点」：给 R3 新增 A 栏测试留余量。**重新触发条件**：正常磁盘条件下实测超过 120 秒时，回到 §15 预写处置——但必须先产出分包耗时数据、定位大户再挪，禁止为过预算整体搬迁。

T11/R3 边界：T11 的 `check-full` 验证真实构建、Playwright、双架构编译和 RT-C；PG13–17 矩阵以及升级/回滚生命周期验证正式延期到 R3 的发布闭环，不作为 T11 resolve 的前置条件。

**理由**

1. **三层会产生判断题。** 「改动级 / 提交级 / CI 级」要求执行者先判断「我这次该跑哪条」，而模型在判断题上的失败率远高于执行题。给它一条命令、红即未完成，是这条闭环唯一真正起作用的形态。
2. **90 秒是行为阈值不是性能指标。** 慢到某个点后，会话（和人）会停止在中途跑它，闭环退化成提交前的一次性仪式——那正是它最不该出现的时刻。
3. **快层必须含漂移门**：改了 spec 没重新生成，是 T6 全套类型保证的唯一逃逸口，且它只花几秒。

**代价**：`make check` 依赖一个已起的 PG（见 D2），不是纯离线命令。这是 [T5 §5.1](05-backend-code-structure.md) 「不造 mock」的直接推论，已被上游接受。

> **收口增补 2026-08-05：RT-C 去向。** RT-C 是分钟级容量 / 延迟实测，不接入每次 `check-full`；当前由人工按 `scripts/rt-c/run.sh` 的完整参数执行，作为 R3 发布门槛接管。`check-full` 仍负责 T11 的构建、E2E 与双架构编译。

---

## 2. D2 · 本地开发环境：Docker Compose，两个 profile

**结论**：`make dev-up` 用 Docker Compose 起测试用 PG。**容器只进开发环境，绝不进交付**（[T8 D1](09-packaging-and-deployment.md) 否决的是交付物形态，与本条不冲突）。

- **默认 profile**：`postgres:17` 一个容器，作**平台库**，供 `make check` 用。
- **`matrix` profile**：PG 13/14/15/16/17 五个容器，作**被监控库**，供 R3 发布闭环的采集矩阵用（兑现 [T4 §5.5](06-metric-dictionary-and-collection-plan.md) 「本地环境需能起 5 个 PG 版本」）。
- **逃生舱**：`PGHOST_EXTERNAL` 已设时 `make dev-up` 跳过容器，直接接管现成实例（CI、无 Docker 的机器、离线内网都靠这条）。`PGHOST` 保持默认的 `localhost`，不作为逃生舱开关。

**理由**：把开发环境也逼成 [T8 D2](09-packaging-and-deployment.md) 那样源码自建 PG，等于每个新会话开局先编译 20 分钟——这是**真会被绕过**的东西，而绕过的形态就是 T5 §5.1 警告的「开始造 mock」。

### 2.1 一条必须钉死的同构性检查

[T8 D2](09-packaging-and-deployment.md) 把交付的自带 PG 写死为 `--without-icu`，而官方 `postgres:17` 镜像**带 ICU**。若开发库以 ICU provider 建库，排序与索引行为与交付环境不是一回事，而**这类差异不报错，只给出不同结果**。

**规定**：开发容器建库钉死 `initdb --locale=C --encoding=UTF8`（libc provider），并在 `make check` 中断言 `datlocprovider <> 'i'`。

> 让「开发库与交付库不同构」变成红灯，而不是某天线上排序诡异。同 [T8 D11](09-packaging-and-deployment.md) 把时钟偏移前移成安装期失败，是同一个动作。

---

## 3. D3 · 强制测试清单：两栏 + 准入判据

### 3.1 准入判据（三问全中才进清单）

1. 它是 **R1 / R2 已冻结的语义**吗？
2. 改坏了，**编译器与类型系统都拦不住**吗？
3. 它有**唯一实现点**，可表驱动吗？

**任一条不中即不进。** 没有这条判据，清单三个月后会膨胀成「什么都必须表驱动」，届时整体失效——**清单的价值来自它短**。

### 3.2 A 栏 · 语义表驱动单测（9 条）

| # | 内容 | 出处 | 兑现时机 |
|---|---|---|---|
| A1 | 告警五状态机的**全部**迁移 | `02` §3 | R4 |
| A2 | 滞回恢复阈值 | `02` §3 | R4 |
| A3 | `NO_DATA` 连续 2 周期计数（含「缺数不推进也清零未完成计数」） | `02` §3.4 | R4 |
| A4 | 暂停采集的冻结语义（不转 `RECOVERED`、解冻不回放） | `02` §3.4 / §6 | R4 |
| A5 | 实例健康最坏归并 + 已暂停 override | 不变式 ② | R4/R5 |
| A6 | 差分指标遇 reset 不得产生负值/尖峰 | [T2 §6](04-metric-storage-model.md) | **T11** |
| A7 | 指标字典交集：解析 `01` §3 总览表与 Go 声明比对 | [T4 §2.1](06-metric-dictionary-and-collection-plan.md) | R3 |
| A8 | 每个指标恰好被一个采集任务产出 | [T4 §3.3](06-metric-dictionary-and-collection-plan.md) | R3 |
| A9 | **枚举码表 golden 快照** | 本票新增，见下 | **T11** |

**A9 的理由**（本票主动加的一条）：[T2 §4](04-metric-storage-model.md) 那句「码值一经发布只增不改」原计划只写进 `CLAUDE.md`。**写进 `CLAUDE.md` 的纪律拦不住手滑**；一个 golden 文件把它升级成一道门——改码表 = 测试红 = 必须显式更新 golden = 一次有意识的行为。成本约 20 行。覆盖：`Unavailability` 13 码（[T6 §8.3](07-api-contract-and-codegen.md)）、告警五状态、能力四档、已实现的非数值指标编码（`internal/metric/enum_test.go`；复制状态在 R3 实现时追加）、三条内置采集状态规则（见 D7）。

### 3.3 B 栏 · 结构守卫（不是表驱动单测，但同样跑在 `make check` 里）

| # | 内容 | 出处 | 兑现时机 |
|---|---|---|---|
| B1 | `arch_test.go`：四层偏序 + 新包默认拒绝 | [T5 §2.4](05-backend-code-structure.md) | T11 |
| B2 | `make gen && git diff --exit-code` 漂移门 | [T6 D3](07-api-contract-and-codegen.md) | T11 |
| B3 | `x-required-role` × `operationId` 全覆盖 | [T6 D10.3](07-api-contract-and-codegen.md) | T11 |
| B4 | Go 侧解析 spec 的枚举穷尽测试 | [T6 D9](07-api-contract-and-codegen.md) | T11 |
| B5 | `migrations/` 无 down 语句 | [T8 D9.2](09-packaging-and-deployment.md) | T11 |
| B6 | 前端必测四项（13 码全覆盖 / 缺数不是 0 / `dataUpdatedAt` / search params 往返） | [T7 D10.1](08-frontend-stack-and-ui.md) | T11 |
| B7 | 响应 schema 秘密字段禁名单 | 本票新增，见 D7 | T11 |
| B8 | `CLAUDE.md` 路径存在性 | 本票新增，见 D5 | T11 |
| B9 | `web/src/domain/` 一级条目 = 登记表 | [T7 D8.2](08-frontend-stack-and-ui.md) → D8 | T11 |
| B10 | 迁移可用性三连（`goose up` ×2 幂等 + `sqlc vet`） | 见 D9 | T11 |

### 3.4 清单是一张有约束力的登记表

A 栏九条中六条要到 R3–R5 才有代码。**本票现在只能登记，但登记有约束力**：

> **实现某语义时，清单里有对应项而提交中没有对应的表驱动测试 = 未完成。** 这条写进根 `CLAUDE.md`。

**代价与理由**：远程约束确实弱于机器守卫，但替代方案（只留 T11 真会触及的 A6/A9 两条）等于把 R1 花整条路线冻结的六条语义**在最容易被忘记的时刻**留给记忆——而 A1–A5 恰好全是「改坏了没人看得出来」那一类。留一份短清单，比留零份强。

---

## 4. D4 · `CLAUDE.md`：边界、体裁、预算、份数

### 4.1 准入判据（两条同时满足才进）

1. 它是**禁令或强约定**；
2. **违反后 `make check` 不会红**。

**会红的东西一律不进**——闭环已经在管了，写进去只是复述。按这条筛，B 栏 10 条守卫**全部不进** `CLAUDE.md`（仅在「一条命令」那节整体提一句）。

**明确接受的代价**：新会话只读 `CLAUDE.md` 不会知道 `arch_test.go` 的存在，要靠跑 `make check` 撞上。这是有意的——**撞上一次红灯比读到一行文字更能建立约束**，而反过来，为了「让它知道」而复述全部守卫，会让文件长到没人读完。

### 4.2 体裁

- **祈使句，一条一行。**
- **禁止解释「为什么」**，理由一律链到 `docs/design/0X.md#锚点`。需要理由的会话会点进去；不需要的不该被它占上下文。
- **禁止贴代码片段**（见 D5）。

### 4.3 预算

**≤ 150 行**，写在文件头。超了必须删或下沉，**不许加节**。

### 4.4 分两份

| 文件 | 内容 |
|---|---|
| 根 `CLAUDE.md` | 项目定位与必读指针、一条命令、依赖方向、后端禁令、强制测试登记表指针、A 栏远程约束 |
| `web/CLAUDE.md` | 三个状态桶、`domain/` 封闭清单、`?? 0`、`assertNever`、`dataUpdatedAt`、Router 先例指针 |

**理由**：子目录 `CLAUDE.md` 在读到该目录文件时才加载——**改后端的会话不必付前端规则的上下文成本**，反之亦然。Go 侧不再细分：后端禁令大多跨包，拆了每份都读不全。

草案见附录 A / B。

---

## 5. D5 · 先例 = 指向活代码的指针，不是贴片

**结论**：`CLAUDE.md` 里**一行代码都不贴**，只写真实文件路径。

**理由**：贴进 Markdown 的片段**会腐烂，且腐烂时没有任何东西会红**——它不编译、不被测试覆盖，半年后教给会话的是一个已不存在的写法。指向 `web/src/routes/...` 的一行指针，指的是一直被编译和测试守着的活代码。R2 的 Destination 本来就写着「靠既有先例判断自己写对了」，**先例的正确载体就是骨架本身**。

**守卫 B8**：一条测试断言两份 `CLAUDE.md` 中出现的所有仓库相对路径真实存在。文件改名而指针没跟着改 = 红。约 30 行，消掉「文档指针腐烂」整类静默失效。

### 5.1 回写 [T10](https://github.com/liumingjian/dbs-monitor/issues/28) 的一条切片下限

[T7 D6.3](08-frontend-stack-and-ui.md) 要求 `CLAUDE.md` 补足 TanStack Router 的三个用法先例（路由定义、`validateSearch`、跨页继承）。骨架若只有一条扁平路由，「跨页继承」就没有产地。

> **切片下限**：walking skeleton 必须含**至少两级路由 + 一个带 `validateSearch` 的时间范围参数**。

这恰好是 [T6 D7](07-api-contract-and-codegen.md)「时间一律绝对、可分享链接」本来就要求的东西，**不构成骨架加厚**。

---

## 6. D6 · 强制点：不装 hook，把「完成」定义成绿灯

**结论：不装 pre-commit hook。**

**理由**（第三条是决定性的）

1. hook 要求每个克隆多一步安装，装不上的机器**静默**失去保护；
2. `--no-verify` 一秒绕过；
3. **对 Claude Code 会话它会帮倒忙**——被 hook 拦下的输出长得像「git 提交失败」，会话的自然反应是把提交做成功（重试、绕过、改 hook），而不是理解成「我的代码没写完」。**把红灯挂在 `git commit` 这个动作上，等于把它伪装成工具故障。**

**替代：两个强制点，一软一硬**

- **软（天天起作用的那个）**：根 `CLAUDE.md` 把「完成」直接定义为 **`make check` 全绿**——不是「提交前请跑一下」，而是「没跑绿就是活没干完，不要报告完成」。它落在会话的**完成条件**上，贴合其实际行为回路。
- **硬**：CI 的 PR 门跑 `make check`，主干 / 发版跑 `make check-full`，红即不可合入。

### 6.1 CI 只定接口，不定流水线

**已知事实**：CI 平台为 **GitHub Actions**（本票询问所得，记录在案）。

**本票只规定**：CI 必须跑上述两条命令、红即不可合入。**谁来跑、什么触发、怎么发版**留在地图迷雾「CI 与发布流水线」那条，等它 graduate——那条明说剩下的正是这三个问题，本票越界去定属于抢 scope。

---

## 7. D7 · 四条不变式的可执行化：两条已成立，两条补守卫

| 不变式 | 现状 | 处置 |
|---|---|---|
| ① 五档状态，压制正交 | **已结构性成立**：[T6 D9](07-api-contract-and-codegen.md) 把 `status`（5 值）与 `suppressions`（数组）做成两个类型，「第六档」在契约层写不出来；另有 B4 穷尽测试兜底 | 不加东西 |
| ② 健康单一来源 | **已结构性成立**：[T5 §2.2](05-backend-code-structure.md) 层序让 `instance` 够不着 `alerting`，「实例列表自己算健康」写不出来；B1 兜底 | 不加东西 |
| ③ 凭据永不回显 | **半成立**：[T6 D10.4](07-api-contract-and-codegen.md) 的请求/响应双 schema 让明文回显写不出来，但**双 schema 本身靠人守**——新增响应对象时手滑塞进 `password` 字段，没有任何东西会红 | **补 B7** |
| ④ 三条内置采集状态规则不可删、不可停用、下限 `warning` | **完全没有结构保证**，今天只活在文档里 | **补 A9 + A 栏远程约束** |

### 7.1 B7 · 响应 schema 秘密字段禁名单

一条 spec 级测试：**所有响应 schema 的属性名匹配 `password` / `secret` / `token` / `credential` / `dsn` 即红**，需要例外必须在测试的白名单里显式登记。

**取宽不取窄**（否决只匹配 `password/secret/dsn` 的窄版）：宽名单会误伤合法字段（如「令牌是否已签发」这类布尔量），代价是偶尔要登记一次例外——而**每次登记都是一次有意识的确认**，漏掉一次明文回显则是 R1 花整条路线守的东西。摩擦在正确的方向上。

### 7.2 不变式 ④ 的落法

- **A9 golden** 覆盖三条内置规则的**存在性与 severity 下限**（`warning`）；
- 「删除 / 停用内置规则的 API 必须拒绝」要 R4 才有代码，按 §3.4 的远程约束兑现。

> 本表**不进 `CLAUDE.md`**：四条全部有机器在守或将要守，按 D4 判据属复述。

---

## 8. D8 · [T7](08-frontend-stack-and-ui.md) 派下的三笔：能机械化到什么程度

| T7 的要求 | 判定 | 落法 |
|---|---|---|
| `?? 0` 能否落 lint（「缺数不是 0」） | **能，精确** | eslint `no-restricted-syntax` 匹配 `??` 与 `\|\|` 右值为 `0`。全局禁；豁免须 `eslint-disable-next-line` **且写明理由**（分页计数这类合法场景确实存在，但它该是一次有意识的豁免） |
| 第四个状态桶禁令（[T7 D7.1](08-frontend-stack-and-ui.md)） | **一半精确，一半靠约定** | `no-restricted-imports` 禁 `redux` / `@reduxjs/toolkit` / `zustand` / `jotai` / `mobx` / `valtio`——零误伤。**但 `createContext` 手搓全局 store 抓不住**：它与合法的主题/权限 Context 语法上无区别 ⇒ 见 §8.1 |
| `invalidateQueries` 只许在对应域 mutation hook 里（[T7 D5](08-frontend-stack-and-ui.md)） | **能** | eslint override：仅 `web/src/domain/*/mutations.ts` 允许出现 `invalidateQueries`，其余文件出现即红。**代价：本票顺手钉死该文件名约定** |
| `domain/` 封闭清单（[T7 D8.2](08-frontend-stack-and-ui.md)） | **能** | B9：一条 vitest 断言 `web/src/domain/` 一级条目集合 == `web/CLAUDE.md` 登记表。与 [T5](05-backend-code-structure.md) 对付新包的招数同构 |

### 8.1 `createContext` 进白名单，新增须登记

**结论**：`createContext` 只允许出现在白名单文件中，新增须先在 `web/CLAUDE.md` 登记（与 `domain/` 封闭清单、T5 新包默认拒绝同一招）。

**否决**「接受口子、只写禁令」：那个口子恰好是 [T7 D7.1](08-frontend-stack-and-ui.md) 说「一旦存在就会蚕食『可分享链接』与『缓存失效』两条保证」的东西。
**否决**「彻底禁止 `createContext`」：合法用途（主题、当前用户角色）真实存在，全禁会逼出更差的绕法。

**代价**：合法 Context 多一次登记摩擦。判为可接受——会话本来就要在「新增须登记」的节奏里工作，多一处不增加认知负担。

---

## 9. D9 · 迁移与安装脚本各进哪一层

**进快闭环**（秒级，B10）：

1. 对空库跑全部 `goose up`；**再跑第二次**断言无 pending、无变更（幂等）；
2. 迁移后 schema 与 sqlc 生成物一致（`sqlc vet` 对真库逐条 prepare，复用同一个开发库）；
3. `migrations/` 无 down 语句（B5，[T8 D9.2](09-packaging-and-deployment.md)）。

三条合起来把「改了迁移忘了改查询」「写了 down」「迁移跑不动」全部前移到本地。

**进 R3 发布闭环**（分钟级）：安装 → 起服务 → healthcheck → 升级到当前构建 → 回滚并恢复控制面 → 再 healthcheck 整条。它要 root、要 systemd、要真机或特权容器。T11 只执行安装与首启人工验收。

> 因此 [T8 §13](09-packaging-and-deployment.md) 那笔的答案是：**安装/升级脚本纳入 R3 发布验证闭环**；T11 只把离线包安装和首启作为人工验收。

同性质判断：**`vite build` 与 `go:embed` 真构建不进快闭环**，`tsc --noEmit` 已能抓住类型错。

---

## 10. D10 · 工作方式：写行为要求，不写 skill 名字

**结论**：`CLAUDE.md` 里**不出现 skill 名字**。

**理由**：skill 是**个人环境里的东西**（本地插件缓存）。别的开发者、CI、换一个工具的会话都不一定有；一份指名 `/tdd` 的 `CLAUDE.md` 对它们是死链，且会诱发「我没有这个 skill，那这条大概不适用于我」的推理。

改写成 skill-agnostic 的行为要求：

1. **触碰 A 栏任一项语义的改动，必须先写失败测试**（红 → 绿）。这是把 `/tdd` 的实质而非名字写进来。
2. **其余改动不强制任何流程**，只强制 `make check` 全绿。全面强制 TDD 会在 CRUD 这类地方产生大量仪式性测试，**稀释 A 栏九条的信号**。
3. **评审建议**（写在本文档，不进 `CLAUDE.md`）：PR 前跑一次 `/code-review`——其 Standards 轴的「本仓库标准」正好是本票产出的两份 `CLAUDE.md` + 决策文档。这条是给人与 wayfinder 会话看的，不是给每个改代码的会话看的。

---

## 11. 附录 A · 根 `CLAUDE.md` 草案

> 先例路径为占位符（`«…»`），由 [T11](https://github.com/liumingjian/dbs-monitor/issues/29) 填真实路径后 B8 才会绿。

```markdown
# dbs-monitor

PostgreSQL 私有化监控平台。Go 后端 + Agent，TS/React SPA（`go:embed` 进主二进制）。
本文件预算 150 行，只写「违反后 `make check` 不会红」的规则；会红的东西不写在这里。

开工前必读：`docs/design/00-decision-index.md`（R1 十项 ADR + 否决记录 + 四条不变式）。
决策文档在 `docs/design/`，编号即决策顺序；推翻任何一条须新开决策记录，不原地改写。

## 完成的定义

`make check` 全绿才算完成。没跑绿不要报告完成——它红了不是工具坏了，是活没干完。
`make dev-up` 起开发用 PG（`make check` 依赖它）。`make check-full` 是 CI / 发版跑的慢层，本地不必跑。
改了 `api/*.yaml` 必须 `make gen`；生成物入库。

## 强制测试登记表

见 `docs/design/10-ai-guardrails-and-verification.md` §3.2。
实现该表中任一项语义时，提交里必须有对应的表驱动测试，且先写失败测试再实现。
表中没有的东西不必表驱动——清单的价值来自它短，不要往里加。

## 依赖方向

`internal/` 四层偏序，只许上层依赖下层：L3 `cmd` → L2 编排/循环 → L1 领域 → L0 基础设施。
同层禁止互相依赖，唯一例外 `collect → capability`。
新增包默认拒绝，须先在 `arch_test.go` 登记。
禁止的包名：`common` / `util` / `utils` / `shared`。
共享面只有生成物 `internal/api`，人不写进去。指标字典不共享。
理由与目录树：`docs/design/05-backend-code-structure.md`。

## 后端禁令

不造 mock。接缝白名单封闭为五个：`pgconn.Dialer` / `collect.Collector` / `clock.Clock` /
  `notify.Channel` / sqlc `DBTX`。其余一律直接 import 具体类型；测试用真库。
领域包不开事务，只接 `DBTX`。事务由 L2 编排层持有；通知在事务提交之后发。
`clock` 止步 L2。状态机是无时间的纯函数。
12 种空状态是值不是 error。Go `error` 只表失败。
新增枚举码只许追加，禁止修改或复用既有码值——历史数据会被读成另一个状态。
  golden 测试变红时只允许新增行；改既有行前先回到 `docs/design/04-metric-storage-model.md` §4。
不补 0：桶内无样本就不出现在结果里，禁止 `generate_series` 补齐时间轴 + `COALESCE(avg, 0)`。
「最新值」查询必须带时间下界（`AND ts > now() - interval '1 hour'`），否则分区裁剪失效。
采集 SQL 走 pgx 直连，不走 sqlc；被监控库连接与平台库连接类型不可互换。
不新增指标、不改指标口径、不改采集 SQL、不加采集任务——这些只能发版改，且要先改决策文档。
不做用户自定义 SQL 探针（安全边界，见 `docs/design/06-...` §8.3）。
不写版本分支：禁止 `CASE WHEN version >= N` 与动态拼接 SQL；版本门槛走接入校验。
`migrations/` 只写 up，不写 down。回滚靠备份。

## 先例

后端一条完整采集→存储链路：«占位符»
一次 `DBTX` 事务的编排写法：«占位符»
表驱动状态机测试的形状：«占位符»
```

---

## 12. 附录 B · `web/CLAUDE.md` 草案

```markdown
# 前端

TS + React + Vite 纯 SPA，AntD 6 + ECharts 6，TanStack Router + openapi-react-query。
理由与目录结构：`docs/design/08-frontend-stack-and-ui.md`。

## 状态只有三个桶，没有第四个

服务端状态 → TanStack Query；URL 状态（时间范围、筛选、`step`）→ search params；组件局部 → `useState`。
不引入 Redux / Zustand / Jotai / MobX / Valtio。
不用 `createContext` 手搓全局 store：`createContext` 只许出现在下方白名单，新增须先在此登记。
  白名单：«占位符»
需要第四个桶时先改决策文档，不许就地引入。
`step` 可进 URL，但渲染永远用响应回传的粒度值。

## 缺数不是 0

禁止 `?? 0` / `|| 0`。缺数就是缺数——需要豁免时写 `eslint-disable-next-line` 并注明理由。
图表一律用 `domain/` 里的领域组件，其 `unavailability` 参数必填；不装 `echarts-for-react`。
轮询数据必须用 `dataUpdatedAt` 判新鲜度再渲染，不能把上次成功的数据画成实时数据。

## 枚举

对枚举做映射（状态 → 颜色、码 → 文案）必须用带 `assertNever` 的穷尽 `switch`。
禁止 `default:` 兜底成 fallback 文案——兜底会把「漏了一档」伪装成正常渲染。
三套状态词汇（告警状态 / 实例健康 / 采集状态）是三个不相交类型，禁止通用 `StatusBadge`。
颜色永不单独承载信息；绿色永不表示「什么都没有」。

## 目录

路由树即页面树；页面私有件不上浮。
`domain/` 是封闭清单，新增一项须先在此登记：«占位符»
不建 `components/` / `utils/` / `shared/` / `common/`。
`invalidateQueries` 只许出现在 `domain/<域>/mutations.ts` 里，不许散落在组件中。

## 先例

路由定义：«占位符»
`validateSearch`（时间范围）：«占位符»
跨页继承 search params：«占位符»
领域图表组件：«占位符»
```

---

## 13. 交付边界：本票只产文档

与 [T6](07-api-contract-and-codegen.md) 同构（*本票只产决策文档，`api/*.yaml` / `Makefile` / 三条守卫测试随 T11 落地*）。

**本票产出**：本文档（闭环两层定义、清单两栏 + 准入判据、不变式→守卫对照表、`CLAUDE.md` 边界与体裁、两份草案）。

**随 [T11](https://github.com/liumingjian/dbs-monitor/issues/29) 落地**：根 `CLAUDE.md` 与 `web/CLAUDE.md` 真文件（含填实的先例路径）、`Makefile` 的 `check` / `check-full` / `dev-up` / `gen`、`compose.yaml` 两 profile、B 栏 10 条守卫、GitHub Actions 两个 workflow。PG13–17 矩阵和安装/升级生命周期的发布接线延期到 R3。

**理由**：仓库现在一行代码都没有。此刻落盘 `CLAUDE.md`，其中每个先例指针都是死的，B8 从诞生就是红的——**一条从来没绿过的守卫，等于没有守卫**。

---

## 14. 交给下游的三笔

| 去向 | 内容 |
|---|---|
| [T10 · Walking skeleton 切片定义与验收标准](https://github.com/liumingjian/dbs-monitor/issues/28) | **切片下限**：至少两级路由 + 一个带 `validateSearch` 的时间范围参数（D5.1），否则 T7 D6.3 的 Router 先例无产地 |
| [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29) | ① 落地 §13 全部产物；② `make check` 实测耗时回写本文档 D1（90 秒是预算不是实测）；③ 填实两份 `CLAUDE.md` 的先例路径；④ 首次 `make gen` 时钉死 [T6 D1](07-api-contract-and-codegen.md) 跨文件 `$ref` 的支持情况 |
| 迷雾「CI 与发布流水线」 | 本票只定接口（CI 必须跑两条命令、红即不可合入）+ 已知事实（GitHub Actions）。谁来跑 / 何时跑 / 怎么发，待该条 graduate |

---

## 15. 未决事实（显式记录，不假装已解决）

| 事实 | 状态 |
|---|---|
| `make check` 能否真的 ≤ 90 秒 | **已实测：114 秒**（T11，`docs/validation/t11-linux-amd64-progress.md`）。预算修订为 **≤ 120 秒**，理由与重新触发条件见 D1 的实测回写块（2026-08-05 收口增补）；「往 `check-full` 挪」保留为超 120 秒时的既定处置，前提是先有分包耗时数据 |
| `sqlc vet` 接入闭环 | **从未接入**（2026-08-05 收口登记为显式欠账）：快层、慢层与 §13 产物清单均无 `sqlc vet`，也无放弃记录——这不是「打折」，是完全缺失。RT-D 点名它能把 SQL 对真库逐条 prepare。R3 接入 `check` 或 `check-full`，或新开决策记录显式放弃 |
| goose 并发迁移锁语义 | **无结论**（2026-08-05 收口登记为显式欠账）：RT-D 缺口，T11 未实测。多进程同时启动、同时跑自动迁移（T5 启动形态）的加锁行为未验证；R3 落地前须钉死 |
| 官方 `postgres:17` 镜像与 [T8](09-packaging-and-deployment.md) 自建 PG（`--without-icu`）除 locale 外是否还有影响测试可信度的差异 | 未查全。D2.1 只守住了 locale provider 一条 |
| eslint 规则对 `?? 0` 的实际误报率 | 未实测。若豁免频繁到让人麻木，收窄到图表与 `domain/` 目录 |
| `sqlc vet` 对分区表的解析能力 | 承 RT-D 缺口，仍无一手数据；B10 第 2 条可能因此打折 |
| 两份 `CLAUDE.md` 是否真能压在 150 行内 | 草案已接近上限。若 T11 落地时超出，删禁令而非加预算 |

---

## 16. 否决记录汇总

| 被否决 | 出处 | 为什么 |
|---|---|---|
| 三层闭环（改动级 / 提交级 / CI 级） | D1 | 产生「我该跑哪条」的判断题；模型在判断题上的失败率远高于执行题 |
| 快闭环里跑 PG13–17 矩阵 | D1 | 分钟级，必然击穿 90 秒预算，进而让整条闭环被跳过 |
| 快闭环里跑 `vite build` / `go:embed` 真构建 | D9 | `tsc --noEmit` 已抓住类型错；真构建的增量价值不值它的秒数 |
| 开发环境也用源码自建 PG | D2 | 每个新会话开局编译 20 分钟 ⇒ 真会被绕过，绕法就是造 mock |
| 只留 T11 会触及的两条强制测试 | D3.4 | A1–A5 全是「改坏了没人看得出来」的 R1 冻结语义，在最容易忘的时刻留给记忆 |
| 「枚举码只增不改」只写进 `CLAUDE.md` | D3.2 | 纪律拦不住手滑；golden 快照把它升级成一道必须显式跨过的门 |
| `CLAUDE.md` 收录已被机器守住的规则 | D4.1 | 不增加任何保证，只消耗上下文，并淹没真正只有它能守的那几条 |
| `CLAUDE.md` 里贴代码片段 | D5 | 不编译、不被测试覆盖 ⇒ 会腐烂且腐烂时不会红 |
| 单份 `CLAUDE.md` | D4.4 | 改后端的会话要付前端规则的上下文成本，反之亦然 |
| pre-commit hook | D6 | 装不上即静默失去保护；`--no-verify` 一秒绕过；**且会让「代码没写完」伪装成「git 提交失败」，诱导会话去绕过而非去修** |
| 在本票定 CI 流水线（触发、发版） | D6.1 | 属迷雾「CI 与发布流水线」，本票只定接口 |
| 秘密字段窄禁名单（只 `password/secret/dsn`） | D7.1 | 漏一次明文回显 = 破不变式 ③；宽名单的代价只是偶尔登记一次例外 |
| 接受 `createContext` 手搓 store 的口子 | D8.1 | 恰是 T7 D7.1 说「一旦存在就会蚕食可分享链接与缓存失效」的那个东西 |
| 彻底禁止 `createContext` | D8.1 | 主题、当前角色是合法用途；全禁会逼出更差的绕法 |
| 全面强制 TDD | D10 | CRUD 处产生大量仪式性测试，稀释 A 栏九条的信号 |
| `CLAUDE.md` 中指名 skill（`/tdd`、`/code-review`） | D10 | skill 是个人环境的东西，对 CI 与其他读者是死链，且诱发「不适用于我」的推理 |
| 本票直接落盘两份 `CLAUDE.md` | §13 | 仓库尚无代码 ⇒ 先例指针全死、B8 从诞生就红；一条从没绿过的守卫等于没有 |
