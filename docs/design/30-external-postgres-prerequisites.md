---
status: active
kind: decision
note: 正文矩阵计数已过期，真值在 test/acceptance/matrix.yaml
---
# 30 · 外部前置 PostgreSQL 的版本要求与部署前置条件

> 出处：[外部前置 PostgreSQL 的版本要求与部署前置条件 #116](https://github.com/liumingjian/dbs-monitor/issues/116)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> **编号勘误**：本文最初在 `6692f51` 以 `27-external-postgres-prerequisites.md` 落盘，与 `9c5db89` 的 `27-v1-deliverables-and-candidate-provenance.md` 撞号（[29](29-production-security-boundary.md) §14 记为待处置项，建议重编更晚落盘的本份）。28/29 已被占用，故重编为 **30**。GitHub 票据与既有决策文档中的 `27-ext` / `27-external-postgres-prerequisites` 均指本文；除本条注记、标题编号与各文档链接目标外，内容一字未改。
> 定位：填 [18](18-v1-delivery-boundary-bs-binary.md) D2 留下的洞。该记录把平台库改为**客户自备的外部前置**、把 T8 D4（自带 PG 钉死 17、不接管客户既有 PG）**整条作废**并注明「版本要求另议」，同时在 §4 第 3 点把「要求专属实例 / 独立 database / 最小权限集 / 启动时校验前置条件并快速失败」四件事**显式移交**给本票。本文结案「另议」，并接下那四件事。
> **本文不原地改写 20 / 21 / 22 / 23 / 24 / 26 任何一条**，只在矩阵的既有 `REC` 横切组上追加三条。
> 输入边界（不重议）：[18](18-v1-delivery-boundary-bs-binary.md) D2（交付形态）、§6 D5 第 1 条、§8 D7；[25](25-master-key-provenance-and-startup-failure.md) D1（配置文件是规范来源）、D4（启动失败语义）；[26](26-data-and-recovery-gate.md) D2（三把 advisory lock）、D5（启动失败按失败性质两分）、D7（执行序）、§9（客户责任清单七条 + 两条措辞硬要求）；[20](../acceptance/20-v1-acceptance-matrix.md) D4/D5/D6/D8；[21](../acceptance/21-v1-acceptance-entries-a.md) D1（加深基线准入）、D8（`test_ref` 形态）；[24](../acceptance/24-v1-acceptance-entries-d.md) D7（`rides_on` 语义）、D14（执行序硬约束）；[19](19-agent-distribution-and-upgrade.md)（信任根与「全程无 `-k`」）；一手事实见 [平台库 PG13+ 的一手事实核实 #107](https://github.com/liumingjian/dbs-monitor/issues/107)。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**平台库钉死 PostgreSQL 17（17.x 任意小版本），主版本不是 17 即拒绝启动，不设任何逃生舱；承诺面要求客户提供专属 PG 17 实例，断言面只断到 database 级（专属 database + 独立 schema `dbsmon` + schema 内无非平台对象），两者的差额显式记账而非假装能断；启动前置校验按「能否自愈」而非「启动阶段」归位——版本 / 编码 / 权限 / schema 洁净 / TLS 生效属不可自愈，与迁移执行失败同侧拒启动，locale、时区、同实例多库、小版本落后属告警档只 `DEGRADED`；平台不需要 superuser、不需要任何扩展；「可用磁盘」这一项本票撤回，外部形态下平台断不了，只进客户责任清单；矩阵追加 `REC-11..13`，硬底 88 → 91、条目 91 → 94。**

---

## 1. D1 · 版本：钉死 PG 17，主版本不符即拒启动

**结论：平台库 = PostgreSQL 17，任意 17.x 小版本；`server_version_num` 不落在 17 主版本区间 → 拒绝启动。**

### 1.1 一手事实（[#107](https://github.com/liumingjian/dbs-monitor/issues/107)，findings 在 `research/plat-pg13-floor`）

- `date_bin()` 是 PG14 新增函数，[`internal/metric/queries.sql:21`](../../internal/metric/queries.sql) 已在用 ⇒ **技术下限 PG14**，PG13 上直接 `42883`。
- pgx 第一方只承诺 PG14+；goose 依赖 pgx v5，下限继承。
- PG13 已于 2025-11-13 EOL；PG14 亦临近到期；PG17 社区支持至 2029-11-08。
- **护栏漏洞**：sqlc 不连服务端，其解析器固定为 PG17 语法（pg_query_go v6 → libpg_query 17-latest）。写下目标库没有的函数时 `make gen` 全绿、只在运行期炸。

### 1.2 为什么是「钉死」而不是「区间 14–17」

决定这条的**不是下限，是 §1.1 最后那条护栏漏洞**。

1. **只有交付版本 = sqlc 解析器版本，`make gen` 那层保护才是真的。** 支持 14–17 区间意味着这层静态保护结构上是假的：任何人写一句 PG17-only 的语法，`make gen`、`make check` 全绿，在客户的 PG15 上运行期炸。要把这层保护补回来只能靠真跑四个版本的矩阵——而那笔成本会整个落到 [Go/No-Go 质量门组成 #114](https://github.com/liumingjian/dbs-monitor/issues/114) 上。
2. **验收矩阵是 94 条**（见 D11）。多支持一个版本就是多跑一遍 94 条，且每条都要判「这个版本上不适用算不算达标」。
3. **17 的社区支持到 2029-11-08**，对 v1 足够久。
4. **「客户自备」的语义是客户供给一台实例，不是我们迁就客户的存量。** 这与 D2 是同一个决策的两面：一旦开始迁就存量版本，就会接着迁就存量实例、存量扩展、存量 DBA 习惯，`04` 的全部存储结论逐条失效——那正是 T8 D4 四条理由要挡的东西，而 [18](18-v1-delivery-boundary-bs-binary.md) §4 第 3 点已明说「这些理由没有消失，只是变成了必须显式应对的前置条件」。

**技术下限 PG14 只写进本文作为「为什么不能更低」的解释，不构成任何支持承诺。** 「v1 之后是否放宽版本上界（PG18+）」是新决策，已进地图 **Not yet specified**。

> **不要串台**：**被监控** PG 为 13–17（[06](06-metric-dictionary-and-collection-plan.md) §5.1，PG12 接入即拒），与本条无关。本条只管**平台自身**那个库。

---

## 2. D2 · 独占边界：承诺面写实例级，断言面写 database 级，差额显式记账

**结论：文档要求客户提供 (a) 专属 PG 实例；启动硬门只断到 (b) 专属 database + 独立 schema + schema 内无非平台对象；同实例存在其他非系统 database → 告警档，不拒启动。**

三档摆过：(a) 专属实例、(b) 专属 database 接受共用实例、(c) 共用 database 下的独立 schema。

**为什么承诺面与断言面不重合，且必须承认这一点**：平台连过去只是一个普通角色。它**能**可靠断言 (b)/(c)——查 `pg_database`、查 owner、查目标 schema 里有没有别人的表；它**断不死** (a)——同实例里别的库、别的连接、别的 DBA 半夜的 `ALTER`，平台看得到一部分（`pg_database` 列表）却完全管不着。

- 把承诺面降到 (b) 以求「说到做到」是错的：`04` 的分区预建、`DROP` 滚动删除、goose 启动自动迁移全部建立在**这台机器的资源归平台**这个前提上，共用实例下一个邻居的批量作业就能让分区维护超时。承诺面必须写实例级。
- 把断言面抬到 (a) 去做「实例洁净度检查」也是错的：查得到的只有 `pg_database` 列表，据此拒启动会把「客户在同实例上放了一个 `pgbouncer` 的配置库」变成部署失败，而它并不真正威胁我们；据此不拒启动又等于没查。**折中不是模糊，是分档**：它进告警档（D3.2）。
- **差额本身写进文档**：客户责任清单第 ① 条写「提供专属实例」，同时写明「平台只能验到 database 级，实例级的独占性由客户保证」。假装能断，比不断更危险。

`04` D6 的分区机制、goose 自动迁移的正确性，此后建立在「**这个 database 归平台独占**」上——那是真能断的那一层。

---

## 3. D3 · 启动前置校验：按「能否自愈」归位，三档，外加自愈路径上的那道缝

[26](26-data-and-recovery-gate.md) D5 已定「连不上平台库不拒启动（HTTP 起、健康 `FAILED`、业务端点 503、库回来后后台补跑迁移自愈），迁移**执行**失败拒启动」。前置校验是第三种东西，必须给它归位。

**归位原则：按能否自愈分，不按启动阶段分。** 版本不对、编码不对、没有 `CREATE` 权限——重试一万次结果一样，属「迁移执行失败」那一侧；连不上是瞬态，属 503 那一侧。这条原则同时解释了为什么 `26` D5 那两侧是那样切的，两文一致。

**校验点：连上库之后、跑迁移之前。**

### 3.1 拒启动档（非零退出，日志指名具体项）

| 项 | 判据 | 为什么在这一档 |
|---|---|---|
| 主版本 | `server_version_num` 不在 17 主版本区间 | D1 的直接落地；不拒就等于 D1 没有 |
| 数据库编码 | `pg_database.encoding` ≠ `UTF8` | 平台存中文实例名与告警文案；非 UTF8 是数据损坏级问题，不可自愈 |
| 建表权限 | 平台角色对目标 schema 无 `CREATE` | 迁移必然失败；早一秒说，比在 goose 半路上说清楚得多 |
| schema 洁净 | 目标 schema 内存在**非平台对象** | **这条比版本更能救命**：它拦的是「配置指错了库」——指到客户的业务库上，goose 会在别人的 schema 里建表 |
| TLS 生效 | 自身 backend 在 `pg_stat_ssl` 中 `ssl = false` | 见 D5 |

### 3.2 告警档（warn + 平台事件 + 健康 `DEGRADED`，不拒启动）

| 项 | 为什么不升到硬门 |
|---|---|
| collation provider / locale 非 `C` / `POSIX` | 平台库里全是英文标识符、UUID 与时间戳；唯一受影响的是 `instance.name` 的**展示排序**，不影响正确性。[09](09-packaging-and-deployment.md) 里 `--without-icu` + libc + `C` locale 那段论证在外部形态下依然成立，只是它现在是**客户的 initdb 参数**，我们只能观察不能规定 |
| `TimeZone` 非 UTC | 分区边界由 Go 侧按 UTC 计算（[`internal/metric/partitions.go:65`](../../internal/metric/partitions.go)），`date_bin` 走 `timestamptz` 绝对时刻，均不受会话时区影响。升成硬门属于无根据的严格 |
| 同实例存在其他非系统 database | D2 的承诺面与断言面之差额，正是这一格 |
| PG 小版本落后 | 是运维建议，不是正确性问题 |

### 3.3 撤回项：可用磁盘——外部形态下平台断不了

**[#116](https://github.com/liumingjian/dbs-monitor/issues/116) 正文把「可用磁盘」列进启动校验清单，本文撤回该项。**

外部 PG 上平台是普通角色：看得到 `pg_database_size` 与表空间大小，**看不到主机的可用空间**（那需要 superuser + `pg_ls_dir` 一类，而 D4 已承诺不要 superuser，且托管服务上根本没有）。留着它只会长成一段「查了个假数、绿了个假门」的代码。

⇒ 容量只进客户责任清单（[26](26-data-and-recovery-gate.md) §9 第 6 条，30 天全量 30 个分区实测 **≈49.1 GB**）。这与 [14](14-platform-observability-and-diagnostics.md) §4 的磁盘分级不冲突：那管的是**平台 server 主机自己**的磁盘，仍然能看能报。

### 3.4 自愈路径上的那道缝

`26` D5 定了「库回来后后台补跑迁移自愈」。**那条路径上也要跑这套前置校验，而此时进程已在对外 503，无处可"拒启动"。** 不定死这一格，实现时只有两种坏收场：要么在自愈路径上跳过校验（硬门半途失效），要么在自愈路径上调 `os.Exit`（把一个已经在服务的进程杀掉）。

**结论：自愈路径上前置校验失败 → 不退出进程；健康转 `FAILED` 并指名具体项；退避重试；发平台事件，绝不静默。** 落成 `REC-13`，与断「退出」的 `REC-11` 互斥并存。

---

## 4. D4 · schema 归属与最小权限集：独立 schema `dbsmon`，不要 superuser

**结论：独立 database（名由配置给）+ 独立 schema `dbsmon`（不用 `public`）；`search_path` 在连接串上显式钉死；平台角色只需该 schema 的 owner。**

### 4.1 一手事实

`migrations/00001_walking_skeleton.sql` 与 `00002_collection_plan.sql` **零 `CREATE EXTENSION`**；`uuid` 主键由 Go 侧生成（DDL 中无 `gen_random_uuid()` 默认值）；[26](26-data-and-recovery-gate.md) D2 的三把 advisory lock 用 `pg_advisory_lock` 系列，任何角色可调。现有 migrations 与 sqlc 生成物**全部无 schema 限定**，等于隐式吃 `public`。

### 4.2 为什么是独立 schema 而不是 `public`

1. PG15+ 已收紧 `public` 的默认权限，靠 `public` 会让「需要什么权限」随客户的 PG 版本与既有 grant 漂移。
2. **只有独立 schema 才让 D3.1 那条「schema 内无非平台对象」可判定**——在 `public` 上这条判据几乎必然误报。
3. 让 D2 的 (b) 档在技术上不是灾难（虽然文档不推荐共用 database）。

**SQL 一句不改**：`search_path=dbsmon` 写在连接串上即可，不必逐条加 schema 限定。这是本票唯一一处外溢到实现的 schema 相关要求（D12 第 1 条）。

### 4.3 最小权限集

**平台角色 = 目标 schema 的 owner。不需要 superuser、不需要 `CREATEDB`、不需要 `CREATEROLE`、不需要任何扩展。**

**「平台不需要 superuser」写成文档里的一条正式承诺**——这是客户 DBA 审这类平台时问的第一个问题，也是外部前置形态相对「整包塞一个 PG 进去」白送的一个真实优势（[25](25-master-key-provenance-and-startup-failure.md) §威胁模型已记过一次同类增强）。

---

## 5. D5 · 连接平台库的 TLS：配置期与运行期两处都断

地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105) Notes 第 5 条已定「连接平台库改 TCP 强制 TLS」。**但强制 TLS 是配置意图，能不能被断言是另一回事。**

**风险具体形态**：pgx 的 `sslmode` 写成 `prefer` 时，服务端不支持 TLS 就**静默降级成明文**，不报错。而平台库密码此刻正明文躺在 `0600` 配置文件里（[25](25-master-key-provenance-and-startup-failure.md) D6 已把「配置文件泄露 = 平台库凭据泄露」写进不保护面）——连接一降级，它就裸奔上网。这正是「配置写对了才安全」那一类，本仓库一贯拒绝。

**结论：两处都断，一处管意图、一处管事实。**

1. **配置解析期**：`sslmode` 弱于 `verify-full` 一律**拒收**（`disable` / `allow` / `prefer` / `require` / `verify-ca` 全部拒绝），理由与 [19](19-agent-distribution-and-upgrade.md) 的「全程无 `-k`」同源。
2. **连接建立后**：查 `pg_stat_ssl` 中自身 backend 的 `ssl`，为 `false` → **拒绝启动**（D3.1 末行）。配置对了不代表握手成功。

---

## 6. D6 · 客户升级其 PG 大版本时，平台承诺什么

**三条，第三条必须正面写死：**

1. **大版本升级是客户的独立工程**，平台不参与、不隐含在启动时的 goose 迁移里。T8 D4 那句「goose 管 schema，不管 PG 大版本」的**原则**在本形态下保留（条款本身已随 T8 D4 作废）；[26](26-data-and-recovery-gate.md) §9 第 5 条已是底稿。
2. **升级前必须停平台 server**——连接与 [26](26-data-and-recovery-gate.md) D2 的三把 advisory lock 都在场上。
3. **升级后主版本若离开 17，平台直接拒绝启动。** 这是 D1 硬门的必然后果，也是钉死 17 最刺痛的地方，**必须在客户文档里正面写死，不许藏**（措辞硬要求见 D10）。

---

## 7. D7 · 开发与 CI 触及的一切平台库必须是 17：原则归本票，落地归 #114

[#114](https://github.com/liumingjian/dbs-monitor/issues/114) 正文点名「必须决定 `make check` / `make dev-up` 所用 PG 版本与交付版本的绑定关系，否则 sqlc 那层保护是假的」。

**本票定死原则**：既然交付钉死 17（D1），则 `make dev-up`、`make check`、`make check-full`、`make acceptance` 以及 CI 中**一切平台库实例必须是 PG 17，这是硬门不是建议**。任何一处漂移都会让 D1 §1.2 第 1 条的全部理由落空——护栏的价值来自「开发期用的就是交付的那个解析器版本」。

**落地归 [#114](https://github.com/liumingjian/dbs-monitor/issues/114)**：跑在哪个 job、compose profile 怎么写、版本漂移怎么被自动断（是 `make check` 里加一句断言，还是 CI 层校验镜像 tag）。#114 由此接到的是**已决策的输入**，不是开放题。

> **与既有 profile 不冲突**：[#113](https://github.com/liumingjian/dbs-monitor/issues/113) 已定 `compose.yaml` 增 `postgres:12`（**目标库**，供接入即拒用）与 `restore-target`（**恢复靶库**）两个 profile；D12 再增一个**非 17 平台库** profile（`postgres:16`，供 `REC-11` 用）。三者角色互不相干，不要混为一谈。

---

## 8. D8 · 承诺面边界：托管 PG 服务不禁止、不背书

客户「自备一台 PG 17」时，相当一部分会直接开一个云托管实例（RDS / 各家云 PG）。

**我们的硬门没有一条会拦住它**：不要 superuser（D4）、零扩展、schema owner 拿得到、advisory lock 可用。所以它**技术上会跑起来**——问题从来不是能不能跑，是跑起来之后我们承诺了什么。

**真实差异在 [26](26-data-and-recovery-gate.md) 的 `REC-3`**：「`pg_dump` → 灌进一个**真空 PG** → 起 server → 直接可用」这条端到端承诺，在托管服务上的恢复路径是客户的控制台快照，跟我们验的**不是同一条链**。

**结论：不禁止、不背书。** `27-` 与客户文档明写：v1 的承诺面是**客户自管的专属 PG 17 实例**；托管服务未被禁止，硬门也不会拦它，但 **v1 未在托管服务上产出任何 Go/No-Go 证据，`REC-3` 的恢复承诺在托管形态下不适用**。

**不加检测去识别托管服务**：识别不可靠（各家伪装程度不同），且会变成猫鼠游戏。「是否把某家托管服务纳入承诺面」是 v1 之后的新决策，已进地图 **Not yet specified**。

---

## 9. D9 · 硬门不设逃生舱，一个都不给

**结论：不提供 `--skip-preflight`、不提供任何 override 配置项。唯一的"逃生舱"是改配置指向一个合格的库。**

客户现场撞上拒启动，第一反应一定是找开关——**有开关的硬门就是建议**。客户打开它跑在 PG16 上，D1 §1.2 第 1 条的护栏立刻回到「`make gen` 全绿、运行期炸」的原状，而钉死 17 的**全部理由**就是让那层护栏为真。

宁可让客户在**部署期**撞墙（可诊断、日志指名具体项、文档有对应条目），也不要在半夜某条 SQL 上炸。

---

## 10. D10 · 客户责任清单：本文定条目与措辞，成文归 #110

[26](26-data-and-recovery-gate.md) §9 已给七条 + 两条措辞硬要求，并声明「本票不建第二份部署前置条件文档，#116 正是管那份文档的票」。本文**原样接收那七条，不改写一字**，在其上补齐外部 PG 形态自己的五条。

### 10.1 接收自 [26](26-data-and-recovery-gate.md) §9（原样，编号沿用）

1. 备份频率、保留与**验证**由客户定，平台不做任何自动备份；
2. **keyring 必须与库分开备份**，遗失即密文数据**不可恢复**、平台**无后门**；
3. 恢复步骤：灌库 → 把匹配该备份的 keyring 放回原路径、恢复属主与 `0600` → 启动；
4. 回滚 = 装回旧二进制 + 恢复备份，**不存在 down 迁移**；
5. PG **大版本**升级是独立工程，不隐含在启动时的 goose 迁移里；
6. 磁盘容量依据：30 天全量 30 个分区实测 **≈49.1 GB**；
7. 升级前**建议**手工备份控制面。

### 10.2 本票新增（编号续）

8. 客户提供**专属 PostgreSQL 17 实例**（17.x 任意小版本）。平台只能验到 database 级，**实例级的独占性由客户保证**（D2 的差额，明写不藏）。
9. 该实例的 `initdb` 参数、备份、监控、补丁与升级节奏全部由客户负责；平台只在启动时**观察并报告**编码 / locale / 时区（D3.2），不代管。
10. **主机可用空间由客户保证**——外部形态下平台**断不了**这一项（D3.3），容量依据见第 6 条。
11. **升级到 PG 18+ 会导致平台拒绝启动**（D6 第 3 条）。
12. 平台角色**不需要 superuser**，只需目标 schema 的 owner；平台不安装任何 PostgreSQL 扩展（D4.3）。

### 10.3 措辞硬要求

在 [26](26-data-and-recovery-gate.md) §9 两条之上加第三条：

- （承自 `26`）第 2 条**必须出现「不可恢复」与「无后门」字样**。
- （承自 `26`）第 7 条**不得写成平台会做**。
- （**本票新增**）**第 11 条不得软化成「建议不要升级」。** 它是硬门的事实后果，不是运维建议。软化就是骗人——客户按"建议"的口气理解，升完发现平台起不来，那一刻我们既没提醒过也没有开关可给（D9）。

**成文归 [v1 交付物与候选留痕 #110](https://github.com/liumingjian/dbs-monitor/issues/110)**：本文只定「必须原样出现的条目与措辞」，不写面向客户的成品文档——两份并存必然分叉，正是 `26` §9 拒绝自己动笔的同一个理由。

---

## 11. D11 · 矩阵条目：`REC` 组追加三条，硬底 88 → 91、条目 91 → 94

**结论：不开第三个横切组，追加进既有 `REC` 组。**

`REC` 组（[26](26-data-and-recovery-gate.md) D1）已经在收启动语义（`REC-6` 库不可达、`REC-7` 迁移执行失败），前置校验是同一族的第三面，同组自然。开第三个横切组只会让「启动语义达标了吗」这个问题分散到两处。

**现有 91 条里没有落点**：`REC-6` 是「库**不可达**」、`REC-7` 是「迁移**执行**失败」、`AC-08-F4` 是 keyring——三条硬门（版本 / 编码 / schema 洁净 / TLS）、告警档、自愈路径全部无处可落。写进文档而不进矩阵，等于没写。

三条全部 `baseline: true`，准入照 [21](../acceptance/21-v1-acceptance-entries-a.md) D1（每条是某条已作出承诺在自动判定面上的**唯一**落点）：

| ID | 层 | 一句话 | 唯一性（为何升基线） |
|---|---|---|---|
| `REC-11` | db | 前置不满足（非 17 / schema 内有非平台对象）→ 非零退出、日志指名具体项 | D1 硬门与 D3.1 的唯一验收面；不断这条，硬门与建议无法区分 |
| `REC-12` | api | 告警档（非 `C` locale / 同实例多 database）→ **起得来**、健康 `DEGRADED`、平台事件含该项、业务端点可用 | 守「告警档不许悄悄升级成硬门」——反向失效同样是产品事故（客户库跑得好好的却起不来） |
| `REC-13` | api | 库回来后自愈路径上前置校验失败 → **不退出**、健康 `FAILED` 指名项、退避重试、平台事件 | D3.4 那道缝的唯一验收面；`REC-11` 断「退出」、本条断「不退出」，互斥，**不能合并** |

**`REC-11` 与 `REC-13` 不能合并**：一条断进程必须退出、一条断进程必须不退出，塞进一条会让断言互相打架，且实现上最容易犯的错恰恰是把两条路径写成同一段代码。

**全矩阵终态：94 条条目、91 条硬底，`n-a` 仍 5、`pending` 仍 2、`exceptions` 仍 `[]`。** 三条 `rides_on: []`（`REC` 组一律独立执行，[26](26-data-and-recovery-gate.md) D7），`after` 按执行序全序落在 `REC-*` 段内。

---

## 12. D12 · 外溢到实现的硬要求（六条 + 一条环境要求）

1. **连接串显式钉 `search_path=dbsmon`**，目标 schema 由配置给；SQL 与 sqlc 生成物**一句不改**（D4.2）。
2. **前置校验模块置于「连上库之后、跑迁移之前」**，三档行为按 D3.1 / D3.2 分流；拒启动档**非零退出且日志指名具体项**（不是笼统的「前置检查失败」）。
3. **自愈路径复用同一段校验代码，但失败处理不同**：不退出、健康 `FAILED`、退避、发事件（D3.4）。同一段判据、两套失败处理，**不许写成两份判据**。
4. **`sslmode` 弱于 `verify-full` 在配置解析期拒收**；连接建立后查 `pg_stat_ssl` 断自身 backend `ssl = true`（D5）。
5. **不新增任何跳过前置校验的开关或配置项**（D9）。
6. **告警档的每一项必须落成一个可枚举的平台事件**（[14](14-platform-observability-and-diagnostics.md) 的事实源），否则 `REC-12` 的「事件含该项」无从断起。

**环境要求一条**：`compose.yaml` 新增一个**非 17 平台库** profile（`postgres:16`），供 `REC-11` 用。与 [#113](https://github.com/liumingjian/dbs-monitor/issues/113) 的 `postgres:12`（目标库）、`restore-target`（恢复靶库）互不相干。

---

## 13. 与既有文档的关系（记账）

**本文不 supersede 任何在效条款**——[18](18-v1-delivery-boundary-bs-binary.md) D2 已经清过场，没有可推翻的东西。只做两件事：

| 动作 | 对象 | 说明 |
|---|---|---|
| **接管** | [18](18-v1-delivery-boundary-bs-binary.md) §4 第 3 点显式移交的四件事 | 「专属实例 / 独立 database / 最小权限集 / 启动时校验前置条件并快速失败」→ 本文 D2 / D4 / D3 |
| **结案** | [18](18-v1-delivery-boundary-bs-binary.md) 表格中 T8 D4 一行的「（版本要求另议）」 | 「另议」在此结案：D1 |
| **指向改写** | [04](04-metric-storage-model.md) §7.4 末句「具体版本由 T8 定」 | 该指向现已悬空（T8 D4 已被 `18` 作废）。**平台库版本的规范来源改为本文 D1**。遵守「决策文档不原地改写」，**不改 `04` 原文**，只在此记账 |
| **追加** | `test/acceptance/matrix.yaml` | 只追加 `REC-11..13` 与头部统计注释；20 / 21 / 22 / 23 / 24 / 26 号既有条目**一字未动** |

**不受影响、明确点名**：[26](26-data-and-recovery-gate.md) D5 的两分（本文 D3 与之同源并延伸，不推翻）、D2 三把 advisory lock、§9 七条（本文 §10.1 原样接收）；[25](25-master-key-provenance-and-startup-failure.md) 全部条款（keyring 故障不拒绝启动与本文的前置校验拒启动分属不同判据，互不冲突）；[06](06-metric-dictionary-and-collection-plan.md) §5.1 被监控 PG13–17 那条独立版本线。

---

## 14. 否决记录

| 被否决 | 出处 | 理由 |
|---|---|---|
| 平台库支持 PG 14–17 区间 | D1 | sqlc 解析器固定 PG17，区间支持会让 `make gen` 的护栏结构上为假；补回来只能真跑四版本 × 94 条 |
| 承诺面降到「专属 database」以求说到做到 | D2 | `04` 的分区与维护结论建立在资源独占上；共用实例下一个邻居的批量作业就能让维护超时 |
| 断言面抬到实例级做「实例洁净度检查」 | D2 | 只查得到 `pg_database` 列表，据此拒启动会把无害的邻居库变成部署失败 |
| 把「可用磁盘」留在启动校验清单里 | D3.3 | 外部 PG 上平台是普通角色，看不到主机可用空间；留着会长成一段查假数、绿假门的代码 |
| 用 `public` schema | D4.2 | PG15+ 权限已收紧；且「schema 内无非平台对象」这条判据在 `public` 上必然误报 |
| 只在配置解析期断 `sslmode` | D5 | 配置对了不代表握手成功；`prefer` 会静默降级成明文，而平台库密码正明文躺在配置文件里 |
| 提供 `--skip-preflight` 逃生舱 | D9 | 有开关的硬门就是建议；客户打开它跑在 PG16 上，D1 的全部理由当场落空 |
| 检测并拒绝托管 PG 服务 | D8 | 识别不可靠且是猫鼠游戏；正确做法是划清承诺面而不是加检测 |
| 前置校验开第三个横切组 | D11 | `REC` 已在收启动语义，分两处会让「启动语义达标了吗」无法一处回答 |
| `REC-11` 与 `REC-13` 合并成一条 | D11 | 一条断必须退出、一条断必须不退出；合并会让断言互相打架，而这恰是实现最容易写错的地方 |
| 由本票直接写面向客户的部署前置条件成品文档 | D10 | 与 #110 两份并存必然分叉，正是 `26` §9 拒绝自己动笔的同一个理由 |

---

## 15. 移交

| 收方 | 内容 |
|---|---|
| [#114](https://github.com/liumingjian/dbs-monitor/issues/114) Go/No-Go 质量门组成 | D7 的落地形态：PG 17 绑定跑在哪个 job、compose profile 怎么写、版本漂移怎么自动断；`REC-11..13` 进哪一层门 |
| [#110](https://github.com/liumingjian/dbs-monitor/issues/110) v1 交付物与候选留痕 | D10 的成文：客户责任清单 12 条 + 三条措辞硬要求，落成面向客户的部署前置条件文档 |
| [#115](https://github.com/liumingjian/dbs-monitor/issues/115) 真 Linux 环境适配与最终验收 | `REC-11..13` 在真机上的执行归属 |
| 地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105) **Not yet specified** | ① v1 之后是否放宽平台库版本上界（PG18+）；② 是否把某家托管 PG 服务纳入承诺面 |
