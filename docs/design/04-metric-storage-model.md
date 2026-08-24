---
status: partially-superseded
kind: decision
superseded_by: 18-v1-delivery-boundary-bs-binary.md, 30-external-postgres-prerequisites.md
superseded_parts: 「自带 PostgreSQL」前提与 ≥14 版本口径；schema、分区、查询纪律、事务边界全部在效
---
# 指标存储选型与数据模型 v1.0

> 出处：[T2 · 时序存储选型与指标数据模型](https://github.com/liumingjian/dbs-monitor/issues/20)（R2 地图 [#15](https://github.com/liumingjian/dbs-monitor/issues/15)）。
> 研究输入：[RT-C · 时序数据存储候选方案](https://github.com/liumingjian/dbs-monitor/issues/16)，findings 见 `docs/research/timeseries-storage/findings.md`。
> 定位：本文档回答「指标样本存哪、按什么模型存」。**结论 + 理由 + 被否决的方案**同页，体例承 R1 决策索引。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。


> **当前适用性（2026-08-24 治理复核）**
> **架构结论全部在效**：单存储不引第二引擎、窄表 `metric_sample` + series 元数据、按天 UTC 原生分区、
> 写入侧差分只存速率、最新值查询必须带时间下界、样本与告警状态同库同事务。
>
> **已失效的是前提措辞，不是结论**：全文行文假定「平台自带 PostgreSQL」，而
> [`18-v1-delivery-boundary-bs-binary.md`](18-v1-delivery-boundary-bs-binary.md) D2 已把平台库改为**客户自备的外部 PostgreSQL**，
> 版本口径也由本文的「≥14，推荐 17，具体由 T8 定」收紧为
> [`30-external-postgres-prerequisites.md`](30-external-postgres-prerequisites.md) D1 的**钉死 17.x、主版本不符拒绝启动、无逃生舱**（T8 已整条作废，本文对它的指向悬空）。
> 读本文时请把「自带 PG」一律替换为「客户自备的外部 PG 17」。
>
> 本块只标注失效点，不改写原结论。

---

## 0. 一句话结论

**自带 PostgreSQL + 原生声明式分区，样本与配置/告警状态同库；窄表 + series 元数据表；差分在写入侧完成、只存速率；按天 UTC 分区由平台 Go 进程建删。**

---

## 1. D1 · 存储选型

**结论**：维持地图 Notes 第 5 条的默认假设——**自带 PostgreSQL + 原生声明式分区，样本与配置/告警状态同库**。不引入第二存储引擎。

**理由**（按权重）：

1. **容量与吞吐都不构成瓶颈。** 按 PG 官方页/行开销推算，即使按 RT-C 修正后的 15M 点/天、无压缩、带索引，30 天也只有约 35–45 GB；写入约 175 行/s。压缩在这个规模上是「省几十 GB」，不是救命。
2. **跨切面成本压倒存储效率，且顶在 R1 不变式上。** 样本与告警状态分离后，「时序库短暂不可用」会被读成「窗口内无样本」= 假 `NO_DATA`。而 `NO_DATA` 是 R1 一等状态，也是三条内置采集状态规则的对冲面。为省几十 GB 新增一条不变式级防线，不划算。
3. **同库带来两个结构性收益**（详见 §8）：采集状态永不说谎、假 `NO_DATA` 不可能发生。

**否决了什么**：

| 被否决 | 为什么 |
|---|---|
| **TimescaleDB** | 收益（压缩、连续聚合、自动分区）全部落在 `tsl/` 目录，受 Timescale License 约束；整包私有化交付落在 TSL 的 "Value-Added Products" 分支，该条款要求客户被合同或技术手段禁止修改数据库 schema——整包交付时客户具备 OS/DB 权限，「技术手段禁止」基本不成立。且升级路径变二维（PG 小版本 × 扩展版本，官方点名过不兼容组合）。收益/风险不对称 |
| **VictoriaMetrics 单节点** | 是最干净的第二引擎候选（Apache-2、单二进制、可降级升级），但今天引入付的是组件数 +1 与跨存储对账的**确定成本**，换的是本基线下**用不上**的容量收益 |
| **全自研存储** | 地图 Notes 第 3 条已否决（时序压缩/乱序写入是有深坑的成熟领域） |

**被推翻的条件**：RT-C §7 给出六条可证伪门槛。其中 **T1（查询延迟）/ T2（容量占比）/ T3（控制面劣化）三条需实测**，已并入 [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29)。任一门槛被实测击穿，**首选替代方案为 VictoriaMetrics**（其次 TimescaleDB）；届时必须同时满足 RT-C 的反向门槛——先证明 `NO_DATA` 判定能可靠区分「时序库不可用」与「时序库无数据」。

---

## 2. D2 · 样本表形态

**结论**：窄表 `metric_sample(series_id, ts, value)` + 独立 `metric_series` 元数据表。样本表**不建唯一约束**。

```sql
-- 元数据：一个 series = 一个实例 + 一个指标 + 一组维度
CREATE TABLE metric_series (
  series_id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  instance_id bigint      NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
  metric_id   text        NOT NULL,               -- 字典的稳定指标 ID，如 'pg.connection.total'
  labels      jsonb       NOT NULL DEFAULT '{}',  -- {"database":"app"} / {"device":"sda"} / {"slot":"s1"}
  labels_key  text        NOT NULL,               -- labels 的规范化串，供唯一约束与等值命中
  first_seen  timestamptz NOT NULL DEFAULT now(),
  last_seen   timestamptz NOT NULL,
  UNIQUE (instance_id, metric_id, labels_key)
);

-- 样本：定长窄行
CREATE TABLE metric_sample (
  series_id bigint           NOT NULL,
  ts        timestamptz      NOT NULL,
  value     double precision NOT NULL
) PARTITION BY RANGE (ts);

CREATE INDEX ON metric_sample (series_id, ts DESC);
```

**理由**：

1. **宽表在本领域不成立。** 35 个 P0 指标里有 per-database（临时文件，字典 §4.5）、per-device（磁盘，§4.2）、per-slot（复制槽，§4.8）三类多维指标，维度基数运行期才知道。宽表要么每个维度组合一张表，要么随发现的设备/库动态 `ALTER TABLE`——后者是运维灾难。
2. **采样周期不齐。** 字典 §7 明确分层：连通性 10s–30s、事务/临时文件 30s–60s、增强监控 5s。宽表一行装多指标要求同一时刻齐采，与分层直接冲突，会制造大量 NULL 列——而 NULL 在本系统里**不是空位，是「缺数」这个一等语义**，不能被行对齐凭空造出来。
3. **窄表让「缺数 = 没有行」成为唯一表达**，正对 R1「缺数不是 0」硬约束：不需要区分「行存在但列为 NULL」和「行不存在」两种缺数。
4. **`series_id bigint` 而非样本行内存 `(instance_id, metric_id text)`**：文本指标名每点重复 ~20 字节且撑大索引。§1 的 52 字节/点推算即按本形态计。

**不建唯一约束的理由**：`(series_id, ts)` 的唯一性没有业务价值——同序列同时刻重复写入是采集器 bug；而唯一约束会让整批 `CopyFrom` 因单点污染而失败。可用性优先于对无价值不变量的强制。

**已知代价**：`labels_key` 是 `labels` 的冗余规范串。接受这份冗余，换唯一性口径显式可读、且能被 sqlc 生成的查询按等值命中。

---

## 3. D3 · series 的生命周期

**结论**：

1. **采集侧 get-or-create，进程内缓存。** 采集器持有 `(instance_id, metric_id, labels_key) → series_id` 的内存 map；miss 时 `INSERT ... ON CONFLICT (instance_id, metric_id, labels_key) DO UPDATE SET last_seen = EXCLUDED.last_seen RETURNING series_id`。稳态下几乎全部命中缓存，只在新库/新设备/新槽出现时打一次 DB。50 实例 × 35 指标 × 少量维度 ≈ 数千行，整表常驻内存。
2. **series 永不自动删除**，只随 `instance` 级联删除。30 天滚动删除会让老 series 暂时没有样本，但一行元数据几十字节；写 GC 反而要处理「刚删完又采到」的竞态。
3. **维度消失 ≠ `NO_DATA`。**

**第 3 条展开（本节最容易被写反的一条）**：

`NO_DATA` 的判定挂在**告警规则的评估目标**上，不挂在 series 上。

- 某个 per-database 序列因 `DROP DATABASE` 不再产生样本 → **结构性不适用**（承 R1 [T10](https://github.com/liumingjian/dbs-monitor/issues/13) 的采集能力三态契约），不得触发 `NO_DATA`。
- 实例整体采集失败 → 规则的评估目标没数据 → 这才是 `NO_DATA`。

推论：**`metric_series` 是发现结果，不是期望清单。** 「本实例此刻应该有哪些序列」由指标字典 + 能力探测（[T1](https://github.com/liumingjian/dbs-monitor/issues/19) D4 的 5 分钟循环）回答，**不由 `metric_series` 里有没有行回答**。

> ⚠️ 反面实现：「查 series 表发现没数据 → 报 `NO_DATA`」。这是最自然、也最错的写法，会把「库被删了」渲染成告警。

---

## 4. D4 · 非数值指标（state / enum / timestamp）

35 个 P0 指标中有 5 个 `类型 = state`，塞不进 `value double precision`。按三条线切：

### 线 1 · 编码进 `metric_sample` 的 float8

适用：`pg.availability.reachable`（0/1）、`pg.replication.role`、`pg.replication.connection_state`。枚举映射为小整数码，**码表写进 T4 的机器可读字典载体**。

理由：这三个都需要时序语义——`reachable` 的聚合方式含 `success_rate`（要算失败率就得有历史点），`connection_state` 可告警且需「连续 N 次」判定。与数值指标共用同一张表 = 共用同一套分区、保留、查询路径、**以及同一条告警评估取数路径**。开第二张 text 值表会让告警引擎有两条取数路径，是 T5 模块边界最不该背的负债。

> ⚠️ **枚举码值一经发布只增不改。** 改码表会让 30 天内的历史点被读成另一个状态，且没有任何东西会报错。此条必须进 `CLAUDE.md`（T9）。

### 线 2 · 不进样本表的派生量

适用：`collector.last_success_time`、`agent.status`。

这两个**不是采上来的样本，是采集链路自身的投影**——字典自己写明「数据来源 = 监控平台自身 / Agent 心跳」。而 [T3](https://github.com/liumingjian/dbs-monitor/issues/21) 已定死 `agent.status=offline` 与 `NO_DATA` 共用同一时间戳与同一门槛。若再往样本表写一条 `agent.status` 序列，就凭空多了第二个真相源，直接违反 R1 不变式 2 的「单一来源」。

权威来源为控制面表 `instance_collect_state`（每实例每采集源一行，存 `last_success_at` / `last_report_at` / `last_error`）；两个「指标」是从它算出来的**视图**，不落时序。内置的「Agent 离线」「数据过期」两条规则直接读它。

> 与字典 v1.0 的关系：这两条 P0「指标」在存储层没有对应 series。这与字典表述有张力但不矛盾（字典定的是口径与展示，不是存储位置），已显式接受。

### 线 3 · 失败原因文本进控制面

字典要求 `reachable` 的 UI 是「状态卡 + 最近探测时间 + **失败原因**」，`pg.probe.latency_ms` 缺数时要「显示失败原因而不是 0ms」。失败原因是变长文本、只有「最近一次」有意义 ⇒ `instance_collect_state.last_error`，结构化为 `code` + `message`，其中 **`code` 供前端映射到 R1 IA §1.4 的十二种空状态之一**。

### 由三条线导出的可检查规则

> **`metric_sample` 里只有能被画成曲线的东西；解释「为什么没有曲线」的东西一律在控制面表。**

---

## 5. D5 · 差分指标：写入侧差分，只存速率

约 10 个差分指标（`xact_commit` / `xact_rollback` / `tup_*` / `temp_files` / `temp_bytes` / OS 磁盘 IO 计数器 / OS 网络字节计数器，字典 §6）。

**结论**：**写入侧差分，只存速率，不存累计原始值。**

字典 §6 的六条规则全部落成一个纯函数：

```
rate(prev Sample, cur Sample, dt time.Duration) → (value float64, ok bool, reason ResetReason)
```

**理由**：

1. **reset 规则只有一处实现。** 纯函数可表驱动单测——正对 T9 强制测试清单的「差分指标遇 reset 不得产生负值/尖峰」。若放查询侧，这条规则活在**每一句取数 SQL 里**，改坏了编译器拦不住、测试也照不到全部调用点。
2. **查询路径统一。** 存储层所有 series 都是 gauge 语义，告警评估与图表都不必知道哪些指标要差分，T5 的模块边界薄一层。
3. **告警评估读取代价更低。** 「连续 N 个周期」在查询侧差分要读 N+1 个点 + window function；写入侧直接读 N 个点。

**已明确接受的代价**：

| 代价 | 说明 |
|---|---|
| **放弃可回溯重算** | 累计原始值不落库 ⇒ 若差分规则写错，30 天历史修不回来。判断为可接受：§6 规则已在 R1 冻结、逻辑简单，且 T9 给它表驱动单测 |
| **重启丢一个点** | 采集器持有 last-value 内存状态，进程重启后每个差分序列首点不可计算。50 实例 × 10 序列 ≈ 一次重启丢 500 点，一个周期后自愈。**这是已知行为，不是 bug** |
| **last-value 不落库** | 落库 = 每周期多 500 次 UPSERT，换一次重启的 500 个点，不划算 |

**否决了什么**：

| 被否决 | 为什么 |
|---|---|
| **存累计值 + 查询侧差分** | reset 规则散落到每一句 SQL，正是本票点名「最容易被后续会话改坏」的失效模式 |
| **双写（累计值 + 速率各一条 series）** | 点数 +~28%（容量仍在 RT-C 余量内），换规则写错时可重算历史。判定为投机性冗余：为一个「规则可能写错」的假设，给稳态系统加 28% 写入与两倍的 series 概念负担 |

**「不可计算点」的表达**：

不可计算 = **样本表里没有行**，与「没采到」在存储层同构。UI 要求的「解释为什么」（字典 §6 规则 6）由控制面回答：`instance_collect_state.last_error` 增设 `COUNTER_RESET` 码（承 §4 线 3）。

这条能成立，是因为 **reset 事实上是实例级事件**：`pg_stat_reset()`、数据库重启、主备切换会一次性重置全部 PG 计数器；主机重启一次性重置全部 OS 计数器。不存在「单个 series 独立 reset」的场景 ⇒ per-instance 记录足够，**不需要给 15M 行/天的样本表加一个 99.99% 为空的 `reason` 列**。

---

## 6. D6 · 分区与保留

**粒度：按天，边界一律 UTC。** 30 天保留 → 31–38 个活分区，远低于 PG 文档所述「a few thousand」的规划开销拐点。

- 不选 6 小时：分区数 ×4 到 120+，收益只是删除更平滑（一天删一个 ~780 MB 分区不构成问题），代价是**跨界事件频率 ×4**，每次跨界都是一个「分区没建好就写」的风险窗口。
- 边界用 UTC 而非本地时区：夏令时与客户改时区会让边界错乱，且不会有人发现。

**执行者：平台自身的 Go 后台循环，不用 `pg_partman`。**
`pg_partman` 需 `CREATE EXTENSION`，与「自带 PG 尽量少装扩展」有张力，且整包升级又多一个版本要锁。而建分区就是一句 `CREATE TABLE ... PARTITION OF`、删就是 `DROP TABLE`，与既有的 goose 迁移和后台循环同构。

**机制四条**：

1. **预建 7 天，不是 1 天。** 启动时跑一次，之后每小时一次，确保「今天 + 未来 7 天」的分区存在；用 `CREATE TABLE IF NOT EXISTS ... PARTITION OF ... FOR VALUES FROM (...) TO (...)` 保持幂等。7 天余量意味着**维护循环整整挂一周才会导致写入失败**，而不是挂一晚就炸。
2. **滚动删除用 `DROP TABLE`**，删 `now() - 31 天` 之前的分区。**绝不用 `DELETE`**——PG 官方文档明确 `DROP`/`DETACH` 远快于批量 `DELETE` 且完全避开 `VACUUM` 开销。保留 31 个分区 ⇒ 实际保留 30–31 天，比规格多留不足一天，无害。
3. **兜底：写失败即修复重试一次。** 不在写入前查系统表确认分区存在（每周期一次太贵）。改为：`CopyFrom` 失败时识别「无匹配分区」错误 → 同步触发一次分区维护 → 重试一次 → 仍失败才报错。这是「凌晨炸」的最后一道防线。
   > **待实测**：PG 对 "no partition of relation found for row" 返回的 SQLSTATE（推测为 `23514` check_violation，**无一手确认**）。**在 T11 骨架中实测钉死该错误码，禁止匹配错误消息字符串**——消息随 PG 版本与 locale 变化。
4. **维护失败必须可见。** 分区维护是**平台级**故障，不是实例级，不写进 `instance_collect_state`。MVP 做到：结构化 error 日志 + 一个平台级健康标志位。完整形态归地图迷雾中的「平台自身的可观测性」，本文档不定。

---

## 7. D7 · 查询形状与索引

**索引：只建 `CREATE INDEX ON metric_sample (series_id, ts DESC);`**
建在父表上，PG 自动在每个分区建局部索引。两类查询都是「给定 series，取时间范围」，`series_id` 必须是前缀。不额外建 BRIN——`ts` 的粗过滤已由分区裁剪完成。

### 7.1 读取模式 A · 告警评估：读最近 N 个原始点，不聚合

R1 语义是「连续 N 个周期」，聚合会改变语义（3 个点平均成 1 个，「连续 3 次超阈」就消失了）。评估路径直接 `WHERE series_id = $1 AND ts > $2 ORDER BY ts`，窗口只有几分钟，落在最新分区内。

### 7.2 读取模式 B · 图表：时间范围 + 粒度，后端算粒度

用 `date_bin('5 minutes', ts, '2000-01-01'::timestamptz)` 分桶（PG14+ 内建，不需要 TimescaleDB 的 `time_bucket`）。

- **粒度由后端按时间范围算出，并在响应中回传实际粒度**；前端不能直接传粒度，否则「30 天 @10s」这种请求能直接打穿后端。回传实际粒度也是前端渲染的必需输入（承 RT-E：前端需判新鲜度，同理需知道拿到的是什么粒度）。
- **逃生舱**：接受显式 `granularity=raw`，但对时间跨度设硬上限 **≤6 小时**，用于下钻看原始点。
- **量级核算**：单实例单指标 30 天 @10s = 259,200 点，@30s = 86,400 点，聚合扫这个量级 PG 毫无压力。RT-C §6 担心的「扫上千万行」是**跨序列**聚合场景，而本系统的图表全是单实例单指标，**结构上不跨**。⇒ **不需要预聚合表**。

### 7.3 两条必须进 `CLAUDE.md` 的查询纪律

> **纪律一 · 空桶不得变成 0。** 桶内无样本 ⇒ 该桶**不出现在结果里**（而非 `value: 0`）。`date_bin` + `GROUP BY` 天然满足（没有行就没有组），但**任何人加一句 `generate_series` 补齐时间轴 + `COALESCE(avg, 0)` 就会破坏它**。RT-E 已查明图表库层不构成风险，真风险全在数据层，就是这里。

> **纪律二 · 「最新值」查询必须带时间下界。** 在分区表上写 `ORDER BY ts DESC LIMIT 1` 而不带 `ts` 条件时，PG **无法裁剪分区**（不知道最新点在哪个分区），会对 31 个分区各做一次 index scan 再归并。正确写法带 `AND ts > now() - interval '1 hour'`，裁到 1–2 个分区。这是写起来自然、错了也不报错的坑。

**MVP 不建「最新值缓存表」**——带时间下界后是纯 index scan，够快；加缓存表就多一个一致性问题。

### 7.4 外溢约束

`date_bin` 要求**自带的平台 PostgreSQL ≥ 14**，推荐 17。具体版本由 [T8 · 打包、部署与运行形态](https://github.com/liumingjian/dbs-monitor/issues/26) 定。

---

## 8. D8 · 与配置/告警状态的事务边界

同库的收益必须落在具体事务边界上，否则「同库」只是物理位置相同。

### 8.1 采集写入：每实例每周期一个事务

```
BEGIN
  CopyFrom(metric_sample, 本实例本周期的 ~35 行)
  UPDATE instance_collect_state SET last_success_at = $ts, last_error = NULL WHERE ...
COMMIT
```

**同库的第一个实质收益。** 字典对 `collector.last_success_time` 的定义原文是「最近一次**成功写入有效样本**的时间」。跨存储时这是两段式，中间失败会产生「写样本失败却标记成功」或反之；同事务后，**这个不一致在结构上不可能发生**。

**每实例一个事务、而非全实例一个**：一个实例写失败不牵连其余 49 个（与 T12 的隔离精神一致）。

**已接受的代价**：每周期每实例多一次 UPDATE（50 次/周期）。换「采集状态永不说谎」——三条内置采集状态规则全靠它。

### 8.2 告警评估：每实例一个事务，读样本与写状态同事务

**同库的第二个实质收益**，正对 RT-C §4.3 第 1 条：跨存储时必须新增一条不变式级防线来区分「查询失败」与「查询成功但为空」，否则产生假 `NO_DATA`。同库同事务后，**读失败即事务失败、状态不变，假 `NO_DATA` 在结构上不可能发生**。

**同样是每实例一个事务，不是每评估周期一个**：50 实例 × N 规则放进一个事务会变成长事务。本平台自身即在监控长事务，不该制造长事务；且长事务会拖住 autovacuum（RT-C 的 T3 门槛）。

### 8.3 autovacuum 与 RT-C T3 门槛

`metric_sample` **只 INSERT，从不 UPDATE/DELETE**（删除靠 `DROP` 分区）⇒ 结构上不产生 dead tuple ⇒ RT-C T3 门槛中「autovacuum 持续追不上、`n_dead_tup` 单调增长」一半**在本设计下不成立**。

仍需 ANALYZE 维持 planner 统计 ⇒ 给样本表设**独立 autovacuum 参数**（拉高 vacuum 阈值、保留 analyze），而不是关掉。T3 门槛的另一半（写入使控制面事务 P95 劣化 > 2 倍）仍需 T11 实测。

### 8.4 物理布局与连接池

- **同一个 database、同一个 schema。** 分 schema 要给 goose 迁移和 sqlc 各多配一层，换来的只是「一眼看出是时序还是控制面」，表名前缀就够。
- **连接池分离**：对自带平台 PG 的连接分为「采集写入池」与「API/控制面池」，采集突发不得饿死 API 请求。
  > **边界声明**：本文档管的是**对自带平台 PG** 的连接池；[T12 · 采集并发限流、超时与背压](https://github.com/liumingjian/dbs-monitor/issues/30) 管的是**对被监控数据库**的连接与并发。两者是不同资源，各自限流，两票不重叠。

---

## 9. 与既有约束的一致性

- 未触碰 R1 决策索引 §4 四条不变式；未重新引入任何 R1 否决项（加权健康评分、`Silence`、`SuppressionPolicy`、实例级授权、`SUPPRESSED` 状态）。
- §3 第 3 条与 §4 线 2 是对**不变式 2「单一来源」**的正面加强。
- §7.3 纪律一是 R1「缺数不是 0」在 SQL 层的落点。

## 10. 下游影响

| 票 | 影响 |
|---|---|
| [T4 · 指标字典机器可读载体](https://github.com/liumingjian/dbs-monitor/issues/22) | 载体必须承载**枚举码表**（§4 线 1），且码值只增不改；必须区分「落时序的指标」与「控制面派生量」（§4 线 2） |
| [T5 · 后端代码结构](https://github.com/liumingjian/dbs-monitor/issues/23) | `rate()` 是纯函数接缝（§5）；存储层对上只暴露 gauge 语义；事务边界为「每实例一事务」（§8） |
| [T6 · API 契约](https://github.com/liumingjian/dbs-monitor/issues/24) | 粒度由后端回传、`granularity=raw` 上限 6 小时、空桶不出现在结果里（§7.2/§7.3）；**「请求跨度超出 30 天保留边界时如何表现」由 T6 定** |
| [T8 · 打包与运行形态](https://github.com/liumingjian/dbs-monitor/issues/26) | 自带 PostgreSQL **≥ 14**（推荐 17）（§7.4） |
| [T9 · AI 开发护栏](https://github.com/liumingjian/dbs-monitor/issues/27) | 三条须进 `CLAUDE.md`：枚举码只增不改、空桶不补 0、最新值查询必带时间下界 |
| [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29) | 并入 RT-C §7 的 T1/T2/T3 三条实测门槛 + 分区缺失 SQLSTATE 实测（§1、§6） |
| [T12 · 采集并发限流](https://github.com/liumingjian/dbs-monitor/issues/30) | 边界划清：T12 管对被监控库的连接，本文档管对平台 PG 的连接池（§8.4） |
