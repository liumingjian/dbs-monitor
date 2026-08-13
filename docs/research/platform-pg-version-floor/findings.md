# 平台自身元数据库的 PostgreSQL 版本下限 · 一手事实核实

> 研究票：[#107 · 平台库 PG13+ 的一手事实核实](https://github.com/liumingjian/dbs-monitor/issues/107)（父票 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)）
> 核实日期：2026-08-13。所有版本分界均引自 postgresql.org 版本号页面 / 官方 release notes / 上游仓库源码与 README，二手文章一律不作为依据。
> 定位：回答「**平台自己那台 PG** 的版本下限承诺为 PG13+ 是否成立」。不重议被监控端矩阵。

---

## 0. 一句话结论

**「平台元数据库 PG13+」不成立，且这条承诺在本仓库里从来没有被文档支持过——它是把「被监控端 PG13–17」误读到平台端的结果。**

三条互相独立的一手证据各自都足以否掉 PG13：

1. **能力硬缺失**：`date_bin()` 是 PG **14** 新增函数，PG13 没有。而 `internal/metric/queries.sql:21` 已经在用它。
2. **驱动不承诺**：`jackc/pgx` 官方 README 明写只支持 **PostgreSQL 14 及以上**；本仓库的 `goose` 也是通过 pgx v5 连库。
3. **PG13 已 EOL**：官方 versioning 页给出 PG13 末版日期 **2025-11-13**，截至今日已停止支持 9 个月。

**现有决策文档本就写的是 ≥14 / 钉死 17**，见 `docs/design/04-metric-storage-model.md` §7.4 与 `docs/design/09-packaging-and-deployment.md` §4。因此本研究**不产生任何决策变更**，只需纠正「平台库 PG13+」这个表述错误。参见 §6 的动作建议。

---

## 1. 先确认 T2 到底依赖了哪些 PG 能力

读 `docs/design/04-metric-storage-model.md`，平台库侧被实际依赖的能力清单如下（每条给出文档落点）：

| # | 能力 | T2 落点 |
|---|---|---|
| C1 | `CREATE TABLE ... PARTITION BY RANGE (ts)` 原生声明式分区 | §2 D2 |
| C2 | `CREATE TABLE IF NOT EXISTS ... PARTITION OF ... FOR VALUES FROM (...) TO (...)`（幂等预建） | §6 机制 1 |
| C3 | `DROP TABLE` 掉整个分区做滚动删除 | §6 机制 2 |
| C4 | 父表建索引自动下发到各分区 | §7 D7 |
| C5 | 分区裁剪（含带时间下界的最新值查询） | §7.3 纪律二 |
| C6 | `date_bin(interval, ts, origin)` 分桶 | §7.2、§7.4 |
| C7 | 「无匹配分区」错误的稳定 SQLSTATE（T2 自陈**无一手确认**，推测 23514） | §6 机制 3 |
| C8 | `metric_series` 上的 `UNIQUE (instance_id, metric_id, labels_key)` + `INSERT ... ON CONFLICT ... RETURNING`（**非分区表**） | §3 D3 |
| C9 | `instance_id ... REFERENCES instance(id) ON DELETE CASCADE`（**非分区表间的外键**） | §2 D2 |
| C10 | 独立 autovacuum 参数（表级 storage parameter） | §8.3 |

**T2 没有依赖的能力**（核实版本分界前先排除，避免做无用功）：

- **不依赖 `ATTACH PARTITION`**：分区一律 `CREATE TABLE ... PARTITION OF` 直接建，从不先建独立表再挂。
- **不依赖 `DETACH PARTITION`（更不依赖 `DETACH CONCURRENTLY`）**：§6 机制 2 明写「滚动删除用 `DROP TABLE`」。
- **不依赖默认分区**：按天预建 7 天，没有兜底分区（§6 机制 3 反而依赖「无分区就报错」这一行为，建了默认分区会把该防线吃掉）。
- **不依赖分区表上的 UNIQUE / PK**：§2 D2 明写「样本表**不建唯一约束**」。
- **不依赖分区表上的外键**：唯一的外键在 `metric_series → instance`，两边都不是分区表。
- **不依赖逻辑复制**：整包交付单机自建 PG（`docs/design/09-packaging-and-deployment.md`），无复制拓扑。

⇒ 因此下面对 `DETACH CONCURRENTLY` / 分区表外键 / 逻辑复制的版本分界核实，属于**旁证**：即使 PG13 缺，也不影响 T2。真正的成败点只有 C6（`date_bin`）和 C7（SQLSTATE）。

---

## 2. PG13 上的原生声明式分区：逐条核实

### 2.1 PG13 **具备** C1–C5、C8–C10

| 能力 | PG13 上是否具备 | 一手出处 |
|---|---|---|
| C1 `PARTITION BY RANGE` | 具备 | [PG13 · CREATE TABLE](https://www.postgresql.org/docs/13/sql-createtable.html) 语法图含 `PARTITION BY { RANGE \| LIST \| HASH } (...)` |
| C2 `CREATE TABLE IF NOT EXISTS ... PARTITION OF` | 具备 | [PG13 · CREATE TABLE](https://www.postgresql.org/docs/13/sql-createtable.html) 语法原文：`CREATE [ ... ] TABLE [ IF NOT EXISTS ] table_name PARTITION OF parent_table [ ( ... ) ] { FOR VALUES partition_bound_spec \| DEFAULT }`。`IF NOT EXISTS` 与 `PARTITION OF` 在同一条产生式里，**幂等预建在 PG13 合法** |
| C3 `DROP TABLE` 删分区 | 具备 | [PG13 · 5.11 Table Partitioning](https://www.postgresql.org/docs/13/ddl-partitioning.html) 原文：“Dropping an individual partition using `DROP TABLE`, or doing `ALTER TABLE DETACH PARTITION`, is far faster than a bulk operation.” —— 与 T2 §6 机制 2 的措辞一致 |
| C4 父表索引自动下发 | 具备 | [PG13 · 5.11](https://www.postgresql.org/docs/13/ddl-partitioning.html) 原文：“Create an index on the key column(s) ... on the partitioned table. ... **This automatically creates a matching index on each partition, and any partitions you create or attach later will also have such an index.**” |
| C5 分区裁剪 | 具备 | [PG13 · 5.11.4 Partition Pruning](https://www.postgresql.org/docs/13/ddl-partitioning.html#DDL-PARTITION-PRUNING)；[PG13 release notes](https://www.postgresql.org/docs/release/13.0/) 还进一步扩大了裁剪场景：“Allow pruning of partitions to happen in more cases” |
| C8 非分区表 UNIQUE + ON CONFLICT | 具备 | 与分区无关，PG9.5 起即有 |
| C9 非分区表外键 | 具备 | 与分区无关 |
| C10 表级 autovacuum 参数 | 具备 | [PG13 · CREATE TABLE Storage Parameters](https://www.postgresql.org/docs/13/sql-createtable.html) |

**分区规模指引在 PG13 就是这句**（T2 §6 引用的「a few thousand」确有其文，且版本无关）：

> “The query planner is generally able to handle partition hierarchies with up to **a few thousand partitions** fairly well, provided that typical queries allow the query planner to prune all but a small number of partitions.”
> —— [PG13 · 5.11 Table Partitioning](https://www.postgresql.org/docs/13/ddl-partitioning.html)

T2 的 31–38 个活分区远在此拐点之下，**在 PG13 上同样成立**。

### 2.2 C6 `date_bin()` —— **PG13 没有，PG14 才有。这是唯一的致命缺失**

- [PG13 · 9.9 Date/Time Functions and Operators](https://www.postgresql.org/docs/13/functions-datetime.html)：全页**不存在** `date_bin`。
- [PG14 · 9.9.3 date_bin](https://www.postgresql.org/docs/14/functions-datetime.html#FUNCTIONS-DATETIME-BIN)：函数在此页首次出现。
- [PostgreSQL 14 Release Notes](https://www.postgresql.org/docs/release/14.0/) 原文：

  > “Add **`date_bin()`** function (John Naylor). This function "bins" input timestamps, grouping them into intervals of a uniform length aligned with a specified origin.”

**这不是理论风险，是当前代码的既成事实**：

```
internal/metric/queries.sql:21
SELECT date_bin(sqlc.arg(bucket)::interval, ts, '2000-01-01'::timestamptz)::timestamptz AS ts,
```

在 PG13 上这句直接 `42883 undefined_function`。⇒ **平台库下限至少为 PG14。**

> 代价评估（若真要下探到 PG13）：需把 `date_bin` 换成 `to_timestamp(floor(extract(epoch from ts) / N) * N)` 一类的手算分桶。这在功能上可行，但要为一个**已 EOL** 的版本引入一条自写的时间对齐表达式，且违反 CLAUDE.md「不写版本分支」的精神。**判定：不值得，且没有任何需求方要求它。**

### 2.3 C7 「无匹配分区」的 SQLSTATE —— 一手确认为 **23514 `check_violation`**

T2 §6 机制 3 自陈「**无一手确认**」，此处补上。PostgreSQL 源码 `src/backend/executor/execPartition.c` 的 `ExecFindPartition()`：

```c
ereport(ERROR,
        (errcode(ERRCODE_CHECK_VIOLATION),
         errmsg("no partition of relation \"%s\" found for row",
                RelationGetRelationName(rel)),
         ...
```

- [REL_13_STABLE · execPartition.c](https://github.com/postgres/postgres/blob/REL_13_STABLE/src/backend/executor/execPartition.c)
- [REL_17_STABLE · execPartition.c](https://github.com/postgres/postgres/blob/REL_17_STABLE/src/backend/executor/execPartition.c) —— 同一 errcode，**PG13→17 未变**
- `ERRCODE_CHECK_VIOLATION` = **`23514`**，见 [Appendix A · PostgreSQL Error Codes](https://www.postgresql.org/docs/17/errcodes-appendix.html)（Class 23 — Integrity Constraint Violation）

⇒ T2 §6 机制 3 的「推测为 23514」**在 PG13–17 全区间一手成立**，且 `pgx` 的 `*pgconn.PgError.Code` 可直接比对该串。仍需注意 23514 也用于普通 CHECK 约束失败，判别时应结合目标表。**结论：这条不再需要在 T11 里靠实测钉死，但仍建议保留一条断言性测试防回归。**

### 2.4 旁证：T2 不依赖、但票里点名要核实的几条版本分界

| 能力 | 引入版本 | 一手出处 |
|---|---|---|
| `ALTER TABLE ... DETACH PARTITION ... CONCURRENTLY` / `FINALIZE` | **PG14** | [PG14 release notes](https://www.postgresql.org/docs/release/14.0/)：“Allow partitions to be detached in a non-blocking manner (Álvaro Herrera). The syntax is `ALTER TABLE ... DETACH PARTITION ... CONCURRENTLY`, and `FINALIZE`.” |
| `ATTACH PARTITION` 只需 `SHARE UPDATE EXCLUSIVE` 锁 | PG13 已如此 | [PG13 · 5.11](https://www.postgresql.org/docs/13/ddl-partitioning.html)：“The `ATTACH PARTITION` command requires taking a `SHARE UPDATE EXCLUSIVE` lock on the partitioned table.” |
| 分区表上的 UNIQUE / PK（要求约束列含全部分区键） | PG11 起，PG13 文档明载该限制 | [PG13 · 5.11](https://www.postgresql.org/docs/13/ddl-partitioning.html)：“To create a unique or primary key constraint on a partitioned table, the partition keys must not include any expressions or function calls and the constraint's columns must include all of the partition key columns.” |
| 默认分区 `DEFAULT` | PG11 起；PG13 文档已含 `DEFAULT` 语法与 ATTACH 时的 CHECK 建议 | [PG13 · CREATE TABLE](https://www.postgresql.org/docs/13/sql-createtable.html)、[PG13 · 5.11](https://www.postgresql.org/docs/13/ddl-partitioning.html) |
| 分区表上的 row-level `BEFORE` 触发器 | **PG13** | [PG13 release notes](https://www.postgresql.org/docs/release/13.0/)：“Support row-level `BEFORE` triggers on partitioned tables (Álvaro Herrera). However, such a trigger is not allowed to change which partition is the destination.” |
| 分区边界不完全匹配时的 partitionwise join | **PG13** | [PG13 release notes](https://www.postgresql.org/docs/release/13.0/)：“Allow partitionwise joins to happen in more cases ... even when their partition bounds do not match exactly.” |
| 分区表作为**发布端**（`publish_via_partition_root`） | **PG13** | [PG13 release notes](https://www.postgresql.org/docs/release/13.0/)：“Allow partitioned tables to be logically replicated via publications (Amit Langote). Previously, partitions had to be replicated individually. ... The `CREATE PUBLICATION` option `publish_via_partition_root` controls whether changes to partitions are published as their own changes or their parent's.” |
| 逻辑复制**写入**分区表（订阅端） | **PG13** | [PG13 release notes](https://www.postgresql.org/docs/release/13.0/)：“Allow logical replication into partitioned tables on subscribers (Amit Langote). Previously, subscribers could only receive rows into non-partitioned tables.” |
| 更新/删除分区表时的执行期裁剪、大幅降低 planner 开销 | **PG14** | [PG14 release notes](https://www.postgresql.org/docs/release/14.0/)：“Improve the performance of updates and deletes on partitioned tables with many partitions ... also allows updates/deletes on partitioned tables to use execution-time partition pruning.” |

**读法**：逻辑复制与分区表的两条分界**恰好都在 PG13**，即「PG13 是分区表逻辑复制的第一个可用版本」。本平台不用逻辑复制，所以这条对下限不产生拉高压力；但它说明**若未来平台库要做主备/逻辑复制，PG13 才刚够，没有余量**——又一条不该把地基压在 13 上的理由。

---

## 3. 工具链：goose / pgx / sqlc 在 PG13 上

### 3.1 pgx —— **第一方明确只承诺 PG14+**

[jackc/pgx · README](https://github.com/jackc/pgx/blob/master/README.md)「Supported Go and PostgreSQL Versions」原文：

> “pgx supports the same versions of Go and PostgreSQL that are supported by their respective teams. For Go that is the two most recent major releases and for PostgreSQL **the major releases in the last 5 years**. This means pgx supports Go 1.25 and higher and **PostgreSQL 14 and higher**.”

- 这是一条**滚动**承诺，绑在 PG 官方 5 年支持窗口上——PG13 于 2025-11 出窗，pgx 的下限随之抬到 14。
- 本仓库 `go.mod` 现钉 `github.com/jackc/pgx/v5 v5.7.5`；旧版本在 PG13 上**大概率**仍能跑（wire protocol 未变，`CopyFrom` 走的是协议级 COPY，自 PG7.4 即有），但那是「碰巧能用」，不是承诺。**把产品下限压在上游明确不承诺的版本上，是没有回旋余地的。**

### 3.2 goose —— 不设 PG 版本门，但下限被 pgx 继承

- [pressly/goose · README](https://github.com/pressly/goose/blob/main/README.md) 只说 “Works against multiple databases: Postgres, MySQL, MariaDB, Spanner, SQLite, YDB, ClickHouse, MSSQL, Vertica, and more.”，**全文未声明任何 PostgreSQL 最低版本**。
- goose 自身对 PG 的用法极其保守：一张版本表 + 事务包裹迁移，无 PG13 之后才有的语法依赖。
- [pressly/goose · go.mod](https://github.com/pressly/goose/blob/main/go.mod)：`module github.com/pressly/goose/v3`、`go 1.25.7`、依赖 `github.com/jackc/pgx/v5 v5.10.0`。⇒ **goose 的 PG 版本下限实际由 pgx 决定，即 §3.1 的 PG14+。** 本仓库钉 `goose/v3 v3.24.3`。
- 唯一与版本无关但必须知道的限制：不能在事务里跑的语句（如 `CREATE INDEX CONCURRENTLY`、`CREATE DATABASE`）要加注解 —— README 原文：“Some statements like `CREATE DATABASE`, however, cannot be run within a transaction. You may optionally add `-- +goose NO TRANSACTION` to the top of your migration file in order to skip transactions within that specific migration file. ... Both Up and Down migrations within this file will be run without transactions.” 这条对本仓库 `migrations/` 只写 up 的纪律无冲突。

### 3.3 sqlc —— 生成物不带服务端版本，但**解析器是 PG17 语法**

- [sqlc · Language and database support](https://docs.sqlc.dev/en/latest/reference/language-support.html) 把 PostgreSQL 标为 **Stable**，但**全文未声明任何最低 PostgreSQL 版本**。
- sqlc 的生成是**纯静态**的：从 `schema:`（本仓库指向 `migrations/`）与 `queries:` 的 SQL 文本生成 Go，**不连服务端、不探测 server_version**。因此「生成代码在 PG13 与 PG17 上不同」这件事**不会发生**——生成物逐字节相同。
- 但版本相关性以另一种方式存在，且方向是**单向危险**的：sqlc 内嵌的解析器是真实 PG 源码。[sqlc · go.mod](https://github.com/sqlc-dev/sqlc/blob/main/go.mod) 依赖 `github.com/pganalyze/pg_query_go/v6 v6.2.2`，而 [libpg_query · README](https://github.com/pganalyze/libpg_query/blob/17-latest/README.md) 的分支映射表把 `17-latest` 对应 **PostgreSQL 17**（v6 即 17 系）。
  ⇒ **sqlc 会照 PG17 的语法接受查询**。写下一句 PG14+ 才有的语法（正是 `date_bin` 这类），`sqlc generate` 与 `make gen` 全绿，**编译期没有任何东西会拦住它**，直到运行时打到旧服务端才炸。
  ⇒ 这正是本仓库 `internal/metric/queries.sql:21` 的现状：`date_bin` 顺利通过 codegen，**若平台库真是 PG13，这个错要到运行期才暴露**。

  > **推论（值得进护栏的一条）**：`make check` 里的真库单测是唯一能挡住「用了平台库版本没有的语法」的关卡。这反过来要求 **CI 与 `dev-up` 用的 PG 版本必须等于交付版本（17）**，而不是随手一个 `postgres:latest`。

---

## 4. 采集侧已知版本分界 vs 平台库下限：不冲突，且属于两个独立坐标

`docs/design/09-packaging-and-deployment.md` §4 已把这一点写清：「**自带 PG 的版本与被监控 PG 的版本无关**」。核实两条 RT-A 事实：

| 事实 | 核实结果 | 一手出处 |
|---|---|---|
| `wal_status` / `safe_wal_size` 自 **PG13** 起可用 | 成立。二者由 `max_slot_wal_keep_size` 特性同批引入 | [PG13 release notes](https://www.postgresql.org/docs/release/13.0/)：“Allow WAL storage for replication slots to be limited by `max_slot_wal_keep_size` (Kyotaro Horiguchi). Replication slots that would require exceeding this value are marked invalid.”；字段清单见 [PG12 catalogs](https://www.postgresql.org/docs/12/view-pg-replication-slots.html) 无 / [PG13 catalogs](https://www.postgresql.org/docs/13/view-pg-replication-slots.html) 有。既有研究 `docs/research/pg-metric-availability/findings.md` §2（分支 `research/rt-a`）已列出 PG12–17 逐版本矩阵 |
| PG12→13 备库列改名 `received_lsn` → `flushed_lsn` | 成立，且更精确的说法是**一拆二**：PG13 起为 `written_lsn`（已写未 flush，不可用于数据完整性判断）+ `flushed_lsn`（已 flush，durable） | [PG12 · monitoring-stats](https://www.postgresql.org/docs/12/monitoring-stats.html) vs [PG13 · monitoring-stats](https://www.postgresql.org/docs/13/monitoring-stats.html)。RT-A findings §1b 已给出逐列对照。**注**：我在 [PG13 release notes](https://www.postgresql.org/docs/release/13.0/) 全文检索未命中该改名的独立条目，因此此条以**版本号文档页**为一手依据，而非 release notes |

另外从 [PG13 release notes · Migration to Version 13](https://www.postgresql.org/docs/release/13.0/) 中筛出的、对监控平台有影响的不兼容项（供采集侧参考，与平台库下限无关）：

> “Prevent display of auxiliary processes in `pg_stat_ssl` and `pg_stat_gssapi` system views (Euler Taveira). Queries that join these views to `pg_stat_activity` and wish to see auxiliary processes will need to use **left joins**.”

**结论**：采集侧的 PG13 下限（`docs/design/06-metric-dictionary-and-collection-plan.md` §5.1）与平台库下限是两条**互不相干**的承诺。采集侧下限由「被监控库里有没有这两个字段」决定；平台库下限由「我们自己写的 SQL 与驱动要求什么」决定。**把两者写成同一条「PG13+」正是问题的根源。**

---

## 5. PG13 的 EOL 与 v1 承诺周期

[PostgreSQL Versioning Policy](https://www.postgresql.org/support/versioning/) 原文：

> “The PostgreSQL Global Development Group supports a major version for **5 years** after its initial release. After this, a final minor version will be released and the software will then be unsupported (end-of-life).”

同页表格（核实于 2026-08-13）：

| 版本 | 末版 / EOL 日期 | 截至 2026-08-13 |
|---|---|---|
| **13** | **2025-11-13** | **已 EOL 9 个月**（官方表标 Supported = No） |
| 14 | 2026-11-12 | 尚在支持，但**3 个月后 EOL** |
| 15 | 2027-11-11 | 支持中 |
| 16 | 2028-11-09 | 支持中 |
| **17** | **2029-11-08** | 支持中，交付选定版本 |
| 18 | 2030-11-14 | 支持中 |

对 v1 承诺周期的影响：

1. **PG13 已 EOL** ⇒ 不再有安全补丁。把平台自带库钉在 EOL 版本上，等于向客户交付一个**永远不会再收到 CVE 修复**的数据库，这在私有化/信创交付场景下是合规问题，不只是技术问题。
2. **PG14 也只剩 3 个月** ⇒ 即使把下限「只抬到 14」，也在一个季度内失效。**「下限抬到 14」是错误的补法。**
3. `docs/design/09-packaging-and-deployment.md` §4 已定的 **PG17（支持至 2029-11-08）** 给 v1 留出 **3 年 3 个月**的地基寿命，是唯一与「数年不动的地基」相称的选择。该文档理由 3 原文即「『保守』换不来兼容性收益，只换来更早的 EOL」——本研究给这句话补上了确切日期。

---

## 6. 结论与建议动作

### 6.1 平台库版本下限的正确表述

> **平台自带 PostgreSQL = 17，不给客户选版本，不接管客户既有实例。**（`docs/design/09-packaging-and-deployment.md` §4，已 v1.0 冻结）
> 若一定要表述为「下限」：**技术下限是 PG14**（`date_bin` + pgx），**可交付下限是 PG17**（EOL 余量 + 已冻结决策）。**PG13 在任何意义上都不是平台库的下限。**

### 6.2 「PG13+」这条承诺应如何处置

它**不是一条需要被推翻的决策**——`docs/design/` 里没有任何文档说过平台库可以是 PG13。它是一处**表述串台**：把 `06-metric-dictionary-and-collection-plan.md` §5.1 的「被监控 PG13–17」误读成了平台库下限。

⇒ **不需要新开决策记录**（没有决策被推翻）。需要的是在 issue #107 及其下游文案里**改口**，并在任何对外承诺文档中把两条版本线**分行写**：

```
被监控 PostgreSQL：13 – 17（PG12 及以下接入即拒）
平台自带 PostgreSQL：17（随整包交付，不可替换）
```

### 6.3 本研究额外产出的两条可落地事实

1. **T2 §6 机制 3 的 SQLSTATE 悬案可以结案**：`23514`（`ERRCODE_CHECK_VIOLATION`），PG13–17 源码一致（§2.3）。T11 不必再靠实测钉死，但应保留一条断言测试防上游变更；**且必须比对 SQLSTATE 而非错误消息**（T2 原纪律不变）。
2. **一条护栏建议**：sqlc 用 PG17 语法解析、且不连服务端（§3.3），意味着 codegen **永远不会**因为「用了目标库没有的函数」而报错。因此 `make check` 的真库版本必须与交付的 PG 大版本一致（17），否则这层保护是假的。建议在 `docs/design/10-ai-guardrails-and-verification.md` 的 `dev` profile 说明中把「PG 版本 = 交付版本」写成显式约束。

---

## 附录 · 一手来源清单

**PostgreSQL 官方**

- [Versioning Policy（EOL 表与 5 年政策）](https://www.postgresql.org/support/versioning/)
- [PG13 · 5.11 Table Partitioning](https://www.postgresql.org/docs/13/ddl-partitioning.html)
- [PG13 · CREATE TABLE](https://www.postgresql.org/docs/13/sql-createtable.html)
- [PG13 · 9.9 Date/Time Functions](https://www.postgresql.org/docs/13/functions-datetime.html)（**无** `date_bin`）
- [PG14 · 9.9.3 date_bin](https://www.postgresql.org/docs/14/functions-datetime.html#FUNCTIONS-DATETIME-BIN)
- [PostgreSQL 13 Release Notes](https://www.postgresql.org/docs/release/13.0/)
- [PostgreSQL 14 Release Notes](https://www.postgresql.org/docs/release/14.0/)
- [PG12 · Monitoring Statistics](https://www.postgresql.org/docs/12/monitoring-stats.html) / [PG13 · Monitoring Statistics](https://www.postgresql.org/docs/13/monitoring-stats.html)
- [PG17 · Appendix A · Error Codes](https://www.postgresql.org/docs/17/errcodes-appendix.html)
- [postgres/postgres · REL_13_STABLE · execPartition.c](https://github.com/postgres/postgres/blob/REL_13_STABLE/src/backend/executor/execPartition.c) / [REL_17_STABLE](https://github.com/postgres/postgres/blob/REL_17_STABLE/src/backend/executor/execPartition.c)

**工具链官方**

- [jackc/pgx · README](https://github.com/jackc/pgx/blob/master/README.md)
- [pressly/goose · README](https://github.com/pressly/goose/blob/main/README.md) / [go.mod](https://github.com/pressly/goose/blob/main/go.mod)
- [sqlc · Language and database support](https://docs.sqlc.dev/en/latest/reference/language-support.html) / [sqlc · go.mod](https://github.com/sqlc-dev/sqlc/blob/main/go.mod)
- [pganalyze/libpg_query · README（分支↔PG 版本映射表）](https://github.com/pganalyze/libpg_query/blob/17-latest/README.md)

**仓库内既有输出**

- `docs/research/pg-metric-availability/findings.md`（RT-A，分支 `research/rt-a`）—— PG12–17 复制/槽字段逐列矩阵
- `docs/design/04-metric-storage-model.md` §7.2 / §7.4 / §6
- `docs/design/06-metric-dictionary-and-collection-plan.md` §5.1
- `docs/design/09-packaging-and-deployment.md` §4
- `internal/metric/queries.sql:21`、`migrations/00001_walking_skeleton.sql:85`、`sqlc.yaml`、`go.mod`
