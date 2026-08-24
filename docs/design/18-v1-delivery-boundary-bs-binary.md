---
status: partially-superseded
kind: decision
superseded_by: ../acceptance/31-real-linux-adaptation-and-final-acceptance.md, 30-external-postgres-prerequisites.md
superseded_parts: D1/D6 的双架构交付目标 → 31 D1 收窄为 linux/amd64；D2「版本要求另议」→ 30 结案为钉死 PG 17
---
# 18 · v1 交付边界：B/S 二进制直接运行验收

> 出处：[交付边界 supersede 记录：B/S 二进制运行验收取代离线 tar 与 macOS .pkg 路线 #106](https://github.com/liumingjian/dbs-monitor/issues/106)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：**supersede 记录**。本文不新开产品或架构讨论，只把「B/S 二进制直接运行验收」这条交付边界固化，并逐条点名它推翻了哪些已冻结结论、哪些结论保持有效。被推翻的原文档一律**不原地改写**，以本文为准。
> 输入边界（不重议，来自 #105 charting 会话）：地图 Notes 第 1–9 条。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。


> **当前适用性（2026-08-24 治理复核）**
> 本文是 supersede 记录，主体在效。两处已被再次改写：
> - **交付架构**：本文 D1/D6 的 `linux/amd64` + `linux/arm64` 已被
>   [`31`](../acceptance/31-real-linux-adaptation-and-final-acceptance.md) D1 收窄为 **`linux/amd64` 单架构**，arm64 整体移出 v1。
> - **平台库版本**：D2 的「版本要求另议」已由 [`30`](30-external-postgres-prerequisites.md) D1 结案为
>   **钉死 PG 17.x、主版本不符拒绝启动、无逃生舱**。
>
> 另两处文内瑕疵：§0 说「保留清单逐条见 §12」，实际在 **§13**（§12 是 macOS 路线作废）；
> §12 称被作废的五份 macOS 文档「从未在仓库中创建」，实际它们在 `5d361cd`（2026-08-15）进入 main，
> 现已移入 [`superseded/`](superseded/)。<!-- allow-superseded-link -->

---

## 0. 一句话结论

**v1 的交付形态是「仓库构建出的 server / agent 二进制直接运行、连接客户自备的外部 PostgreSQL」，交付目标为 `linux/amd64` + `linux/arm64`。** 由此：[T8](09-packaging-and-deployment.md) 中一切依附于「离线 tar 安装包」「自带并自建 PG17」「安装脚本」的结论作废；[T15](15-ci-and-release-pipeline.md) 中依附于「四种架构 × glibc 组合 + 长期 Release assets」的部分作废；地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 的整条 macOS 首发路线（决策票 [#99](https://github.com/liumingjian/dbs-monitor/issues/99)–[#102](https://github.com/liumingjian/dbs-monitor/issues/102)）被本记录取代。

**存储模型（T2）、凭据加密机制（T13）、平台可观测性（T14）、采集并发与背压（T12）不受影响，逐条见 §12。** 本记录推翻的是**交付面**，不是架构面。

**本记录不表示仓库已可投产。** 它只挪动交付边界；投产判定走地图 #105 的验收矩阵与 Go/No-Go 门禁票。

---

## 1. 推翻 / 保留总表

| 原结论 | 处置 | 本文条目 |
|---|---|---|
| T8 D1 离线 tar 包 + 安装脚本 + systemd unit 三件套 | **作废** | D1 |
| T8 D2 自建可重定位 `pgsql/` 随包分发 | **作废** | D2 |
| T8 D4 自带 PG 钉死 17、不接管客户既有 PG | **作废**（版本要求另议） | D2 |
| T8 D5 socket-only、零 TCP 端口、peer 认证 | **作废** | D3 |
| T8 D6 自举四条 | **两条保留、两条改写** | D5 |
| T8 D8 首次启动三项人工输入 + 8.1 CA 指纹 | **改写**（规范来源改为配置文件） | D4 |
| T8 D3 双架构 + 每架构 glibc 下限 + 否决 qemu | **架构保留、glibc 下限承诺作废、否决 qemu 作废** | D6 |
| T8 D9 升级前自动备份控制面 | **作废**（备份责任整体划给客户） | D7 |
| T8 D9.2 `migrations/` 只写 up、回滚靠备份 | **保留** | §12 |
| T8 D10 资源基线与安装前置检查 | **归属改写**（安装脚本 → 部署前置条件 + 启动自检） | D8 |
| T8 D11 时钟同步安装期硬检查 | **归属改写**（安装脚本 → Agent 启动自检 + server 接收侧比对） | D8 |
| T8 §12 交付物清单、§14 自带 PG 相关未决事实 | **作废** | D1 / D9 |
| T15 D3 Release assets 归档、D4 四组合原生 runner、D5 assets 长期保留 | **作废** | D10 |
| T15 D1/D2 GitHub Actions 唯一执行者、PR 门 = `make check` | **保留** | §12 |
| 地图 #98 与决策票 #99–#102（macOS .pkg 首发路线） | **整体作废** | D11 |

---

## 2. D1 · 交付物形态：二进制直接运行，不再有安装包

**结论：v1 不产出安装包，不产出安装脚本。** 交付物是仓库构建出的 `server` 与 `agent` 二进制，加上运行它们所需的配置样例、systemd unit 模板与部署前置条件文档。运行方式是「把二进制放到机器上、给一份配置、拉起来」。

**T8 D1「离线 tar 包 + `install.sh` / `upgrade.sh` + 装完是宿主机上两个 systemd 服务」整条作废**，T8 §12 交付物清单随之作废。

**理由**

1. T8 D1 的全部论证建立在「所需一切随包自带」这条地图 Notes 之上，而**自带 PostgreSQL 已被本记录移出交付范围**（D2）。抽掉自带 PG 之后，tar 包里只剩两个静态 Go 二进制——为两个静态二进制维护一套安装/升级/回滚脚本，是纯成本。
2. 安装脚本是 T8 里最重的一笔隐性资产（前置检查、`initdb`、证书生成、写 unit、升级、回滚），且它**从未被任何自动门验证过**。删掉它同时删掉了一整片无证据面。
3. T8 D1 否决容器镜像的理由（信创环境不能假设有容器运行时）**仍然成立**，本记录不重新引入容器交付。

**代价明说**：客户侧的准备动作变多了——自备 PostgreSQL、自己写 systemd unit（我们给模板）、自己做备份。这些代价被显式接受，并必须逐条写进部署前置条件文档（D8、D9）。

**v1 交付物清单的确切内容**（是否附二进制、如何留痕、如何绑定候选提交）见 [v1 交付物与候选留痕 #110](https://github.com/liumingjian/dbs-monitor/issues/110)。

---

## 3. D2 · 平台 PostgreSQL：客户自备的外部前置

**结论：平台自身的元数据库由客户提供并运维。** 平台不安装它、不备份它、不升级它、不接管其小版本或大版本节奏。

**T8 D2（自建可重定位 `pgsql/`）与 D4（钉死 PG 17、不支持接管客户既有 PG）整条作废。**

**理由**

1. 自建 PG 的构建面（glibc 2.17 / 2.28 两套构建容器、可重定位性、信创发行版实测）是整条交付线上最重、证据最薄的一段：T8 §14 至今仍把「自建 PG 在两套构建容器下的可重定位性与 `make check` 通过情况」标为未实测。
2. 私有化客户普遍已有 PostgreSQL 运维能力；把元数据库交给他们，换回的是我们不再承担一个数据库产品的交付责任。
3. T8 D4 拒绝接管客户既有 PG 的理由（别人的扩展、别人的备份策略、别人的 DBA 半夜 `ALTER`、别人的升级节奏）**没有消失，只是变成了必须显式应对的前置条件**——用「要求专属实例 / 独立 database / 最小权限集 / 启动时校验前置条件并快速失败」来应对，而不是用「把 PG 塞进我们的包」来回避。

**由此产生的未决策项，不在本记录内解决**：要求客户提供的**版本区间**、专属实例与否、启动时的前置条件校验清单、最小权限集与 schema 归属、客户大版本升级时平台的承诺——全部归 [外部前置 PostgreSQL 的版本要求与部署前置条件 #116](https://github.com/liumingjian/dbs-monitor/issues/116)。

> **版本口径澄清（避免串台）**：**被监控** PG 为 13–17（[T4](06-metric-dictionary-and-collection-plan.md) §5.1，PG12 接入即拒），这条**不受本记录影响**。**平台自身**库是另一条独立版本线：已核实的一手事实为技术下限 PG14（`date_bin` 是 PG14 新增，`internal/metric/queries.sql:21` 已在用）、既有文档口径为 ≥14 推荐 17（[04](04-metric-storage-model.md) §7.4）／钉死 17（T8 D4），PG13 已于 2025-11-13 EOL。见 [平台库 PG13+ 的一手事实核实 #107](https://github.com/liumingjian/dbs-monitor/issues/107)。

---

## 4. D3 · 平台库连接形态：TCP + 强制 TLS，凭据走 keyring

**结论**

| 项 | 结论 |
|---|---|
| 连接方式 | TCP，**强制 TLS**（外部库不在本机，socket 不再可用） |
| 凭据存放 | 复用 [T13](13-credential-encryption-rotation-and-revocation.md) 的版本化 keyring + AES-256-GCM 加密机制 |
| 秘密的位置 | 不进命令行；配置文件是规范来源，环境变量可覆盖 |

**T8 D5 整条作废**：socket-only、零 TCP 端口、`pg_hba.conf` 除 peer 外全 `reject`、平台与 PG 同用户 `dbsmon` —— 全部依附于「PG 在本机且归我们独占」，前提已不存在。

**随之作废的两条派生结论**

- 「端口冲突结构性消失」与「自带 PG 被外部连上结构性不可能」不再成立。外部库的网络暴露面归客户，我们只承诺自己这一端强制 TLS。
- 「平台配置里不存在 PG 密码」**反转**：现在配置里必然存在平台库凭据，它成为 T13 必须覆盖的一个新秘密。

**T8 D5 移交 T13 的那条威胁模型假设（平台与 PG 同用户 ⇒ 平台被攻破即拿到全部密文）**，在密文与主密钥分离到不同主机之后前提改变，是否仍成立，归 [主密钥来源与启动失败语义（无安装器形态） #109](https://github.com/liumingjian/dbs-monitor/issues/109)。

---

## 5. D4 · 首次启动：配置文件是规范来源，不再有安装期交互

**结论：配置的规范来源是配置文件，环境变量可覆盖，秘密不进命令行。** T8 D8「三项人工输入」作为**安装脚本交互**作废，三项本身按下表重新落位。

| T8 D8 原输入 | 新处置 |
|---|---|
| 数据目录路径 | **消失**。PG 数据目录归客户 |
| 平台对外访问地址（证书 SAN） | **保留为配置文件必填项**。理由不变：自签证书 SAN 必须包含 Agent 将要连的地址，无法自动推断，猜错即全部 Agent TLS 校验失败，而 [T3](https://github.com/liumingjian/dbs-monitor/issues/21) 定死无跳过开关 |
| 初始管理员口令：随机生成并打印一次 | **保留**，触发点从安装脚本移到 **server 首次启动**。否决内置默认口令、否决首访设置向导两条理由不变（[17](17-user-role-and-instance-onboarding.md) D3 与之同构） |

**T8 D8 自动段**（`initdb` → 生成证书 → 写两个 unit → 启动）**作废**；仅「goose 启动时自动迁移」保留（承 [T5](05-backend-code-structure.md)，且其并发启动语义归 [数据与恢复门的具体证据 #113](https://github.com/liumingjian/dbs-monitor/issues/113)）。

**T8 §8.1「安装命令内嵌 CA 指纹」**依附于「平台自分发 Agent + 一条页面上复制的安装命令」，该形态本身待重定，整条移交 [Agent 分发与升级形态（无安装器） #108](https://github.com/liumingjian/dbs-monitor/issues/108)；其原则——**信任根靠带外传递，全程不出现 `-k` / `--insecure`**——保留为该票的输入边界。

**主密钥来源**（环境变量 / 密钥文件 / KMS / 首启生成）不在本记录内决定，归 #109。

---

## 6. D5 · 自举：两条保留，两条改写

T8 D6 的四条逐条处置：

1. **「DB 不可达时 HTTP 层照常起来，返回明确的『平台自身故障』页」——保留，且强化。** 这是 R1 不变式（空状态必须解释原因）在平台自身上的投影：**把「平台挂了」渲染成「没有数据」是本系统最严重的一类谎言**。外部库让「DB 不可达」从边缘情况变成常态风险，本条比原来更重要。
2. **「本地通知快照文件」——保留结论，密钥来源移交。** 它仍是库的只读派生缓存，不是配置源，**不得被引用为「配置可以外置」的先例**；其中凭据密文的密钥来源归 #109。
3. **「两个 systemd unit 各自 `Restart=always`，平台不 `Requires` PG」——改写。** 本机只剩一个 server unit，外部 PG 不受本机 systemd 管辖，`Requires` 之说自动消失。改写为：**server 在外部 PG 不可达时不得退出，应重试等待并对外报告平台自身故障**；重试语义与「外部 PG 短暂不可用时的具体行为」归 #113。
4. **「不做平台自身的高可用/主备，不做第二套监控栈监控本套」——保留。** 残余风险（整台平台机宕机则无人告警）同样保留，且仍须在交付文档中明确告知客户。

---

## 7. D6 · 双架构保留，glibc 下限承诺作废，否决 qemu 作废

**结论**

| 项 | 结论 |
|---|---|
| 交付目标架构 | **`linux/amd64` + `linux/arm64`**，保持不变 |
| glibc 下限 | **不再承诺**。交付物是 `CGO_ENABLED=0` 的静态 Go 二进制，不链接 libc；承诺面收窄为「架构 + Linux 内核下限」 |
| 构建方式 | **交叉编译即可**，不再要求原生 runner |
| 开发与日常验证平台 | macOS/arm64；**但 v1 的 Go/No-Go 证据必须在真实 Linux 上产出** |

**T8 D3 的 glibc 下限表（amd64 = 2.17 / arm64 = 2.28）作废**：那两个数字是**自建 PostgreSQL 的构建容器**的 glibc，不是 Go 二进制的依赖。自带 PG 移出交付范围后，这条承诺失去被承诺物。

**T8 §15 与 [T15](15-ci-and-release-pipeline.md) D4「禁止用 QEMU 替代原生 arm64 构建」作废**：该否决的唯一理由是「PG 自建要跑 `make check`，qemu 下慢到不可接受」。没有 PG 要构建之后，Go 交叉编译天然可行且当前 `make check-full` 已在做。

**但「构建可交叉」不等于「验证可交叉」。** 真实 Linux 上的运行验证不可替代，且 arm64 是否需要真机验证、需要几轮、复跑哪些门，归 [真 Linux 环境适配与最终验收 #115](https://github.com/liumingjian/dbs-monitor/issues/115)。

**T8 §3.1 的实测门槛分级（amd64 全量 / arm64 冒烟 + 容量抽样）**依附于自带 PG 的容量门槛，**作废**；其替代物由 #115 与 [Go/No-Go 质量门组成 #114](https://github.com/liumingjian/dbs-monitor/issues/114) 重定。

**T8 §3「不做发行版清单，只承诺依赖面」保留**，且依赖面进一步收窄——静态二进制的依赖面接近于零。

---

## 8. D7 · 备份与恢复：责任整体划给客户

**结论：备份与恢复责任整体归客户。** 平台只承诺三件事：**库是自描述的**（schema 与数据完整表达平台状态，无外部隐藏状态）、**标准工具可备份**（`pg_dump` / 物理备份皆可，无需平台参与）、**恢复后直接可用**（把备份灌进一个空库、启动 server 即可工作）。

**T8 D9.1「升级前自动备份控制面、排除时序样本表」作废**——平台不再拥有 PG，无法在升级流程里自动备份。其论证（量级差、后果差、「用可容忍的损失换真的会被执行的备份」）作为**给客户的建议**写入部署前置条件文档，不再是平台行为。

**保留**：

- **`migrations/` 只写 up，不写 down**（T8 D9.2）。理由不变：down 迁移是「写了从没测过、却在真正需要它的那个凌晨第一次运行」的代码。这同时是 [T9](10-ai-guardrails-and-verification.md) 的一条可机械检查规则。
- **回滚 = 装回旧二进制 + 恢复备份**，不靠 down 迁移。
- **迁移失败即拒绝启动**：非零退出 + 明确日志，绝不带着半个 schema 对外提供服务。

上述三条承诺的**具体证据形式**（重启恢复断言、`pg_dump` → 空库 → 可读这条链怎么跑、分区生命周期如何自动验证、客户责任清单写什么）归 #113。

---

## 9. D8 · 前置检查的归属：从安装脚本移交给文档与运行时自检

安装脚本消失后，T8 D10 / D11 的检查没有执行者。归属重定如下：

| 原检查 | 新归属 |
|---|---|
| 磁盘 ≥ 200 GB 硬拒绝 | **改为对客户自备 PG 主机的容量前置要求**，写入部署前置条件文档。实测事实保留为容量依据：30 天全量 30 个分区 **49,112,432,640 bytes（≈49.1 GB）**（`docs/validation/t11-linux-amd64-progress.md`）。平台侧无盘可查，硬拒绝无处执行 |
| 内存 ≥ 8 GB / CPU ≥ 4 核（警告） | **降为部署前置条件文档条目**。仍为推算，无实测；不再由任何脚本检查 |
| 时钟同步（Agent 安装期硬检查，±5s） | **改为 Agent 启动自检 + server 接收侧比对**。[T3](https://github.com/liumingjian/dbs-monitor/issues/21) 的「时间戳偏移 > ±30s 即拒收并报 `error`」**不变**；把「装好了、服务起来了、就是没数据」这类模糊故障前移成明确失败的原则**保留**，具体执行形态随 Agent 分发形态定，归 #108 |
| 平台机到被监控 PG、被监控主机到平台 HTTPS 的连通性 | 保留为部署前置条件文档条目 |
| 被监控 PG 大版本在 13–17 | **保留，且执行点已明确**：机器门在平台的实例接入校验（[T4](06-metric-dictionary-and-collection-plan.md) §5.1，PG12 接入即拒），不再有 `install.sh` 这一处执行点 |
| 平台库自身的前置条件（版本、扩展、编码/locale、时区、权限、磁盘） | **新增，归 server 启动自检**；清单与硬门/告警分级归 #116 |

**原则不变**：把运行期的、症状模糊的失败前移成启动期的、症状明确的失败。变的只是执行者——从安装脚本变成 server 与 agent 自身。

---

## 10. D9 · T8 §14 未决事实的处置

| T8 §14 事实 | 处置 |
|---|---|
| 磁盘 / 内存 / CPU 基线 | 磁盘实测值保留为容量依据（D8）；内存 / CPU 仍为推算，降为文档条目 |
| 自建 PG 在 glibc 2.17 / 2.28 构建容器下的可重定位性与 `make check` | **作废**（无自建 PG） |
| arm64 下 PG 原生分区的性能表现 | **不再是我方交付欠账**（PG 归客户）。arm64 上 **server / agent 自身**是否有行为差异需单独断言，归 #115 |
| 整包 tar 的实际体积 | **作废**（无整包）。已实测的 23 MB / 解压 59 MB 含自建 PG，与新形态无关 |
| PG 17 在信创发行版上的运行验证 | **作废**（不再由我方交付 PG）。麒麟 V10 上的 T11 原生验证事实保留为「server/agent 曾在该发行版跑通」的历史证据 |

---

## 11. D10 · 发布线：依附四组合与 Release assets 的部分作废

**T15 保留的部分**

- **D1**：GitHub Actions 是唯一规范 CI 执行者，本地与交付团队复用同一组 `make` 命令，不另建第二套验证体系。
- **D2**：PR 门 = `make check`（失败即阻断合并）；合并到默认分支跑 `make check-full`；`check-full` 失败不回溯阻断已合并 PR，但阻止发布。
- **D3 的原则部分**：只有维护者创建的语义化 tag 触发发布；**tag 指向的精确提交必须已有成功的 `check-full` 结果**，不存在「tag 一打就发」的通路；发布使用最小权限，不用个人长期 token。
- **D6 否决记录**中「本地另建验证体系」「`check-full` 失败回溯阻断已合并 PR」「tag 直接触发发布、无提交校验」「个人长期 token 做发布凭据」四条，理由与新形态无关，**全部保留**。

**T15 作废的部分**

| 原结论 | 为什么作废 |
|---|---|
| D4 构建矩阵四组合（`amd64 × glibc 2.17/2.28`、`arm64 × glibc 2.17/2.28`），全部产出可发布离线 tar | 无离线 tar、无 glibc 承诺（D1、D6） |
| D4 团队维护的原生 amd64 / arm64 runner 执行构建与安装验证；禁止 qemu | 无 PG 要构建，交叉编译足够；无安装脚本可验证（D6） |
| D3.3 / D5 经 Environment 审批归档为 GitHub Release assets、assets 长期保留、按「版本 + OS + 架构 + glibc 下限」命名 | 依附于「有安装包资产」这一前提（D1） |
| §7 收口增补中「发布线的四组合原生 runner、Environment 审批与 Release 归档待团队 runner 就绪」 | 同上，该欠账随形态一并消失 |

**新形态下发布线到底交付什么**（是否附二进制到 Release、校验和与可复现构建承诺、候选提交如何唯一标识、`main` 的 branch protection 与审批门具体要求什么、Go/No-Go 报告如何绑定候选）归 #110；**质量门的组成**归 #114。

---

## 12. D11 · macOS 首发路线整体作废

**地图 [Wayfinder 地图 · v1 macOS 首发适配与发布边界 #98](https://github.com/liumingjian/dbs-monitor/issues/98) 及其四张决策票整体被本记录取代**，不再是 v1 的任何输入：

| 票 | 原结论 | 处置 |
|---|---|---|
| [#99](https://github.com/liumingjian/dbs-monitor/issues/99) | v1 唯一原生交付目标是 macOS 14.0+ `darwin/arm64`，不承诺 Linux 或 amd64 | **作废，且结论反转**：v1 交付目标为 `linux/amd64` + `linux/arm64` |
| [#100](https://github.com/liumingjian/dbs-monitor/issues/100) | 随包交付 PG 17，LaunchDaemon 托管，peer Unix socket，离线闭环安装/备份/升级/回滚/卸载 | **作废**（外部 PG + systemd + 无安装器） |
| [#101](https://github.com/liumingjian/dbs-monitor/issues/101) | 固定 `macos-14` runner 产出签名、公证、stapled `.pkg`；专用干净 Mac 验同一候选 | **作废**（无 `.pkg`、无 codesign / notarization / stapling） |
| [#102](https://github.com/liumingjian/dbs-monitor/issues/102) | 实现票 #92 退出 v1，现有 Linux 资产只留作 `legacy` 参考，post-v1 Linux 须从新 PRD 重启 | **作废**：Linux 重回 v1 主线；#92 的 v1 处置以本记录与 #110 / #114 为准，不再受 #102 约束 |

**macOS 的定位**：开发与日常验证平台（macOS/arm64），**不是交付目标**。`.pkg`、codesign、notarization、stapling、LaunchDaemon、安装器 UX 全部在 #105 的 **Out of scope**。v1 收口后是否恢复 macOS 为交付目标，是另一次独立决策。

> **落盘说明**：#98 的决策当时登记去向为 `docs/design/18-…` 至 `22-…`，**这五份文件从未在仓库中创建**。因此本记录直接占用 18 号，并在此声明：仓库中不存在、也不会补写 #98 路线的落盘文档。

---

## 13. 明确保持有效的结论（点名，防止误读为「整个 R2 被推翻」）

本记录推翻的全部集中在**交付与发布面**。以下逐条**保持有效，未被触碰**：

| 结论 | 为什么不受影响 |
|---|---|
| [T2 · 时序存储选型与指标数据模型](04-metric-storage-model.md) | 原生分区、窄表样本模型、`date_bin` 粒度、差分写入、最新查询带时间下界、空桶不补 0 —— 全部是 **schema 与查询**层的结论，与 PG 由谁提供无关。唯一外溢是「平台库版本下限」，归 #116 |
| [T13 · 凭据加密存储、轮换与吊销](13-credential-encryption-rotation-and-revocation.md) | 版本化 keyring + AES-256-GCM 密文格式、Agent 显式登记与 token 只存哈希、解密故障属平台自身故障、秘密永不出站 —— **机制本身不变**，且新增的平台库凭据也走这套机制。只有 D2（主密钥来源与启动失败语义）因依附安装器自举而重开，归 #109 |
| [T14 · 平台可观测性与自诊断](14-platform-observability-and-diagnostics.md) | 平台健康独立四态、journal 是历史、诊断 API 是管理员入口、**平台自身故障不进入目标告警或 `NO_DATA`**、磁盘紧急时拒写新样本但不自动删旧数据或缩短保留 —— 全部保持 |
| [T12 · 采集并发限流、超时与背压](12-collection-concurrency-timeouts-and-backpressure.md) | 中央调度、探针/查询双槽、分层超时与背压、不补跑不自动降频、采集源完整性水位 —— 全部保持 |
| [T1](https://github.com/liumingjian/dbs-monitor/issues/19) / [T3](https://github.com/liumingjian/dbs-monitor/issues/21) | PG 指标一律 server 直连、Agent 只采 OS + 心跳、Agent 与实例 1:1；单上报端点、强制 TLS 自签 CA 无跳过开关、无下行通道、±30s 时间戳门槛 —— 全部保持 |
| [T4](06-metric-dictionary-and-collection-plan.md) / [T5](05-backend-code-structure.md) / [T6](07-api-contract-and-codegen.md) / [T7](08-frontend-stack-and-ui.md) / [T9](10-ai-guardrails-and-verification.md) | 字典口径、四层偏序与依赖方向、OpenAPI 与生成物漂移门、前端三状态桶、两层验证闭环 —— 全部保持 |
| [17 · 用户与角色管理与实例接入](17-user-role-and-instance-onboarding.md) | 只停用不删除、口令随机生成一次性回显、启用态平台管理员 ≥1、创建实例连接测试 + 版本门都过才落库 —— 全部保持 |
| R1 四条不变式 + 三条内置采集状态规则 | 告警五状态与压制正交、实例健康单一来源、三档角色且凭据永不回显、内置采集状态规则不可删不可停用且严重级别不低于 `warning` —— 全部保持 |

---

## 14. 交给下游

| 去向 | 内容 |
|---|---|
| [#108 Agent 分发与升级形态](https://github.com/liumingjian/dbs-monitor/issues/108) | T8 D7（平台自分发、非 root 运行、绝不自升级）与 §8.1（CA 指纹带外传递）在无安装器下的重定；时钟自检的执行形态（D8） |
| [#109 主密钥来源与启动失败语义](https://github.com/liumingjian/dbs-monitor/issues/109) | T13 D2 重定；本地通知快照文件的密钥来源（D5）；密文与主密钥分离到不同主机后的威胁模型（D3） |
| [#110 v1 交付物与候选留痕](https://github.com/liumingjian/dbs-monitor/issues/110) | 交付物清单确切内容、候选提交标识、二进制归档与校验和、branch protection、Go/No-Go 报告格式（D1、D10） |
| [#113 数据与恢复门的具体证据](https://github.com/liumingjian/dbs-monitor/issues/113) | 三条备份承诺的证据形式、goose 并发启动语义、外部 PG 短暂不可用时 server 的行为（D5、D7） |
| [#114 Go/No-Go 质量门组成](https://github.com/liumingjian/dbs-monitor/issues/114) | 快慢两层边界、PG 版本矩阵、`sqlc vet` 与漏洞扫描归属、哪些门必须在真实 Linux 上跑（D6、D10） |
| [#115 真 Linux 环境适配与最终验收](https://github.com/liumingjian/dbs-monitor/issues/115) | 真机形态、双架构差异断言、Linux 特有适配风险（D6） |
| [#116 外部前置 PostgreSQL 的版本要求与部署前置条件](https://github.com/liumingjian/dbs-monitor/issues/116) | 版本要求、专属实例与否、启动自检清单与硬门分级、最小权限集与 schema 归属、客户升级承诺（D2、D8） |

---

## 15. 作废记录汇总

| 被作废 | 出处 | 为什么 |
|---|---|---|
| 离线 tar 包、`install.sh` / `upgrade.sh`、交付物清单 | T8 D1、§12 | 抽掉自带 PG 后只剩两个静态二进制，脚本是纯成本且从无自动门验证（D1） |
| 自建可重定位 `pgsql/`、PG 钉死 17、不接管客户既有 PG | T8 D2、D4 | 平台库改为客户自备的外部前置（D2） |
| socket-only、零 TCP 端口、peer 认证、`dbsmon` 同用户 | T8 D5 | 外部库不在本机，socket 不可用；改 TCP 强制 TLS（D3） |
| 安装期三项人工输入、`initdb` / 证书 / 写 unit 自动段、内嵌 CA 指纹的安装命令 | T8 D8、§8.1 | 无安装器；配置文件成为规范来源（D4） |
| glibc 下限 2.17 / 2.28 承诺；否决 qemu、要求原生 runner | T8 D3、§15；T15 D4 | 两个数字是自建 PG 构建容器的 glibc，被承诺物已消失；Go 交叉编译天然可行（D6） |
| T8 §3.1 amd64 全量 / arm64 冒烟的实测门槛分级 | T8 §3.1 | 依附自带 PG 的容量门槛（D6） |
| 升级前自动备份控制面 | T8 D9.1 | 平台不再拥有 PG，无法自动备份；备份责任整体划给客户（D7） |
| 磁盘 ≥200 GB 硬拒绝、内存/CPU 警告、时钟安装期硬检查 —— 作为**安装脚本行为** | T8 D10、D11 | 无安装脚本可执行；归属改为文档 + 运行时自检（D8） |
| 四组合构建矩阵、Release assets 归档与长期保留、assets 命名规则 | T15 D3.3、D4、D5 | 依附「有安装包资产」（D10） |
| macOS `.pkg` 首发路线整条（#98 / #99–#102） | 地图 #98 | 交付目标改为 linux 双架构（D11） |
