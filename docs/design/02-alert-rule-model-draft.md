# 告警规则配置模型草案 v0.1

> 目标：定义 PostgreSQL 监控平台 MVP 告警能力的最小领域模型、状态语义和行为边界。  
> 适用范围：首版基础阈值告警、当前告警、告警历史、恢复记录和通知记录。  
> 依赖文档：[`01-pg-mvp-metric-dictionary.md`](01-pg-mvp-metric-dictionary.md)。  
> 当前阶段：产品 / 技术设计草案，不是最终数据库表结构。

---

## 1. 设计目标

告警模型需要支撑以下闭环：

```text
指标采集
→ 指标评估
→ 条件满足
→ 告警触发
→ 去重 / 静默 / 维护窗口处理
→ 通知
→ 持续更新
→ 条件恢复
→ 恢复通知
→ 历史查询
```

首版目标不是建设完整通用告警平台，而是为 PostgreSQL MVP 提供稳定、可解释、低噪声的基础告警能力。

---

## 2. 核心原则

### 2.1 指标字典是告警输入边界

告警规则不能重新定义指标口径，必须引用指标字典中的 `metric_id`。

告警模型依赖指标字典提供：

- 指标名称
- 单位
- 类型：gauge / counter / rate / state
- 维度
- 默认采集周期
- 聚合方式
- 是否可告警
- No Data 语义
- 权限、Agent、扩展依赖
- reset / 差分规则
- 主备角色适用性

### 2.2 首版只支持单指标单条件

MVP 告警规则建议限制为：

```text
一个规则
→ 一个监控对象范围
→ 一个指标
→ 一个条件
→ 一个持续判定
→ 一个恢复策略
→ 一个通知策略
```

暂不支持复杂 AND / OR、多指标计算、PromQL 类表达式或动态基线。

### 2.3 缺数不是 0

No Data 必须作为独立状态或策略处理，不得默认等同于：

- 正常
- 异常
- 0
- 已恢复

### 2.4 规则不等于告警实例

- `AlertRule` 表示用户配置或模板生成的规则。
- `AlertInstance` 表示某个资源上的一次持续异常。
- `AlertEvent` 表示状态变化、通知、确认等历史事件。

同一条规则可以在不同实例或维度上产生多个告警实例。

---

## 3. 领域对象

### 3.1 `AlertRule` — 告警规则

表示一条可启停、可评估的告警配置。

建议字段：

| 字段 | 说明 |
|---|---|
| `id` | 规则 ID |
| `name` | 规则名称 |
| `description` | 规则说明 |
| `enabled` | 是否启用 |
| `severity` | 严重级别，例如 critical / warning / info |
| `scope` | 监控范围，例如实例、实例组、节点、数据库 |
| `metric_id` | 指标字典 ID |
| `condition` | 条件表达 |
| `evaluation_interval` | 评估周期 |
| `window` | 评估窗口 |
| `consecutive_count` | 连续满足次数 |
| `for_duration` | 持续满足时长，可与连续次数二选一 |
| `recovery_policy` | 恢复策略 |
| `no_data_policy` | 无数据策略 |
| `notification_policy_id` | 通知策略 |
| `maintenance_policy_id` | 维护窗口策略 |
| `deduplication_policy` | 去重策略 |
| `created_by` | 创建人 |
| `updated_by` | 更新人 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |
| `version` | 规则版本 |

MVP 约束：

- 一条规则只绑定一个 `metric_id`。
- 一条规则只包含一个条件。
- 复杂表达式延后。
- 规则修改应增加 `version`，历史告警保留规则快照。

---

### 3.2 `AlertCondition` — 条件表达

表示指标值如何触发告警。

建议字段：

| 字段 | 说明 |
|---|---|
| `metric_id` | 指标 ID |
| `aggregation` | `latest` / `avg` / `max` / `min` / `sum` / `count` |
| `operator` | `>` / `>=` / `<` / `<=` / `=` / `!=` |
| `threshold` | 阈值 |
| `unit` | 阈值单位，必须与指标字典一致 |
| `window` | 统计窗口，例如 5m |
| `consecutive_count` | 连续触发点数 |
| `for_duration` | 持续触发时长 |

示例：

```text
metric_id: pg.connection.active
aggregation: latest
operator: >=
threshold: 90
unit: count
window: 5m
consecutive_count: 3
```

语义说明：

- `evaluation_interval` 是告警引擎多久评估一次。
- `window` 是每次评估时回看多长时间的数据。
- `aggregation` 是对窗口内样本的聚合方式。
- `consecutive_count` 表示连续多少次评估满足条件后进入告警。
- `for_duration` 表示条件持续满足多长时间后进入告警。

MVP 可以优先支持 `consecutive_count`，后续再补 `for_duration`。

---

### 3.3 `RecoveryPolicy` — 恢复策略

表示告警如何恢复。

建议字段：

| 字段 | 说明 |
|---|---|
| `auto_recover` | 是否自动恢复 |
| `recovery_operator` | 恢复比较符 |
| `recovery_threshold` | 恢复阈值 |
| `recovery_consecutive_count` | 连续恢复次数 |
| `send_recovery_notification` | 是否发送恢复通知 |

建议首版支持简单滞回：

```text
CPU >= 80% 连续 3 次触发
CPU < 70% 连续 3 次恢复
```

如果某些规则不配置恢复阈值，则默认使用触发条件的反向判断，但需在 PRD 中说明抖动风险。

---

### 3.4 `NoDataPolicy` — 无数据策略

No Data 来源可能包括：

- 数据库不可达
- Agent 离线
- 无权限
- 扩展未安装
- 指标不适用
- 采集超时
- 采集延迟
- 首个样本无法差分
- 主备切换期间指标暂不可用

建议策略：

| 策略 | 含义 | MVP 建议 |
|---|---|---|
| `ignore` | 忽略本次评估，不改变告警状态 | 默认用于短暂缺数 |
| `mark_no_data` | 告警实例进入 NO_DATA 状态 | 推荐支持 |
| `fire_no_data` | 缺数本身触发告警 | 仅用于采集状态类指标 |
| `recover_on_no_data` | 缺数视为恢复 | 不建议默认支持 |

建议：

- 业务指标缺数默认不触发、不恢复，只标记 No Data。
- 采集状态指标可以针对 Agent 离线、数据过期单独告警。
- 告警详情必须显示 No Data 原因。

---

### 3.5 `AlertInstance` — 告警实例

表示某条规则在某个资源上的一次持续异常。

建议字段：

| 字段 | 说明 |
|---|---|
| `id` | 告警实例 ID |
| `rule_id` | 规则 ID |
| `rule_version` | 触发时规则版本 |
| `resource_id` | 实例、节点、数据库、slot 等资源 ID |
| `metric_id` | 指标 ID |
| `state` | 当前状态 |
| `severity` | 严重级别快照 |
| `first_triggered_at` | 首次触发时间 |
| `last_evaluated_at` | 最近评估时间 |
| `last_state_changed_at` | 最近状态变化时间 |
| `recovered_at` | 恢复时间 |
| `current_value` | 当前值 |
| `trigger_value` | 触发值 |
| `threshold_snapshot` | 阈值快照 |
| `trigger_summary` | 触发摘要 |
| `dedup_key` | 去重键 |
| `acknowledged_by` | 确认人 |
| `acknowledged_at` | 确认时间 |
| `suppression_status` | 静默 / 维护窗口 / 抑制状态 |

---

### 3.6 `AlertEvent` — 告警事件 / 历史

表示告警实例生命周期中的事件记录。

事件类型建议：

| 事件类型 | 说明 |
|---|---|
| `PENDING_STARTED` | 条件开始满足，但未达到持续判定 |
| `FIRED` | 告警触发 |
| `UPDATED` | 告警持续中，值或摘要更新 |
| `RECOVERED` | 告警恢复 |
| `NO_DATA_ENTERED` | 进入无数据状态 |
| `NO_DATA_EXITED` | 退出无数据状态 |
| `ACKED` | 用户确认 |
| `IGNORED` | 用户忽略 |
| `SILENCED` | 被静默 |
| `MAINTENANCE_SUPPRESSED` | 被维护窗口抑制通知 |
| `NOTIFICATION_SENT` | 通知发送成功 |
| `NOTIFICATION_FAILED` | 通知发送失败 |

事件应保存关键快照：

- 规则版本
- 指标值
- 阈值
- 数据时间
- 采集时间
- 评估时间
- No Data 原因
- 通知渠道和结果

---

### 3.7 `NotificationPolicy` — 通知策略

建议字段：

| 字段 | 说明 |
|---|---|
| `id` | 通知策略 ID |
| `name` | 策略名称 |
| `contacts` | 联系人 |
| `contact_groups` | 联系人组 |
| `channels` | 通知渠道，例如邮件、短信、Webhook、IM |
| `notify_on_fire` | 触发时通知 |
| `notify_on_recovery` | 恢复时通知 |
| `repeat_interval` | 重复通知间隔 |
| `max_repeat_count` | 最大重复次数，可选 |
| `template_id` | 通知模板 |
| `retry_policy` | 通知失败重试策略 |

MVP 建议：

- 支持一个联系人组。
- 支持有限渠道。
- 支持触发通知和恢复通知。
- 支持重复通知间隔。
- 记录通知发送结果。

暂不做复杂升级、排班和值班路由。

---

### 3.8 `Silence`、`MaintenanceWindow`、`SuppressionPolicy`

三者必须区分：

| 能力 | 作用 | 是否生成告警 | 是否发送通知 |
|---|---|---:|---:|
| 静默 Silence | 临时不打扰 | 是 | 否 |
| 维护窗口 Maintenance Window | 计划内维护期间降噪 | 可配置 | 通常否 |
| 抑制 Suppression | 根因告警存在时抑制派生告警 | 可配置 | 通常否 |

#### Silence — 静默

建议用于某条告警实例或某条规则的短期通知抑制。

字段：

- `scope`
- `starts_at`
- `ends_at`
- `reason`
- `created_by`

MVP 建议：告警仍生成和更新，但不发送通知。

#### MaintenanceWindow — 维护窗口

用于实例、规则或实例组在指定时间段内抑制通知。

字段：

- `scope`
- `starts_at`
- `ends_at`
- `repeat_rule`，可后置
- `reason`

MVP 建议：维护窗口期间记录评估结果，但默认不发送通知，并在历史中标记。

#### SuppressionPolicy — 抑制策略

用于减少同一根因造成的大量派生告警。

MVP 建议只支持固定关系，例如：

- 实例不可达时，抑制该实例下 CPU、连接数、TPS 等派生告警通知。
- Agent 离线时，抑制依赖 Agent 的 OS 指标告警通知。

不建议首版支持任意表达式化抑制规则。

---

### 3.9 `AlertTemplate` — 内置告警模板

内置模板是“一键告警”的基础，不应只是 UI 按钮。

建议字段：

| 字段 | 说明 |
|---|---|
| `id` | 模板 ID |
| `name` | 模板名称 |
| `metric_id` | 适用指标 |
| `default_severity` | 默认严重级别 |
| `default_threshold` | 默认阈值 |
| `default_aggregation` | 默认聚合 |
| `default_window` | 默认窗口 |
| `default_consecutive_count` | 默认连续次数 |
| `default_recovery_policy` | 默认恢复策略 |
| `required_capability` | 所需能力，例如 Agent、扩展、权限 |
| `applicable_pg_versions` | 适用 PG 版本 |
| `editable` | 用户是否可编辑 |

---

## 4. 告警状态机

### 4.1 状态定义

| 状态 | 含义 |
|---|---|
| `OK` | 当前未触发 |
| `PENDING` | 条件满足，但尚未达到连续次数或持续时间 |
| `FIRING` | 告警已触发且未恢复 |
| `NO_DATA` | 评估所需数据不可用或过期 |
| `SUPPRESSED` | 告警通知被静默、维护窗口或抑制策略压制 |
| `RECOVERED` | 告警已恢复，进入历史 |

MVP 可不把 `SUPPRESSED` 作为主状态，而作为 `FIRING` 上的通知状态；但 UI 和历史必须能展示被抑制原因。

### 4.2 推荐状态流转

```text
OK
 ├─ 条件满足但未持续足够久 → PENDING
 ├─ 数据缺失 → NO_DATA

PENDING
 ├─ 连续满足达到阈值 → FIRING
 ├─ 条件不满足 → OK
 ├─ 数据缺失 → NO_DATA

FIRING
 ├─ 恢复条件满足 → RECOVERED
 ├─ 数据缺失 → NO_DATA 或保持 FIRING + 标记数据缺失
 ├─ 被静默 / 维护窗口 / 抑制 → FIRING + suppressed

NO_DATA
 ├─ 数据恢复且条件不满足 → OK
 ├─ 数据恢复且条件满足但未持续足够久 → PENDING
 ├─ 数据恢复且满足触发条件 → FIRING

RECOVERED
 └─ 新一轮条件满足 → 新 AlertInstance
```

### 4.3 确认 / 忽略的语义

`ACKED` 或 `IGNORED` 不应自动恢复告警。

建议：

- 确认表示“有人知道了”。
- 忽略表示“本次不处理或降低关注”。
- 告警是否恢复仍由恢复条件决定。
- 确认和忽略应写入历史。

---

## 5. 去重策略

MVP 默认去重键：

```text
rule_id + resource_id + metric_dimension
```

示例：

- CPU 高：`rule_id + instance_id + node_id`
- 连接数高：`rule_id + instance_id`
- replication slot 积压：`rule_id + instance_id + slot_name`

同一去重键在未恢复前不得每次评估都创建新的告警实例，只更新已有实例。

---

## 6. MVP 内置告警建议

| 告警 | 指标 ID | 建议条件 | 说明 |
|---|---|---|---|
| 实例不可达 | `pg.availability.reachable` | 连续 N 次失败 | 需记录失败原因 |
| 主动探针延迟过高 | `pg.probe.latency_ms` | max 或 avg 超阈值 | 建议支持恢复阈值 |
| Agent 离线 | `agent.status` | offline 持续 N 次 | 抑制依赖 Agent 的派生指标通知 |
| CPU 高 | `host.cpu.usage_percent` | avg >= 80% 持续 | 可用 70% 恢复 |
| 内存高 | `host.memory.usage_percent` | avg >= 85% 持续 | 需固定内存口径 |
| 磁盘不足 | `host.disk.usage_percent` / `host.disk.free_bytes` | 使用率高或剩余空间低 | 推荐同时支持百分比和剩余空间 |
| 连接数过高 | `pg.connection.total` | latest 或 max 超阈值 | 可参考 max_connections |
| 活跃连接数过高 | `pg.connection.active` | max 超阈值 | 关联会话列表 |
| idle in transaction 过多 | `pg.connection.idle_in_transaction` | latest 超阈值 | 关联长事务 |
| 长事务 | `pg.transaction.max_duration_sec` | max 超阈值 | 关联会话详情 |
| 锁等待 | `pg.lock.waiting_count` | latest > 0 持续 | 关联阻塞链 |
| 阻塞会话 | `pg.session.blocked_count` | latest > 0 持续 | 关联阻塞链 |
| 复制延迟过高 | `pg.replication.replay_lag_ms` / `wal_lag_bytes` | 超阈值持续 | 主备切换期间需抑制 |
| Slot WAL 积压 | `pg.replication_slot.retained_wal_bytes` | 超阈值持续 | 关联磁盘风险 |
| 临时文件写入过高 | `pg.temp.bytes_per_sec` | avg 或 max 超阈值 | 关联 SQL / 会话候选 |
| 2PC 数量异常 | `pg.prepared_xacts.count` | latest > 0 或超阈值 | 视业务是否使用 2PC |

---

## 7. 页面与接口需要表达的信息

### 7.1 告警规则页

应展示：

- 规则名称
- 作用范围
- 指标名称
- 条件
- 评估周期
- 窗口
- 连续次数 / 持续时间
- 严重级别
- 启停状态
- 联系人组
- 最近触发时间
- 当前告警数
- 所需能力是否满足

### 7.2 当前告警页

应展示：

- 告警状态
- 严重级别
- 实例 / 资源
- 规则名称
- 触发值
- 阈值
- 首次触发时间
- 持续时长
- 最近评估时间
- 是否静默 / 维护窗口 / 抑制
- No Data 原因
- 关联指标图
- 关联事件 / 会话 / 日志入口

### 7.3 告警详情页

应展示：

- 规则快照
- 触发指标和单位
- 触发时序图
- 恢复条件
- 当前值和历史值
- 评估记录
- 通知记录
- 确认 / 忽略记录
- No Data 记录
- 关联性能事件
- 关联采集状态

### 7.4 告警历史页

应支持筛选：

- 实例
- 规则
- 严重级别
- 状态
- 时间范围
- 是否恢复
- 是否通知失败
- 是否被静默 / 维护窗口抑制

---

## 8. 应避免过度设计

首版不建议实现：

1. 任意多层嵌套 AND / OR。
2. PromQL 类查询语言。
3. 多指标复杂数学表达式。
4. 动态基线和机器学习阈值。
5. 完整异常检测平台。
6. 自动根因分析。
7. 任意告警依赖图。
8. 多级通知升级和排班。
9. 大规模跨实例聚合告警。
10. 复杂租户策略继承。
11. 可编程通知模板和脚本。
12. 自动修复动作。
13. 云厂商告警兼容层。

---

## 9. 待确认事项

1. MVP 是否同时支持 `consecutive_count` 和 `for_duration`，还是先只支持连续次数。
2. 恢复阈值是否首版必填，还是可选。
3. No Data 是否生成独立告警实例，还是作为已有告警状态。
4. 维护窗口期间是否生成告警但不通知，还是完全暂停评估。
5. 告警确认是否需要备注和责任人字段。
6. 告警规则修改后，未恢复的告警实例是否继续按旧规则评估，还是按新规则重算。
7. 联系人、联系人组、通知渠道是否由本平台管理，还是对接外部通知系统。
8. 告警严重级别是否固定为 critical / warning / info，还是支持自定义。
9. 告警模板是否允许用户修改默认阈值并保存为自定义模板。
10. 是否需要为每个告警保存触发时的会话 / 阻塞快照。
