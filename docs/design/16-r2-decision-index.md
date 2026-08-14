# R2 决策索引 v1.0

> 本文把 Wayfinder 地图 [#15 · R2 系统架构骨架与技术选型](https://github.com/liumingjian/dbs-monitor/issues/15) 的 `Decisions so far` 固化到仓库。后续实现会话读本文和对应设计文档，不依赖 issue 正文仍保持可见。
> 状态：R2 决策层已收口；T11 walking skeleton 已验收。实现路线进入 R2 收口与 `/to-spec` 前置。

## 1. 贯穿路线的边界

- PG-only 私有化部署；服务端与 Agent 用 Go，前端是 TypeScript + React + Vite 纯 SPA，静态资源由 `go:embed` 打入主二进制。
- 交付采用离线 tar，自带 PostgreSQL；平台端与 Agent 支持 linux/amd64、linux/arm64，PG 指标由服务端直连采集，Agent 只采 OS 指标与心跳。
- 不引入 Prometheus 全家桶、第二存储引擎、容器交付、实例级授权或 R1 已否决的 `Silence`、`SuppressionPolicy`、`SUPPRESSED` 状态。
- R1 四条不变式继续有效：告警五状态与压制正交、实例健康单一来源、三档角色且凭据永不回显、三条内置采集状态规则不可删不可停用且严重级别不低于 `warning`。

## 2. 已冻结决策

| 票 | 仓库文档 / 研究 | 决策 gist |
|---|---|---|
| T1 | [issue #19](https://github.com/liumingjian/dbs-monitor/issues/19) | 单 Go 服务端承载服务职责；PG 指标一律服务端直连，Agent 只采 OS + 心跳；能力探测和告警评估独立循环，Agent 与实例 1:1，凭据只归服务端。 |
| T3 | [issue #21](https://github.com/liumingjian/dbs-monitor/issues/21) | Agent 使用同一份 OpenAPI 的单上报端点；强制 TLS、自签 CA、令牌只写边界；心跳与离线共用时间门槛，时间偏移超限拒收。 |
| RT-C | [issue #16](https://github.com/liumingjian/dbs-monitor/issues/16) | 保持自带 PG + 原生分区；容量与查询门槛须用真实 PG 实测，未以推算冒充证据。 |
| RT-D | [issue #17](https://github.com/liumingjian/dbs-monitor/issues/17) | `net/http` / 标准库为底，oapi-codegen strict server，sqlc + pgx 逃生舱，goose 自动迁移，Agent 使用 gopsutil。 |
| RT-E | [issue #18](https://github.com/liumingjian/dbs-monitor/issues/18) | 选 AntD 6、ECharts 6、自写图表 wrapper、TanStack Query 与 TanStack Router；缺数保留为 `null` / 缺桶，不补 0。 |
| T2 | [04-metric-storage-model.md](04-metric-storage-model.md) | 自带 PG 原生分区，窄表样本模型；非数值可画曲线的状态编码为 float8，派生采集状态留在控制面；差分写入侧完成，最新查询带时间下界。 |
| T5 | [05-backend-code-structure.md](05-backend-code-structure.md) | 按领域垂直切包，L3 cmd → L2 编排 → L1 领域 → L0 基础设施；同层默认禁止，新包与 interface 必须显式登记，领域包只接 `DBTX`。 |
| T4 | [06-metric-dictionary-and-collection-plan.md](06-metric-dictionary-and-collection-plan.md) | 字典口径编译进 Go，计划参数入库；采集一等公民是 Task；能力是封闭四态；PG13–17 矩阵与任务唯一产出测试属于 R3。 |
| T6 | [07-api-contract-and-codegen.md](07-api-contract-and-codegen.md) | OpenAPI 按域拆分、`make gen` 唯一入口、生成物入库并做漂移门；时间绝对化；空状态 200 + 封闭 13 码；角色声明覆盖每个 `operationId`。 |
| T7 | [08-frontend-stack-and-ui.md](08-frontend-stack-and-ui.md) | 三个状态桶，`domain/` 封闭清单；图表 `unavailability` 必填；枚举用 `assertNever`；轮询渲染用 `dataUpdatedAt`；TimeRangePicker 的骨架内联偏离见该文收口登记。 |
| T8 ⚠ | [09-packaging-and-deployment.md](09-packaging-and-deployment.md) | 离线 tar + 自建 PG17、socket-only、双架构与各自 glibc 下限；升级备控制面、回滚靠备份；时钟和磁盘容量是安装期硬门；整机宕机交外部基础设施。 |
| T9 | [10-ai-guardrails-and-verification.md](10-ai-guardrails-and-verification.md) | `make check` / `make check-full` 两层闭环，现行快层预算为 **≤120 秒**；Docker 真 PG；A/B 护栏登记表、两份 `CLAUDE.md`、CI PR 门；RT-C 由人工 / R3 发布门接管。 |
| T10 | [11-walking-skeleton-slice.md](11-walking-skeleton-slice.md) | 骨架只验证必须跑起来才能证伪的选型：两条采集通路、告警、认证、前端两级路由、分区和真实验收；不预摆 R3–R6 空壳。 |
| T12 | [12-collection-concurrency-timeouts-and-backpressure.md](12-collection-concurrency-timeouts-and-backpressure.md) | 中央调度、探针/查询双槽、分层超时与背压；不补跑、不自动降频；采集源完整性水位；平台自身诊断边界移交 T14。 |
| T13 | [13-credential-encryption-rotation-and-revocation.md](13-credential-encryption-rotation-and-revocation.md) | PG password 使用版本化 keyring + AES-256-GCM；Agent 显式登记、token 只存哈希；解密故障属于平台自身故障；秘密永不出站。 |
| T14 | [14-platform-observability-and-diagnostics.md](14-platform-observability-and-diagnostics.md) | 平台健康独立四态，journal 是历史，诊断 API 是管理员入口；平台故障不进入目标告警或 `NO_DATA`；磁盘紧急时拒写新样本但不自动删旧数据或缩短保留。 |
| T15 ⚠ | [15-ci-and-release-pipeline.md](15-ci-and-release-pipeline.md) | GitHub Actions 是规范执行者；PR 门为 `make check`，默认分支为 `make check-full`；语义化 tag + 精确提交校验 + Environment 审批；四种原生架构/glibc 组合与长期 Release assets。 |

## 3. 收口注记

- P0 已由 PR #34 合入 `9127f90`；本索引以 P0 回写后的文档为准，不复述地图正文中已过时的 T9 `≤90 秒`。
- P1 首轮只落 PR 门与默认分支 / 手动 `check-full` workflow；四组合原生 runner、发布审批与 Release 归档仍交下游。
- `docs/design/01-appendix-implemented.md` 尚未生成，已在 T4 文档登记为 R3 未兑现项。

## 4. 被后续决策取代的部分

> 本节只登记指针，不改写 §2 的原结论。带 ⚠ 的票表示其决策**部分被取代**，读到该行须一并读本节所指的记录。

| 取代记录 | 取代了什么 | 决策票 |
|---|---|---|
| [18-v1-delivery-boundary-bs-binary.md](18-v1-delivery-boundary-bs-binary.md) · v1 交付边界：B/S 二进制直接运行验收 | **T8** 中依附「离线 tar 安装包」「自带并自建 PG17」「安装脚本」的全部结论（D1、D2、D4、D5、D8、D9.1、D10/D11 的执行者、§3 的 glibc 下限与 §12 交付物清单）；**T15** 中依附「四组合架构 × glibc + 长期 Release assets」的部分（D3.3、D4、D5）；地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 及决策票 #99–#102 的整条 macOS `.pkg` 首发路线。**T2 / T12 / T13 机制 / T14 不受影响**，见该文 §13 点名清单。 | [#106](https://github.com/liumingjian/dbs-monitor/issues/106)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
