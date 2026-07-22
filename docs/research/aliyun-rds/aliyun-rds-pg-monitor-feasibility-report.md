# PostgreSQL 数据库监控平台可行性调研报告

> 调研主题：参考阿里云 RDS PostgreSQL「监控与报警」页面，评估其作为私有化 / AP 部署场景下自研数据库监控平台雏形的可行性。  
> 调研日期：2026-07-14  
> 调研范围：标准监控、增强监控、会话管理、性能事件、报警。  
> 明确排除：性能洞察模块。  
> 当前阶段：可行性分析，不涉及开发实现。

---

## 1. 背景与目标

用户计划开发一个数据库监控平台，首版以 PostgreSQL 数据库为基准，未来扩展到其他数据库类型。平台主要面向私有化 / AP 部署场景，希望参考阿里云 RDS PostgreSQL 控制台中的监控页面、指标体系、告警模型和交互风格。

本次调研目标不是立即开发，而是回答以下问题：

1. 阿里云 RDS PostgreSQL 的监控与报警页面有哪些可借鉴的产品设计？
2. 这些监控能力在私有化 PostgreSQL 环境中是否技术可行？
3. 哪些能力适合进入首版 MVP？
4. 哪些能力需要 Agent、插件、扩展或额外权限？
5. 哪些能力不适合首版实现？
6. 如果以阿里云监控为模板，自研平台在稳定性和用户体验上需要注意什么？

---

## 2. 调研对象与取证资产

### 2.1 调研页面

阿里云 RDS PostgreSQL 实例详情页：

```text
https://rdsnext.console.aliyun.com/detail/pgm-wz9t7bv5jsk55jh1/monitorAlarm?spm=5176.28369458.0.0.36961450Jepyio&region=cn-shenzhen&DedicatedHostGroupId=
```

页面位置：

```text
云数据库 RDS / 实例列表 / 监控与报警
```

实例状态：

```text
test / 运行中
```

### 2.2 调研模块

本次重点调研以下模块：

- 标准监控
- 增强监控
- 会话管理
- 性能事件
- 报警

不纳入本次分析：

- 性能洞察

### 2.3 已保存资产

以下文件作为本次调研取证资产，位于本报告同级的 evidence 目录：

```text
docs/research/aliyun-rds/evidence/
```

主要资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-标准监控-viewport.png
evidence/screenshots/aliyun-rds-增强监控-viewport.png
evidence/screenshots/aliyun-rds-会话管理-viewport.png
evidence/screenshots/aliyun-rds-性能事件-viewport.png
evidence/screenshots/aliyun-rds-报警-viewport.png
evidence/screenshots/aliyun-rds-standard-monitor.png
evidence/snapshots/aliyun-rds-standard-monitor.yml
evidence/screenshots/aliyun-rds-standard-monitor-2.png
evidence/snapshots/aliyun-rds-standard-monitor-2.yml
evidence/screenshots/aliyun-rds-enhanced-monitor.png
evidence/snapshots/aliyun-rds-enhanced-monitor.yml
evidence/screenshots/aliyun-rds-session-management.png
evidence/snapshots/aliyun-rds-session-management.yml
evidence/screenshots/aliyun-rds-alarm-tab.png
evidence/snapshots/aliyun-rds-alarm-tab.yml
evidence/snapshots/aliyun-rds-monitor-main.yml
evidence/snapshots/aliyun-rds-current.yml
evidence/extracted-text/aliyun-rds-current-innertext.txt
```

> 说明：截图和快照仅用于产品调研和后续可行性分析，不包含对真实云资源的变更操作。

---

## 3. 阿里云页面整体结构观察

阿里云 RDS 实例监控页采用典型云控制台布局：

1. 顶部全局导航
2. 左侧 RDS 产品导航
3. 实例标题区
4. 实例操作区
5. 实例功能菜单
6. 主内容区 tab 面板

主内容区包含以下 tab：

- 性能洞察
- 标准监控
- 增强监控
- 会话管理
- 性能事件
- 报警

本次调研排除性能洞察，重点观察后五个与监控告警平台 MVP 关系更直接的模块。

页面可见实例操作包括：

- 登录数据库
- 操作指引
- HTAP 加速
- 迁移数据库
- 重启实例
- 备份实例
- 加入全球多活数据库
- 任务中心
- 修改实例

这些操作说明阿里云将监控能力放在实例运维上下文中，而不是孤立的监控系统中。对自研平台而言，这一点有参考价值：监控页面应服务于排障路径，而不仅是指标展示。

---

## 4. 标准监控调研

参考资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-标准监控-viewport.png
evidence/screenshots/aliyun-rds-standard-monitor.png
evidence/snapshots/aliyun-rds-standard-monitor.yml
evidence/screenshots/aliyun-rds-standard-monitor-2.png
evidence/snapshots/aliyun-rds-standard-monitor-2.yml
```

### 4.1 页面结构

标准监控页展示为 PostgreSQL 指标大盘，观察到标题类似：

```text
DAS标准视角 - PostgreSQL指标大盘
```

可见控件包括：

- 数据粒度
- 列数：2 列
- 动态排序
- 光标联动

这说明标准监控不是简单指标列表，而是一个可配置的多图表监控大盘。

### 4.2 可见指标

标准监控中观察到的指标包括：

#### 4.2.1 主机 / 资源类指标

- CPU 使用率
- 用户 CPU
- 系统 CPU
- 内存使用率
- 可用内存
- 磁盘空间
- 磁盘使用量
- 网络流量
- 数据盘 IOPS

#### 4.2.2 数据库性能类指标

- 数据库延迟
- TPS
- 操作行数
- 连接数
- 慢查询
- 长事务
- 2PC
- 膨胀点

### 4.3 产品价值

标准监控适合作为自研平台的默认总览页参考。其价值在于：

1. 覆盖主机资源与数据库核心指标。
2. 使用图表卡片展示趋势。
3. 支持粒度切换。
4. 支持图表联动。
5. 支持两列布局，信息密度较高。
6. 可作为日常运维入口。

### 4.4 自研平台借鉴建议

首版可设计一个“标准监控”页面，包含：

- 实例可用性
- 主动延迟
- CPU / 内存 / 磁盘 / 网络
- 连接数
- 活跃连接数
- TPS
- 提交 / 回滚
- 读写行数
- 临时文件
- 慢查询数量
- 长事务
- 锁等待
- 主备延迟

每个图表卡片建议包含：

- 指标名称
- 当前值
- 单位
- 时间范围
- 趋势图
- 最近采集时间
- 数据来源
- 是否需要 Agent
- 是否估算
- 异常状态

---

## 5. 增强监控调研

参考资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-增强监控-viewport.png
evidence/screenshots/aliyun-rds-enhanced-monitor.png
evidence/snapshots/aliyun-rds-enhanced-monitor.yml
```

### 5.1 页面结构

增强监控相比标准监控更偏实时诊断视图。

可见控制信息包括：

- 时间窗口：约 30 分钟
- 粒度：5 秒
- 聚合方式：平均
- 布局：二列

这说明增强监控用于观察短时间窗口内的高频波动，适合排查瞬时问题。

### 5.2 可见指标

增强监控中观察到的指标包括：

#### 5.2.1 数据库连接与 SQL 活动

- 连接
- SQL tuples/s
- 慢 SQL
- 临时文件 Bytes/s

#### 5.2.2 事务与年龄

- TPS
- 事务状态
- 数据库最大年龄 xids

#### 5.2.3 主机资源

- CPU
- 内存
- IOPS
- 吞吐
- 磁盘

#### 5.2.4 复制相关

- 只读同步延迟
- ReplicationSlot 延迟

### 5.3 标准监控与增强监控对比

| 维度 | 标准监控 | 增强监控 |
|---|---|---|
| 目标 | 日常运维总览 | 高频实时排障 |
| 时间范围 | 常规趋势观察 | 短窗口，约 30 分钟 |
| 数据粒度 | 相对较粗 | 5 秒级 |
| 指标范围 | 基础资源 + DB 核心指标 | 更细的连接、事务、复制、IO 指标 |
| 适用场景 | 日常巡检、健康观察 | 故障定位、抖动分析 |
| 实现难度 | 中 | 较高 |
| MVP 优先级 | 高 | 中高 |

### 5.4 自研平台借鉴建议

不建议首版完整复刻增强监控。更合理的做法是：

1. 以标准监控作为默认主页面。
2. 提供“实时诊断”或“高频监控”入口。
3. 高频监控首版只保留核心轻量指标。
4. 重型指标按需采集或低频采集。

建议采样分层：

| 指标类型 | 建议粒度 |
|---|---:|
| 连通性 / 主动延迟 | 5s - 10s |
| CPU / 内存 / 活跃连接 / TPS | 10s - 30s |
| 普通数据库统计 | 30s - 60s |
| 膨胀估算 / SQL 画像 / 大表诊断 | 5m 或按需 |

增强监控的最大风险是采集开销。如果对大实例每 5 秒全量扫描 `pg_stat_activity`、`pg_locks`、`pg_stat_statements`，可能对数据库造成额外压力。

---

## 6. 会话管理调研

参考资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-会话管理-viewport.png
evidence/screenshots/aliyun-rds-session-management.png
evidence/snapshots/aliyun-rds-session-management.yml
```

### 6.1 当前观察结果

当前资产中，会话管理页没有提取到明确表格数据。

可能原因包括：

1. 当前实例无明显活跃会话。
2. 页面数据未加载完成。
3. 会话列表由动态组件、iframe 或虚拟列表承载。
4. 当前账号权限或 API 返回限制。
5. 快照深度不足。

### 6.2 会话管理应覆盖的典型能力

虽然当前页面未提取到完整表格，但结合 PostgreSQL 运维场景，会话管理模块应至少覆盖：

- 当前会话列表
- 活跃 / 空闲状态
- 用户名
- 数据库名
- 客户端地址
- SQL 文本
- 查询开始时间
- 事务开始时间
- 会话持续时间
- 等待事件
- 锁等待
- 阻塞源
- 阻塞链
- 会话详情

### 6.3 PostgreSQL 可实现性

可基于以下系统视图和函数实现：

- `pg_stat_activity`
- `pg_locks`
- `pg_blocking_pids()`
- `pg_stat_database`
- `pg_stat_replication`
- `pg_stat_ssl`

| 能力 | 可实现性 | 来源 |
|---|---:|---|
| 当前连接数 | 高 | `pg_stat_activity` |
| 会话列表 | 高 | `pg_stat_activity` |
| 活跃 / 空闲状态 | 高 | `pg_stat_activity.state` |
| 当前 SQL | 高，但需权限与脱敏 | `pg_stat_activity.query` |
| 查询持续时间 | 高 | `query_start` |
| 事务持续时间 | 高 | `xact_start` |
| 等待事件 | 高 | `wait_event_type`, `wait_event` |
| 阻塞关系 | 高 | `pg_blocking_pids()`, `pg_locks` |
| cancel query | 可实现但有风险 | `pg_cancel_backend()` |
| terminate session | 可实现但高风险 | `pg_terminate_backend()` |

### 6.4 自研平台建议

会话管理应作为首版重点能力之一，但首版应以只读排障为主。

建议首版支持：

- 会话列表
- 活跃会话筛选
- 长事务识别
- 锁等待识别
- 阻塞链展示
- SQL 文本截断
- SQL 文本脱敏
- 慢查询候选识别

不建议首版默认开放：

- kill session
- cancel query
- 批量终止会话
- 自动处理阻塞

这些属于写操作，应单独设计权限、审计、二次确认和操作回滚策略。

---

## 7. 性能事件调研

参考资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-性能事件-viewport.png
```

### 7.1 页面结构

性能事件页可见信息包括：

- 事件和建议
- 计划中
- 执行中
- 完成
- 时间范围：近 1 小时至 7 天
- 事件级别：
  - 异常
  - 危险
  - 警告
  - 通知
  - 优化
- 事件字段：
  - 事件级别
  - 事件名称
  - 事件类型
  - 时间
  - 持续时长
  - 原因
  - 操作

还观察到优化相关描述：

- SQL 优化
- 回收空间收益

### 7.2 本质判断

性能事件不是 PostgreSQL 原生提供的事件流，而是云平台基于多个数据源派生出的诊断结果。

其底层可能依赖：

1. 监控指标时序
2. PostgreSQL 系统视图
3. SQL / 日志
4. 规则引擎
5. 历史基线
6. 诊断策略
7. 云平台资源状态

因此，自研平台若要实现类似“性能事件”，核心不是读取某一个 PostgreSQL 表，而是建设规则引擎和事件归因能力。

### 7.3 MVP 建议

首版可以做轻量事件中心，不建议直接实现复杂自动诊断。

建议首版事件类型：

- 实例不可达
- 连接数过高
- 活跃会话突增
- TPS 突降
- 慢查询增多
- 长事务
- 锁等待
- 阻塞链
- 主备延迟过高
- 磁盘空间不足
- CPU / 内存 / IO 异常
- 日志中出现错误模式

建议事件字段：

- 事件 ID
- 实例
- 级别
- 类型
- 触发指标
- 首次发生时间
- 最近发生时间
- 持续时长
- 状态：触发中 / 已恢复 / 已确认 / 已忽略
- 原因摘要
- 建议动作
- 关联指标图
- 关联 SQL / 会话 / 日志

---

## 8. 报警模块调研

参考资产：

```text
evidence/extracted-text/aliyun-rds-tabs-innertext.json
evidence/screenshots/aliyun-rds-报警-viewport.png
evidence/screenshots/aliyun-rds-alarm-tab.png
evidence/snapshots/aliyun-rds-alarm-tab.yml
```

### 8.1 页面结构

报警模块可见信息包括：

- 一键告警
- 规则设置
- 规则名
- 监控项
- 周期
- 规则
- 状态
- 联系人组

页面信息显示报警能力由云监控提供。这说明阿里云 RDS 的报警页并不是完全独立实现，而是 RDS 控制台与云监控平台能力的组合。

### 8.2 报警规则模型推断

从可见字段看，报警规则至少包含：

- 规则名称
- 监控指标
- 检测周期
- 阈值规则
- 启停状态
- 联系人组
- 一键告警模板
- 规则设置入口

### 8.3 自研平台告警模型建议

建议抽象为通用告警规则模型：

```text
告警规则
├── 基本信息
│   ├── 规则名称
│   ├── 实例 / 集群
│   ├── 数据库类型
│   ├── 启用状态
│   └── 严重级别
├── 监控对象
│   ├── 实例
│   ├── 节点
│   ├── 数据库
│   ├── 用户
│   └── 查询指纹，可选
├── 指标条件
│   ├── 指标名
│   ├── 聚合方式：avg / max / min / sum / count / latest
│   ├── 时间窗口
│   ├── 比较符：> / >= / < / <= / = / !=
│   ├── 阈值
│   └── 连续触发次数 / 持续时间
├── 通知策略
│   ├── 联系人
│   ├── 联系人组
│   ├── 通知渠道
│   ├── 静默时间
│   ├── 重复通知间隔
│   └── 维护窗口
├── 恢复条件
│   ├── 自动恢复
│   ├── 恢复阈值
│   └── 恢复通知
└── 历史记录
    ├── 告警触发历史
    ├── 恢复历史
    ├── 确认记录
    └── 通知记录
```

### 8.4 首版推荐内置告警

#### 实例可用性

- 实例不可达
- 主动探针延迟过高
- 连接失败率过高

#### 主机资源

- CPU 使用率高
- 内存使用率高
- 磁盘使用率高
- 磁盘剩余空间低
- IOPS 异常
- IO 吞吐异常

#### PostgreSQL 指标

- 连接数过高
- 活跃连接数过高
- 长事务
- 锁等待
- 阻塞会话
- TPS 异常下降
- 慢查询数量升高
- 临时文件写入过高
- 复制延迟过高
- Replication slot 延迟过高
- prepared transaction 数量异常

---

## 9. PostgreSQL 指标采集可行性映射

### 9.1 PostgreSQL 内部可直接采集

| 能力 | 来源 |
|---|---|
| 当前连接 / 会话 | `pg_stat_activity` |
| 活跃会话 | `pg_stat_activity.state` |
| 查询时长 | `pg_stat_activity.query_start` |
| 事务时长 | `pg_stat_activity.xact_start` |
| 等待事件 | `pg_stat_activity.wait_event_type`, `wait_event` |
| 锁等待 | `pg_locks`, `pg_blocking_pids()` |
| 数据库事务统计 | `pg_stat_database` |
| 提交 / 回滚 | `pg_stat_database.xact_commit`, `xact_rollback` |
| 读写行数 | `pg_stat_database.tup_*` |
| 临时文件 | `pg_stat_database.temp_files`, `temp_bytes` |
| 复制状态 | `pg_stat_replication`, `pg_stat_wal_receiver` |
| Replication slot 延迟 | `pg_replication_slots` |
| 2PC | `pg_prepared_xacts` |
| checkpoint / buffer | `pg_stat_bgwriter`, `pg_stat_checkpointer`，视 PG 版本而定 |

### 9.2 需要 OS Agent 的指标

仅通过数据库连接无法稳定获得以下宿主机级指标：

- CPU
- 内存
- 磁盘空间
- 磁盘 IO
- IOPS
- 网络流量
- 文件系统状态
- 进程资源
- 容器 / cgroup 指标
- PG 日志文件采集

这些能力建议通过自研 Agent、node exporter 或类似采集器实现。

### 9.3 需要插件或扩展的能力

| 能力 | 依赖 |
|---|---|
| SQL 维度耗时、调用次数、IO | `pg_stat_statements` |
| SQL 指纹分析 | `pg_stat_statements` 或自研 SQL 归一化 |
| 精确表膨胀分析 | `pgstattuple` |
| 慢 SQL 日志 | `log_min_duration_statement` + 日志采集 |
| 表级膨胀估算 | `pg_stat_user_tables`, 估算 SQL |

### 9.4 需要主动探针的能力

数据库延迟不应简单理解为 PostgreSQL 内部指标，建议通过主动探针定义，例如：

```sql
SELECT 1;
```

或执行一个轻量事务往返，记录连接、认证、执行和返回耗时。

---

## 10. 能力分级建议

### 10.1 MVP 可做

建议首版 MVP 包含：

- 单实例标准监控
- 主动连通性探测
- 主动延迟探测
- CPU / 内存 / 磁盘 / 网络，依赖 Agent
- 连接数
- 活跃连接数
- TPS
- 提交 / 回滚
- 读写行数
- 临时文件
- 长事务
- 锁等待
- 阻塞链
- 基础复制状态
- 主备延迟
- Replication slot 延迟
- 会话列表
- 基础性能事件
- 基础阈值告警
- 告警历史
- 采集状态 / Agent 状态

### 10.2 建议需要 Agent 的能力

- 所有 OS 指标
- 日志采集
- 慢 SQL 日志解析
- 进程级资源
- 容器资源指标
- 磁盘与网络指标
- 高频本地采样
- 跨网络环境下的本地缓存与重试

### 10.3 需要插件 / 扩展的能力

- SQL 画像
- SQL 指纹级耗时
- SQL 调用次数
- SQL 维度 IO
- 精确膨胀分析
- 更深入的表级诊断

### 10.4 暂不建议首版实现

- 完整性能洞察
- SQL 自动优化
- 自动优化收益量化
- 精确膨胀扫描
- 自动空间回收建议
- 任意 SQL 诊断
- 调用链分析
- kill / cancel 会话操作
- 强依赖云厂商专有事件的平台能力

---

## 11. 稳定性风险分析

### 11.1 采集开销风险

高风险操作包括：

- 每 5 秒全量扫描 `pg_stat_activity`
- 每 5 秒全量扫描 `pg_locks`
- 高频读取 `pg_stat_statements`
- 采集完整 SQL 文本
- 高频计算阻塞链
- 对大表做膨胀估算
- 全库扫描诊断 SQL

建议：

1. 分层采集。
2. 重型指标低频或按需。
3. 限制 Top-N。
4. SQL 文本截断。
5. SQL 文本脱敏。
6. 采集 SQL 设置 timeout。
7. 采集任务错峰执行。
8. 大实例使用分页和采样。

### 11.2 统计视图差分风险

PostgreSQL 很多统计视图是累计值，必须做时间差分才能得到速率。例如：

- `xact_commit`
- `xact_rollback`
- `tup_returned`
- `tup_fetched`
- `tup_inserted`
- `tup_updated`
- `tup_deleted`
- `temp_files`
- `temp_bytes`

必须处理：

- 数据库重启
- 统计 reset
- 主备切换
- 计数器回绕
- 采集缺口
- 时间点乱序
- 实例角色变化

### 11.3 权限风险

建议使用监控专用账号，而不是业务账号。

可能需要：

- `pg_monitor`
- 系统视图读取权限
- 复制状态读取权限
- 日志读取权限，通常需 Agent
- OS 指标读取权限，通常需 Agent

不能默认假设数据库账号可以读取宿主机级资源。

### 11.4 多版本兼容风险

PostgreSQL 不同版本的统计视图和字段存在差异。例如：

- checkpoint 相关视图在不同版本中变化
- 复制状态字段存在版本差异
- 权限角色能力存在差异
- 扩展可用性取决于部署环境

建议首版做能力探测：

- PG 版本
- 是否主库 / 备库
- 是否允许创建扩展
- 是否存在 `pg_stat_statements`
- 是否具备 `pg_monitor`
- 是否具备日志采集能力
- 是否运行在容器 / VM / 物理机

### 11.5 告警噪声风险

告警系统需避免：

- 单点抖动误报
- 缺数被当作 0
- 维护窗口误报
- 主备切换期间误报
- 网络短暂抖动误报
- 同一根因产生大量重复告警

建议能力：

- 连续 N 个点触发
- 持续时间触发
- 自动恢复
- 告警去重
- 告警抑制
- 维护窗口
- 依赖关系降噪
- no data 状态单独处理

---

## 12. 用户体验风险与建议

### 12.1 不建议照搬阿里云复杂导航

阿里云控制台面向云产品全家桶，导航复杂度较高。自研私有化 / AP 平台不建议照搬整体导航。

建议首版信息架构：

```text
实例总览
├── 标准监控
├── 会话与阻塞
├── 性能事件
├── 告警规则
├── 告警历史
└── 采集状态 / Agent 状态
```

### 12.2 空状态必须解释清楚

调研中观察到页面存在 Loading 或无数据状态。自研平台应明确区分：

1. 加载中
2. 暂无样本
3. 无权限
4. Agent 离线
5. 功能不支持
6. 采集异常
7. 当前时间范围无数据

不要只显示空图表，否则用户无法判断是数据库健康、权限不足、采集失败还是系统异常。

### 12.3 指标口径必须透明

每个指标建议说明：

- 指标定义
- 数据来源
- 采集频率
- 聚合方式
- 是否估算
- 是否需要 Agent
- 是否需要扩展
- 最近更新时间

例如：

- “数据库延迟”应说明是主动探针延迟。
- “膨胀点”应说明是估算还是精确扫描。
- “慢 SQL”应说明来源是日志还是 `pg_stat_statements`。

### 12.4 以排障路径组织页面

自研平台更应围绕运维人员排障路径设计，而不是简单堆叠指标。

推荐用户路径：

1. 实例是否可用？
2. 资源是否异常？
3. 数据库负载是否异常？
4. 是否有慢 SQL / 长事务 / 锁等待？
5. 是否有复制延迟？
6. 是否有事件？
7. 是否已触发告警？
8. 下一步建议是什么？

---

## 13. 推荐 MVP 产品范围

### 13.1 首版页面

建议首版包含以下页面：

```text
实例总览
标准监控
会话与阻塞
性能事件
告警规则
告警历史
采集配置 / Agent 状态
```

### 13.2 首版核心指标

#### 可用性

- 实例连通性
- 主动探针延迟
- 最近采集时间
- Agent 状态

#### 主机资源

- CPU
- 内存
- 磁盘空间
- 磁盘 IO
- 网络流量

#### PostgreSQL

- 总连接数
- 活跃连接数
- idle in transaction
- TPS
- 提交 / 回滚
- 读写行数
- 临时文件
- 慢查询数量
- 长事务
- 锁等待
- 阻塞链
- 2PC 数量

#### 复制

- 主备状态
- 复制延迟
- Replication slot 延迟
- WAL 接收 / 回放状态

#### 告警

- 当前触发告警
- 告警规则
- 告警历史
- 恢复记录

### 13.3 首版事件

建议首版内置事件：

- 实例不可达
- Agent 离线
- CPU 高
- 内存高
- 磁盘不足
- 连接数过高
- 活跃连接突增
- 长事务
- 锁等待
- 阻塞链
- 慢查询增多
- 复制延迟过高
- Replication slot 积压
- 临时文件写入异常

---

## 14. 结论

### 14.1 是否可以参考阿里云 RDS 监控设计？

可以参考，但不建议完整照搬。

阿里云 RDS 监控页面有较强参考价值的部分包括：

- 标准监控的大盘式指标组织
- 增强监控的高频诊断视角
- 会话管理的排障方向
- 性能事件的事件化表达
- 报警模块的一键告警和规则设置模型

不建议照搬的部分包括：

- 云控制台复杂导航
- DAS / 云监控强耦合模式
- 首版覆盖过多高级指标
- 自动优化和收益量化
- 完整性能洞察
- 云厂商专有事件模型

### 14.2 技术可行性结论

整体技术可行，但要明确采集边界：

| 采集模式 | 可覆盖能力 |
|---|---|
| 仅数据库连接 | PG 内部状态、连接、事务、锁、复制、部分慢 SQL |
| 数据库连接 + Agent | 完整资源监控、日志、OS、磁盘、网络、高频指标 |
| 数据库连接 + Agent + 扩展 | SQL 画像、SQL 指纹、膨胀分析、深度诊断 |

### 14.3 推荐方向

建议将自研平台定位为：

```text
私有化 PostgreSQL 运维排障工作台
```

而不是：

```text
阿里云 RDS 监控页面复刻版
```

首版应优先解决：

1. 当前 PG 实例是否健康？
2. 哪些资源或数据库指标异常？
3. 异常从什么时候开始？
4. 是否存在慢 SQL、长事务、锁等待、复制延迟？
5. 是否已经触发告警？
6. 用户下一步应该看哪里？

### 14.4 最终判断

以阿里云 RDS PostgreSQL 监控页面作为方向参考是可行的。建议采用“标准监控 + 会话阻塞 + 性能事件 + 告警规则”的轻量化组合进入首版 MVP，增强监控作为后续高频诊断能力逐步补充。

首版不要追求完整复刻阿里云，而应更聚焦私有化 / AP 场景下的稳定采集、清晰解释、低权限运行和可操作排障路径。
