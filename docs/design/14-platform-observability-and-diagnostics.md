# 平台自身运行可观测性与诊断出口 v1.0

> 目标：定死「平台自己坏了」如何被看见——运行事实模型、诊断/导出入口、非递归告警边界、保留与磁盘水位策略、验收场景。
> 决策票：[T14 · 平台自身运行可观测性与诊断出口](https://github.com/liumingjian/dbs-monitor/issues/32)。
> 输入边界：[T2 · 时序存储选型与指标数据模型](04-metric-storage-model.md)（分区维护是平台级故障，完整可见形态移交本票）、[T8 · 打包、部署与运行形态](09-packaging-and-deployment.md)（D6 自举不许说谎、本地通知快照；D10 运行期磁盘水位保护经 [T12](12-collection-concurrency-timeouts-and-backpressure.md) 移交本票）、[T12 · 采集并发限流、超时与背压](12-collection-concurrency-timeouts-and-backpressure.md)（D8 已产出每任务状态与 60 秒汇总日志；诊断 API / Prometheus 出口 / 平台自身告警 / 保留策略移交本票）、[T13 · 凭据加密存储、轮换与吊销](13-credential-encryption-rotation-and-revocation.md)（密钥类故障是平台自身故障；诊断出口服从其秘密禁区）。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。
> **本票只产决策文档，不修改业务代码。** 后续实现交给 R3。
> 落盘说明：本票 2026-08-05 在 [#32 关票评论](https://github.com/liumingjian/dbs-monitor/issues/32) 中冻结结论，当时链接的即本路径但文件未随票落盘；本文档为该冻结结论的仓库落盘，内容以关票评论为准，未新增决策。

---

## 0. 一句话结论

**平台自身可观测性的规范入口是结构化 systemd journal 加同一 HTTPS 下仅 `PLATFORM_ADMIN` 可访问的只读诊断 API；平台健康是独立的四态快照 `OK / DEGRADED / FAILED / UNKNOWN`，不复用目标实例健康、告警状态或 `NO_DATA`；平台自身故障绝不进入目标告警链路；journal 是持久化历史，当前快照不落库；磁盘按预警/临界/紧急分级保护，紧急时拒写新样本，但绝不自动删除旧分区或缩短 30 天保留期。**

---

## 1. D1 · 规范入口：journal + 只读诊断 API

- **结构化 systemd journal** 是基础出口：平台所有自身运行事实的规范历史记录都写在这里（承 [T12](12-collection-concurrency-timeouts-and-backpressure.md) D8：状态变化事件 + 每 60 秒结构化汇总）。
- **只读诊断 API**：挂在与产品 UI 同一个 HTTPS 端点下，仅 `PLATFORM_ADMIN` 角色可访问，只读。这是管理员在 UI 侧看「平台自己怎么样」的规范入口。
- **诊断包**是限时、限量、脱敏的**附加支持 artifact**——面向现场支持流程，不是常态出口。
- **R2 不提供 Prometheus 文本出口**（见 §8 否决记录）。

## 2. D2 · 平台健康模型：独立四态快照

平台健康是一个**独立的四态快照**：`OK / DEGRADED / FAILED / UNKNOWN`，归并顺序为 `FAILED > UNKNOWN > DEGRADED > OK`。

- **不复用**目标实例健康模型（R1 不变式 2）、告警状态五档或 `NO_DATA`。平台故障被渲染成「没有数据」与假 `NO_DATA` 是同类错误（承 [T8](09-packaging-and-deployment.md) D6「自举不许说谎」）。
- 服务端进程、自带 PostgreSQL、采集调度器、**分区维护**、证书、Agent 接入与磁盘水位各自产生的运行事实归并进这一个快照，其中分区维护失败的完整可见形态即 [T2](04-metric-storage-model.md) §6 机制 4 预留给本票的那半（见 §7 第 1 笔）。

## 3. D3 · 非递归告警边界

- 平台自身故障**只走三个出口**：journal、[T8](09-packaging-and-deployment.md) D6 的本地通知快照、诊断 API。**不创建目标告警实例**——不进入产品的存储/评估/通知链路，否则平台会经正在故障的同一链路递归告警自身（承 [T12](12-collection-concurrency-timeouts-and-backpressure.md) §9.2 的论证）。
- 进程存活沿用 systemd `Restart=always`。**不增加** watchdog、第二套监控栈或高可用（承 [T8](09-packaging-and-deployment.md) §15 已否决项，本票确认边界不变）。

## 4. D4 · 持久化与磁盘分级保护

**持久化**：

- journal 保存历史，由系统 journald 轮转限额约束体量。
- 当前健康快照是瞬时态：**不写入目标 `metric_sample`，也不建平台运行事实表**（不给平台自身长出第二套时序存储）。

**运行期磁盘水位保护**（承 [T8](09-packaging-and-deployment.md) §10 D10 移交，经 [T12](12-collection-concurrency-timeouts-and-backpressure.md) 二次转手，见 §7 第 2 笔）：

- 磁盘水位**按预警 / 临界 / 紧急分级**，逐级反映进 §2 的健康快照与 §3 的出口。
- **紧急水位时拒绝写入新样本**——用可见的采集失败换存储安全。
- **绝不自动删除旧分区、绝不自动缩短 30 天保留期**：任何保护都不能静默改变保留承诺（[T8](09-packaging-and-deployment.md) D10 移交时的原话边界）。

## 5. D5 · 安全边界：诊断出口的秘密禁区

诊断出口（journal、诊断 API、诊断包）**不得包含**：密码、密文、主密钥、Agent token、`Authorization` 头、DSN、原始 SQL、请求体。

这是 [T13](13-credential-encryption-rotation-and-revocation.md) §9.2 秘密禁区在诊断面的延续（见 §7 第 4 笔），也覆盖 [T12](12-collection-concurrency-timeouts-and-backpressure.md) D8 对状态变化日志的脱敏要求。

## 6. D6 · 验收：四类可重复故障注入

用四类**可重复**的故障注入验收「平台故障给出可行动且不自相矛盾的诊断」：

1. 平台自带 PG 不可达；
2. 磁盘水位越级（预警 → 临界 → 紧急）；
3. 采集池饱和；
4. 证书过期。

每类注入后，§1 的入口须给出与 §2 四态一致、可行动的诊断，且不违反 §3 非递归边界与 §5 秘密禁区。

## 7. 上游移交的接收登记

本票是四笔上游移交的显式去向，均已在上文接住：

| # | 出处 | 移交内容 | 在本票的落点 |
|---|---|---|---|
| 1 | [T2 · 04-metric-storage-model](04-metric-storage-model.md) §6 机制 4 | 分区维护失败的完整可见形态（MVP 只有结构化 error 日志 + 平台级健康标志位） | §2：分区维护是四态快照的一等事实来源；§1 的 journal 与诊断 API 是其出口 |
| 2 | [T8 · 09-packaging-and-deployment](09-packaging-and-deployment.md) §10 D10（经 [T12](12-collection-concurrency-timeouts-and-backpressure.md) §9.2 二次转手） | 运行期磁盘水位保护：告警 / 拒写 / 删分区 / 缩保留期如何取舍 | §4：分级组合——预警/临界反映进健康与出口，紧急拒写新样本；删旧分区与缩短保留期被否决 |
| 3 | [T12 · 12-collection-concurrency…](12-collection-concurrency-timeouts-and-backpressure.md) §9.2 | 诊断 API、Prometheus 文本出口、平台自身告警、日志/指标保留 | §1：诊断 API 定形，Prometheus 出口 R2 否决；§3：平台自身告警走非递归三出口；§4：保留归 journald 轮转 |
| 4 | [T13 · 13-credential-encryption…](13-credential-encryption-rotation-and-revocation.md) §13 | 密钥缺失、权限错误、密文认证失败作为平台自身故障暴露；诊断出口服从秘密禁区 | §2：密钥类故障是平台健康事实、不投影成目标实例 `DB_UNREACHABLE` / `NO_DATA`；§5：秘密禁区全文照收 |

## 8. 否决记录

| 被否决 | 为什么 |
|---|---|
| Prometheus 文本出口（R2） | 平台已否决 Prometheus 全家桶做底座（地图约束 3）；R2 无消费者，出口即负债。需要时另开决策记录 |
| 平台自身告警进入产品告警链路 | 经正在故障的同一存储/评估/通知链路递归告警自身；平台库故障时会把「观测通道坏了」表现成「没有问题」（[T12](12-collection-concurrency-timeouts-and-backpressure.md) §9.2） |
| 平台运行事实写入 `metric_sample` / 复用 `NO_DATA` | 同上游 [T12](12-collection-concurrency-timeouts-and-backpressure.md) D8 边界；平台故障渲染成「没有数据」与假 `NO_DATA` 同类错误 |
| watchdog / 第二套监控栈 / 高可用 | 承 [T8](09-packaging-and-deployment.md) §15 既有否决：与 50 实例规模基线不成比例，毁掉整包交付简单性；整机宕机的外部存活探测在地图 Out of scope |
| 磁盘保护自动删旧分区 / 自动缩短保留期 | 静默改变 30 天保留承诺；[T8](09-packaging-and-deployment.md) D10 移交时点名的禁区 |

## 9. 交给下游

| 去向 | 内容 |
|---|---|
| R3 | 四态健康快照与归并实现、只读诊断 API、诊断包生成、磁盘三级水位保护、四类故障注入验收场景落成测试 |
