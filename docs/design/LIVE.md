---
status: active
kind: index
---
# 当前真值索引

**每个会话开局读这一份，通常也只读这一份。** 一行一条决策，只给结论和出处；
需要理由、被否决的方案或边界条件时，再展开该行指到的文档。

`docs/design/` 全量约 25 万 token，是 smart zone 的一倍半——**不要 glob 决策正文**。
约定见 [`README.md`](README.md)。术语见根目录 [`CONTEXT.md`](../../CONTEXT.md)。

> 本索引由 2026-08-24 的一次全量治理复核建立，覆盖 HEAD `0c960c7`。
> 后续新增或推翻决策时，**在这里改一行**，否则它会像它取代的那四份索引一样过期。

---

## 1. 交付与运行形态 · 三个月里被改写了三次，最容易读错

| 结论 | 出处 |
|---|---|
| **v1 只交付 `linux/amd64`**，arm64 整体移出 v1 | [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) D1 |
| 交付物 = 仓库构建的 `server` / `agent` 二进制**直接运行**，不产安装包、不产安装脚本、不发布二进制 | [`18`](18-v1-delivery-boundary-bs-binary.md) D1、[`27`](27-v1-deliverables-and-candidate-provenance.md) D2 |
| 平台库由**客户自备外部 PostgreSQL**，钉死 **17.x**，主版本不符拒绝启动，无逃生舱 | [`18`](18-v1-delivery-boundary-bs-binary.md) D2、[`30`](30-external-postgres-prerequisites.md) D1 |
| 平台连库走 **TCP + 强制 TLS**，凭据进 keyring；备份责任整体划给客户 | [`18`](18-v1-delivery-boundary-bs-binary.md) D3/D7 |
| **否决容器镜像交付**（信创环境不能假设有容器运行时）——此条历经三次改写始终保留 | [`09`](09-packaging-and-deployment.md) D1 |
| 候选身份 = 40 位 SHA，tag 只是别名；不承诺 bit-for-bit 可复现 | [`27`](27-v1-deliverables-and-candidate-provenance.md) D1/D2 |
| 降级**不受支持**、无 down 迁移、升级顺序先 server 后 agent | [`27`](27-v1-deliverables-and-candidate-provenance.md) D10 |
| Agent 经 `GET /api/v1/agent/download` 平台自分发，**绝不自升级**；CA 指纹钉扎全程无 `-k` | [`19`](19-agent-distribution-and-upgrade.md) |
| Agent 与 server 时钟偏差超 **±5 秒拒绝启动** | [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) D7、[`19`](19-agent-distribution-and-upgrade.md) D6 |
| 二进制 `CGO_ENABLED=0` 构建，以 ELF/ldd 证明无动态 libc 依赖，**不承诺 glibc 最低版本** | [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) D7 |

> ⚠️ [`09`](09-packaging-and-deployment.md) 的交付形态（离线 tar / 自建 PG / socket-only / 安装脚本）**已整体作废**，
> 但它残留约十二条仍然有效的原则，见其文首当前适用性。整条 macOS 首发路线在
> [`superseded/`](superseded/)，**不得据以行事**。<!-- allow-superseded-link -->

## 2. 架构与代码结构

| 结论 | 出处 |
|---|---|
| 四层偏序 **L3 `cmd` → L2 编排 → L1 领域 → L0 基础设施**，同层禁止互依（唯一例外 `collect → capability`） | [`05`](05-backend-code-structure.md)，由 `internal/arch_test.go` 机器断言 |
| 新增包默认拒绝，须先在 `arch_test.go` 登记；禁止 `common`/`util`/`shared`；共享面只有生成物 `internal/api` | [`05`](05-backend-code-structure.md) |
| 不造 mock，接缝白名单封闭；领域包不开事务只接 `DBTX`，事务由 L2 持有；`clock` 止步 L2 | [`05`](05-backend-code-structure.md) |
| **空状态是值不是 error**，Go error 只表操作失败 | [`05`](05-backend-code-structure.md) |
| 单 Go 服务端；**PG 指标一律服务端直连**，Agent 只采 OS + 心跳，与实例 1:1，凭据只归服务端 | [`16`](16-r2-decision-index.md) T1 |
| Agent 单上报端点、强制 TLS 自签 CA 无跳过开关、**无下行通道**、±30s 时间戳门槛 | [`16`](16-r2-decision-index.md) T3 |
| **DB 不可达时 HTTP 层照常起来**，呈现「平台自身故障」，绝不渲染成没有数据；业务端点 503 而非 13 码 | [`09`](09-packaging-and-deployment.md) D6.1、[`26`](26-data-and-recovery-gate.md) D3 |
| 启动失败按**能否自愈**两分；迁移锁 / 进程锁 / rotate 锁**三把独立且行为必须不同** | [`26`](26-data-and-recovery-gate.md) D2/D3 |
| `migrations/` 只写 up 不写 down，迁移失败即拒绝启动；goose 启动时自动迁移 | [`09`](09-packaging-and-deployment.md) D9.2、[`16`](16-r2-decision-index.md) RT-D |
| `.tool-versions` 是工具链单一来源，`check` 内有漂移守卫 | [`27`](27-v1-deliverables-and-candidate-provenance.md) D8 |

## 3. 采集与存储

| 结论 | 出处 |
|---|---|
| 窄表 `metric_sample` + series 元数据，按天 UTC **原生分区**；不引第二存储引擎 | [`04`](04-metric-storage-model.md) |
| 差分只在**写入侧**完成存速率；**最新值查询必须带时间下界**；桶内无样本就不出现，**不补 0** | [`04`](04-metric-storage-model.md) |
| 采集一等公民是 **Task** 不是指标；字典口径**编译进 Go**，计划参数入库 | [`06`](06-metric-dictionary-and-collection-plan.md) |
| 前置条件建模为**封闭能力枚举四态**；被监控 PG **13–17**，PG12 接入即拒 | [`06`](06-metric-dictionary-and-collection-plan.md) |
| 中央调度按 `(instance,task)` 确定性错峰；探针与普通查询**双连接生命周期**；探针永不退避 | [`12`](12-collection-concurrency-timeouts-and-backpressure.md) |
| 背压只保留最新到期意图，**不补跑、不自动降频** | [`12`](12-collection-concurrency-timeouts-and-backpressure.md) |
| 背压跳过报 **`NO_DATA`**（平台按设计主动放弃，观测义务未完成）——优先于「平台故障不计 NO_DATA」 | [`23`](../acceptance/23-v1-acceptance-entries-c.md) D5 |
| 采集源完整性水位：一个任务成功不能推进水位，除非同源所有到期任务都已满足 | [`12`](12-collection-concurrency-timeouts-and-backpressure.md)、`CONTEXT.md` |
| 代码实际采什么，查生成物 [`01-appendix-implemented.md`](01-appendix-implemented.md)（`make gen` 产出，勿手改） | 由 `internal/metric/dictionary.go` 生成 |

## 4. 告警与产品语义 · R1 十项 ADR 无一被推翻

| 结论 | 出处 |
|---|---|
| **四条不变式**：告警五状态与压制正交 / 实例健康单一来源 / 三档全局角色且凭据永不回显 / 三条内置采集状态规则不可删不可停用且级别 ≥ `warning` | [`00`](00-decision-index.md) §4 |
| 告警五状态 `OK / PENDING / FIRING / NO_DATA / RECOVERED`，只支持 `consecutive_count`，恢复阈值必填形成滞回 | [`02`](02-alert-rule-model-draft.md)、ADR-03 |
| 规则改版**不回放**；三档固定级别；内置模板只读 | ADR-03 |
| 实例健康 = 未恢复告警的**最坏归并**，不加权，info 不染色 | ADR-08 |
| 性能事件 = 告警派生的轻量实例级对象，**零第二套评估引擎** | ADR-01 |
| 慢查询用 `pg_stat_activity` **采样**（语义是采样不是审计）；**R1 不承载任何 SQL 文本**，用 PID / queryid 定位 | ADR-04、ADR-06 |
| 暂停采集 = 停采集 + 停评估 + override + 冻结不回放；配置缺失待办清单**三态契约，绝不报假绿** | ADR-10 |
| 「未知」一次性单调：唯一入口是从未成功采集，首采成功后永不再回 | [`23`](../acceptance/23-v1-acceptance-entries-c.md) D4 |
| 用户**只停用不删除**；口令随机生成一次性回显；启用态平台管理员 ≥1；实例**创建即验**，三类拒绝都不落库 | [`17`](17-user-role-and-instance-onboarding.md)、[`24`](../acceptance/24-v1-acceptance-entries-d.md) D4 |
| 接触秘密的写操作全归平台管理员，唯一例外：实例元数据编辑 = 告警管理员 | [`24`](../acceptance/24-v1-acceptance-entries-d.md) D6 |
| 顶层页面树为**五区**（`00`/`03` 写的「四区」已过期，`17` D5 新增「用户管理」） | [`17`](17-user-role-and-instance-onboarding.md) D5 |

## 5. API 与前端

| 结论 | 出处 |
|---|---|
| OpenAPI 按域拆分，`make gen` 唯一入口，**生成物入库 + `git diff` 漂移门** | [`07`](07-api-contract-and-codegen.md) |
| 空状态一律 **200 + 封闭 13 码**；新增枚举码只许追加，禁止修改或复用既有码值 | [`07`](07-api-contract-and-codegen.md) |
| `VERSION_UNSUPPORTED` 是**写操作的失败分类**，不是空状态码——13 码专指观测面不可用性 | [`24`](../acceptance/24-v1-acceptance-entries-d.md) D3 |
| 服务端会话 cookie + `x-required-role` 覆盖每个 `operationId`；轮询，MVP 内禁 WS/SSE | [`07`](07-api-contract-and-codegen.md) |
| AntD 6 + ECharts 6（自写 wrapper）+ TanStack Query / Router；**三个状态桶，明令禁止第四个** | [`08`](08-frontend-stack-and-ui.md) |
| 图表 `unavailability` 必填；缺数保留为 `null` / 缺桶，**不补 0**；`assertNever` 穷尽 | [`08`](08-frontend-stack-and-ui.md) |
| AntD 实测卡顿属**结构型停下，不得自行换库**；ECharts 卡顿有预授权后备 uPlot | [`23`](../acceptance/23-v1-acceptance-entries-c.md) D3 |

## 6. 安全与凭据

| 结论 | 出处 |
|---|---|
| 威胁模型：只防「库或备份单独泄露」，**不防宿主失陷**；配置文件泄露 = 平台库凭据泄露 | [`13`](13-credential-encryption-rotation-and-revocation.md) D1、[`25`](25-master-key-provenance-and-startup-failure.md) |
| PG 密码走**版本化 keyring + AES-256-GCM**；Agent 令牌只存哈希且须显式登记；秘密永不进响应/日志/诊断出口 | [`13`](13-credential-encryption-rotation-and-revocation.md) |
| **密钥文件是主密钥唯一规范来源**，环境变量只覆盖路径，KMS 出局；三条件 + `O_EXCL` 自动生成 | [`25`](25-master-key-provenance-and-startup-failure.md) D2 |
| **keyring 故障不拒绝启动**，只降平台健康 | [`25`](25-master-key-provenance-and-startup-failure.md) |
| 主密钥轮换是**离线维护窗口命令**（停 server → 跑 → 重启），且必须可被非交互调用 | [`24`](../acceptance/24-v1-acceptance-entries-d.md) D14 |
| 「凭据永不回显」三层分工：schema 守卫 + 声明守卫 + 端到端**响应体全文正则**（防的是意料之外的字段） | [`24`](../acceptance/24-v1-acceptance-entries-d.md) D12 |
| 安全头六项取值、**TLS 1.3 硬下限**、HSTS 永不 preload、`__Host-` cookie、不引入 CSRF token、**不建 `audit_log`** | [`29`](29-production-security-boundary.md) |
| CA 证书是**每套部署运行期生成的实例私有物**，不进交付物 | [`27`](27-v1-deliverables-and-candidate-provenance.md) D3 |

## 7. 平台自身可观测性

| 结论 | 出处 |
|---|---|
| 平台健康是**独立四态快照**，归并序 `FAILED > UNKNOWN > DEGRADED > OK`，不复用目标模型 | [`14`](14-platform-observability-and-diagnostics.md) D2 |
| journal 是历史，只读诊断 API 是管理员入口；**平台故障绝不进目标告警链路** | [`14`](14-platform-observability-and-diagnostics.md) D1/D3 |
| 事实源清单**当前九源**（`14` 正文的「七源」与「自带 PostgreSQL」条目已过期） | [`34`](34-platform-health-tls-dead-source.md) D4 |
| **源清单里每一项都必须有真实写入者**，登记与写入者同生同灭——死源会把总态永久钉在 `UNKNOWN` | [`34`](34-platform-health-tls-dead-source.md) D2 |
| 「枚举只许追加」守的是**已承载语义的线上码值**；未发布 + 从未承载 + 无持久化引用可一次性移除，不构成先例 | [`34`](34-platform-health-tls-dead-source.md) D3 |
| 磁盘保护拆成**两条互不推导的水位**：平台库容量 / server 本机盘；预算未配置时 `UNKNOWN` 不猜 | [`32`](32-platform-storage-watermark-and-write-protection.md) |
| **绝不自动删旧分区、绝不缩短 30 天保留期** | [`14`](14-platform-observability-and-diagnostics.md) D9 |

## 8. 验证与发布

| 结论 | 出处 |
|---|---|
| 两层闭环 `make check`（**快层 ≤120 秒，一字不改**）/ `make check-full`；**不许加第三层** | [`10`](10-ai-guardrails-and-verification.md) D1、[`28`](../acceptance/28-v1-go-no-go-gates.md) D1 |
| 完成的定义：`make check` 全绿。`make acceptance` **绝不进** `make check` | [`10`](10-ai-guardrails-and-verification.md) D6、[`28`](../acceptance/28-v1-go-no-go-gates.md) D12 |
| A 栏语义单测 + B 栏结构守卫两栏登记表，三问准入判据；不装 git hook | [`10`](10-ai-guardrails-and-verification.md) D3 |
| GitHub Actions 是**唯一规范 CI 执行者**；PR 门 = `make check`，默认分支 = `make check-full` | [`15`](15-ci-and-release-pipeline.md) D1/D2 |
| **GitHub runner 上的绿永不构成 Go/No-Go 证据**——`GITHUB_ACTIONS` 下强制 `provisional: true` | [`28`](../acceptance/28-v1-go-no-go-gates.md) D9 |
| JSON 的机器 verdict 是唯一结论，Markdown / tag / 人工说明**均不得把 NO-GO 覆盖为 GO** | [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) D9 |
| 正式兼容性证据只认 **Kylin V10 (Sword) / x86_64 / KVM**，需连续两轮 GO | [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) |
| 验收矩阵终态 **104 条 / 101 条硬底**——所有决策文档里的计数都已过期，真值在 `test/acceptance/matrix.yaml` | [`20`](../acceptance/20-v1-acceptance-matrix.md) 起逐次累加 |
| `check` / `check-full` / main required check 现已**在岗**；仅 `acceptance` workflow 仍停用 | [`33`](../acceptance/33-phase-ci-gate-suspension.md)（已查实逆转） |

---

## 9. 去哪里找什么

| 你要找 | 去哪 | 不要去哪 |
|---|---|---|
| 术语定义 | 根目录 `CONTEXT.md` | 决策文档 |
| 「为什么这样设计、为什么不是别的」 | 本索引 → 指到的 `docs/design/NN-*.md` | 不要 glob 整个目录 |
| 「产品是什么样」 | `docs/design/01`/`02`/`03`、`docs/spec/mvp-master-spec.md` | |
| 验收条目、门禁、发布留痕 | `docs/acceptance/`、`test/acceptance/matrix.yaml` | 决策目录里已经没有它们了 |
| 代码实际怎么采指标 | `docs/design/01-appendix-implemented.md`（生成物） | 手写的指标字典 |
| 当时为什么那样决定（已作废的） | `docs/design/superseded/`、`docs/design/00`、`16` | **不得据以行事** |
| 门禁/workflow 当前开着还是关着 | `gh workflow list`、GitHub 分支保护 | 任何仓库内文档都答不了 |
