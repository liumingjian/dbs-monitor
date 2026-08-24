---
status: partially-superseded
kind: decision
superseded_by: 26-data-and-recovery-gate.md, 30-external-postgres-prerequisites.md
superseded_parts: D4「唯一拒启动情形」→ 26 D3 扩为四类、30 D3.1 再加前置校验五项
---
# 25 · 主密钥来源与启动失败语义（无安装器形态）

> 出处：[主密钥来源与启动失败语义（无安装器形态） #109](https://github.com/liumingjian/dbs-monitor/issues/109)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：**supersede 记录**。[T13](13-credential-encryption-rotation-and-revocation.md) D2 依附于「离线整包 + 安装脚本自举 + 自带 PostgreSQL」的形态，该形态已被 [18](18-v1-delivery-boundary-bs-binary.md) 作废。本文在「二进制直接运行 + 客户自备外部 PostgreSQL」的交付边界下重定主密钥的来源、首启语义与启动失败语义，重估 D1 的威胁模型，重定 D7 轮换的执行形态，并接下 [18](18-v1-delivery-boundary-bs-binary.md) D5 第 2 条移交的本地通知快照密钥来源。被推翻的原文档**不原地改写**，以本文为准。
> 输入边界（不重议）：[T13](13-credential-encryption-rotation-and-revocation.md) D3–D6、D8、D9（密文格式与 AAD、两种写操作不合并、Agent 令牌只存哈希、轮换立即生效、备份与密钥两个独立制品、秘密永不出站）、[18](18-v1-delivery-boundary-bs-binary.md) D3/D4/D5（配置文件是规范来源、环境变量可覆盖、**秘密不进命令行**；无安装期交互；DB 不可达时 HTTP 层照常起来）、[T14](14-platform-observability-and-diagnostics.md) D2/D3（平台健康独立四态、平台故障不进目标告警链路）、[24](../acceptance/24-v1-acceptance-entries-d.md) D14（`AC-08-S7` 排执行序最末、轮换命令须可非交互调用）、地图 [#105](https://github.com/liumingjian/dbs-monitor/issues/105) Notes 第 7 条。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**主密钥的唯一规范来源是平台私有目录下的密钥文件，环境变量只覆盖路径、绝不承载密钥材料；无密钥且库中无密文时自动生成并显式喊一声，否则快速失败；keyring 故障不拒绝启动，而是起 HTTP、报平台自身故障、拒绝一切解密——唯一拒绝启动的情形是配置文件读不到；平台库凭据以明文留在 `0600` 的配置文件里并被显式记进威胁模型；主密钥轮换是停机执行的子命令，靠平台库 advisory lock 把「忘了停 server」从静默数据损坏变成一句拒绝。**

外部 PG 让密文与主密钥天然分离到两台机器，D1 的**保护面因此升级**：外部 PG 主机整机失陷也不泄露目标库密码。同一形态又把平台库凭据变成一个新秘密，**不保护面同步新增一条**：配置文件泄露 = 平台库凭据泄露。

---

## 1. 推翻 / 保留总表

| 原结论 | 处置 | 本文条目 |
|---|---|---|
| T13 D2.1 首启生成、放平台私有配置目录、版本命名、`current` 原子 rename、`0600` | **保留，路径与格式定死**（原路径依附 `/opt` 整包布局） | D2、D3 |
| T13 D2.1「密钥不得进入环境变量」 | **保留并细化**：环境变量可覆盖**路径**，不得承载密钥材料 | D1 |
| T13 D2.1「只在密钥环不存在且库中尚无加密凭据时才允许自动生成」 | **保留，收紧为三条件 + 显式事件 + `O_EXCL`** | D3 |
| T13 D2.1「自带 PostgreSQL 与服务端同属一个 OS 用户」的风险论述 | **作废**（无自带 PG），威胁模型重估 | D6 |
| T13 D2.2 缺失/损坏必须显式失败、绝不悄悄生成新密钥、绝不降格为 `DB_UNREACHABLE`/`NO_DATA` | **保留，进程行为定死为「不拒绝启动」** | D4 |
| T13 D1 保护面 / 不保护面清单 | **改写**：保护面升级、不保护面收窄并新增一条 | D6 |
| T13 D7.1 步骤 1「停止服务端**或取得排他维护锁**」 | **改写**：只停机，不实现在线维护锁；改用 advisory lock 做互斥**拒绝** | D5 |
| T13 D8.3 本地通知快照密文与主线同一 keyring | **保留，落位确认** | D7 |
| T8 D8 安装期三项人工输入中的「自举」触发点 | 已由 [18](18-v1-delivery-boundary-bs-binary.md) D4 作废，本文不重议 | — |

---

## 2. D1 · 主密钥来源：密钥文件是唯一规范来源

**结论**：密钥文件。环境变量只覆盖路径，KMS 出局，不设第二来源。

```text
/etc/dbs-monitor/credentials/        # 默认路径，配置项可覆盖；目录 0700
├── current                          # 单行，内容是当前用于新写入的版本号
└── master-key-v1                    # 单行 base64（标准编码），解码后须恰好 32 字节；0600
```

- 属主固定为 server 运行用户，目录 `0700`、文件 `0600`；
- 路径可由配置项覆盖，环境变量按 [18](18-v1-delivery-boundary-bs-binary.md) D3 可覆盖该配置项——**覆盖的是路径，不是密钥材料**；
- `current` 通过原子 rename 更新（T13 D2.1 保留）；
- 密钥不得打印、不得进入环境变量、命令行参数、数据库、日志或诊断出口。

**否决「环境变量承载密钥材料本体」**。`/proc/<pid>/environ`、systemd unit 文件、崩溃转储、进程诊断包都会泄露它，与 [T13](13-credential-encryption-rotation-and-revocation.md) D9.2 的秘密禁区正面冲突。T13 原文的「不得进入环境变量」在无安装器形态下**不需要放宽**：文件路径这一层的可配置性已经满足所有部署方式（含容器 secret 挂载）。

**否决外部 KMS / Vault / TPM**。它与「不依赖客户环境」直接冲突（[T13](13-credential-encryption-rotation-and-revocation.md) D1 已否决过一次，无安装器不改变这条理由），且内网离线部署常无可用 KMS。安装器消失削弱的是「自带自举」，不是「不引入外部依赖」。

**否决 raw 二进制密钥文件**，采用单行 base64 文本。换机恢复（[T13](13-credential-encryption-rotation-and-revocation.md) D8.1）与备份归档必然经过 `scp`、复制粘贴、secret 挂载；二进制文件在这些路径上被加 BOM、被改行尾的概率不低，而损坏后的表现是 AES-GCM 认证失败——本系统最难归因的一类故障。文本编码让「文件坏没坏」在 `wc -c` 层面就能判，校验规则相应写死为「base64 解码后恰好 32 字节」。

### 2.1 运维自带密钥不是独立特性

运维预先放置一个自己生成的 `master-key-v1`，与自动生成走**同一条校验路径**：server 只校验「存在、属主与权限正确、解码后 32 字节」，不追问来源、不记录来源、不提供密钥策略。

这正是换机恢复（[T13](13-credential-encryption-rotation-and-revocation.md) D8.1）的同一机制。**否决把它命名为 BYOK 或做成独立配置开关**：那会引出密钥托管、轮换策略、审计链的期待，本产品一条都不提供。交付文档只写一句——把匹配该备份的 keyring 放回原路径，恢复属主与 `0600`，再启动。

---

## 3. D2 · 目录由 root 预建，server 不创建父目录

**结论**：server 只在目录已存在且可写时创建密钥文件；目录创建是部署前置条件文档里的一次性 root 动作。

```bash
install -d -m 0700 -o <运行用户> -g <运行用户> /etc/dbs-monitor/credentials
```

**否决「server 自己 `mkdir -p` 父目录」**。`/etc` 归 root，要么让 server 跑 root（与 [19](19-agent-distribution-and-upgrade.md) D4「跑不要 root」的同一条理由冲突，客户安全评审最常卡的就是这里），要么在写不动时静默降级到别的路径——那意味着密钥可能落在谁也不知道的地方，是比启动失败严重得多的后果。

**无安装器不等于零 root 前置，只等于零安装脚本。** 这与 [19](19-agent-distribution-and-upgrade.md) D4「装要 root、跑不要 root」同构；该区分必须写进交付文档。

---

## 4. D3 · 首启自动生成：三条件、`O_EXCL`、显式喊一声

**结论**：保留自动生成，因为无安装器后它是唯一的无人值守自举点；但把它收紧到「不可能误触发」的程度。

### 4.1 三条件同时成立才生成

1. keyring 目录中不存在任何 `master-key-v*`；
2. 平台库中不存在任何密文行（因此**这一判定必须在连库之后**，见 D4 的启动序）；
3. 目录存在且可写。

任一不满足即**快速失败**——不生成、不覆盖、不降级。特别地，「库里有密文但 keyring 没了」永远走 D4 的故障路径，绝不生成新密钥（[T13](13-credential-encryption-rotation-and-revocation.md) D2.2 保留）。

### 4.2 并发首启

密钥文件用 `O_EXCL` 创建，`current` 用原子 rename 写入。创建失败即读已存在的那一份，**不重试、不覆盖**。

场景是真实的：误配双实例、systemd 重启风暴、蓝绿切换都会让两个 server 进程同时首启，各生成一份 `v1` 互相覆盖的后果是一半密文永久不可解。

**归口**：本文只定密钥这一半。「多进程同时首启」的另一半——goose 并发迁移——[18](18-v1-delivery-boundary-bs-binary.md) D5 已划给 [#113](https://github.com/liumingjian/dbs-monitor/issues/113)，本文不越界替它决定。

### 4.3 生成动作必须显式可见

生成成功后写一条结构化日志与一条 [T14](14-platform-observability-and-diagnostics.md) 平台事件，写明**密钥版本号与文件路径**，不写密钥材料。

理由：这条决策唯一真正的风险面是「运维忘了挂载密钥卷 → 三条件恰好成立 → 平台默默生成新密钥」。三条件把它挡在了「不会毁掉已有密文」这条线上（库里没密文才生成），但仍会让平台带着一把**错误的**新密钥继续跑；显式事件是运维事后能发现这件事的唯一线索。首启生成**不是故障**，平台健康仍是 `OK`（见 D8）。

---

## 5. D4 · 启动失败语义：只有配置文件读不到才拒绝启动

**结论**：keyring 故障**不拒绝启动**。

### 5.1 启动序

1. 加载配置文件。**读不到或解析失败 → 拒绝启动**，这是唯一一处。
2. **尽早起 HTTP 监听**。
3. 异步执行：keyring 自检（存在性、属主/权限、base64 解码长度、`current` 指向的版本是否存在）→ 连平台库 → goose 迁移 → 首启生成判定（D3）。
4. 全部结果投影进 [T14](14-platform-observability-and-diagnostics.md) 的平台健康四态快照（见 D8）。

**唯一拒绝启动的情形是配置文件读不到或解析失败**——那时连监听地址都不知道，起不来是物理事实而不是策略选择。

**否决「先自检再监听」的一切排法**。自检失败时平台会变成一个不回应的端口，而 `Restart=always` 会把它变成一个反复重启的进程：运维看到的信息量为零。这正是 [18](18-v1-delivery-boundary-bs-binary.md) D5 第 1 条「把平台挂了渲染成没有数据是本系统最严重的谎言」要根除的形态，进程层面的对应物就是「把平台挂了渲染成端口不通」。

### 5.2 keyring 故障时的行为

出现以下任一（[T13](13-credential-encryption-rotation-and-revocation.md) D2.2 全文保留）：密钥文件缺失、解码后长度错误、不可读、属主或权限不符、密文引用未知密钥版本、AES-GCM 认证失败、`current` 指向不存在的版本——

- HTTP 起来、可登录、平台自身故障可见，健康子系统 `keyring` 取 `FAILED`（D8）；
- 一切需要解密的操作（服务端直连采集、可用性探针、能力探测、凭据连接测试）失败，并归类为**平台自身故障**；
- **绝不**降格成目标实例的 `DB_UNREACHABLE` 或 `NO_DATA`；
- **绝不**悄悄生成新密钥；
- **绝不**自动 `chmod` 修复权限——替客户改权限会掩盖挂载方式本身的错误，而权限一旦曾经过宽，改回来也不能撤销「可能已被他人读过」这个事实。

### 5.3 不为「keyring 整个不存在」设快速失败例外

「库里已有密文、keyring 目录整个不存在」几乎必然是挂载没生效，一度考虑让它快速失败以避免半残运行。**否决**，理由两条：

1. 运维视角里「目录没挂上」和「属主错了」是同一类事故，分叉两种进程行为只会让文档和排障多一条无收益的分支；
2. [24](../acceptance/24-v1-acceptance-entries-d.md) 的 `AC-08-F4` 只有一条真实手段（把 `master-key-vN` `chmod` 成错误权限，或让 `current` 悬空）。若「不存在」走另一条进程路径，验收就结构性地覆盖不到它。

**统一行为，统一验收面。**

---

## 6. D5 · 主密钥轮换：停机子命令 + advisory lock 拒绝并发

**结论**：`server rotate-master-key` 子命令，须先停 server，非交互可调用，中断可重跑。

1. **同一 `server` 二进制的子命令**，不做第二个二进制（承 [T5](05-backend-code-structure.md) 编译期同源）。
2. **要求先停 server**，不实现在线排他维护锁。[T13](13-credential-encryption-rotation-and-revocation.md) D7.1 步骤 1 原文的「或取得排他维护锁」在此**改写为只保留停机**：50 实例量级不值得为一个低频运维动作支付「在线重加密与在线写入并发正确」的那套复杂度。
3. **必须可被非交互调用**（[24](../acceptance/24-v1-acceptance-entries-d.md) D14 硬要求）：不得只有交互式确认入口，否则 `AC-08-S7` 跑不动。
4. **失败恢复照搬** [T13](13-credential-encryption-rotation-and-revocation.md) D7.2：新密钥未落稳不切 `current`；事务失败则全部旧行保持原版本；`current` 已切但重加密未完成时 keyring 同时保留两版，**重跑命令即可**；仍有在线行引用旧版本时不得删除旧密钥。**「中断可重跑」是验收断言点**（`AC-08-S7`）。

### 6.1 advisory lock：把「忘了停」变成一句拒绝

server 运行期间在平台库上持有一把固定 key 的会话级 `pg_advisory_lock`；轮换命令启动时 `pg_try_advisory_lock`，**拿不到就拒绝执行并明确报错**（「server 仍在运行，请先停止后重试」）。

这不构成第 2 条否决的那套在线维护锁——它不允许任何并发，只做一件事：把「运维忘了停 server」从**静默的数据损坏**变成一句人能读懂的拒绝。

**否决 PID 文件**：容器、换机、残留 PID 文件三个场景下它同时存在假阳与假阴，而这里的代价是密文损坏。**否决探测端口**：探不到端口不等于没有别的进程连着同一个库（这恰恰是外部 PG 形态新引入的可能性）。advisory lock 的成本是一条 SQL，且天然覆盖「另一台机器上还连着同一个平台库的 server」。

---

## 7. D6 · 威胁模型重估：保护面升级，不保护面收窄并新增一条

[T13](13-credential-encryption-rotation-and-revocation.md) D1 的原假设是「主密钥与密文同机，因此防不住整机失陷」。客户自备外部 PostgreSQL 之后，**密文在客户的 PG 主机上、主密钥在平台 server 主机上，两者天然分离到两台机器**，该假设的前提改变。

### 7.1 保护面（升级）

1. 控制面数据库备份、`pg_dump` 或数据目录快照单独泄露（保留）；
2. 攻击者只有平台数据库读取能力（保留）；
3. 运维误把数据库备份交给不应接触目标库密码的人（保留）；
4. **新增：外部 PostgreSQL 主机整机失陷（含 root）**。密文全在那台机上，主密钥一份都不在。这是交付形态变更白送的一条真实增强，**必须写进交付文档**——客户的 DBA 团队与平台运维团队通常不是同一批人，这条直接决定了「谁能看到目标库密码」。

### 7.2 不保护面（收窄，并新增一条）

- **平台 server 主机失陷**（root 或运行用户）——主密钥与配置文件都在那里。原文的「宿主机失陷」在此收窄为特指这一台；
- 服务端进程被控制或进程内存被读取（保留）；
- 攻击者同时取得数据库密文与主密钥文件（保留）；
- 恶意平台管理员通过产品已有的连接能力滥用凭据（保留）；
- **新增：配置文件泄露 = 平台库凭据泄露**（见 D7）。

原否决保留：把同机加密描述成能防 root；随包内置固定主密钥；引入外部秘密管理依赖。「同机」现在特指平台 server 那一台。

---

## 8. D7 · 平台库凭据与本地快照：两个被移交的落位

### 8.1 平台库凭据：配置文件明文，代价显式记账

[18](18-v1-delivery-boundary-bs-binary.md) §4 把「平台配置里不存在 PG 密码」反转了——外部库形态下配置里必然存在平台库凭据，它是 T13 必须覆盖的一个新秘密。

**结论：配置文件明文，`0600`，属主运行用户。**

这里有一个不可绕过的鸡生蛋：主密钥用来解开**库里**的密文，而连库本身就需要密码——平台库凭据**在结构上不可能**被主密钥保护。

- **否决「引入第二个更早的密钥」**：只是把同一个问题推迟一层（第二密钥又从哪来），净增一条密钥生命周期而不减少任何暴露面；
- **否决「只支持免密方式（`.pgpass` / 客户端证书 / IAM）」**：强加客户环境要求，违反 [18](18-v1-delivery-boundary-bs-binary.md) 的部署前置最小化。可作为**可选**替代被支持，但不作为承诺面、不写进验收；
- 代价显式接受并写进 D6.2 与交付文档：**读到配置文件 = 拿到平台库**。

**配置文件本身的约定**（[18](18-v1-delivery-boundary-bs-binary.md) D3/D4 只定了「它是规范来源」，未定路径与权限，本文补齐）：

- 默认 `/etc/dbs-monitor/config.yaml`，配置项/环境变量可覆盖路径；
- `0600`，属主运行用户；
- 启动时校验属主与权限，**过宽只 warn + 平台健康降 `DEGRADED`，不拒绝启动**。

与 keyring 的差别是**严重度而非行为**：keyring 坏了采集全停（`FAILED`），配置文件权限过宽只是暴露面变大而功能完好。把后者做成拒启动会在客户第一次部署时挡住一大批人，而他们唯一的应对是 `chmod` 后重来——没有信息增量。

### 8.2 本地通知快照文件：无变化，仅确认落位

承 [18](18-v1-delivery-boundary-bs-binary.md) D5 第 2 条移交。[T13](13-credential-encryption-rotation-and-revocation.md) D8.3 的结论**全文保留**：同一版本化 keyring、同一密钥版本标识，**不另设第二密钥源、不落明文、不设独立的快照重加密流程**（D5 轮换后，快照在下一次派生刷新时自然携带新密文）。

落位确认：快照文件与 keyring 同属平台 server 主机，`0600`、属主运行用户，路径同样是「硬编码默认 + 配置项可覆盖」。它是库的只读派生缓存，**不得被引用为「配置可以外置」的先例**（[18](18-v1-delivery-boundary-bs-binary.md) D5 已钉）。

---

## 9. D8 · keyring 进入平台健康四态，取 `FAILED`

[T14](14-platform-observability-and-diagnostics.md) §2 归并的子系统是「服务端进程、自带 PostgreSQL、采集调度器、分区维护、证书、Agent 接入、磁盘水位」七项，其中没有 keyring；§7 第 4 笔只说了「密钥类故障是平台健康事实」，没说它是不是独立条目。

**结论：追加为第八个独立子系统条目**（追加不改写，服从「新增枚举码只许追加」的禁令），故障时取 **`FAILED`** 而非 `DEGRADED`——keyring 一坏，所有需要解密的采集与连接测试全停，这是全局失能不是局部降级。归并顺序 `FAILED > UNKNOWN > DEGRADED > OK` 不变。

- **首启自动生成不是故障**，走平台事件（D3.3），健康仍是 `OK`；
- 配置文件权限过宽是 `DEGRADED`（D7.1），不是 `FAILED`；
- [T14](14-platform-observability-and-diagnostics.md) §2 里「自带 PostgreSQL」那一项已被 [18](18-v1-delivery-boundary-bs-binary.md) 改成外部 PG，那是 [#113](https://github.com/liumingjian/dbs-monitor/issues/113) 的事，**本文不碰**。

---

## 10. 外溢到实现的硬要求

无安装器形态下配置文件是唯一的秘密载体，而诊断包是设计上就要交给外人的东西——**这两者相撞是本决策最可能出事的一处**。

1. **诊断包绝不包含配置文件原文。** 只输出结构化的、脱敏后的有效配置摘要，密码类字段固定渲染为 `[REDACTED]`。
2. **「平台库密码」加入秘密扫描禁名单**（[T14](14-platform-observability-and-diagnostics.md) §5 秘密禁区、[T9](10-ai-guardrails-and-verification.md) 相关守卫）。原禁名单写于「平台配置里不存在 PG 密码」的年代，不知道有这个东西。
3. **`rotate-master-key` 必须可非交互调用**（[24](../acceptance/24-v1-acceptance-entries-d.md) D14 已登记，本文重申）。
4. **server 运行期持有平台库 advisory lock**，轮换命令 `pg_try_advisory_lock` 失败即拒绝（D5.1）。
5. **部署前置条件文档**新增：keyring 目录的 root 预建命令（D2）、配置文件权限要求（D7.1）、换机恢复时的 keyring 搬迁步骤（D1.1）。

---

## 11. 验收记账：不新增矩阵条目，只修订既有判据文字

[24](../acceptance/24-v1-acceptance-entries-d.md) 已把矩阵定稿在 **81 条条目、78 条硬底**，`matrix.yaml` 自此无 `TBD`。本文定的语义按下表挂到既有条目：

| 既有条目 | 补记内容 |
|---|---|
| `AC-08-F4`（keyring 故障不降格） | keyring 故障**不拒绝启动**：HTTP 可达、可登录、平台健康 `keyring` 子系统为 `FAILED` 且可见；解密类操作失败归平台自身故障，不出现 `DB_UNREACHABLE` / `NO_DATA` |
| `AC-08-S7`（主密钥离线轮换，执行序末） | 非交互调用；server 运行时**拒绝执行**（advisory lock）；中断可重跑 |
| B7 / 秘密扫描面（既有守卫） | 诊断包不含配置文件原文、密码类字段 `[REDACTED]`、平台库密码入禁名单 |

**否决新增条目。** 矩阵与票是**单向对齐（矩阵 → 票）**，本文是被矩阵覆盖的下游；往回加条目会破坏那条单向性，也会让 81/78 这个刚定死的硬底变成一个活数。

---

## 12. 保持有效（本文不触碰）

| 来源 | 内容 |
|---|---|
| [T13](13-credential-encryption-rotation-and-revocation.md) D3 | 密文格式 `format_version \|\| nonce \|\| ciphertext_and_tag`、AES-256-GCM、每次写入独立 nonce、AAD 绑定实例与字段、`password_key_version` 独立成列、不新增加密接缝 |
| [T13](13-credential-encryption-rotation-and-revocation.md) D4 | 两种写操作不合并、连接测试后写入、失败不做部分更新、轮换归客户 DBA |
| [T13](13-credential-encryption-rotation-and-revocation.md) D5/D6 | Agent 显式登记、令牌只存哈希、一次性回显、轮换立即生效、吊销 ≠ 停用、不做机器绑定 |
| [T13](13-credential-encryption-rotation-and-revocation.md) D7.1 步骤 2–6、D7.2 | 版本化 keyring 轮换流程与失败恢复（只有步骤 1 的「或取得排他维护锁」被 D5 改写） |
| [T13](13-credential-encryption-rotation-and-revocation.md) D8.1/D8.2 | 备份与 keyring 是两个独立制品、换机恢复、主密钥遗失即不可恢复且无后门、walking skeleton 迁移边界 |
| [T13](13-credential-encryption-rotation-and-revocation.md) D9 | 允许解密的四类内部操作、不构造完整 DSN、不作虚假内存擦除保证、秘密永不出站 |
| [T14](14-platform-observability-and-diagnostics.md) D2/D3 | 平台健康独立四态与归并顺序、平台故障只走三个出口、不进目标告警链路 |
| [18](18-v1-delivery-boundary-bs-binary.md) D4/D5 | 配置文件是规范来源、初始管理员口令首启一次性打印、DB 不可达时 HTTP 照常起来 |

---

## 13. 交给下游

| 去向 | 内容 |
|---|---|
| [#113 数据与恢复门的具体证据](https://github.com/liumingjian/dbs-monitor/issues/113) | 多进程同时首启的 goose 并发迁移那一半（D3.2）；外部 PG 短暂不可用时的重试语义；[T14](14-platform-observability-and-diagnostics.md) §2「自带 PostgreSQL」子系统条目改写为外部 PG（D8）；换机恢复演练须带独立 keyring |
| [#112 生产安全边界的具体断言集](https://github.com/liumingjian/dbs-monitor/issues/112) | §10 第 1、2 条如何写成自动断言（诊断包无配置文件原文、平台库密码入秘密扫描禁名单）；配置文件与 keyring 的权限断言 |
| [#110 v1 交付物与候选留痕](https://github.com/liumingjian/dbs-monitor/issues/110) | 部署前置条件文档新增的三项（§10 第 5 条）；配置样例中平台库凭据字段的写法与告知 |
| [#116 外部前置 PostgreSQL 的版本要求与部署前置条件](https://github.com/liumingjian/dbs-monitor/issues/116) | `pg_advisory_lock`（D5.1）对外部 PG 的版本与权限要求——该函数各版本皆有，但须确认平台库角色可用 |
| 实现票（另开，不属本地图） | keyring 加载与自检、`O_EXCL` 首启生成与平台事件、`server rotate-master-key` 子命令与 advisory lock、配置文件权限校验、诊断包脱敏、`keyring` 健康子系统接入四态归并 |
