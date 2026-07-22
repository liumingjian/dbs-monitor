# PG MVP 指标字典 v0.1

> 目标：将可行性报告中的 MVP 指标范围转化为采集、展示、告警、事件和验收可以共同引用的指标契约。  
> 适用范围：PostgreSQL 首版 MVP，面向私有化 / AP 部署场景。  
> 参考依据：[`../research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md`](../research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md)。  
> 当前阶段：产品与技术设计草案，不是最终数据库表结构或采集 SQL 实现。

---

## 1. 设计原则

### 1.1 指标字典解决什么问题

本字典用于回答每个 MVP 指标的核心问题：

1. 这个指标具体代表什么？
2. 从哪里采集？
3. 多久采一次？
4. 原始值是否需要差分、聚合或估算？
5. 缺数、无权限、Agent 离线时如何解释？
6. 是否可以用于告警？
7. 前端如何展示？
8. 是否属于标准监控或增强监控？

可行性报告已经证明这些能力整体可行，但尚未定义逐指标口径。本字典是采集器、时序存储、告警引擎、性能事件和前端页面之间的共同契约。

### 1.2 MVP 边界

首版优先定义 P0 指标：

- 可用性与采集状态
- 主机资源
- PostgreSQL 核心统计
- 会话与阻塞汇总
- 基础复制状态
- 基础告警可用指标

首版不追求完整复刻阿里云 RDS 指标，也不覆盖完整性能洞察、SQL 画像、精确膨胀分析或自动优化收益量化。

### 1.3 指标状态不能混淆

必须区分以下状态：

| 状态 | 含义 |
|---|---|
| 有值 | 采集成功，指标值有效 |
| 值为 0 | 采集成功，实际值为 0 |
| 暂无样本 | 指标可采，但当前时间范围没有样本 |
| 采集延迟 | 最近样本超过新鲜度阈值 |
| 采集失败 | 采集任务执行失败或超时 |
| 数据库不可达 | 主动探针或数据库连接失败 |
| Agent 离线 | 依赖 Agent 的指标不可用 |
| 无权限 | 账号缺少读取所需系统视图、函数或日志的权限 |
| 不适用 | 当前实例角色或部署形态不适用，例如无备库时无复制延迟 |
| 功能未启用 | 扩展、日志或采集能力未开启 |
| 版本不支持 | 当前 PostgreSQL 版本缺少对应视图或字段 |

告警引擎不得把缺数、无权限、Agent 离线、不适用简单转换为 0。

---

## 2. 字典字段规范

后续每个指标建议按以下字段维护。

| 字段 | 说明 |
|---|---|
| 指标 ID | 稳定唯一标识，供采集、告警、前端共用 |
| 中文名 | 面向用户展示的名称 |
| 分类 | 可用性、采集状态、主机、连接、事务、IO、复制、会话阻塞等 |
| 定义 | 指标业务语义和统计范围 |
| 单位 | %, ms, count, bytes/s, tx/s 等 |
| 类型 | gauge、counter、rate、state、event |
| 维度 | instance、node、database、slot、role 等 |
| 数据来源 | 主动探针、PostgreSQL 系统视图、Agent、日志、扩展 |
| 采集方式 | DB 连接、Agent、扩展、日志、主动探针 |
| 默认采样周期 | 标准采集频率 |
| 增强采样周期 | 若进入增强监控，建议采集频率 |
| 聚合方式 | latest、avg、max、min、sum、count |
| 计算方式 | 原始值、差分、比率、估算、状态映射 |
| 前置条件 | 权限、Agent、扩展、角色、版本要求 |
| 缺数语义 | 缺数时如何解释和展示 |
| 采集成本 | 低、中、高 |
| 标准监控 | 是否进入标准监控 |
| 增强监控 | 是否进入增强监控候选 |
| 是否可告警 | 是否适合作为告警指标 |
| 关联事件 | 是否可触发性能事件 |
| UI 展示 | 单值、趋势图、状态卡、表格、详情页等 |

---

## 3. P0 指标总览

| 分类 | 指标 ID | 中文名 | 标准监控 | 增强监控 | 可告警 |
|---|---|---|---:|---:|---:|
| 可用性 | `pg.availability.reachable` | 实例连通性 | 是 | 是 | 是 |
| 可用性 | `pg.probe.latency_ms` | 主动探针延迟 | 是 | 是 | 是 |
| 采集状态 | `collector.last_success_time` | 最近成功采集时间 | 是 | 否 | 是 |
| 采集状态 | `agent.status` | Agent 状态 | 是 | 否 | 是 |
| 主机 | `host.cpu.usage_percent` | CPU 使用率 | 是 | 是 | 是 |
| 主机 | `host.memory.usage_percent` | 内存使用率 | 是 | 是 | 是 |
| 主机 | `host.disk.usage_percent` | 磁盘使用率 | 是 | 是 | 是 |
| 主机 | `host.disk.free_bytes` | 磁盘剩余空间 | 是 | 否 | 是 |
| 主机 | `host.disk.iops` | 磁盘 IOPS | 是 | 是 | 是 |
| 主机 | `host.disk.throughput_bytes_per_sec` | 磁盘吞吐 | 是 | 是 | 是 |
| 主机 | `host.network.bytes_per_sec` | 网络流量 | 是 | 是 | 视情况 |
| 连接 | `pg.connection.total` | 总连接数 | 是 | 是 | 是 |
| 连接 | `pg.connection.active` | 活跃连接数 | 是 | 是 | 是 |
| 连接 | `pg.connection.idle_in_transaction` | idle in transaction 连接数 | 是 | 是 | 是 |
| 事务 | `pg.tps` | TPS | 是 | 是 | 是 |
| 事务 | `pg.xact.commit_per_sec` | 提交速率 | 是 | 是 | 视情况 |
| 事务 | `pg.xact.rollback_per_sec` | 回滚速率 | 是 | 是 | 视情况 |
| 行操作 | `pg.tuples.read_per_sec` | 读行速率 | 是 | 是 | 视情况 |
| 行操作 | `pg.tuples.write_per_sec` | 写行速率 | 是 | 是 | 视情况 |
| 临时文件 | `pg.temp.files_per_sec` | 临时文件数量速率 | 是 | 是 | 是 |
| 临时文件 | `pg.temp.bytes_per_sec` | 临时文件写入速率 | 是 | 是 | 是 |
| 会话阻塞 | `pg.transaction.long_count` | 长事务数量 | 是 | 是 | 是 |
| 会话阻塞 | `pg.transaction.max_duration_sec` | 最长事务时长 | 是 | 是 | 是 |
| 会话阻塞 | `pg.lock.waiting_count` | 锁等待数量 | 是 | 是 | 是 |
| 会话阻塞 | `pg.session.blocked_count` | 被阻塞会话数 | 是 | 是 | 是 |
| 2PC | `pg.prepared_xacts.count` | 2PC 数量 | 是 | 否 | 是 |
| 复制 | `pg.replication.role` | 实例角色 | 是 | 否 | 否 |
| 复制 | `pg.replication.connection_state` | 复制连接状态 | 是 | 是 | 是 |
| 复制 | `pg.replication.replay_lag_ms` | 复制回放延迟 | 是 | 是 | 是 |
| 复制 | `pg.replication.wal_lag_bytes` | WAL 延迟字节数 | 是 | 是 | 是 |
| 复制槽 | `pg.replication_slot.retained_wal_bytes` | Replication slot WAL 积压 | 是 | 是 | 是 |

---

## 4. 指标明细

### 4.1 可用性与采集状态

#### `pg.availability.reachable` — 实例连通性

| 字段 | 内容 |
|---|---|
| 定义 | 监控系统能否在超时时间内完成数据库连接和轻量查询 |
| 单位 | state / boolean |
| 类型 | state |
| 维度 | instance |
| 数据来源 | 主动探针 |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 5s - 10s |
| 聚合方式 | latest、success_rate |
| 计算方式 | 连接、认证、执行 `SELECT 1`、返回成功则 reachable |
| 前置条件 | 数据库网络可达、监控账号可登录 |
| 缺数语义 | 采集器未执行或结果过期，不等于实例不可达 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 实例不可达、连接失败率过高 |
| UI 展示 | 状态卡 + 最近探测时间 + 失败原因 |

#### `pg.probe.latency_ms` — 主动探针延迟

| 字段 | 内容 |
|---|---|
| 定义 | 主动探针完成轻量查询的端到端耗时 |
| 单位 | ms |
| 类型 | gauge |
| 维度 | instance |
| 数据来源 | 主动探针 |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 5s - 10s |
| 聚合方式 | avg、max、p95、latest |
| 计算方式 | 建连、认证、执行 `SELECT 1`、返回的总耗时；是否复用连接需在实现中固定 |
| 前置条件 | 数据库可达，探针账号可登录 |
| 缺数语义 | 探针无结果；若连通性失败，应显示失败原因而不是 0ms |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 主动延迟过高、连接失败率过高 |
| UI 展示 | 趋势图 + 当前值 + 超时阈值 |

#### `collector.last_success_time` — 最近成功采集时间

| 字段 | 内容 |
|---|---|
| 定义 | 指标采集源最近一次成功写入有效样本的时间 |
| 单位 | timestamp / age |
| 类型 | state |
| 维度 | instance、source_type |
| 数据来源 | 监控平台自身 |
| 采集方式 | 采集任务元数据 |
| 默认采样周期 | 随采集任务更新 |
| 增强采样周期 | 不适用 |
| 聚合方式 | latest |
| 计算方式 | 按采集源记录最近成功时间 |
| 前置条件 | 采集任务上报元数据 |
| 缺数语义 | 未初始化或采集链路异常 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 否 |
| 是否可告警 | 是 |
| 关联事件 | 数据过期、采集异常 |
| UI 展示 | 数据新鲜度标签、采集状态卡 |

#### `agent.status` — Agent 状态

| 字段 | 内容 |
|---|---|
| 定义 | 目标实例所在节点的 Agent 运行和上报状态 |
| 单位 | enum |
| 类型 | state |
| 维度 | instance、node |
| 数据来源 | Agent 心跳 |
| 采集方式 | Agent |
| 默认采样周期 | 10s - 30s 心跳 |
| 增强采样周期 | 不适用 |
| 聚合方式 | latest |
| 计算方式 | online、offline、not_installed、permission_denied、error |
| 前置条件 | 安装并注册 Agent |
| 缺数语义 | Agent 未注册或心跳链路异常 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 否 |
| 是否可告警 | 是 |
| 关联事件 | Agent 离线、Agent 采集异常 |
| UI 展示 | 状态卡 + 受影响指标列表 |

---

### 4.2 主机资源指标

> 主机资源指标仅通过数据库连接无法稳定获得，MVP 默认依赖 Agent 或 node exporter 类采集器。若 Agent 离线，不得将指标显示为 0。

#### `host.cpu.usage_percent` — CPU 使用率

| 字段 | 内容 |
|---|---|
| 定义 | 实例所在节点或容器可见范围内的 CPU 使用率 |
| 单位 | % |
| 类型 | gauge |
| 维度 | instance、node |
| 数据来源 | Agent / OS 指标 |
| 采集方式 | Agent |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 5s - 10s 或 10s - 30s |
| 聚合方式 | avg、max、latest |
| 计算方式 | 按 Agent 固定口径采集用户态、系统态、总使用率 |
| 前置条件 | Agent 在线，具备 OS / cgroup 读取权限 |
| 缺数语义 | Agent 离线、无权限或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | CPU 高 |
| UI 展示 | 趋势图 + 当前值 + user/system 可选拆分 |

#### `host.memory.usage_percent` — 内存使用率

| 字段 | 内容 |
|---|---|
| 定义 | 实例所在节点或容器可见范围内的内存使用率 |
| 单位 | % |
| 类型 | gauge |
| 维度 | instance、node |
| 数据来源 | Agent / OS 指标 |
| 采集方式 | Agent |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、latest |
| 计算方式 | 已用内存 / 可用总内存；需固定是否扣除 cache/buffer |
| 前置条件 | Agent 在线，具备 OS / cgroup 读取权限 |
| 缺数语义 | Agent 离线、无权限或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 内存高 |
| UI 展示 | 趋势图 + 当前值 + 可用内存 |

#### `host.disk.usage_percent` / `host.disk.free_bytes` — 磁盘使用率 / 剩余空间

| 字段 | 内容 |
|---|---|
| 定义 | PostgreSQL 数据目录或指定挂载点的磁盘使用率和剩余空间 |
| 单位 | %, bytes |
| 类型 | gauge |
| 维度 | instance、node、mount |
| 数据来源 | Agent / 文件系统指标 |
| 采集方式 | Agent |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 30s - 60s |
| 聚合方式 | latest、min、max |
| 计算方式 | 按数据目录所在文件系统统计，避免误用宿主机全部磁盘 |
| 前置条件 | Agent 在线，能识别 PG 数据目录或配置监控挂载点 |
| 缺数语义 | Agent 离线、路径不可见、权限不足或未配置 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 磁盘不足 |
| UI 展示 | 趋势图 + 当前使用率 + 剩余空间 |

#### `host.disk.iops` / `host.disk.throughput_bytes_per_sec` — 磁盘 IOPS / 吞吐

| 字段 | 内容 |
|---|---|
| 定义 | 实例相关磁盘设备或挂载点的读写 IOPS 与吞吐速率 |
| 单位 | ops/s、bytes/s |
| 类型 | rate |
| 维度 | instance、node、device |
| 数据来源 | Agent / OS 指标 |
| 采集方式 | Agent |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 5s - 10s 或 10s - 30s |
| 聚合方式 | avg、max、sum |
| 计算方式 | OS 累计计数器差分得到读写速率 |
| 前置条件 | Agent 在线，设备映射准确 |
| 缺数语义 | Agent 离线、设备映射失败或采集失败 |
| 采集成本 | 低 - 中 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | IO 异常 |
| UI 展示 | 读写分组趋势图 |

#### `host.network.bytes_per_sec` — 网络流量

| 字段 | 内容 |
|---|---|
| 定义 | 实例所在节点或容器相关网卡的收发流量 |
| 单位 | bytes/s |
| 类型 | rate |
| 维度 | instance、node、interface |
| 数据来源 | Agent / OS 指标 |
| 采集方式 | Agent |
| 默认采样周期 | 10s - 30s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、sum |
| 计算方式 | 网卡累计字节计数器差分 |
| 前置条件 | Agent 在线，网卡映射准确 |
| 缺数语义 | Agent 离线、网卡映射失败或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 视情况 |
| 关联事件 | 网络流量异常 |
| UI 展示 | 入方向 / 出方向趋势图 |

---

### 4.3 PostgreSQL 连接与会话指标

#### `pg.connection.total` — 总连接数

| 字段 | 内容 |
|---|---|
| 定义 | `pg_stat_activity` 中当前连接总数 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance，可选 database / state |
| 数据来源 | `pg_stat_activity` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max、avg |
| 计算方式 | `count(*)`；需明确是否排除监控账号连接 |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 连接数过高 |
| UI 展示 | 趋势图 + 当前值 + max_connections 参考线 |

#### `pg.connection.active` — 活跃连接数

| 字段 | 内容 |
|---|---|
| 定义 | 当前 `state = 'active'` 的连接数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance，可选 database / user |
| 数据来源 | `pg_stat_activity` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max、avg |
| 计算方式 | 按 state 聚合；需明确是否排除监控自身查询 |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 活跃会话突增 |
| UI 展示 | 趋势图 + 跳转会话列表 |

#### `pg.connection.idle_in_transaction` — idle in transaction 连接数

| 字段 | 内容 |
|---|---|
| 定义 | 当前 `state = 'idle in transaction'` 的连接数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance，可选 database / user |
| 数据来源 | `pg_stat_activity` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | 按 state 聚合 |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 长事务、连接异常 |
| UI 展示 | 趋势图 + 跳转会话与阻塞 |

---

### 4.4 事务与行操作指标

#### `pg.tps` — TPS

| 字段 | 内容 |
|---|---|
| 定义 | 单位时间事务数，通常为提交事务数与回滚事务数增量之和 |
| 单位 | tx/s |
| 类型 | rate |
| 维度 | instance，可选 database |
| 数据来源 | `pg_stat_database.xact_commit`, `pg_stat_database.xact_rollback` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、latest |
| 计算方式 | `delta(xact_commit + xact_rollback) / delta(time)` |
| 前置条件 | 可读取 `pg_stat_database` |
| 缺数语义 | 首个样本无法计算速率；reset / 回退 / 重启后当前点标记不可计算 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是，但 TPS 下降需持续时间或基线 |
| 关联事件 | TPS 突降 |
| UI 展示 | 趋势图；需说明为差分速率 |

#### `pg.xact.commit_per_sec` / `pg.xact.rollback_per_sec` — 提交 / 回滚速率

| 字段 | 内容 |
|---|---|
| 定义 | 单位时间提交事务数和回滚事务数 |
| 单位 | tx/s |
| 类型 | rate |
| 维度 | instance，可选 database |
| 数据来源 | `pg_stat_database.xact_commit`, `pg_stat_database.xact_rollback` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、latest |
| 计算方式 | 累计计数器差分 |
| 前置条件 | 可读取 `pg_stat_database` |
| 缺数语义 | 首个样本、reset、重启、计数器回退时不可计算 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 视情况；回滚率异常可作为事件候选 |
| 关联事件 | 事务异常、回滚增多 |
| UI 展示 | 提交 / 回滚双线趋势图 |

#### `pg.tuples.read_per_sec` / `pg.tuples.write_per_sec` — 读写行速率

| 字段 | 内容 |
|---|---|
| 定义 | 单位时间读取和写入的行数 |
| 单位 | rows/s |
| 类型 | rate |
| 维度 | instance，可选 database |
| 数据来源 | `pg_stat_database.tup_returned`, `tup_fetched`, `tup_inserted`, `tup_updated`, `tup_deleted` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、sum |
| 计算方式 | 累计计数器差分；读可由 returned/fetched 组合展示，写可由 inserted/updated/deleted 组合展示 |
| 前置条件 | 可读取 `pg_stat_database` |
| 缺数语义 | 首个样本、reset、重启、计数器回退时不可计算 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 视情况 |
| 关联事件 | 负载突增、操作行数异常 |
| UI 展示 | 读写行趋势图 |

---

### 4.5 临时文件指标

#### `pg.temp.files_per_sec` / `pg.temp.bytes_per_sec` — 临时文件数量 / 写入速率

| 字段 | 内容 |
|---|---|
| 定义 | 单位时间产生的临时文件数量和临时文件字节数 |
| 单位 | files/s、bytes/s |
| 类型 | rate |
| 维度 | instance，可选 database |
| 数据来源 | `pg_stat_database.temp_files`, `pg_stat_database.temp_bytes` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | avg、max、sum |
| 计算方式 | 累计计数器差分 |
| 前置条件 | 可读取 `pg_stat_database` |
| 缺数语义 | 首个样本、reset、重启、计数器回退时不可计算 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 临时文件写入异常、排序/hash 内存不足候选 |
| UI 展示 | 临时文件数量和字节趋势图 |

---

### 4.6 长事务、锁等待与阻塞指标

#### `pg.transaction.long_count` — 长事务数量

| 字段 | 内容 |
|---|---|
| 定义 | 当前事务持续时间超过阈值的会话数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance，可选 database / user |
| 数据来源 | `pg_stat_activity.xact_start` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | `now() - xact_start > threshold`；默认阈值需配置，例如 5m 或 10m |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 低 - 中 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 长事务 |
| UI 展示 | 数量趋势 + 跳转长事务列表 |

#### `pg.transaction.max_duration_sec` — 最长事务时长

| 字段 | 内容 |
|---|---|
| 定义 | 当前未结束事务中最长持续时间 |
| 单位 | seconds |
| 类型 | gauge |
| 维度 | instance |
| 数据来源 | `pg_stat_activity.xact_start` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | `max(now() - xact_start)` |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 无事务时为 0；采集失败时为无数据 |
| 采集成本 | 低 - 中 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 长事务 |
| UI 展示 | 趋势图 + 当前最长事务摘要 |

#### `pg.lock.waiting_count` — 锁等待数量

| 字段 | 内容 |
|---|---|
| 定义 | 当前等待锁的会话或锁请求数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance |
| 数据来源 | `pg_locks`, `pg_stat_activity`, `pg_blocking_pids()` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | 统计未 granted 的锁或存在 blocking pids 的会话；口径需固定 |
| 前置条件 | 可读取 `pg_locks` 和 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 中；大实例需限制明细 Top-N |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 锁等待、阻塞链 |
| UI 展示 | 趋势图 + 跳转阻塞链 |

#### `pg.session.blocked_count` — 被阻塞会话数

| 字段 | 内容 |
|---|---|
| 定义 | 当前存在阻塞源的会话数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance |
| 数据来源 | `pg_blocking_pids()`, `pg_stat_activity` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | 对活动会话调用阻塞关系函数并统计存在阻塞者的会话 |
| 前置条件 | 可读取 `pg_stat_activity`；建议 `pg_monitor` |
| 缺数语义 | 数据库不可达、无权限或采集失败 |
| 采集成本 | 中；需避免高频全量计算复杂阻塞链 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 阻塞链 |
| UI 展示 | 趋势图 + 阻塞链详情入口 |

---

### 4.7 2PC 指标

#### `pg.prepared_xacts.count` — 2PC 数量

| 字段 | 内容 |
|---|---|
| 定义 | 当前 prepared transaction 数量 |
| 单位 | count |
| 类型 | gauge |
| 维度 | instance、database |
| 数据来源 | `pg_prepared_xacts` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 60s |
| 增强采样周期 | 不建议高频 |
| 聚合方式 | latest、max |
| 计算方式 | `count(*) from pg_prepared_xacts` |
| 前置条件 | 可读取 `pg_prepared_xacts` |
| 缺数语义 | 无权限或采集失败；真实无 2PC 时为 0 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 否 |
| 是否可告警 | 是 |
| 关联事件 | prepared transaction 数量异常 |
| UI 展示 | 单值 + 趋势图 |

---

### 4.8 复制指标

#### `pg.replication.role` — 实例角色

| 字段 | 内容 |
|---|---|
| 定义 | 实例当前是主库、备库或单实例 |
| 单位 | enum |
| 类型 | state |
| 维度 | instance |
| 数据来源 | `pg_is_in_recovery()` 等 |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 不适用 |
| 聚合方式 | latest |
| 计算方式 | `pg_is_in_recovery()` 映射角色，结合配置判断单实例 / 集群 |
| 前置条件 | 可执行基础查询 |
| 缺数语义 | 数据库不可达或采集失败 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 否 |
| 是否可告警 | 否；角色变化可作为事件 |
| 关联事件 | 主备切换 |
| UI 展示 | 状态标签 |

#### `pg.replication.connection_state` — 复制连接状态

| 字段 | 内容 |
|---|---|
| 定义 | 主备复制连接或 WAL receiver 的状态 |
| 单位 | enum |
| 类型 | state |
| 维度 | instance、replica |
| 数据来源 | 主库 `pg_stat_replication`，备库 `pg_stat_wal_receiver` |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest |
| 计算方式 | 按主库/备库角色读取对应视图并映射状态 |
| 前置条件 | 具备复制状态读取权限；不同 PG 版本字段可能不同 |
| 缺数语义 | 无复制拓扑时为不适用；权限不足或采集失败需区分 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 复制中断 |
| UI 展示 | 状态卡 + 复制节点列表 |

#### `pg.replication.replay_lag_ms` — 复制回放延迟

| 字段 | 内容 |
|---|---|
| 定义 | 备库 WAL 回放相对主库的时间延迟；在不具备可靠时间口径时需标记估算 |
| 单位 | ms |
| 类型 | gauge |
| 维度 | instance、replica |
| 数据来源 | `pg_stat_replication` / `pg_stat_wal_receiver`，视角色和版本而定 |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max、avg |
| 计算方式 | 使用 PG 提供的 lag 字段或基于 WAL 位置/时间估算；实现需固定口径 |
| 前置条件 | 复制环境、读取复制状态权限、时钟口径可信 |
| 缺数语义 | 无备库为不适用；主备切换期间应标记状态变化，避免误报 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | 复制延迟过高 |
| UI 展示 | 趋势图 + 复制节点维度 |

#### `pg.replication.wal_lag_bytes` — WAL 延迟字节数

| 字段 | 内容 |
|---|---|
| 定义 | 主备间 WAL 发送、接收或回放位置差对应的字节量 |
| 单位 | bytes |
| 类型 | gauge |
| 维度 | instance、replica |
| 数据来源 | `pg_stat_replication`, `pg_stat_wal_receiver`，LSN 差值函数 |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | LSN 差值转换为 bytes；需区分 sent/write/flush/replay lag |
| 前置条件 | 复制环境、读取复制状态权限 |
| 缺数语义 | 无复制拓扑为不适用；采集失败需单独显示 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | WAL 积压、复制延迟 |
| UI 展示 | 趋势图 + 节点维度 |

#### `pg.replication_slot.retained_wal_bytes` — Replication slot WAL 积压

| 字段 | 内容 |
|---|---|
| 定义 | Replication slot 导致保留的 WAL 字节量 |
| 单位 | bytes |
| 类型 | gauge |
| 维度 | instance、slot |
| 数据来源 | `pg_replication_slots`，结合当前 WAL LSN |
| 采集方式 | 数据库连接 |
| 默认采样周期 | 30s - 60s |
| 增强采样周期 | 10s - 30s |
| 聚合方式 | latest、max |
| 计算方式 | 当前 WAL LSN 与 slot restart/confirmed_flush LSN 差值；具体字段按 slot 类型和 PG 版本确定 |
| 前置条件 | 读取 replication slot 状态权限；PG 版本字段差异需适配 |
| 缺数语义 | 无 slot 为 0 或不适用需按产品口径固定；无权限为不可用 |
| 采集成本 | 低 |
| 标准监控 | 是 |
| 增强监控 | 是 |
| 是否可告警 | 是 |
| 关联事件 | Replication slot 积压、磁盘风险 |
| UI 展示 | 按 slot 列表 + 趋势图 |

---

## 5. 增强监控候选指标

增强监控用于短窗口、高频排障，不建议首版完整复刻。以下指标可先作为候选，按采集成本和依赖逐步启用。

| 指标 | 建议状态 | 原因 |
|---|---|---|
| SQL tuples/s | P1 | 需要明确与 `tup_*` 差分口径的关系 |
| 慢 SQL 数量 | P1 | 需先确定来源：日志、`pg_stat_statements` 或活动会话推断 |
| 事务状态分布 | P1 | 来自 `pg_stat_activity.state`，可高频但需控制扫描成本 |
| 数据库最大年龄 xids | P1 | 对 vacuum / wraparound 风险有价值，但需要版本和权限确认 |
| Checkpoint / buffer 指标 | P1/P2 | PG 版本字段变化，需要适配 |
| SQL 指纹耗时 / 调用次数 | P2 | 依赖 `pg_stat_statements`，涉及扩展和脱敏 |
| 表膨胀估算 | P2 | 低频或按需，不适合高频增强监控 |
| 精确膨胀扫描 | P2 | 依赖 `pgstattuple`，成本高 |

---

## 6. 差分与 reset 规则

以下指标来自累计计数器，必须差分后才能展示速率：

- `xact_commit`
- `xact_rollback`
- `tup_returned`
- `tup_fetched`
- `tup_inserted`
- `tup_updated`
- `tup_deleted`
- `temp_files`
- `temp_bytes`
- OS 磁盘 IO 计数器
- OS 网络字节计数器

建议规则：

1. 首个样本只保存原始值，不输出速率。
2. 当前值小于前值时，标记为 reset / restart / role switch，不输出负速率。
3. 采集间隔过长时，不跨过长缺口计算速率。
4. 主备切换、统计 reset、数据库重启后，重新建立基线。
5. 告警评估应忽略不可计算点，而不是将其视为 0。
6. UI 应能解释“当前点因统计重置不可计算”。

---

## 7. 标准监控与增强监控采样分层

| 指标类型 | 标准监控建议粒度 | 增强监控建议粒度 | 说明 |
|---|---:|---:|---|
| 连通性 / 主动延迟 | 10s - 30s | 5s - 10s | 低成本，可高频 |
| CPU / 内存 / 网络 / IO | 10s - 30s | 5s - 30s | 依赖 Agent，注意存储压力 |
| 连接 / 活跃连接 / TPS | 30s - 60s | 10s - 30s | 可适度高频 |
| 事务 / 行操作 / 临时文件 | 30s - 60s | 10s - 30s | 差分指标需稳定间隔 |
| 长事务 / 锁等待 / 阻塞 | 30s - 60s | 10s - 30s | 大实例需 Top-N 和限流 |
| 复制 / slot 延迟 | 30s - 60s | 10s - 30s | 主备切换期间需抑制误报 |
| SQL 画像 / 膨胀 / 大表诊断 | 5m 或按需 | 不建议默认高频 | 成本高，后置 |

---

## 8. 告警适用性原则

可告警指标必须满足：

1. 口径稳定。
2. 单位明确。
3. 缺数语义明确。
4. 支持连续 N 次或持续时间判断。
5. 能定义恢复条件。
6. 能区分真实 0 与采集异常。
7. 不因主备切换、reset、Agent 离线产生大量误报。

首版推荐内置告警指标：

- 实例不可达
- 主动探针延迟过高
- Agent 离线
- CPU 使用率高
- 内存使用率高
- 磁盘使用率高 / 剩余空间低
- 连接数过高
- 活跃连接数过高
- idle in transaction 过多
- 长事务
- 锁等待
- 阻塞会话
- 复制延迟过高
- Replication slot WAL 积压
- 临时文件写入速率过高
- 2PC 数量异常

---

## 9. 暂不进入 MVP 的指标

以下指标或能力暂不作为 MVP 稳定承诺：

1. 完整性能洞察。
2. SQL 自动优化。
3. SQL 优化收益量化。
4. SQL 指纹级完整画像。
5. 精确表膨胀扫描。
6. 自动空间回收建议。
7. 任意 SQL 诊断。
8. 调用链分析。
9. 全量 5 秒级 `pg_stat_activity` / `pg_locks` / `pg_stat_statements` 扫描。
10. 云厂商专有事件或指标。

---

## 10. 后续待确认事项

1. 监控账号默认权限是否要求 `pg_monitor`。
2. 主动探针是否复用连接，还是每次新建连接。
3. CPU / 内存口径是否以宿主机、容器 cgroup 还是数据库进程为准。
4. 连接数是否默认排除监控账号连接。
5. TPS 是否按实例汇总还是按 database 维度保留。
6. 慢查询数量的 MVP 数据源是否采用日志、活动会话推断，还是延后。
7. 复制延迟优先采用时间延迟还是 WAL bytes 延迟。
8. 无 replication slot 时，slot 指标展示为 0 还是不适用。
9. 增强监控是否与标准监控共用采集数据，还是单独高频采集。
10. 指标字典是否需要维护为机器可读配置，例如 YAML / JSON。
