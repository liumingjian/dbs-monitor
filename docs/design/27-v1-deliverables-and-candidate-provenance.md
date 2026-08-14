# 27 · v1 交付物与候选留痕

> 出处：[v1 交付物与候选留痕 #110](https://github.com/liumingjian/dbs-monitor/issues/110)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：把 [18](18-v1-delivery-boundary-bs-binary.md) D1/D10 明文移交的「交付物清单的确切内容、候选提交如何唯一标识、二进制如何产出与归档、`main` 的 branch protection 与审批门、Go/No-Go 报告格式与绑定」落成决策，并接下 [19](19-agent-distribution-and-upgrade.md) D7 的 plan B 备料清单。**本文不原地改写 20 / 21 / 22 / 23 / 24 / 26 任何一条**；对 `matrix.yaml` 的触碰仅限修订 `AC-08-S8` 一条判据文字，条目数与硬底一字不变（91 / 88）。
> 输入边界（不重议）：[18](18-v1-delivery-boundary-bs-binary.md) D1（无安装包无安装脚本）、D6（双架构 + `CGO_ENABLED=0` + 交叉编译 + 真机验证不可替代）、D7（备份责任归客户、`migrations/` 只写 up、回滚 = 装回旧二进制 + 恢复备份）、D10（T15 D1/D2/D3 原则保留、D3.3/D4/D5 作废）；[19](19-agent-distribution-and-upgrade.md) D1（下载端点为主线、手工分发为 plan B）、D2（server 回自身版本、Agent 落后 warn、超窗拒收）、D5（CA 指纹钉扎）、D7（备料清单、否决代码签名）；[T15](15-ci-and-release-pipeline.md) D1/D2/D3 保留部分；[20](20-v1-acceptance-matrix.md) D5/D6（横切独立计分、`pending` 政策）；[26](26-data-and-recovery-gate.md) 客户责任清单（给 #116 的单向输入，本文不复述）。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**候选身份是 40 位 commit SHA，语义化 annotated tag 是它的人类可读别名；仓库公开，因此我方不发布任何二进制——交付载体是一个 tag 指向的仓库快照 + 一份可照做的构建指令，产物由交付团队在自己的构建机上跑出来；`SHA256SUMS` 降格为构建期副产物，语义收窄为「plan B 手工搬运的 agent 与 server 正在服务的那份同源」；`main` 只开 required status check `check` + 禁 force push / 禁删除、管理员不豁免、不要求 approval（单维护者下 approval 是把自己锁在门外）；Go/No-Go 报告由 `make acceptance` 生成为机器可读 JSON 并入库 `docs/validation/`，tag 打在候选 SHA 上，新增 `release-gate` workflow 把「不存在 tag 一打就发的通路」变成一个会变红的执行者——正式 tag 要求 `verdict: GO`，`-rc.N` 只要求「被正式评判过」。**

---

## 1. D1 · 候选标识与版本号语义：SHA 是身份，tag 是别名

**结论**

| 项 | 结论 |
|---|---|
| 候选的唯一身份 | **40 位 commit SHA**。所有留痕（验收报告、构建元数据、二进制自报身份）一律以 SHA 为主键 |
| 人类可读别名 | **语义化 annotated tag** `vMAJOR.MINOR.PATCH`，v1 首发 `v1.0.0`；只由维护者创建 |
| 版本注入 | `-ldflags -X` 注入三项：`version`（tag）、`commit`（短 SHA）、`buildTime`；无 tag 时为 `0.0.0-dev+<sha>` |
| 二进制自报身份 | `server --version` / `agent --version`，是二进制自报身份的**唯一**面 |
| 版本比较口径 | 只看 `MAJOR.MINOR`，**不解析 build 元数据**；[19](19-agent-distribution-and-upgrade.md) D2 的「超窗」= **MAJOR 落后 ≥ 2**（承 [T3](https://github.com/liumingjian/dbs-monitor/issues/21)「向后兼容一个大版本」） |

**理由**

1. tag 可移动、可删除、可指向任意提交；SHA 不可。留痕要活到 v1.x，主键必须是不可变的那个。
2. [19](19-agent-distribution-and-upgrade.md) D2 要求版本号**能被程序比较**（Agent 落后即 warn、超窗即拒收）。这条承诺此前**没有被承诺物**——全仓 `grep` 无 ldflags 注入、无 `Version` 常量。本条给它落一个来源。
3. 「超窗 = MAJOR 落后 ≥2」直接从 T3 的兼容窗口推出，不新造数字：兼容一个大版本，则落后一个大版本仍在窗内，落后两个即出窗。

---

## 2. D2 · 二进制归档：我方不发布二进制，只承诺可构建

**结论：v1 不产出 GitHub Release assets，不发布任何二进制。** 承诺收窄为一句：**给定 commit SHA + 给定工具链版本，按 `docs/deploy/build.md` 可构建出功能等价的产物。**

**理由**

1. **仓库是 public**。发布 Release assets 等于把私有化产品的可执行文件公开分发。这是产品决定，已由维护者拍板：不发。
2. **不承诺 bit-for-bit 可复现**。Go `-trimpath` + `CGO_ENABLED=0` 已经很接近，但前端资产经 `npm ci` 构建后 `go:embed` 进 server，硬承诺 bit-for-bit 需要额外锁定整条 npm 产物链，而 v1 没有这个基础设施。**承诺一件测不了的事，等于给自己立一条永远红的假门。**
3. 由此，[T15](15-ci-and-release-pipeline.md) D3.3 / D5（Environment 审批后归档为 Release assets、assets 长期保留）在 [18](18-v1-delivery-boundary-bs-binary.md) D10 判其作废之后**不再复活**——它们失去的不只是「tar 这种载体」，而是**被归档物本身**。

**代价明说**：交付团队必须自己有一台能跑 Go + Node 的构建机；我方无法在事后证明「客户手上那个二进制是我们的」。后者本来也无从证明——[19](19-agent-distribution-and-upgrade.md) D7 已否决代码签名（无密钥保管与吊销基础设施）。

---

## 3. D3 · v1 交付物：规范层与产物层两分

**结论：交付物分两层，只有规范层由我方交付并承诺内容。**

**规范层 = 一个 tag 指向的仓库快照**，其中与交付直接相关的受版本管理文件为：

| 交付物 | 说明 |
|---|---|
| server 配置样例 | 规范来源是配置文件（[18](18-v1-delivery-boundary-bs-binary.md) D4、[25](25-master-key-provenance-and-startup-failure.md)） |
| agent 配置样例 | 同上（[19](19-agent-distribution-and-upgrade.md) D7） |
| server systemd unit 模板 | 验收只断言「能拉起、能重启恢复」 |
| agent systemd unit 模板 | 同上；装要 root、跑不要 root |
| 部署前置条件文档 | 含 PG 前置（指向 [#116](https://github.com/liumingjian/dbs-monitor/issues/116)）与客户责任清单（指向 [26](26-data-and-recovery-gate.md)），**不复述** |
| 手工接入步骤文档（plan B） | 含令牌落盘路径与 `0600`、专用非 root 用户、CA 证书取用方式 |
| 构建指令 `docs/deploy/build.md` | 三行：装 `.tool-versions` 里的两个版本、`npm ci`、`make package-binaries-linux-<arch>` |

**产物层 = 交付团队在自己构建机上跑出的四个二进制**（server / agent × amd64 / arm64）**+ 构建期生成的 `SHA256SUMS`**。产物层**不由我方归档、不由我方承诺内容**。

**不进交付物的两项，逐条给理由**

- **迁移文件**：`migrations/*.sql` 已 `go:embed` 进二进制（[`migrations/migrate.go:14`](../../migrations/migrate.go)），天然不是独立交付物。
- **CA 证书文件**：它是**每套部署运行期生成的实例私有物**。把它放进交付物等于发布一个所有客户共享的信任根——那是安全事故，不是便利。[19](19-agent-distribution-and-upgrade.md) D7 备料清单里的「CA 证书文件」读作**部署现场从该套部署取出并带外分发**，不是我方随交付物发出。

**死资产处置：删除，不留 legacy。** `scripts/package-linux.sh`（自建 PG、glibc 精确匹配、拒绝交叉编译）、`packaging/bundle/install.sh`、`packaging/systemd/dbs-monitor-postgres.service` 全部依附 [18](18-v1-delivery-boundary-bs-binary.md) 已作废的形态。**留着当 legacy 参考被否决**：判死的东西留在仓库里，迟早有人照着它跑。`packaging/` 重写为只剩两个 unit 模板 + 配置样例 + README。删除动作走普通实现票，不在本票内做。

---

## 4. D4 · branch protection：required status check，不要求 approval

**一手事实**：`main` 当前**无 branch protection、无 ruleset**；近期若干 docs 提交是**直推 main**（`check-full` 在 push 上跑绿），历史上也有走 PR 的（[#117](https://github.com/liumingjian/dbs-monitor/issues/117) / #104 / #103）。仓库 owner 单人。

**结论**

| 项 | v1 要求 |
|---|---|
| required status checks | **`check` 必过**（PR 门），且 `Require branches to be up to date` |
| force push / 分支删除 | **禁止** |
| 管理员豁免 | **不开**。bypass 一开，规则就只是建议 |
| required approvals | **不要求**，且这是**有理由的决策而非疏漏**（见下） |

**为什么不要求 approval**：GitHub 上作者不能 approve 自己的 PR。单维护者仓库要求 1 个 approval 等于把唯一的人锁在门外，实际结果只会是「用管理员权限 bypass」——那比不设更糟，因为它把一条被绕过的规则伪装成一条生效的规则。**v1 没有第二双眼睛的代码审查，这是团队规模的事实，写进文档而不是假装有。**

**发布侧的「刻意停顿」落在哪**：[T15](15-ci-and-release-pipeline.md) D3.3 的 GitHub Environment 审批门在 D2 之后**失去了被审批物**（无 assets 可发布）。停顿改为落在**打 tag 这个动作本身**——tag 只由维护者手工创建，创建前须确认该 SHA 的 `check-full` 与验收报告为 GO。这条本身是人的纪律，机器强制由 D6 的 `release-gate` 补上。

---

## 5. D5 · Go/No-Go 报告：机器生成的 JSON 入库，薄壳 Markdown 承载判不了的东西

**结论**

1. `make acceptance` 产出 **`acceptance-report.json`**：
   - **头部四字段**：候选 SHA、tag（可空）、执行环境（OS / arch / 平台库与目标库两个 PG 版本）、开始与结束时间。
   - **主体**：逐条目 ID 记 `pass` / `fail` / `n-a` / `pending` + `test_ref` + 耗时。
   - **顶层 `verdict`**：**判定规则写死在生成器里而不是报告里**——88 条硬底（[26](26-data-and-recovery-gate.md) 定的终态）任一非 `pass` 即 `NO-GO`。报告是事实的载体，不是规则的载体；规则若跟着报告走，改报告就能改结论。
2. **薄壳 Markdown 结论页** `docs/validation/v1-go-no-go-<短SHA>.md`，只承载机器判不了的东西：[#115](https://github.com/liumingjian/dbs-monitor/issues/115) 的真机观感、AntD 观感门 `AC-05-S4`（[23](23-v1-acceptance-entries-c.md) 定为 `n-a`、判定归 #115）、遗留风险与例外说明。**它必须引用 JSON 的候选 SHA，且不得覆盖 `verdict`。**
3. **两份文件都入库 `docs/validation/`**，文件名含候选短 SHA。
4. **绑定**：tag 打在**候选 SHA** 上（保证 tag 指的就是被验的那棵树，这是 [T15](15-ci-and-release-pipeline.md) D3.2「精确提交校验」的字面要求）；报告作为后续提交合入，靠文件内 SHA 字段**单向指回**候选。

**否决记录**

| 被否决 | 为什么 |
|---|---|
| tag 打在「报告提交」上（报告的树包含对其父候选的判定） | tag 就不再指向被验的那棵树，与 T15 D3.2 直接冲突 |
| 报告只进 Actions artifact 不入库 | 90 天蒸发，而结论要活到下一个 v1.x；证据的保存期必须长于它所支撑的承诺 |
| 人工撰写整份报告 | 91 条条目人手汇总必然漂移，且「达标与否」会变成一句话而不是一组事实 |

---

## 6. D6 · `release-gate`：把纪律变成一个会变红的执行者

**结论：新增 `release-gate` workflow，tag push 触发，不构建、不发布，只校验并把结论写进 run。**

| 校验 | 正式 tag（无 `-rc` 后缀） | `-rc.N` |
|---|---|---|
| 该 SHA 有成功的 `check-full` | 要求 | 要求 |
| 仓库中存在引用该 SHA 的验收报告 | 要求，且 **`verdict: GO`** | 要求**存在**，`GO` / `NO-GO` 皆可 |
| tag 是 annotated 且由维护者创建 | 要求 | 要求 |

报告允许在 tag 之后才合入，因此该 workflow 支持手动重跑。

**为什么设 rc**：[#115](https://github.com/liumingjian/dbs-monitor/issues/115) 的真机验收必然多轮，且早期必然红。若每一轮候选都要 GO 才配有 tag，实际结果是**为了不打红 tag 而干脆不打 tag**，候选就此失去唯一标识——而唯一标识正是本票要解决的问题。**rc tag 的语义因此定为「这是一个被正式评判过的候选」，不是「这个候选通过了」。** 副产物是 NO-GO 报告也被永久留痕，这正是可审计要的东西。

---

## 7. D7 · `SHA256SUMS`：构建期副产物，语义收窄为同源性

**一手事实**：[19](19-agent-distribution-and-upgrade.md) D7 要求「另发布二进制的 SHA-256 校验和清单，随交付物提供，供 plan B 手工路径核对」，其前提是**我方发布二进制**。D2 之后该前提消失。

**结论：清单改为 `make package-binaries-linux-<arch>` 的构建期副产物**（`dist/bin/<os-arch>/SHA256SUMS`），**语义收窄为同源性**——证明手工搬到 Agent 主机上的那个 agent 二进制，与 server 主机上正在服务下载端点的那份，是**同一次构建**的产物。

**否决「我方在 CI 里只发布 checksum 不发布二进制」**：不承诺 bit-for-bit 可复现（D2）时，我方发布的 checksum 与交付团队自己构建出的二进制**必然对不上**。那是一条注定变红且无人能修的假门，比没有更坏。

**否决「取消清单，plan B 只靠带外传文件」**：[19](19-agent-distribution-and-upgrade.md) D7 的原意是 plan B 不能没有校验手段——它的适用前提恰恰是主线通路不可信。

**必须显式记账的一点**：校验的**信任根从我方移到了交付团队的构建机**。这不是措辞调整，是信任模型的改动，必须写进 [19](19-agent-distribution-and-upgrade.md) 的下游落盘，不得默默改义。

---

## 8. D8 · 工具链钉法：`.tool-versions` 是单一来源

**一手事实**：`go.mod` 钉 `go 1.23.0` 但**无 `toolchain` 指令**；Node **只在两个 workflow 里钉 `22.x`**，仓库无 `.nvmrc` / `.tool-versions`。即「交付团队照着构建」目前**没有可照的钉子**，且 `x` 通配意味着不同时间构建出的 Go 版本不同。

**结论**

1. 新增 **`.tool-versions`**，写 Go 与 Node 的**精确版本**；`go.mod` 补 `toolchain go1.23.<patch>`。
2. **两个 workflow 改为从 `.tool-versions` 读版本**，消灭第二处真相。
3. `docs/deploy/build.md` 只写三行：装这两个版本、`npm ci`、`make package-binaries-linux-<arch>`。
4. `check` 内加一条守卫：**workflow 与 `.tool-versions` 版本不一致即失败**（承 [T9](10-ai-guardrails-and-verification.md) 生成物漂移门的同一手法）。

没有这四条，D2 的「给定工具链可构建」就是一句没有指称对象的话。

---

## 9. D9 · agent 二进制如何抵达 server 主机

[19](19-agent-distribution-and-upgrade.md) D1 的主线是 server 经 `GET /api/v1/agent/download?arch=` 分发 agent 二进制。D2 之后 server 由交付团队构建，agent 二进制的来源必须落地。

**结论：部署时把双架构 agent 二进制放进配置项 `agent_binary_dir` 指定的目录，server 从磁盘读。** server 启动自检该目录下 `dbs-monitor-agent-linux-{amd64,arm64}` 与 `SHA256SUMS` 是否齐全；**缺失时下载端点返回明确失败而非 404 沉默**，且这是一条健康事实（承 [T14](14-platform-observability-and-diagnostics.md)）。

**否决 `go:embed` 进 server**：会让 amd64 的 server 二进制里带一份 arm64 agent，体积与构建复杂度换来的只是省一次 `cp`；且 agent 单独更新必须重建整个 server。

**否决「主线降级、只留 plan B」**：[19](19-agent-distribution-and-upgrade.md) D1 刚刚把主线定死，本票无权翻它。若将来要翻，须新开决策记录。

---

## 10. D10 · 升级、降级与版本偏斜的对外承诺

| 项 | 承诺 |
|---|---|
| 升级 | v1.x 之内任意版本**可直升最新**；跨 MAJOR **不承诺跳版**，必须逐 MAJOR |
| 降级 | **不受支持**。装回旧二进制会遇到新 schema，而 `migrations/` 只写 up（[18](18-v1-delivery-boundary-bs-binary.md) D7）；回退路径只有「装回旧二进制 + 恢复升级前备份」，而备份是客户责任（[26](26-data-and-recovery-gate.md)） |
| 版本偏斜 | 升级顺序 **先 server 后 agent**。期间 agent 落后 warn 但仍工作（[19](19-agent-distribution-and-upgrade.md) D2）；**反序（先升 agent）不承诺** |
| v1 起点 | **v1.0.0 是首发，不存在从 0.x 的升级路径**。walking skeleton 期的库不承诺可迁移，任何 v1 前部署一律**重装** |

**措辞硬要求**（照 [26](26-data-and-recovery-gate.md) 客户责任清单的先例）：文档中必须出现「**降级不受支持**」与「**未备份即升级 = 不可回退**」两句原文，**不得软化成「建议先备份」**。软化的措辞在真正需要它的那个凌晨读起来像客套话。

---

## 11. D11 · 对验收矩阵的触碰：只修订一条判据，91 / 88 一字不变

**结论：不新增条目，不动硬底。** 仅修订 `AC-08-S8` 一条判据文字，补上 D9 的失败语义。

逐类给理由：

| 本票产出 | 为什么不进矩阵 |
|---|---|
| `release-gate` 校验、`.tool-versions` 漂移守卫、`SHA256SUMS` 构建期产出 | 全是**构建与发布线的自检**，执行者是 CI 而不是 `make acceptance`。进矩阵等于让验收去验流水线——[20](20-v1-acceptance-matrix.md) 定的矩阵射程是产品语义 |
| `--version` 子命令与版本注入 | 二进制自报身份，无产品语义面 |
| `agent_binary_dir` 缺失时的下载端点失败语义 | **唯一真有产品语义的一条**，落在 `AC-08-S8` 已有的下载端点面上，补一句判据即可 |

由此 [24](24-v1-acceptance-entries-d.md) 的「矩阵自此无 `TBD`」与 [26](26-data-and-recovery-gate.md) 的终态 **91 条条目 / 88 条硬底 / `n-a` 5 / `pending` 2 / `exceptions` `[]`** 不被本票动摇。`AC-08-S8` 仍是那两条允许 `pending` 的普通加深之一。

---

## 12. D12 · 不建交付前检查表

**结论：本票不汇总 [21](21-v1-acceptance-entries-a.md)–[26](26-data-and-recovery-gate.md) 的外溢实现硬要求，只在 §14 放一张**指针表**（每条 → 出处票号与文档章节），不复述内容、不加判定。

**理由**：汇总表是**第二份真相**。它一旦与原票不一致，人会照着表走，而原票里每条硬要求的**论证**就丢了；且这些硬要求的正确出口是**实现票**，不是又一份文档。若要一张能勾选的表，那属于 [#114](https://github.com/liumingjian/dbs-monitor/issues/114) 的门组成，且应由 `make acceptance` 的报告去承载而非人手维护——与 D5 让报告机器生成是同一条理由。

---

## 13. 外溢到实现的硬要求

1. **版本注入**：`-ldflags -X` 注入 `version` / `commit` / `buildTime`；`server --version`、`agent --version` 子命令；无 tag 时 `0.0.0-dev+<sha>`（D1）。
2. **工具链单一来源**：`.tool-versions` + `go.mod` `toolchain` 指令；两个 workflow 从中读版本；`check` 加不一致即失败的守卫（D8）。
3. **`SHA256SUMS` 构建期产出**：`make package-binaries-linux-<arch>` 输出 `dist/bin/<os-arch>/SHA256SUMS`（D7）。
4. **`agent_binary_dir`**：配置项 + 启动自检 + 缺失时下载端点明确失败并进健康事实源（D9）。
5. **`acceptance-report.json` 生成器**：头部四字段 + 逐条目状态 + 顶层 `verdict`，判定规则写死在生成器里（D5）。
6. **`release-gate` workflow**：tag push 触发，三条校验，正式 tag 与 rc 判定不同，支持手动重跑（D6）。
7. **死资产删除**：`scripts/package-linux.sh`、`packaging/bundle/`、`packaging/systemd/dbs-monitor-postgres.service`（D3）。
8. **文档新增**：`docs/deploy/build.md`、部署前置条件文档、手工接入步骤文档、双 unit 模板与双配置样例（D3）；措辞硬要求两句（D10）。

## 14. 各票外溢实现硬要求的指针（不复述、不判定）

| 出处 | 章节 |
|---|---|
| [21 · A 组](21-v1-acceptance-entries-a.md) | 时间参数化取值表（分区跨度 1min） |
| [22 · B 组](22-v1-acceptance-entries-b.md) | `repeat_interval` 下限可配、快照截断上限可配 |
| [23 · C 组](23-v1-acceptance-entries-c.md) | 无（C 组零外溢） |
| [24 · D 组](24-v1-acceptance-entries-d.md) | `10` §3.2 登记 `A10`/`A11`、采集新鲜度阈值可配、轮换命令可非交互调用 |
| [25 · 主密钥](25-master-key-provenance-and-startup-failure.md) | 五条，含诊断包绝不含配置文件原文 |
| [26 · 数据与恢复门](26-data-and-recovery-gate.md) | 五条 + 环境要求 `restore-target` profile |
| 本文 | §13 |

---

## 15. 交给下游

| 去向 | 内容 |
|---|---|
| [#114 Go/No-Go 质量门组成](https://github.com/liumingjian/dbs-monitor/issues/114) | 报告 `verdict` 之外的门（快慢两层边界、哪些门必须在真实 Linux 上跑）；若要可勾选的交付前检查表，落点在此而非本票（D12） |
| [#115 真 Linux 环境适配与最终验收](https://github.com/liumingjian/dbs-monitor/issues/115) | rc 轮次与真机验收的关系（D6）；薄壳 Markdown 结论页里人工承载的部分（D5） |
| [#116 外部前置 PostgreSQL 的版本要求与部署前置条件](https://github.com/liumingjian/dbs-monitor/issues/116) | 部署前置条件文档的内容（D3 只定它是交付物之一，不定它写什么） |
| 实现票 | §13 八条 |

---

## 16. 否决记录汇总

| 被否决 | 为什么 |
|---|---|
| 发布 GitHub Release assets | 仓库公开，等于公开分发私有化产品二进制（D2） |
| 承诺 bit-for-bit 可复现构建 | 前端产物链未锁定，v1 无签名基础设施；承诺一件测不了的事等于立一条假门（D2） |
| 我方只发布 checksum 不发布二进制 | 不承诺 bit-for-bit 时必然对不上，是注定变红且无人能修的假门（D7） |
| 取消 plan B 校验清单 | plan B 的适用前提正是通道不可信，那时它将完全没有校验手段（D7） |
| CA 证书文件进交付物 | 等于发布所有客户共享的信任根（D3） |
| 死资产留作 legacy 参考 | 判死的东西留在仓库里，迟早有人照着它跑（D3） |
| 要求 PR approval | 单维护者下作者不能自批，实际结果是管理员 bypass——把被绕过的规则伪装成生效的规则（D4） |
| 给管理员开 bypass | bypass 一开，规则就只是建议（D4） |
| tag 打在报告提交上 | tag 不再指向被验的那棵树，与 T15 D3.2 冲突（D5） |
| 报告只进 Actions artifact | 90 天蒸发，证据保存期必须长于它支撑的承诺（D5） |
| 不设 rc、只有正式 tag | 真机验收多轮且早期必然红，会逼出「为了不打红 tag 而不打 tag」，候选失去唯一标识（D6） |
| agent 二进制 `go:embed` 进 server | 异构架构产物进同一二进制，换来的只是省一次 `cp`；agent 单更必须重建 server（D9） |
| 本票汇总交付前检查表 | 第二份真相，且会丢掉每条硬要求的论证（D12） |
