---
status: active
kind: decision
note: 正文所有矩阵条目/硬底计数均已过期，真值在 test/acceptance/matrix.yaml；D2/D3 启动语义是全仓规范来源
---
# 26 · 数据与恢复门的具体证据

> 出处：[数据与恢复门的具体证据 #113](https://github.com/liumingjian/dbs-monitor/issues/113)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：把 [18](18-v1-delivery-boundary-bs-binary.md) §8 D7 明文移交的「三条承诺的**具体证据形式**」落成可自动判定的矩阵条目，并顺带切死启动失败语义中「连不上库」与「迁移执行失败」的分界。**本文不原地改写 20 / 21 / 22 / 23 / 24 任何一条**，只在矩阵上新增一个横切组。
> 输入边界（不重议）：[20](../acceptance/20-v1-acceptance-matrix.md) D3（三层分工）、D4（反假覆盖禁令）、D5（横切独立计分）、D6（粒度与 `pending` 政策）、D7（执行环境）、D8（时间不伪造、故障不模拟）；[21](../acceptance/21-v1-acceptance-entries-a.md) D1（加深基线准入）、D7（时间参数化取值表）、D8（`test_ref` 形态）、D9（真实手段的定性）；[24](../acceptance/24-v1-acceptance-entries-d.md) D7（`rides_on` 语义）、D14（执行序两条硬约束）；[18](18-v1-delivery-boundary-bs-binary.md) §6 D5 第 1 条与 §8 D7；[25](25-master-key-provenance-and-startup-failure.md) D4/D5/D7 与 §5.1 的异步自检序；[`04` §6](04-metric-storage-model.md) D6 分区与保留四条机制；[`14` §4](14-platform-observability-and-diagnostics.md) D4 磁盘分级；[`13`](13-credential-encryption-rotation-and-revocation.md) D7/D8（备份不含 keyring、遗失即不可恢复、无后门）；地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105) Notes 第 3 / 8 条。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**恢复门落成矩阵上第二个横切组 `REC-1..10`，全部 `baseline: true`，硬底 78 → 88、条目 81 → 91；启动失败按「失败性质」而非「启动阶段」两分——连不上平台库不拒启动（HTTP 起、健康 `FAILED`、业务端点 503、库回来后后台补跑迁移自愈），迁移执行失败拒绝启动（非零退出，绝不带着半个 schema 对外服务）；场上三把平台库 advisory lock（rotate / 迁移 / 进程）互相独立且行为各不相同；备份恢复断到「灌进一个真空 PG」，并把 `13` D7/D8 的「keyring 与库分开、遗失即不可恢复、无后门」正反两面都断死。**

---

## 1. D1 · 载体：矩阵上的第二个横切组，不拆进片、不退成人工检查表

**结论：新增独立横切组 `REC-1..10`，独立计分，硬底 78 → 88。**

三条路都摆过：拆进现有九片当加深条目、完全不进矩阵做成 [#114](https://github.com/liumingjian/dbs-monitor/issues/114) 的一道人工门、或独立成组。

理由与 [20](../acceptance/20-v1-acceptance-matrix.md) D5 给 `INV`/`BUILTIN` 的完全同构：这十条**跨片**（重启恢复同时牵动采集、告警、Agent 会话；备份恢复牵动全库），任何一片全绿都不构成它们的覆盖；且它们恰是「改坏了没人看得出来」那一类——没有人会在每次发版前手工灌一次 `pg_dump`。

- 拆进片里会让「恢复门是否达标」永远得靠人肉汇总九片，且每条都得在某片里找一个并不自然的归属。
- 不进矩阵则直接逃出 `make acceptance` 的自动判定，退回成人工检查表——那是本地图从一开始就在根除的形态。

**十条全部 `baseline: true`**，准入照 [21](../acceptance/21-v1-acceptance-entries-a.md) D1：每条都是某条**已经作出的产品承诺**在自动判定面上的**唯一**落点，没有一条是锦上添花的加深。逐条的唯一性见 §10 总表的 `reason` 列。

> 成本最高的两条是 `REC-9`（分区兜底）与 `REC-10`（进程锁）。它们曾被考虑降为允许 `pending` 的普通加深（硬底 86），**否决**：`REC-9` 恰恰是没有人会手工验的那种——`04` D6 机制 3 自称是「凌晨炸的最后一道防线」，一道从不执行的防线等于没有。

---

## 2. D2 · 迁移并发与进程并发：两层都做，三把锁互相独立

### 2.1 一手事实

`go.mod` 用 `github.com/pressly/goose/v3 v3.24.3`，该版本已提供 `WithSessionLocker` + `lock.NewPostgresSessionLocker()`（会话级 advisory lock），而 [`migrations/migrate.go`](../../migrations/migrate.go) 建 provider 时**没有启用**——当前是裸并发。两份迁移均无 `-- +goose NO TRANSACTION`，即每笔迁移单事务，**「部分应用」在单笔内不可能**，只可能发生在多笔之间。

### 2.2 两层各自的结论

| 层 | 结论 | 拿不到锁时 |
|---|---|---|
| 迁移 | goose provider 启用 `WithSessionLocker` | **等待**（自带重试），超时后按启动失败退出 |
| 进程 | server 启动在平台库上取一把 advisory lock | **立即拒绝启动**，并说明「已有 server 实例在运行」 |

地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105) 与 [18](18-v1-delivery-boundary-bs-binary.md) D5 都明说不做平台自身高可用/主备，所以「多副本」不是承诺形态。但**并发启动不需要多副本形态就会发生**：systemd 重启抖动、运维手滑双起、滚动替换二进制时的时序重叠。裸并发下两个进程同时 `CREATE TABLE` 的结果是随机一个报错退出，而那个错误面目全非——排查成本远高于一把锁。

进程锁是 [25](25-master-key-provenance-and-startup-failure.md) D5 的对称扩展：那里已经用平台库 advisory lock 把「轮换时忘了停 server」从静默数据损坏变成一句拒绝。同一台机器双起 server 的后果（两套采集调度器、两套分区维护循环、两套通知发送）比轮换更脏。

### 2.3 三把锁独立，且行为必须不同

**场上三把平台库 advisory lock：rotate 锁（`25` D5，本文不动）、迁移锁、进程锁。锁 ID 各不相同。**

**否决合成一把。** 拿不到锁时的正确行为三者不同：

- 迁移锁被占 = 「另一个进程正在把库带到我要的版本」，**等一下就好**，这正是滚动重启的正常时序；
- 进程锁被占 = 「已经有一个 server 在跑」，**等待毫无意义**，只会变成两个进程轮流抢；
- rotate 锁被占 = 「server 还在跑」，立即拒绝并提示先停机。

合成一把会让「正在迁移」与「已经在跑」这两个完全不同的处境退化成同一句错误信息。

**锁等待超时做成配置项**（外溢实现硬要求，§11 第 1 条），验收里参数化到秒级——否则 `REC-5` 跑不完。

---

## 3. D3 · 启动失败语义：按失败性质两分，不按启动阶段

### 3.1 三条既有结论的正面冲突

| 出处 | 原话 |
|---|---|
| [18](18-v1-delivery-boundary-bs-binary.md) §8 D7 | 迁移失败即拒绝启动：非零退出 + 明确日志，**绝不带着半个 schema 对外提供服务** |
| [18](18-v1-delivery-boundary-bs-binary.md) §6 D5 第 1 条 | DB 不可达时 HTTP 层照常起来，返回明确的「平台自身故障」页——**保留且强化** |
| [25](25-master-key-provenance-and-startup-failure.md) D4 | **唯一拒绝启动的情形是配置文件读不到** |

外部 PG 形态下「启动时连不上库」从边缘情况变成常态风险，三条必须有一个明确的切法。

### 3.2 结论

**按失败性质切，不按启动阶段切。**

| 情形 | 行为 |
|---|---|
| **连不上平台库**（网络不通、PG 没起、认证失败、库不存在） | **不拒启动**。HTTP 端口起、健康端点如实报 `FAILED`、业务端点 503 且响应体指名平台自身故障；后台按退避重连，**连上后补跑迁移并自愈**，不要求人工重启 |
| **连上了但迁移执行失败**（SQL 报错、脏状态、版本落后于二进制预期） | **拒绝启动**，非零退出，日志指名失败笔次 |
| **迁移锁拿不到** | 等待至超时；超时后归入「迁移执行失败」按拒绝启动处理 |
| **进程锁拿不到** | 立即拒绝启动（D2.2） |
| **配置文件读不到** | 拒绝启动（`25` D4 原结论，不动） |

**理由**：D5 第 1 条护的是「把平台挂了渲染成没有数据」这类谎言——库不可达时**最**需要那张平台故障页；而迁移执行失败是 **schema 与二进制不匹配**，此时起 HTTP 只会让 API 以未知方式半工作，正是 D7 要禁的。

「库可达但锁被占」既不是「连不上库」也不是「迁移执行失败」，故在表中单列，归入后者。

### 3.3 库不可达期间业务端点返回 503，不返回 13 码

**否决「各端点走各自的空状态 13 码」。** 13 码是**目标库**的空状态词汇表；用它描述**平台**故障，就是把两个域混为一谈——那正是全矩阵九条 `F4` 类条目整条存在的理由。也否决 500：500 是「操作失败」，而这里是「服务尚不可用」，语义上 503 才对。

**`REC-6` 的断言不写「可登录」。** 登录本身依赖平台库，库不可达时登录必然失败。`25` §5.2 那句「HTTP 起来、可登录」说的是 **keyring 坏而库好**的场景，两者不能照抄——这是本文对 `25` 的一处**适用面澄清**，不是推翻。

---

## 4. D4 · 备份恢复：语义覆盖门槛，且 keyring 分离正反两断

### 4.1 链条形态

```text
片条目全部跑完（库里已有真实业务数据）
  → pg_dump 平台库
  → 灌进 compose 里独立 profile 的空 restore-target 容器
  → server 配置指向新库、重启
  → 断言：配置 / 规则 / 未恢复告警 / 加密凭据 / 样本全可读，且采集继续出点
```

### 4.2 数据量门槛：语义覆盖，不是行数

**否决行数门槛。** 任何数字都是拍脑袋，且「一万行样本」证明不了什么。改为**每类事实至少一行真实数据**：

1. 至少一个实例（含加密凭据）；
2. 至少一条告警规则（含版本快照）；
3. 至少一条**未恢复**的告警实例 + 其 AlertEvent；
4. 样本**跨 ≥ 2 个分区**（分区跨度已由 [21](../acceptance/21-v1-acceptance-entries-a.md) D7 参数化到 1min，天然满足）。

这四类才是 restore 真会漏掉的东西——外键级联、分区表的子表是否随 dump 一起走、加密列是否原样还原。

数据由**片条目跑完后自然存在**，不额外造（承 [20](../acceptance/20-v1-acceptance-matrix.md) D4，`exceptions` 保持 `[]`）。

### 4.3 keyring 分离：正反两条都断

[`13`](13-credential-encryption-rotation-and-revocation.md) D7/D8 承诺备份**不含** keyring、遗失即不可恢复、无后门。恢复链是这条承诺唯一会被真正执行的场合，故正反都断：

- **正面**（`REC-3` 内）：带同一份 keyring 恢复 → 密文可解、采集正常出点；
- **反面**（`REC-4`）：换一份 keyring 或整个缺失 → **明确失败**，且**绝不生成新密钥**。

反面的失败形态**必须与 [25](25-master-key-provenance-and-startup-failure.md) D4 一致**：不是拒绝启动，而是起 HTTP + `keyring` 子系统取 `FAILED` + 解密类操作全部失败。`25` §5.3 已明确否决「keyring 整个不存在」的快速失败例外，本文不动它。

### 4.4 与 `AC-08-F4` 的划界

两条都碰 keyring 故障，重复计分等于虚增硬底。**按场景来源划死，两条的 `reason` 互指**：

| 条目 | 管什么 | 现场 |
|---|---|---|
| `AC-08-F4`（[24](../acceptance/24-v1-acceptance-entries-d.md)，**一字不动**） | **运行中**的 keyring 故障（权限错、文件被改）：不降格、不静默生成、恢复权限后版本号一致 | 库与密钥在同一台机器上 |
| `REC-4`（本文） | **恢复链上**的密钥—库分离：换一份或缺失时明确失败 | 库来自备份，keyring 来自另一台机器 |

---

## 5. D5 · 重启恢复：三项进断言，连接池不进，且 server / agent 分开

### 5.1 断言集边界

[#113](https://github.com/liumingjian/dbs-monitor/issues/113) 票正文列了四项，结论是**前三项进、连接池不进**：

| 项 | 进否 | 断什么 |
|---|---|---|
| 进行中的采集 | ✅ | 重启后调度器按**最新意图**续采，不补跑、不造占位样本 |
| 未决告警 | ✅ | 实例 ID 与首次触发时间保持、连续计数不重置、不谎报 `RECOVERED` |
| Agent 会话 | ✅ | token 仍有效、agent 无需重新接入；重启窗口的缺数按 T14 不进 `NO_DATA` |
| 连接池 | ❌ | 纯进程内实现细节，重启后必然重建，**外部无可观测语义** |

连接池不进这件事**写进 `REC-1` 的 `reason`**，避免下一个读矩阵的人以为漏了。

### 5.2 两种重启分开断

`REC-1` 断 server 重启，`REC-2` 断 agent 重启。agent 重启多一条断言：**重启期间的缺数是真缺数**，按缺桶呈现、不补 0（承 [`04`](04-metric-storage-model.md) 不补 0 与 [20](../acceptance/20-v1-acceptance-matrix.md) D2 `F2`）。合成一条会让「谁重启导致的缺数」这个区分永远测不出来——而那正是 T14 要分开的两个域。

---

## 6. D6 · 分区生命周期：只收肯定面，否定面不重复

[`matrix.yaml`](../../test/acceptance/matrix.yaml) 的 `AC-09-F5`（磁盘紧急拒写）已经断死了**否定面**：分区清单与最旧分区前后完全一致、绝不自动删旧分区、绝不缩短保留期。[`04`](04-metric-storage-model.md) §6 D6 的**肯定面**目前无落点，由 `REC-8` / `REC-9` 收下：

- `REC-8`：预建覆盖面 = 当前 + 7 个跨度（机制 1 的幂等 `CREATE TABLE IF NOT EXISTS`）；过期分区走 **`DROP` 而不是 `DELETE`**（机制 2，对账 `pg_class`——`DELETE` 会留下空表，这是唯一能把两者区分开的观测点）；维护循环失败时**平台健康出现 `分区维护` 事实源**且 journal 有结构化 error（机制 4，承 [`14`](14-platform-observability-and-diagnostics.md) §2）。
- `REC-9`：机制 3 的兜底——写入撞上无匹配分区 → 同步触发一次维护 → 重试一次成功。

**`REC-8` 的 `reason` 显式指向 `AC-09-F5`**，写明否定面不在此处重复。

### 6.1 `REC-9` 怎么造出「无匹配分区」

**手段：把分区维护间隔做成部署期配置项，验收里设成极大值使循环事实上停摆，再让时间走过预建边界。**

这与 [21](../acceptance/21-v1-acceptance-entries-a.md) D7 / [22](../acceptance/22-v1-acceptance-entries-b.md) 已批准的一整排参数化同源（保留期 2 分钟、`repeat_interval` 30s、快照截断 5）——**调配置项走的是与生产完全相同的判定代码路径**，这正是 [20](../acceptance/20-v1-acceptance-matrix.md) D8「时间参数化不伪造」的定义。

两条否决：

| 否决项 | 理由 |
|---|---|
| 加 test-only 开关关掉维护循环 | 往生产代码里种测试专用分支，[`10`](10-ai-guardrails-and-verification.md) 的护栏该拦 |
| DB 层手工 `DROP` 掉未来分区 | [20](../acceptance/20-v1-acceptance-matrix.md) D4 明令禁止 DB 层写操作，D3 已把「DB 层只读」写死 |

---

## 7. D7 · restore 靶库：独立 profile 的真空 PG，排在片条目之后

**结论：`compose.yaml` 新增 `restore-target` 空 PG 容器，独立 profile（与 [24](../acceptance/24-v1-acceptance-entries-d.md) 给 `postgres:12` 的处理同构），只在 `REC-3` / `REC-4` 执行时起。**

**否决「在平台库同实例上 `createdb` 一个空 database」**：看着省事，但它证明不了「灌进一个**空 PG**」——共用 initdb 参数、locale、编码、扩展、角色，恰好把 [18](18-v1-delivery-boundary-bs-binary.md) D7「库是自描述的」最容易翻车的那部分（依赖宿主库既有状态）整个掩盖掉。而这条承诺的全部价值就在客户拿一台全新机器恢复时成立。

**执行序**：`REC-3` / `REC-4` 排在**全部片条目之后、`AC-08-S7`（主密钥轮换）之前**。这样既白嫖片条目造出的真实数据（§4.2 的四类语义覆盖几乎自动满足），又不动 [24](../acceptance/24-v1-acceptance-entries-d.md) D14 已定的两条执行序硬约束。

矩阵执行序因此为：`AC-08-S1` 最先 → 其余片条目 → `REC-*` → `AC-08-S7` 最末。

---

## 8. D8 · REC 组一律独立执行，`rides_on` 留空，新增 `after` 字段

[24](../acceptance/24-v1-acceptance-entries-d.md) D7 给横切组定的是「自带断言集 + 搭车执行 + `rides_on` 记账」。**REC 组不搭车**：它的注入手段（杀进程、断库、灌 dump、停维护循环）会**改变现场**，与被搭车条目的执行相互污染。

- `rides_on` **保留字段并写 `[]`**，以示这是刻意的（对齐 `BUILTIN-2` / `BUILTIN-3` 的先例，那两条也是 `rides_on: []` + `reason` 说明独立执行）。
- `REC-3` / `REC-4` 对片条目的依赖是**执行序依赖**，不是搭车，用**新字段 `after:`** 记账。**不塞进 `rides_on`**：搭车是「在别人的执行现场上顺带断言」，排序是「必须等别人跑完」，两者混用会让 `rides_on` 这个刚定义一票的字段立刻失去单一含义。

---

## 9. D9 · 客户责任清单：本票只出条目与措辞硬要求，文档由 #116 持笔

**本票不建第二份部署前置条件文档。** [外部前置 PostgreSQL 的版本要求与部署前置条件 #116](https://github.com/liumingjian/dbs-monitor/issues/116) 正是管那份文档的票；两份并存必然分叉。本文只产出**单向输入**：

1. 备份频率、保留与**验证**由客户定，平台不做任何自动备份；
2. **keyring 必须与库分开备份**，遗失即密文数据**不可恢复**、平台**无后门**（[`13`](13-credential-encryption-rotation-and-revocation.md) D7/D8）；
3. 恢复步骤：灌库 → 把匹配该备份的 keyring 放回原路径、恢复属主与 `0600` → 启动（[25](25-master-key-provenance-and-startup-failure.md) D1.1 原话）；
4. 回滚 = 装回旧二进制 + 恢复备份，**不存在 down 迁移**（[18](18-v1-delivery-boundary-bs-binary.md) D7 保留 T8 D9.2）；
5. PG **大版本**升级是独立工程，不隐含在启动时的 goose 迁移里（[`09`](09-packaging-and-deployment.md) D4 的原则在外部 PG 形态下保留：goose 管 schema，不管 PG 大版本）；
6. 磁盘容量依据：30 天全量 30 个分区实测 **≈49.1 GB**（`docs/validation/t11-linux-amd64-progress.md`），作为对客户 PG 主机的容量前置要求；
7. 升级前**建议**手工备份控制面。

**两条措辞硬要求**：

- 第 2 条**必须出现「不可恢复」与「无后门」字样**。这是产品承诺不是免责话术，含糊化会给客户留下「找厂商捞一把」的错觉，而那一刻我们什么也做不了。
- 第 7 条**不得写成平台会做**。[18](18-v1-delivery-boundary-bs-binary.md) D7 已把 T8 D9.1「升级前自动备份控制面」作废并降为**给客户的建议**；文档里出现「平台自动备份」即与交付边界矛盾。

---

## 10. D10 · 条目总表：`REC-1..10`，硬底 78 → 88

全部 `baseline: true`、`slice: crosscut`、`kind: rec`、`rides_on: []`、`status: pending`（实现落地前占位，v1 判定时仍 `pending` 即未达标）。

| ID | 层 | 一句话 | 唯一性（为何升基线） |
|---|---|---|---|
| `REC-1` | api | server 重启：采集续采不补跑、未决告警实例 ID 与首触时间保持、Agent 会话免重接入 | 「库是自描述的、无外部隐藏状态」在进程侧的唯一验收面 |
| `REC-2` | api | agent 重启：缺数是真缺数、按缺桶呈现不补 0、不进 `NO_DATA` | 「谁重启导致的缺数」这一区分的唯一落点 |
| `REC-3` | api（含 DB 只读对账） | `pg_dump` → 空库 restore → 起 server → 直接可用（语义覆盖门槛） | [18](18-v1-delivery-boundary-bs-binary.md) D7 三条承诺的唯一端到端验收面 |
| `REC-4` | api | keyring 分离反面：换一份或缺失必须明确失败、绝不生成新密钥 | [`13`](13-credential-encryption-rotation-and-revocation.md) D7/D8「遗失即不可恢复、无后门」在恢复链上的唯一验收面 |
| `REC-5` | db | 两 server 进程同时冷启动：一个应用一个等锁、终态版本一致、无部分应用 | 迁移并发语义的唯一验收面 |
| `REC-6` | api | 平台库不可达启动：HTTP 起、健康 `FAILED`、业务端点 503、库回来后补跑迁移自愈 | [18](18-v1-delivery-boundary-bs-binary.md) D5 第 1 条「不许把平台挂了说成没数据」的唯一验收面 |
| `REC-7` | db | 迁移执行失败：非零退出、拒绝对外服务、日志指名失败笔次 | 「绝不带着半个 schema 对外服务」的唯一验收面 |
| `REC-8` | db | 分区肯定面：预建 = 当前 + 7 跨度、过期走 `DROP` 非 `DELETE`、维护失败进健康事实源 | [`04`](04-metric-storage-model.md) D6 机制 1/2/4 的唯一验收面（`AC-09-F5` 只管否定面） |
| `REC-9` | db | 分区兜底：无匹配分区 → 同步触发维护 → 重试一次成功 | [`04`](04-metric-storage-model.md) D6 机制 3 自称「凌晨炸的最后一道防线」，无人会手工验 |
| `REC-10` | api | 第二个 server 实例启动：进程锁拿不到，直接拒启动并说明原因 | 双起 server 的后果（两套调度器 / 维护循环 / 通知）无其他拦截点 |

**硬底 78 → 88，全矩阵条目 81 → 91，`n-a` 仍 5、`pending` 仍 2、`exceptions` 仍 `[]`。**

---

## 11. D11 · 外溢到实现的硬要求（五条 + 一条环境要求）

照 [21](../acceptance/21-v1-acceptance-entries-a.md)–[24](../acceptance/24-v1-acceptance-entries-d.md) 的惯例逐条列出，不藏：

1. [`migrations/migrate.go`](../../migrations/migrate.go) 给 goose provider 加 `WithSessionLocker`，**锁等待超时可配**（D2）；
2. server 启动取平台库 advisory lock，第二实例拒启动（D2 / `REC-10`）；
3. **分区维护间隔可配**（D6.1 / `REC-9`）；
4. 平台库不可达时业务端点 503 + 平台自身故障语义，且库恢复后**后台补跑迁移并自愈**，不要求人工重启（D3 / `REC-6`）；
5. 迁移执行失败非零退出、日志指名失败笔次，**不得**降级成 503 继续运行（D3 / `REC-7`）。

**环境要求**：`compose.yaml` 新增 `restore-target` 空 PG 独立 profile（D7）。

> 第 4 条是唯一有实现风险的一条：它意味着迁移不能只挂在启动路径上跑一次。替代形态「库回来后进程自行退出、交给 systemd `Restart=` 重来」曾被考虑并**否决**——那把一次短暂抖动放大成一次进程重启，会连带丢掉 `REC-1` 想保住的那些进行中状态。

---

## 12. 被本文取代 / 澄清的既有结论

| 出处 | 原结论 | 本文 |
|---|---|---|
| [25](25-master-key-provenance-and-startup-failure.md) D4 | 「**唯一**拒绝启动的情形是配置文件读不到」 | **取代**。拒启动情形扩为四类：配置文件读不到、迁移执行失败、迁移锁等待超时、进程锁被占。`25` 关于 **keyring 故障不拒绝启动**的结论**完全不受影响**（D3.2） |
| [18](18-v1-delivery-boundary-bs-binary.md) §8 D7 | 「迁移失败即拒绝启动」 | **澄清适用面**。「迁移**执行**失败」拒启动；「连不上库因而迁移尚未执行」不拒启动（D3.2） |
| [18](18-v1-delivery-boundary-bs-binary.md) §6 D5 第 1 条 | 「DB 不可达时 HTTP 层照常起来，返回明确的平台自身故障页」 | **保留并细化**：健康端点如实报 `FAILED`、业务端点 503 而非 13 码、不承诺「可登录」（D3.3） |
| [25](25-master-key-provenance-and-startup-failure.md) §5.2 | 「HTTP 起来、**可登录**、平台自身故障可见」 | **适用面澄清，不推翻**：那句描述的是 keyring 坏而库好的场景；库不可达时登录必然失败，`REC-6` 不断「可登录」（D3.3） |

`25` 的 D1/D2/D3/D5/D6/D7/D8 与 §5.3、[18](18-v1-delivery-boundary-bs-binary.md) 其余各条、[20](../acceptance/20-v1-acceptance-matrix.md)–[24](../acceptance/24-v1-acceptance-entries-d.md) 全部条目**一字未动**。`AC-08-F4` 与 `AC-09-F5` 保持原样（D4.4 / D6）。

---

## 13. 否决记录

| 被否决 | 出处 | 理由 |
|---|---|---|
| 恢复门拆进现有九片当加深条目 | D1 | 达标判定永远得靠人肉汇总九片 |
| 恢复门完全不进矩阵、做成 #114 的人工门 | D1 | 逃出自动判定即退回人工检查表 |
| `REC-9` / `REC-10` 降为允许 `pending` 的普通加深（硬底 86） | D1 | `REC-9` 恰是无人会手工验的那种；一道从不执行的防线等于没有 |
| 三把 advisory lock 合成一把 | D2.3 | 「正在迁移」与「已经在跑」会退化成同一句错误信息 |
| 迁移执行失败也做成「起 HTTP + `FAILED` + 业务 API 503」 | D3.2 | 那是把「拒绝服务」伪装成「在运行」 |
| 库不可达时各端点走各自的 13 码 | D3.3 | 13 码是目标库的空状态词汇表；用它描述平台故障就是把两域混为一谈 |
| 备份恢复设行数门槛 | D4.2 | 任何数字都是拍脑袋；会漏的是语义类别不是行数 |
| 在平台库同实例上 `createdb` 空库做 restore 靶 | D7 | 共用 initdb 参数 / locale / 扩展 / 角色，掩盖「库是自描述的」最容易翻车的那部分 |
| 加 test-only 开关关掉分区维护循环 | D6.1 | 往生产代码里种测试专用分支 |
| DB 层手工 `DROP` 未来分区制造无匹配分区 | D6.1 | [20](../acceptance/20-v1-acceptance-matrix.md) D4 禁止 DB 层写操作 |
| `REC-3` / `REC-4` 的执行序依赖写进 `rides_on` | D8 | 搭车与排序是两件事，混用会让刚定义一票的字段立刻失去单一含义 |
| 本票另建一份部署前置条件文档 | D9 | 与 #116 必然分叉 |
| 库恢复后进程自退、交给 systemd 重来 | D11 | 把短暂抖动放大成进程重启，丢掉 `REC-1` 要保住的进行中状态 |

---

## 14. 与下游票的界

| 本文（#113） | [#114](https://github.com/liumingjian/dbs-monitor/issues/114) | [#115](https://github.com/liumingjian/dbs-monitor/issues/115) | [#116](https://github.com/liumingjian/dbs-monitor/issues/116) |
|---|---|---|---|
| 恢复门有哪些条目、怎么判定、执行序在哪 | `REC` 组是否阻断、阈值、跑几轮 | 在哪台真机上跑才算数 | 客户责任清单落进哪份文档（本文 §9 是其单向输入） |
