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
| T13 ⚠ | [13-credential-encryption-rotation-and-revocation.md](13-credential-encryption-rotation-and-revocation.md) | PG password 使用版本化 keyring + AES-256-GCM；Agent 显式登记、token 只存哈希；解密故障属于平台自身故障；秘密永不出站。 |
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
| [19-agent-distribution-and-upgrade.md](19-agent-distribution-and-upgrade.md) · Agent 分发与升级形态（无安装器） | **T8** D7 全条与 §8.1（安装脚本自举形态下的自分发、CA 指纹内嵌、plan B 依托 tar 包）、以及 T8 D11 时钟检查的执行者；载体全部改写为「接入设置页生成的安装命令 + 下载端点 + Agent 启动自检」。**「绝不自升级」「装要 root 跑不要 root」「信任根带外传递、全程无 `-k`」「编译期同源 / 运行期容一个大版本」四条原则保留。** | [#108](https://github.com/liumingjian/dbs-monitor/issues/108)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
| [25-master-key-provenance-and-startup-failure.md](25-master-key-provenance-and-startup-failure.md) · 主密钥来源与启动失败语义（无安装器形态） | **T13** D2 全条（主密钥来源、首启自举、缺失/损坏的进程行为）、D1 的保护面/不保护面清单、D7.1 步骤 1 的「或取得排他维护锁」。改定为：密钥文件是唯一规范来源（环境变量只覆盖路径、KMS 出局）、三条件 + `O_EXCL` 自动生成并显式记事件、**keyring 故障不拒绝启动**（唯一拒启动情形是配置文件读不到）、轮换为停机子命令 + 平台库 advisory lock 拒绝并发。**威胁模型保护面升级**（外部 PG 主机整机失陷不泄露目标库密码），**不保护面新增「配置文件泄露 = 平台库凭据泄露」**（平台库凭据明文留在 `0600` 配置文件，鸡生蛋不可避）。并接下 [18](18-v1-delivery-boundary-bs-binary.md) D5 移交的本地通知快照密钥来源（无变化）。**T13 D3–D6、D8、D9 不受影响**，见该文 §12。 | [#109](https://github.com/liumingjian/dbs-monitor/issues/109)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |

## 5. v1 投产路线新增的决策（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)）

> 本节登记**不取代既有结论、纯新增**的决策文档。取代类记录仍在 §4。

| 文档 | 决策 gist | 决策票 |
|---|---|---|
| [20-v1-acceptance-matrix.md](20-v1-acceptance-matrix.md) · v1 验收矩阵的骨架与判定规则 | 矩阵沿 spec 十片切（片⑩不出条目），页面树与 `operationId` 作覆盖维；每片 `S1` + `F1..F4` 五条基线，加七条横切（四不变式 + 三内置规则，独立计分、必须单测 + 端到端各一）= 52 条硬底，基线不许 `pending`；API 层为默认断言层，浏览器只覆盖 IA §6 五条关键路径 + B6，DB 层只读不写；**测试数据只许经业务 API 或真实采集管线产生**，禁止直插业务表（新增守卫 B11）与 `covered` 漂移门（B12）；载体为本文 + `test/acceptance/matrix.yaml`，执行入口 `make acceptance`（进 `check-full`，不进 `check`），环境为平台库 / 目标库两个 PG + 真 Agent 真实接入；时间参数化不伪造、故障用真实手段不 mock。 | [#111](https://github.com/liumingjian/dbs-monitor/issues/111)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
| [21-v1-acceptance-entries-a.md](21-v1-acceptance-entries-a.md) · v1 验收矩阵条目 A 组 · 片①⑦⑨ | A 组 **23 条**（片① 8 / 片⑦ 7 / 片⑨ 8），`n-a` 1、加深基线 8。**S1 亲口拍板的验收判据一律落成 `baseline: true` 的加深条目**（全字典对账、待办三态、紧急拒写），硬底算式由「45+7」改为「45+7+逐组加深基线」，**A 组后 60**；能力四态拆三条路径（`MISSING` / 整份 `UNKNOWN` 不得反推 / `NOT_APPLICABLE` 不得冒充 0）；IA §6.5 整条路径由 `AC-01-F2` 一条承载；**journal 载体明确不在 `make acceptance` 内验证**（裸进程无 systemd，载体真实性移交片⑩，写进矩阵不藏）；片⑨ `F2` 记 `n-a` 由 `F6` 补回并补 `F7` 收证书过期注入；时间参数化取值表定稿（分区跨度 1min 是外溢实现的唯一硬要求，单轮 ≤10 分钟）；`test_ref` 须内含条目 ID 字面量使 B12 退化为单条 grep；`operations` 预写 12 个尚不存在的 `operationId`，使缺口报告不把「还没做」伪装成「没缺口」。 | [#118](https://github.com/liumingjian/dbs-monitor/issues/118)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
| [22-v1-acceptance-entries-b.md](22-v1-acceptance-entries-b.md) · v1 验收矩阵条目 B 组 · 片②③④ | B 组 **23 条**（片② 6 / 片③ 9 / 片④ 8），`n-a` 2、加深基线 7、普通加深 1（全矩阵唯一），**硬底 60 → 67**。**通知端到端断到真收端**——compose 增本地 SMTP sink + Webhook 回环接收端两个进程外真实对端，断真投递与 HMAC 签名头，否决「只断投递记录与三次退避终态」（出口一断就绿，正是反假覆盖要根除的形态）；`F3` 在片④读作「外部依赖不可用」，语义映射记账不默默扩义；四条 `F4` 手段同源断言点不同全部保留，`AC-04-F4` 是「待发通知落表而非内存队列」的唯一验收面；**三处参数化**（`repeat_interval` 下限 30s、快照截断 100→5、告警历史保留 90 天→2min）与**两处坚决不参数化**（`NO_DATA` 门槛 2 周期、退避 3 次——那是「平台固定不可配」的语义本身）；片② `S1` 用真开 5 条连接推阈值，否决用内置规则触发（会绕过规则 CRUD 一半）；IA §6.4 由 `AC-03-S3` 一条承载。外溢实现硬要求两条：repeat 下限可配、快照截断上限可配。 | [#119](https://github.com/liumingjian/dbs-monitor/issues/119)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
| [23-v1-acceptance-entries-c.md](23-v1-acceptance-entries-c.md) · v1 验收矩阵条目 C 组 · 片⑤⑥ | C 组 **15 条**（片⑤ 10 / 片⑥ 5），`n-a` 1、加深基线 4，浏览器执行 7 次（全矩阵最密），**硬底 67 → 71**。IA §6.1/§6.2/§6.3 各挂一条浏览器条目，否决合并与「一条页面树大走查」；**「13 码后端真实驱动」拆两条**——`AC-05-F2`(api) 管码的广度（片①能产出的 **11 码**逐码真实驱动、200+ 码不走 error、不补 0）、`AC-05-F5`(browser) 管 UI 覆盖，「11 码不是漏两码」的对账三句写死在 `reason` 里；**AntD 观感门 = `AC-05-S4` 记 `n-a`**，判定归 #115、卡顿即触发 `11` §9 二类停下不得自行换库；**归并全语义单设 `AC-05-S5`**（纯函数单测证明不了它被接进投影路径，入参跨片②③④⑦，是全矩阵准备成本最高的一条）；**背压—`NO_DATA` 张力显式记账**（`12` D7 口径优先于 T14 一般表述，`AC-06-F4` 显式不断 `NO_DATA` 豁免）；**片⑥ `F1` 不记 `n-a`**，「本页无写语义 `x-required-role`」是「无写面」决策的唯一验收面。C 组零新增长杆、零外溢实现硬要求。 | [#120](https://github.com/liumingjian/dbs-monitor/issues/120)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
| [24-v1-acceptance-entries-d.md](24-v1-acceptance-entries-d.md) · v1 验收矩阵条目 D 组 · 片⑧ + 横切组 | D 组 **20 条**（片⑧ 13 / 横切 7），**硬底 71 → 78；四票条目定稿完毕，`matrix.yaml` 自此无 `TBD`。全矩阵终态 81 条条目、78 条硬底、`n-a` 5、`pending` 2。** **接入前置 = 业务 API 登记直起 agent 真进程**（安装命令 / CA 指纹钉扎 / unit 写入 / 安装期时钟自检移交 #110——compose 无 systemd，跑真实安装脚本必然退化成「一半步骤被跳过」的假动作）；**横切组落成「自带断言集 + 搭车执行」并新增 `rides_on` 字段**（达标由横切自己的 `test_ref` 证明、与承载执行是否绿无关），否决「各自独立执行一次」与「就是别片的别名」；**A 栏新增 `A10`/`A11`**（B 栏扫 schema 只证明「声明齐全」，把所有端点声明成 `viewer` 也能全绿，顶不了表驱动判定）；`F2` 记 `n-a`、`VERSION_UNSUPPORTED` 归 `F3`（接入即拒是写操作失败分类不是空状态码，本片使该码有了唯一产生者）；**「凭据永不回显」三层分工**（B7 管 schema、B3 管声明、端到端管响应体**全文正则**——防的是意料之外的字段）；**执行序两条硬约束**（`AC-08-S1` 最先、`AC-08-S7` 主密钥轮换最末）。**`exceptions` 全程 `[]`**（四组无一条需要绕过业务 API，矩阵最强的一条自证）。外溢实现硬要求三条：`10` §3.2 登记 `A10`/`A11`、采集新鲜度阈值可配、轮换命令可非交互调用。 | [#121](https://github.com/liumingjian/dbs-monitor/issues/121)（地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105)） |
