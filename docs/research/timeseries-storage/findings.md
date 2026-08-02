# RT-C · 时序数据存储候选方案 · findings

> 本文件为 **RT-C 调研产出**，服务于 [issue #16](https://github.com/liumingjian/dbs-monitor/issues/16)，**不构成决策**。决策在 T2。
> 纪律：结论只依据一手来源（官方文档、许可证原文、源码仓库）。凡无一手数据处，本文直接写明「无一手数据」并给出自测方法，**不引用二手 benchmark 数字**。

## 0. 输入约束（来自 [地图 #15](https://github.com/liumingjian/dbs-monitor/issues/15) Notes，不重议）

- 整包自带、不依赖客户环境、交付团队运维；每多一个组件，备份/升级/打包/排障各多一份。
- 后端与 Agent 用 Go；否决 Prometheus 全家桶做底座；否决全自研存储。
- 规模基线：50 实例 × ~35 序列 × 10s–60s 采样 × **原始保留 30 天、不降采样**，约 500 万点/天。
- 默认假设：**自带 PostgreSQL + 原生分区一把梭**；引入第二存储引擎需被实测数据推翻。

**基线的量级修正（本调研的第一条结论）**：500 万点/天对应接近 60s 的平均采样。若 P0 序列全部落在 10s，写入量为
`50 × 35 × 8640 = 15.1M 点/天`；再算上增强监控 5s 的临时窗口，**设计余量应按 15–20M 点/天取**，而不是 500 万。下文容量推算同时给出两档。

---

## 1. 候选一：自带 PostgreSQL + 原生声明式分区

### 一手事实

| 事实 | 来源 |
|---|---|
| 范围分区（`PARTITION BY RANGE`）为内建声明式能力，按时间列切分；分区裁剪 `enable_partition_pruning` 默认开启，且支持**执行期**裁剪 | [PostgreSQL 17 · Table Partitioning](https://www.postgresql.org/docs/17/ddl-partitioning.html) |
| 「Dropping an individual partition using `DROP TABLE`, or doing `ALTER TABLE DETACH PARTITION`, is far faster than a bulk operation. These commands also entirely avoid the `VACUUM` overhead caused by a bulk `DELETE`.」→ **30 天滚动删除应当是 `DROP TABLE` 分区，不是 `DELETE`** | 同上 |
| 「Inserting data into the parent table that does not map to one of the existing partitions will cause an error; an appropriate partition must be added manually.」→ **PG 不自动建分区** | 同上 |
| 主键/唯一约束的列**必须包含全部分区键列** | 同上 |
| 「The query planner is generally able to handle partition hierarchies with up to a few thousand partitions fairly well」；分区过多会拉长规划时间、抬高内存 | 同上 |
| 堆存储开销：页头 `PageHeaderData` 24 字节；每行 4 字节行指针 `ItemIdData`；行头 `HeapTupleHeaderData` 23 字节（多数平台）；用户数据起点按 MAXALIGN 对齐 | [PostgreSQL 17 · Database Page Layout](https://www.postgresql.org/docs/17/storage-page-layout.html) |
| 批量装载：「Use `COPY` to load all the rows in one command, instead of a series of `INSERT` commands」，且 COPY 快于 INSERT（即便用 PREPARE） | [PostgreSQL 17 · Populating a Database](https://www.postgresql.org/docs/17/populate.html) |

### 由一手事实推出的容量估算（推算，非实测）

窄表 `(ts timestamptz, value float8, series_id int4)`：23 字节行头 → MAXALIGN 后 24，加 8+8+4 补齐到 8 的倍数 = **48 字节/行**，再加 4 字节行指针 ≈ **52 字节/点**（不含索引、不含 fillfactor 浪费）。

| 写入档 | 堆 / 天 | 30 天堆 | 加 `(series_id, ts)` btree 后的粗量级 |
|---|---|---|---|
| 500 万点/天 | ≈ 260 MB | ≈ 7.6 GB | ~12–15 GB |
| 1500 万点/天（全 10s） | ≈ 780 MB | ≈ 23 GB | ~35–45 GB |

> PostgreSQL 对这种窄行**没有列式压缩**；TOAST 压缩只对超阈值大字段生效，对 8 字节 float 无效。上表即「无压缩」的真实量级。
> 若采用**分桶行**（一行装一个时间窗内的定长 `float8[]`），行头开销被摊薄且数组走 TOAST 压缩，占用可再降一个量级——代价是写入需缓冲、查询需展开，属 T2 可选优化，非 MVP 前提。

**量级判断**：即便按最悲观的 15M 点/天、无压缩、带索引，30 天原始数据也在**几十 GB**，是单机 PostgreSQL 的舒适区。**存储容量不构成引入第二引擎的理由。**

### 分区维护的成熟做法

- PG 不提供自动建分区（见上表）。两条路：外部扩展 `pg_partman`（需 `CREATE EXTENSION`，与「自带 PG 少装扩展」有轻微张力），或**由平台自身的 Go 进程按调度建/删分区**。
- 后者对本项目更自然：整包自带、schema 迁移本归平台管；建分区即 `CREATE TABLE ... PARTITION OF`，删分区即 `DROP TABLE`，与既有迁移/后台循环同构。**建议按天或按 6 小时分区**：30 天保留 → 30–120 个分区，远低于文档所述「a few thousand」的规划开销拐点。
- 必须**预建未来若干个分区**（否则跨天瞬间写入报错），并对「分区缺失」做兜底自检——这是纯 PG 方案里最容易在凌晨炸的一处。

### 写入策略

每采集周期一批（50 × 35 ≈ 1750 行/10s ≈ 175 行/s），用一次 `CopyFrom`（pgx）或多值 `INSERT`。**该量级下的上限无一手 benchmark 数据**；175 行/s 相对 PostgreSQL 单机已知能力量级有两个数量级余量，此为量级推理，非实测。

### 它会在什么条件下撑不住（调研的首要任务）

1. **序列数或采样率跨数量级增长**：实例 ×10 + 全 10s → ~1.5 亿点/天、30 天数百 GB，索引进不了内存，范围查询开始扫盘。
2. **保留期从 30 天变成 1 年**：无压缩线性增长顶穿磁盘，且此时**降采样**需求同时出现——正是 TimescaleDB 连续聚合 / VM 的强项。
3. **图表要求跨长区间实时聚合**（30 天曲线按 5 分钟粒度），每次现场 `avg()` 扫上千万行。纯 PG 的解法是自建汇总表（= 自己实现连续聚合）。
4. **样本表与配置/告警表争抢同实例的 IO 与 autovacuum 预算**，把控制面拖慢。

---

## 2. 候选二：TimescaleDB

### 一手事实

| 事实 | 来源 |
|---|---|
| 双许可：核心 Apache-2；**`tsl/` 目录（Continuous Aggregates、Compression、时序查询优化）受 Timescale License**——「The TimescaleDB TSL library is licensed under the Timescale License」 | [timescaledb/tsl/README.md](https://github.com/timescale/timescaledb/blob/main/tsl/README.md) |
| TSL 允许：为**自身内部业务目的**使用；开发以 Timescale 为后端组件的**增值产品（Value-Added Products）**，但前提是**客户被合同或技术手段禁止定义/重定义/修改数据库 schema**，且须被告知 TSL 条款；允许「copy and distribute the Timescale Software source code and binaries **solely in unmodified standalone form**」 | [tsl/LICENSE-TIMESCALE](https://github.com/timescale/timescaledb/blob/main/tsl/LICENSE-TIMESCALE) |
| TSL 禁止：提供 time-sharing / database-as-a-service / 任何把 TSL 软件本身提供给第三方的 SaaS 形态；衍生作品与转售受限 | 同上 |
| 安装要求：`shared_preload_libraries = 'timescaledb'`，**必须重启 PostgreSQL**，再逐库 `CREATE EXTENSION IF NOT EXISTS timescaledb;` | [Self-hosted 安装文档](https://www.tigerdata.com/docs/self-hosted/latest/install/installation-linux) |
| PG 版本绑定：与 PG 次版本 ABI 绑定；官方明确「We recommend not using TimescaleDB with PostgreSQL 17.1, 16.5, 15.9, 14.14, 13.17, or 12.21」（这些 PG 小版本引入破坏性 ABI 变更），需用 17.2 / 16.6 及以上 | 同上 |

### 解读

- **收益**：hypertable 自动分区（消除上文「凌晨没建分区」的整类故障）、原生压缩、连续聚合（未来降采样的现成答案）。恰好覆盖纯 PG 的三个薄弱点——但**压缩与连续聚合都在 `tsl/`，即受 TSL 约束的部分**。
- **许可证风险点（需法务确认，本调研只陈述条文）**：本平台私有化整包交付、交付团队运维，落在 TSL 的 "Value-Added Products" 一支，而该支款要求**客户被合同或技术手段禁止修改数据库 schema**。整包交付时客户通常具备 OS/DB 访问能力，「技术手段禁止」很难成立，只能靠合同兜。**与 Apache-2 有实质差异，不是"随便打包进去"的许可证。**
- **工程代价**：需重启 PG 才能加载；**升级路径变成二维**（PG 小版本 × 扩展版本，且官方点名过特定 PG 小版本不可用），整包升级须锁定组合并做版本矩阵测试。
- **无一手数据**：未获得 TimescaleDB 在本基线（500 万–1500 万点/天）下的官方 benchmark；厂商性能对比材料属营销性质，不作事实引用。

---

## 3. 候选三：VictoriaMetrics 单节点

### 一手事实

| 事实 | 来源 |
|---|---|
| **Apache License 2.0**（仓库 LICENSE 原文，版权 VictoriaMetrics, Inc. 2019–2026）；单节点与集群版均开源 | [LICENSE](https://github.com/VictoriaMetrics/VictoriaMetrics/blob/master/LICENSE) |
| 「a single small executable without external dependencies」——单二进制、无外部依赖；默认端口 8428 | [Single-server-VictoriaMetrics](https://docs.victoriametrics.com/single-server-victoriametrics/) |
| 关键参数：`-storageDataPath`（数据目录）、`-retentionPeriod`（**默认 31 天**，最小 24 小时） | 同上 |
| 写入协议：Prometheus remote write 与 exposition、InfluxDB line protocol、Graphite、OpenTSDB、OpenTelemetry、CSV / JSON line、native binary | 同上 |
| 查询接口：`/api/v1/query`、`/api/v1/query_range`、`/api/v1/export`、`/api/v1/series`；PromQL 与 MetricsQL；自带 `/vmui` | 同上 |
| 备份：`/snapshot/create` 即时快照 + `vmbackup`/`vmrestore`；`vmbackup` 为**开源**，支持 `fs://`（本地目录）、`s3://`、`gs://`、`azblob://`；禁止备份到 `-storageDataPath` 目录 | [vmbackup 文档](https://docs.victoriametrics.com/victoriametrics/vmbackup/) |
| 升级：SIGINT 优雅停后换二进制启动，「It is safe upgrading VictoriaMetrics to new versions unless release notes say otherwise」，**支持降级**，数据文件跨版本兼容 | [Single-server-VictoriaMetrics](https://docs.victoriametrics.com/single-server-victoriametrics/) |
| **Enterprise-only**（社区版不含）：**Downsampling**、**Multiple retentions**、**Backup automation（vmbackupmanager）**、异常检测、vmgateway 鉴权/限流、多租户 vmalert、FIPS 构建、LTS 版本 | [Enterprise 文档](https://docs.victoriametrics.com/victoriametrics/enterprise/) |

### 解读

- **随包分发可行性最好**：Apache-2 + 单静态二进制，法务与打包都无摩擦；备份/升级故事是三者中最简单的（快照 + 换二进制 + 可降级）。
- **降采样是 Enterprise 功能**——但地图已把「降采样与长期留存」判为 out of scope，30 天原始保留正好落在社区版 `-retentionPeriod` 默认值（31 天）附近。**VM 社区版对当前基线够用，但它不是"未来长期留存的免费答案"**。
- **与自研告警引擎的配合成本**：VM 提供 PromQL/MetricsQL over HTTP。我们只把它当**样本读写端点**用（不用 vmalert，因为 R1 语义与 Prometheus 系冲突，已被地图否决）。代价：告警评估取数从「同库一条 SQL」变成「HTTP + PromQL + JSON 解码」，且**样本与告警状态不在同一事务里**（见 §4.3）。
- **组件数 +1** 的直接后果：整包多一个进程要拉起/健康检查/日志/端口/磁盘配额/升级编排；备份从「一次 PG 备份」变成「PG 备份 + VM 快照，且两者时间点不一致」。

---

## 4. 跨切面对比

### 4.1 备份 / 恢复

| 方案 | 备份手段 | 一致性 |
|---|---|---|
| 纯 PG | `pg_dump` / `pg_basebackup` / PITR，**配置、告警状态、样本同库** | 单点一致，一次恢复全部对齐 |
| TimescaleDB | 同 PG 工具链，但恢复端**必须先装同版本扩展并重启** | 单点一致，恢复前置条件更多 |
| VictoriaMetrics | `/snapshot/create` + `vmbackup`（开源，支持 `fs://`）；自动化调度属 Enterprise | **两套备份、两个时间点**，恢复点必然错位 |

### 4.2 升级路径

- 纯 PG：一维——PG 大版本升级 + 平台自身 schema 迁移。
- TimescaleDB：二维——PG 小版本 × 扩展版本，官方点名过具体不兼容组合；扩展升级需 `ALTER EXTENSION UPDATE` 且可能重启。
- VM：一维且最轻——换二进制，官方声明可降级、数据文件兼容。

### 4.3 跨存储对账（配置/告警在 PG、样本在别处）——分离方案的真实痛点

1. **`NO_DATA` 判定的时效性**。R1 不变式规定 `NO_DATA` 是一等状态。判定「某序列在窗口内没有样本」需读样本存储。同库时是一条 SQL，与告警状态写入**可在同一事务内读写**；跨存储时变成「HTTP 查 VM → 拿结果 → 写 PG 状态」两段式，中间失败可能造成**假 `NO_DATA`**（VM 短暂不可用被读成「没数据」）。必须显式区分「查询失败」与「查询成功但为空」，且前者不改状态——这是分离方案必须新增的一条不变式级防线。
2. **写入路径的双写与错序**：同库时一次事务写完；跨存储时样本写 VM、状态写 PG，失败语义不同，需要幂等重放。
3. **时间语义不一致**：VM 有自己的时间戳对齐、去重与乱序容忍策略；两侧「最新样本时间」可能不同，而采集状态三态契约依赖它，须指定单一权威来源。
4. **调试与取证**：无法用一条 SQL 把「规则、状态、触发时刻的样本」join 起来看，排障成本上升。

---

## 5. 对比表（汇总）

| 维度 | 自带 PG + 原生分区 | TimescaleDB | VictoriaMetrics 单节点 |
|---|---|---|---|
| 组件数增量 | **0** | 0（同 PG，+1 扩展） | **+1 进程** |
| 许可证 | PostgreSQL License（宽松） | 核心 Apache-2，**压缩/连续聚合受 TSL**；增值产品分发含 schema 限制条款 | **Apache-2** |
| 随包分发 | 无摩擦 | 需法务确认 TSL 增值产品条款 | 无摩擦 |
| 30 天容量（本基线） | 约 10–45 GB（无压缩，推算） | 更低（压缩，无一手数据） | 更低（压缩，无一手数据） |
| 自动分区 / 保留 | **需自建**（Go 侧建/删分区） | hypertable 内建 | `-retentionPeriod` 内建 |
| 降采样 / 连续聚合 | 需自建汇总表 | 连续聚合（TSL） | **Enterprise-only** |
| 查询接口 | SQL（自研引擎直接可用） | SQL | HTTP + PromQL/MetricsQL（需适配层） |
| 备份 | 与配置同点一致 | 同点一致，恢复前置条件多 | 独立快照，**与 PG 时间点错位** |
| 升级 | 一维 | **二维（PG 小版本 × 扩展版本）** | 一维，可降级 |
| `NO_DATA` 判定 | 同库同事务，最稳 | 同库同事务 | 跨进程，需防「查询失败被读成无数据」 |

---

## 6. 推荐（不构成决策）

**推荐维持默认假设：自带 PostgreSQL + 原生声明式分区，样本与配置/告警状态同库。**

理由（按权重）：

1. **容量与吞吐都不构成瓶颈**。按一手的页面/行头开销推算，即使 15M 点/天、无压缩、带索引，30 天也只有几十 GB；写入率约 175 行/s。压缩在这个规模上是「省几十 GB」，不是「救命」。
2. **跨切面成本压倒存储效率**。§4.3 的四类对账痛点全部由「样本与告警状态分离」引入，而 R1 的 `NO_DATA` 一等状态使这个痛点直接顶在不变式上。
3. **许可证与打包**：纯 PG 无摩擦；TimescaleDB 的收益（压缩、连续聚合）恰好都在 TSL 一侧，且整包交付落在「增值产品」分支需法务判断，收益/风险不对称。
4. **VM 是最干净的第二引擎候选**（Apache-2、单二进制、可降级升级）。若将来必须分离，它是首选——但今天引入它，付的是组件数 +1 与跨存储对账的确定成本，换的是本基线下用不上的容量收益。

配套必要工程动作（若采纳）：

- 分区由平台 Go 进程管理，**预建未来 N 个分区** + 「分区缺失」自检；滚动删除用 `DROP TABLE`，**不用 `DELETE`**。
- 写入走 `CopyFrom` 批量。
- 样本表与控制面表同库，但独立 autovacuum 参数 / tablespace 可后置，先观测。

---

## 7. 推荐被推翻所需的条件（可证伪门槛）

以下任一条被**实测**满足，则「纯 PG」推荐失效，应重新评估（优先 VictoriaMetrics，其次 TimescaleDB）：

| # | 门槛（必须实测，不接受推理） | 检验方法 |
|---|---|---|
| T1 | 目标硬件上按 **15M 点/天**灌满 30 天等量数据后，典型图表查询（单实例单序列 24 小时、按 1 分钟聚合）P95 > **500 ms** | `generate_series` 造数后 `EXPLAIN (ANALYZE, BUFFERS)` |
| T2 | 30 天全量数据（含索引）占交付规格磁盘 > **30%**，或绝对值 > **100 GB** | `pg_total_relation_size()` 逐分区求和 |
| T3 | 采集写入使控制面事务 P95 劣化 > **2 倍**，或 autovacuum 持续追不上（`n_dead_tup` 单调增长） | 写入压测同时跑控制面读写；观测 `pg_stat_user_tables` |
| T4 | 保留期需求从 30 天变为 **≥180 天**，或降采样进入范围（地图 out-of-scope 被推翻） | 产品范围变更 |
| T5 | 规模基线上调至 **≥200 实例**或全量 5s 采样（≥1 亿点/天量级） | 产品范围变更 |
| T6 | 分区维护被证明是**反复发生**的线上故障源（预建失败、跨天写入报错）且加固后仍复发 | 运行期事故统计 |

**反向门槛**（引入 VM 需同时成立）：必须先证明 `NO_DATA` 判定能可靠区分「VM 不可用」与「VM 无数据」，否则违反 R1 不变式 1。

---

## 8. 本调研的已知缺口（明示）

- **无一手 benchmark 数字**：三者在本基线下的写入吞吐、查询延迟、压缩后实际磁盘占用，**均未获得可引用的一手实测数据**。本文所有容量数字为基于 PostgreSQL 官方存储布局文档的**推算**并已标注。第 7 节的门槛即「把推算换成实测」的执行清单。
- TimescaleDB 的 TSL 增值产品条款对本交付形态的适用性**需法务确认**，本文只陈述条文原文。
- **未覆盖**：其他候选（ClickHouse、InfluxDB、Prometheus + 远程存储）——前两者组件更重，后者已被地图否决。
