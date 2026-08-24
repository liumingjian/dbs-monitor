---
status: partially-superseded
kind: execution-record
note: G1–G8 门总表在效；§2 与 §11 的条目计数自相矛盾（94 vs 104）
---
# 28 · v1 Go/No-Go 质量门组成

> 出处：[Go/No-Go 质量门组成 #114](https://github.com/liumingjian/dbs-monitor/issues/114)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：把 [20](20-v1-acceptance-matrix.md)–[24](24-v1-acceptance-entries-d.md) 定稿、[26](../design/26-data-and-recovery-gate.md)/[27-ext](../design/30-external-postgres-prerequisites.md)/[29](../design/29-production-security-boundary.md) 追加后的 104 条矩阵、[26](../design/26-data-and-recovery-gate.md) 的恢复门、[27](../design/27-v1-deliverables-and-candidate-provenance.md) 的候选留痕与报告、[27-ext](../design/30-external-postgres-prerequisites.md) D7 移交的「开发与 CI 触及的一切平台库必须是 17」**接进真实的执行者**——哪些检查是硬门、跑在哪、失败即阻断什么。
> **本文不原地改写 [20](20-v1-acceptance-matrix.md) / [21](21-v1-acceptance-entries-a.md) / [22](22-v1-acceptance-entries-b.md) / [23](23-v1-acceptance-entries-c.md) / [24](24-v1-acceptance-entries-d.md) / [26](../design/26-data-and-recovery-gate.md) / [27-ext](../design/30-external-postgres-prerequisites.md) 任何一条，矩阵终态（[29](../design/29-production-security-boundary.md) 追加 `SEC-1..10` 后为 **104 条条目 / 101 条硬底** / `n-a` 5 / `pending` 2 / `exceptions` `[]`）不被本票动摇。** 本票**不新增、不删除任何矩阵条目**。
> **本文不 supersede 任何在效条款。** 对 [10](../design/10-ai-guardrails-and-verification.md) D1 只加适用面澄清，对 [15](../design/15-ci-and-release-pipeline.md) D2 只增不改（见 §16）。
> 输入边界（不重议）：[10](../design/10-ai-guardrails-and-verification.md) D1（两层闭环、≤120 秒预算与其重新触发条件）、D2（compose 两 profile 与 `PGHOST_EXTERNAL` 逃生舱、`datlocprovider` 同构性）、D3（A/B 两栏登记表与三问准入判据）；[15](../design/15-ci-and-release-pipeline.md) D1/D2（Actions 唯一规范执行者、复用同一组 `make` 命令、PR 门 = `make check`）与 D3.4（最小权限、不用个人长期 token）；[18](../design/18-v1-delivery-boundary-bs-binary.md)（交付形态、废 T15 D3.3/D4/D5、解除 qemu 否决）；[20](20-v1-acceptance-matrix.md) D4（反假覆盖）、D6.6（`test_ref` 漂移门）、执行入口 `make acceptance`；[27](../design/27-v1-deliverables-and-candidate-provenance.md) D1（SHA 是身份）、D3（`main` 保护）、D5（报告与判定规则写死在生成器里）、D6（`release-gate` 与 rc 语义）；[27-ext](../design/30-external-postgres-prerequisites.md) D1（平台库钉死 17）、D7（原则归其、落地归本票）；一手事实见 [平台库 PG13+ 的一手事实核实 #107](https://github.com/liumingjian/dbs-monitor/issues/107)（sqlc 解析器固定 PG17 语法）。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**开发者面对的命令仍然只有两条（`make check` / `make check-full`），`make acceptance` 与 `release-gate` 定性为非开发者入口，[10](../design/10-ai-guardrails-and-verification.md) D1 不需要 supersede；104 条验收矩阵每次 push `main` 全量跑、红即阻断，但在 CI 里是独立 job；被监控 PG13–17 矩阵只跑采集与能力探测子集、跑在手工发起的发布门；平台库必须是 17 落成结构守卫 B14（既查连上的库也扫 compose 与 workflow 的 image tag）；供应链扫描的归属让给先落地的 [29](../design/29-production-security-boundary.md) D7、本票只定它不进 `evidence` 也不参与 `verdict`；RT-C 是记录门不是阈值门，缺数据即 NO-GO；三份证据合成唯一 `verdict`，判定规则仍写死在生成器里，缺失与 SHA 不一致是两种不同的失败；GitHub runner 上跑绿的验收一律标 `provisional` 且永不出 `GO`——能进 `verdict` 的证据只能来自真机 Linux。**

---

## 1. D1 · 层次：语义仍是两层，执行者可以有四个

**结论**

| 入口 | 谁调用 | 是不是「层」 |
|---|---|---|
| `make check` | **开发者每次改动**；PR 门 | 是（快层） |
| `make check-full` | CI（push `main`）、交付团队复跑 | 是（慢层） |
| `make acceptance` | **非开发者入口**：由 `make check-full` 调用，或 CI 独立 job 调用 | 否 |
| `release-gate` | CI，tag push 触发（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D6） | 否 |

1. **`make check-full` 作为命令仍然包含 `make acceptance`**——交付团队与真机验收要的是「一条命令复跑全部」，这是 [15](../design/15-ci-and-release-pipeline.md) D1「本地与 CI 复用同一组 `make` 命令」的直接要求。
2. **在 CI 里 `acceptance` 拆成独立 job**，与 `check-full` 并行（见 D2）。
3. [10](../design/10-ai-guardrails-and-verification.md) D1「不许加第三层」**不 supersede，继续成立**：它的论证目标是「不让执行者面对判断题」，而判断题的数量取决于**开发者要记住几条命令**，不取决于 CI 有几个 job。`make acceptance` 与 `release-gate` 开发者从不手敲。

**理由**：把百余条矩阵塞进现有 `check-full` job 有两个具体后果——现有 30 分钟 timeout 直接爆掉（单轮 ≤10 分钟只是 [21](21-v1-acceptance-entries-a.md) 对**时间参数化**的约束，不是全部条目的执行时长）；失败时一条日志里分不清是交叉编译挂了还是第 71 条挂了。拆 job 是执行编排，不是加层。

---

## 2. D2 · 验收矩阵：每次 push `main` 全量跑，红即阻断

**结论**

- 触发：push `main` + `workflow_dispatch`，与 `check-full` 同触发、不同 job。
- 范围：**104 条全量**，不设子集、不设采样、不设「快的先跑」。
- 失败语义：红即阻断（阻断的含义见 D11——`main` 允许红，但红着不许打 tag）。
- 接受它是一个 40–60 分钟量级的 job。

**理由**

1. [20](20-v1-acceptance-matrix.md) D4 的反假覆盖与四组条目全程 `exceptions: []` 是这套矩阵最值钱的性质，而**一个不常跑的门等于没有门**。
2. 「push main 跑子集、全量留给 rc」的直接后果是：难跑的那些条目——真 Agent 接入、备份恢复灌进真空 PG、主密钥轮换停机——**永远只在发版前才第一次红**，那正是最没有时间修的时刻。
3. 「CI 完全不跑、只在真机人工跑」会让百余条退化成人工检查表，[10](../design/10-ai-guardrails-and-verification.md)「把规范做成结构，而不是做成约定」当场失守。
4. **若 runner 撑不住，宁可先接受 job 时长，也不先砍覆盖面。** 这条写进本文，是为了让日后的提速动作必须从并行度、缓存、容器预热入手，而不是从删条目入手。

**与 D9 的关系**：CI 上这一遍产出的报告**不是** Go/No-Go 证据，是开发期信心。它的价值在于让 94 条**每天都在跑**，从而不会在真机那一轮才发现有几十条根本跑不起来。

---

## 3. D3 · 被监控 PG13–17 矩阵：只跑采集与能力探测，跑在发布门

**结论**

| 项 | 结论 |
|---|---|
| 跑不跑 | **跑**。v1 对 PG13–17 的支持承诺不降级为「尽力」 |
| 跑什么 | **采集与能力探测子集**：片①（采集）、片⑤⑥（观察面 / 增强监控）中依赖目标库版本的条目，以及能力四态探测 |
| 不跑什么 | 告警状态机、通知、维护窗口、权限、凭据接入、恢复门——**与目标库版本无关，跑五遍是纯浪费** |
| 版本 | PG 13/14/15/16/17 五个，复用 `compose.yaml` 已有的 `matrix` profile |
| 跑在哪 | **发布门**（D8 的 `release-evidence`），不进 PR 门、不进 push `main` |
| 失败语义 | 任一版本任一条目非 `pass` 即 **NO-GO** |

**必须产出的东西**：一张 **「每版本 × 每能力」的三态表**（`AVAILABLE` / `MISSING` / `NOT_APPLICABLE`），进证据 JSON。

**理由**：版本差异的唯一真实落点就是采集 SQL 与 `pg_stat_*` 系视图的可用性。更要紧的是——[24](24-v1-acceptance-entries-d.md) 已经把 `VERSION_UNSUPPORTED` 的唯一产生者钉在片⑧的**接入拒绝**上，[23](23-v1-acceptance-entries-c.md) 也据此在 `AC-05-F2` 记了缺口：**采集侧的版本差异目前没有任何验收面**。这张三态表就是那个面。它不进矩阵（矩阵射程是产品语义，且已定稿无 `TBD`），而是作为发布门的独立证据存在。

**不在射程内**：平台库版本矩阵。平台库已由 [27-ext](../design/30-external-postgres-prerequisites.md) D1 钉死 17，不存在矩阵可跑。

---

## 4. D4 · B14 · 平台库必须是 17：既查连上的库，也扫 image tag

**结论**：新增结构守卫 **B14**，跑在 `make check` 里，两条断言同时成立才绿：

1. **运行期**：当前连接的平台库 `server_version_num` 落在 17 大版本区间；并一并断言 [10](../design/10-ai-guardrails-and-verification.md) §2.1 早已规定、但至今仍是纸面的 `datlocprovider <> 'i'`。
2. **静态**：扫 `compose.yaml` 与 `.github/workflows/*.yml`，凡作**平台库**用的 PostgreSQL image tag 字面量必须是 `17`。被监控库（`matrix` profile）与 `postgres:12`、非 17 平台库这两个专用 profile 不在射程内，靠 service / profile 名白名单区分。

**理由**：这条门保护的不是客户，是**本仓库自己**。[27-ext](../design/30-external-postgres-prerequisites.md) D1 的运行期拒启动挡住的是「客户接了 PG16」；挡不住的是「AI 会话把开发库换成 16、写了一段 PG17 才有的语法、`make gen` 全绿、合进 `main`」——因为 [#107](https://github.com/liumingjian/dbs-monitor/issues/107) 已经查实：**sqlc 不连服务端，解析器固定为 PG17 语法**。只做运行期断言而不扫 image tag，等于把一道结构门降级成「希望没人改配置」。

**这条同时结清** [27-ext](../design/30-external-postgres-prerequisites.md) D7 移交给本票的落地项。

---

## 5. D5 · 供应链扫描：归属让给 [29](../design/29-production-security-boundary.md) D7，本票只定它怎么进报告

**结论**

1. **扫描的归属与阻断语义以 [29](../design/29-production-security-boundary.md) D7 为准，本票不推翻**：`govulncheck` 进 `check-full` 且阻断；`npm audit` 进 `check-full` 只报告不阻断；两者都不进矩阵；**`release-gate` 不重跑扫描、只读报告**。
2. 本票只定 [29](../design/29-production-security-boundary.md) 明确移交过来的那一件事——**结果怎么进 Go/No-Go 报告**：
   - `acceptance-report.json` 的 `evidence` 块**不设 vuln 子块**，扫描结果**不参与 `verdict`**。
   - `govulncheck` 的阻断作用**已经完整体现在「该 SHA 有成功的 `check-full`」这一条里**（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D6），报告不必重复记账。
   - `npm audit` 的输出进薄壳 Markdown 结论页的「遗留风险」段（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D5.2），**不得影响 `verdict`**——它本来就是报告不是门。
3. **不产 SBOM**：[27](../design/27-v1-deliverables-and-candidate-provenance.md) D2 已定 v1 不发布任何二进制、交付团队自建，SBOM 在这个形态下没有消费者。
4. Dependabot 开启（PR 形式的升级建议不是门）。

**记账：本票原本推荐把扫描拆成独立 `vuln-scan` workflow 并明确不计入 `release-gate`**，理由是「新披露一个 CVE，昨天还绿的候选今天重跑必红，tag 就此打不出来」。[29](../design/29-production-security-boundary.md) D7 用另一条路径解掉了同一个问题——**`release-gate` 只读报告、不重跑扫描**，那么已记录的成功 `check-full` 不会被后来的 CVE 追溯作废。两条路径解的是同一个失效面，[29](../design/29-production-security-boundary.md) 先落地且顺带解决了离线构建机上「跳过即绿」这一条本票没看到的退化，**故本票让步，不新开第二套安排**。`npm audit` 只报告的理由（传递依赖与 devDependency 噪音大，天天红的门等于没有门）也强于本票原推荐的 `--audit-level=high` 阻断。

## 6. D6 · RT-C：记录门，不是阈值门

**结论**

- 发布前**必须执行** `scripts/rt-c/run.sh` 并把 `summarize.py` 的结果作为证据归档。
- **不设自动通过阈值**。数字进报告，Go/No-Go 由人在薄壳 Markdown（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D5.2）里判。
- **但「缺数据」是硬失败**：证据块缺失即 `verdict: NO-GO`（见 D7）。

**理由**

1. 现在没有任何一台代表性生产硬件的基线。凭空写死阈值只会得到「CI 上红、真机上绿」或反过来，两种的实际结局都是把阈值调到绿——**那比没有门更坏**。
2. RT-C 结构上就进不了自动 CI：`scripts/rt-c/run.sh` 要求 `RT_C_CONFIRM=load-450m-points`、一个可弃的已迁移库、一个数据盘路径，灌约 4.536 亿点。
3. 强制执行 + 强制留数已经兑现了 RT-C 那条原始结论（**容量与查询门槛须用真实 PG 实测，不得以推算冒充证据**）——它要防的是「拿推算冒充实测」，不是「没达到某个数」。
4. 阈值等 [#115](https://github.com/liumingjian/dbs-monitor/issues/115) 跑出第一批真机数据后**新开决策记录**，本票不预写。

---

## 7. D7 · 三份证据合成唯一 `verdict`

**结论**：扩 `acceptance-report.json` 的 schema，顶层新增 `evidence` 块，**判定规则仍然写死在生成器里**（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D5 不变）。

```
{
  "candidate_sha": "<40 位>",
  "verdict": "GO" | "NO-GO" | "PROVISIONAL-PASS",
  "provisional": true | false,
  "evidence": {
    "matrix":   { "candidate_sha": ..., "source": ..., "result": ... },   // 104 条
    "pg_range": { "candidate_sha": ..., "source": ..., "result": ... },   // D3 五版本三态表
    "rt_c":     { "candidate_sha": ..., "source": ..., "summary": ... }   // D6 数字，无阈值
  }
}
```

**判定规则（生成器内）**

1. **101 条硬底**任一非 `pass` → `NO-GO`（[29](../design/29-production-security-boundary.md) `SEC-1..10` 落地后的终态：条目 104 / 硬底 101，本票不动）。
2. **三个证据块任一缺失 → `NO-GO`**。不是 `pending`，不是忽略。
3. **任一证据块的 `candidate_sha` 与顶层不一致 → `NO-GO`**。
4. 规则 2 与规则 3 必须给出**两种不同的失败信息**（「证据缺失」vs「证据不属于本候选」）。
5. `pg_range` 任一版本任一条目非 `pass` → `NO-GO`。`rt_c` 只校验存在与 SHA 一致，不校验数值。

**理由**

1. [27](../design/27-v1-deliverables-and-candidate-provenance.md) D5 的核心论证是「**规则若跟着报告走，改报告就能改结论**」。把「三份都要在」这条规则挪进 `release-gate`，等于让规则分散在两个执行者里，同一个论证同样适用。
2. 规则 3 防的是一件必然会发生的事：真机验收多轮，**拿上一轮的 RT-C 数字配这一轮的候选**。不设这条，多轮验收会自动演化成「贵的证据只跑一次」。
3. 规则 4 不是措辞洁癖：两种失败的处置动作完全不同（去跑 vs 去核对 SHA），合成一句话的结局是每次都先去跑一遍。

---

## 8. D8 · `release-evidence`：一切证据类 workflow 以 SHA 为入参手工发起

**结论**：新增 **`release-evidence` workflow**。

| 项 | 结论 |
|---|---|
| 触发 | **`workflow_dispatch`，入参是 40 位候选 SHA**（必填） |
| 跑什么 | D3 的五版本采集与能力探测矩阵 |
| 产物 | `pg_range` 证据 JSON，带候选 SHA |
| 归档 | 人工提 PR 合入 `docs/validation/`，与验收报告同侧 |
| 不做什么 | **不由 tag 触发**；**不定时跑** |

**理由**

1. **不由 tag 触发**：`release-gate` 校验的前提是报告**已经存在**。tag 触发会造成「打 tag → 跑证据 → 补报告 → 重跑 gate」的环形依赖；而 rc tag 的语义 [27](../design/27-v1-deliverables-and-candidate-provenance.md) D6 已经定死为「**这是一个被正式评判过的候选**」，不能兼作「请开始评判」。
2. **不定时跑**：每晚跑 `main` 打的是移动靶，产出的证据不绑定任何候选——正好是 D7 规则 3 要防的那件事。
3. 一般化为一条纪律：**一切证据类 workflow 一律以 SHA 为入参手工发起**，与「候选身份 = SHA」（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D1）同构。

---

## 9. D9 · GitHub runner 上的绿不构成 Go/No-Go 证据

**结论**

1. **GitHub Actions 上执行的 `make acceptance` 结果是开发期信心，不构成 Go/No-Go 证据；其报告一律不入库 `docs/validation/`。**
2. 生成器硬规则：**执行环境为 GitHub Actions 时强制 `provisional: true`，`verdict` 只允许取 `NO-GO` 或 `PROVISIONAL-PASS`，永不出 `GO`。**
3. 用于 `verdict: GO` 的那份报告，必须来自 [#115](https://github.com/liumingjian/dbs-monitor/issues/115) 定义的真机 Linux。
4. **本票只给这一条判据：能进 `verdict` 的证据只能来自真机。**「哪些门必须真机复跑几轮、amd64 与 arm64 各要什么」归 [#115](https://github.com/liumingjian/dbs-monitor/issues/115)。

**理由**：地图 Notes 第 4 条定死「v1 的 Go/No-Go 证据必须在真实 Linux 上产出」，而 GitHub runner 字面上也是 Linux——这两条不冲突但显然不是一个意思，必须在本票写死判据，否则日后一定有人拿 CI 的绿当验收通过。具体差异是可点名的：runner 是容器化、**无 systemd**、cgroup 分母被限、磁盘与时钟都不是客户形态。于是 [26](../design/26-data-and-recovery-gate.md) 的重启恢复、[25](../design/25-master-key-provenance-and-startup-failure.md) 的 `0600` 权限与 root 预建目录、[23](23-v1-acceptance-entries-c.md) 的 `AC-05-S4` 观感门，在那上面跑绿说明不了任何事。

`PROVISIONAL-PASS` 不是新的成功状态，是**「除了执行环境这一项以外都过了」**的显式记账。它存在的唯一目的是让 CI 的绿在字面上就不可能被误读成 `GO`。

---

## 10. D10 · 不加 arm64 runner

**结论**：CI 只用 `ubuntu-latest`(amd64)。构建面维持 `CGO_ENABLED=0` 双架构交叉编译（`check-full` 现状），行为面的双架构差异归 [#115](https://github.com/liumingjian/dbs-monitor/issues/115) 的真机 arm64。

**理由**：D9 刚定了 runner 上的结果不算证据，那么加一个 arm64 runner 得到的是「**更多不算数的绿**」，成本翻倍、收益为零。arm64 的真实风险（若有）只在真机上有意义。

---

## 11. D11 · `main` 允许红，兜底不在 PR 门

**结论**

1. 维持 [27](../design/27-v1-deliverables-and-candidate-provenance.md) D3：`main` 的 required status check **只有 `check`**。`check-full`（含漏洞扫描）与 `acceptance` 全部是事后门，**`main` 可以红**。
2. 配套纪律：**`main` 红时，除修复外不合入任何 PR、不打任何 tag。**
3. 机器兜底：`release-gate` 拿不到该 SHA 成功的 `check-full` 就发不出 tag（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D6），这已经是结构性的。

**理由**：把 `check-full` 或 `acceptance` 提为 required check，会让每个 PR 等 40–60 分钟。单人仓库下的真实结局是**本地跑一遍就直接 push 到 `main` 绕开 PR**——与 [27](../design/27-v1-deliverables-and-candidate-provenance.md) 拒绝 required approval 是同一个论证：**把会被绕过的规则伪装成生效的规则，比没有规则更坏**。

---

## 12. D12 · 快层预算：一字不改

**结论**

1. [10](../design/10-ai-guardrails-and-verification.md) D1 的 **≤120 秒预算与其重新触发条件一字不改**。
2. 新增的 B11 / B12 / B14 三条守卫全部进 `make check`：两条静态扫描 + 一次库查询 + 一次 tag 文本扫描，量级在秒。
3. **`make acceptance` 绝不出现在 `make check` 里。**
4. 若 B 栏新增把快层推过 120 秒，仍走 [10](../design/10-ai-guardrails-and-verification.md) §15 既定处置——**先产出分包耗时数据、定位大户再挪，禁止为过预算整体搬迁**。

**理由**：借每张票放宽一点预算，是快层死亡的标准路径。两年后十分钟的快层没人跑，而它恰恰是唯一一道**开发者会主动跑**的门。

---

## 13. D13 · 登记表落位：B 栏加一列，不开新栏

**结论**

1. [10](../design/10-ai-guardrails-and-verification.md) §3.3 的 B 栏**新增一列「跑在哪层」**。
2. B 栏新增四条：**B11**（禁止对业务表直插，[20](20-v1-acceptance-matrix.md) D4）、**B12**（`test_ref` 覆盖漂移门，[20](20-v1-acceptance-matrix.md) D6.6）、**B13**（安全头 golden，[29](../design/29-production-security-boundary.md) D9）、**B14**（本票 D4）。**本票的平台库守卫编号为 B14 而非 B13**——`B13` 已被先落地的 [29](../design/29-production-security-boundary.md) 占用。
3. A 栏新增三条：**A10**（三档角色可见性 / 写能力判定表）、**A11**（三条内置规则受限语义判定表）——兑现 [24](24-v1-acceptance-entries-d.md) D10；**A12**（必须可归因的写操作登记表）——兑现 [29](../design/29-production-security-boundary.md) D5.2。
4. **不开 C 栏。** `acceptance`、五版本矩阵、RT-C、漏洞扫描、`release-gate` 只登记在本文 §14 的门总表里。

**理由**：A/B 两栏的准入判据（[10](../design/10-ai-guardrails-and-verification.md) §3.1 三问）要求「唯一实现点、可表驱动或可结构化扫描」，`acceptance` 与 RT-C 都不满足；硬塞会稀释 [10](../design/10-ai-guardrails-and-verification.md) 自己那句「**清单的价值来自它短**」。而新开一栏等于在 `10` 号里再造一份门总表，与本文构成第二份真相。加一列成本最低且诚实——B 栏本来就已经不全在一层（B11 的射程覆盖 `test/` 下只在 `check-full` 里执行的东西）。

---

## 14. D14 · 门总表：唯一载体，无处可勾选

**结论**

- **唯一真相 = 本节这张表 + 报告生成器里写死的判定规则。**
- **不产任何 Markdown 勾选表。** 执行结果全部由 `acceptance-report.json` 承载。

| # | 门 | 执行者 | 触发 | 硬门？ | 阻断什么 | 进 `verdict`？ |
|---|---|---|---|---|---|---|
| G1 | `make check` | Actions `check` | PR | 是 | **合并** | 否 |
| G2 | `make check-full`（不含 acceptance 的其余部分） | Actions `check-full` | push `main` / 手动 | 是 | 打 tag（经 `release-gate`） | 否 |
| G3 | `make acceptance`（104 条） | Actions `acceptance` job | push `main` / 手动 | 是 | 打 tag | **是**（仅真机那份） |
| G4 | 五版本采集与能力探测矩阵 | Actions `release-evidence` | 手动，SHA 入参 | 是 | 发布 | **是** |
| G5 | RT-C 容量与延迟 | 人工 `scripts/rt-c/run.sh` | 发版前 | 记录门 | 发布（仅缺数据时） | **是**（只校验存在与 SHA） |
| G6 | `govulncheck`（阻断）+ `npm audit`（只报告） | 含在 `check-full` 内（[29](../design/29-production-security-boundary.md) D7） | push `main` / 手动 | `govulncheck` 是 | 同 G2；`release-gate` **不重跑扫描、只读报告** | 否 |
| G7 | `release-gate` 校验 | Actions `release-gate` | tag push | 是 | 发布 | 否（它读 `verdict`） |
| G8 | 真机 Linux 复跑与人工观感 | 人工，归 [#115](https://github.com/liumingjian/dbs-monitor/issues/115) | 发版前 | 是 | 发布 | **是**（G3 的真机那一份 + 薄壳 Markdown） |

**理由**：[27](../design/27-v1-deliverables-and-candidate-provenance.md) 已定「不建交付前检查表（第二份真相），勾选表归 #114 且应由报告承载」——本票兑现的方式就是**不建**。把门总表放进 `matrix.yaml` 同样不行：[20](20-v1-acceptance-matrix.md) 定的矩阵射程是**产品语义**，[27](../design/27-v1-deliverables-and-candidate-provenance.md) 已用同一条理由把 `release-gate` 挡在矩阵外。**门总表是决策文档里的表，判定是生成器里的代码，执行结果是 JSON——三者各司其职、无一处可勾选。**

---

## 15. D15 · 外溢到实现的硬要求

1. `Makefile` 新增 `acceptance` 目标；`check-full` 调用它；**`check` 绝不调用它**。
2. `Makefile`/`compose.yaml` 补齐两个尚不存在的 profile：`postgres:12`（[24](24-v1-acceptance-entries-d.md) 要求）、非 17 平台库（[27-ext](../design/30-external-postgres-prerequisites.md) 要求）。
3. 落地 B11 / B12 / B14 三条守卫进 `make check`；B14 含 `datlocprovider <> 'i'` 断言。（B13 = [29](../design/29-production-security-boundary.md) 的安全头 golden，不是本票的。）
4. 新增两条 workflow：`acceptance`（push `main`，独立 job）、`release-evidence`（`workflow_dispatch` + SHA 入参）。漏洞扫描按 [29](../design/29-production-security-boundary.md) D7 进 `check-full`，不另建 workflow。
5. 报告生成器实现 D7 的 `evidence` 块与五条判定规则，其中**规则 2 与规则 3 必须输出两种不同的失败信息**。
6. 报告生成器实现 D9 的环境判定：`GITHUB_ACTIONS` 环境下强制 `provisional: true` 且封死 `GO`。
7. `release-evidence` 产出「每版本 × 每能力」三态表，格式与 `pg_range` 证据块对齐。
8. 在 [10](../design/10-ai-guardrails-and-verification.md) §3.2/§3.3 登记 A10 / A11 / A12 / B11 / B12 / B13 / B14，并给 B 栏加「跑在哪层」一列。**本票随文一并落地**，一并结清 [24](24-v1-acceptance-entries-d.md) D10 与 [29](../design/29-production-security-boundary.md) §11 原本挂在片⑧实现票上的登记项（登记的约束力由「兑现时机」列承载，提前登记不改变兑现时点）。
9. `main` 分支保护按 [27](../design/27-v1-deliverables-and-candidate-provenance.md) D3 配置；required check 保持只有 `check`，**不因本票新增 job 而扩充**。

**环境要求**：`acceptance` job 需要 docker compose 起平台库 + 目标库 + 真 Agent + SMTP sink + Webhook 回环 + `restore-target`，runner 为 `ubuntu-latest`。

---

## 16. 与既有文档的关系（记账）

| 文档 | 处置 |
|---|---|
| [10](../design/10-ai-guardrails-and-verification.md) D1 | **不 supersede**。加适用面澄清（D1）：「两层」约束的是开发者面对的命令数，不是 CI job 数。预算与重新触发条件一字不改（D12） |
| [10](../design/10-ai-guardrails-and-verification.md) §2.1 `datlocprovider` | 由 B14 落地（D4）。规定本身不改 |
| [10](../design/10-ai-guardrails-and-verification.md) §3.2 / §3.3 | **只增不改**：加两条 A 栏、三条 B 栏、一列「跑在哪层」 |
| [15](../design/15-ci-and-release-pipeline.md) D1 | 保留，是本票地基 |
| [15](../design/15-ci-and-release-pipeline.md) D2 触发表 | **不 supersede，只增不改**。现有四行没有一行是错的，全部仍成立；新增两行（`acceptance` 独立 job、`release-evidence` 手工触发）**写在本文 §14 而不是回填 `15` 号** |
| [15](../design/15-ci-and-release-pipeline.md) D3.1 / D3.2 | 仍成立，但**执行者已由人变机器**（[27](../design/27-v1-deliverables-and-candidate-provenance.md) D6 的 `release-gate`）。记账，不改写 |
| [15](../design/15-ci-and-release-pipeline.md) D3.3 / D4 / D5 | 早已由 [18](../design/18-v1-delivery-boundary-bs-binary.md) 作废，本票不复活 |
| [15](../design/15-ci-and-release-pipeline.md) D3.4 | 保留：最小权限、不用个人长期 token |
| [20](20-v1-acceptance-matrix.md)–[24](24-v1-acceptance-entries-d.md)、[26](../design/26-data-and-recovery-gate.md)、[29](../design/29-production-security-boundary.md) | **一字不动**。104 条条目 / 101 条硬底不变 |
| [27](../design/27-v1-deliverables-and-candidate-provenance.md) D5 | **扩写不推翻**：判定规则仍写死在生成器里，本票只加 `evidence` 块与三条新规则 |
| [27](../design/27-v1-deliverables-and-candidate-provenance.md) D6 | 保留。按 [29](../design/29-production-security-boundary.md) D7，`release-gate` **不重跑扫描、只读报告**（D5） |
| [27-ext](../design/30-external-postgres-prerequisites.md) D7 | **结清**：落地形态 = B14（D4） |
| [29](../design/29-production-security-boundary.md) D7 | **本票让步**：漏洞扫描的归属与阻断语义以其为准；本票只定「结果怎么进报告」（D5） |
| [29](../design/29-production-security-boundary.md) D9 `B13` | 保留。本票的平台库守卫顺延取 **B14** |
| [29](../design/29-production-security-boundary.md) `SEC-1..10` | **一字不动**。本票的硬底基数取其终态 101 条 |

**另记一笔**：`docs/design/` 中存在两份 27 号（`27-v1-deliverables-and-candidate-provenance.md` 与 `27-external-postgres-prerequisites.md`），[#110](https://github.com/liumingjian/dbs-monitor/issues/110) 与 [#116](https://github.com/liumingjian/dbs-monitor/issues/116) 撞号。[29](../design/29-production-security-boundary.md) 为避开撞号跳过了 `28-`，本文补上该空缺。撞号本身不在本票射程内，**不原地改名**（改名会打断所有既有互链），留待专门处置。

---

## 17. 否决记录

| 被否决的方案 | 理由 |
|---|---|
| 正式 supersede [10](../design/10-ai-guardrails-and-verification.md) D1 改写成三层 | 「两层」约束的是开发者的判断题数量，CI job 数与之无关。改写会让后来的会话以为可以随便加层 |
| 把 94 条塞进现有 `check-full` job | 30 分钟 timeout 直接爆；失败时分不清是编译挂了还是第 71 条挂了 |
| push `main` 只跑矩阵子集，全量留给 rc | 难跑的条目永远只在最没时间修的时刻第一次红 |
| CI 完全不跑 acceptance，只人工真机跑 | 94 条退化成人工检查表，「把规范做成结构」当场失守 |
| 94 条 × 5 个 PG 版本全跑 | 告警状态机、通知、权限与目标库版本无关，跑五遍是纯浪费 |
| v1 不跑 PG13–17，支持承诺降级为「尽力」 | 采集侧的版本差异**目前没有任何验收面**（[23](23-v1-acceptance-entries-c.md) 已记该缺口），不跑等于承诺无据 |
| 平台库 = 17 只写成纪律，靠运行期拒启动兜底 | 运行期拒启动保护客户，保护不了「会话在 PG16 上写 PG17 语法而 `make gen` 全绿」这条路径 |
| 漏洞扫描进 PR 门 | 「什么都没改却红了」是教会所有人绕过门的最快机制（[29](../design/29-production-security-boundary.md) D7 同侧，未进 PR 门） |
| 把漏洞扫描拆成独立 `vuln-scan` workflow 并排除出 `release-gate` | **本票原推荐，已撤回**：[29](../design/29-production-security-boundary.md) D7 先落地，且「`release-gate` 只读报告不重跑」已解掉同一个失效面，另建 workflow 只会造出第二套安排 |
| 扫描结果进 `evidence` 块参与 `verdict` | `govulncheck` 的阻断已完整体现在「该 SHA 有成功的 `check-full`」里；`npm audit` 本来就不是门 |
| v1 设性能阈值硬门 | 无基线数据，阈值必然被调到绿，比没有门更坏 |
| 三份证据各自独立报告，由 `release-gate` 分别校验 | 规则分散在两个执行者里，触犯「规则若跟着报告走，改报告就能改结论」 |
| 矩阵与 RT-C 结果只进薄壳 Markdown 由人判 | 把两个硬门降格成人可以看漏的段落 |
| 发布证据由 `-rc.N` tag 触发 | 造成「打 tag → 跑证据 → 补报告 → 重跑 gate」的环形依赖；且 rc 的语义已定死为「被评判过」 |
| 定时每晚跑 `main` 产出证据 | 打的是移动靶，证据不绑定候选 |
| CI 的 acceptance 报告也入库、算 Go/No-Go 证据 | runner 无 systemd、cgroup 分母被限、磁盘与时钟非客户形态，`REC-*` 与权限类条目在其上跑绿说明不了任何事 |
| 加免费的 `ubuntu-24.04-arm` runner | D9 之后，得到的是「更多不算数的绿」，成本翻倍收益为零 |
| 把 `check-full` 或 `acceptance` 提为 required check | PR 等 40–60 分钟，真实结局是直接 push `main` 绕开 PR |
| 借本票放宽快层预算 | 每票放宽一点是快层死亡的标准路径 |
| 在 [10](../design/10-ai-guardrails-and-verification.md) 新开 C 栏「发布线门禁」 | 在 `10` 号里再造一份门总表，与本文构成第二份真相 |
| 另出 `docs/deploy/go-no-go-checklist.md` 勾选表 | 正是 [27](../design/27-v1-deliverables-and-candidate-provenance.md) 点名拒绝的第二份真相 |
| 门总表进 `matrix.yaml` | 违反矩阵射程 = 产品语义，同 [27](../design/27-v1-deliverables-and-candidate-provenance.md) 挡 `release-gate` 的理由 |
| supersede [15](../design/15-ci-and-release-pipeline.md) D2 整条并重写触发表 | 现有四行没有一行是错的；supersede 没错的条款只会让后来者去比对两份表找差异 |

---

## 18. 移交

- **[#115](https://github.com/liumingjian/dbs-monitor/issues/115)（地图末票）**：本票给出的唯一判据是 **D9——能进 `verdict` 的证据只能来自真机**，以及门总表 G8 那一行。真机环境形态、必须复跑哪些门、跑几轮、amd64/arm64 分工、Go/No-Go 归档，全部归 #115。RT-C 的阈值（若日后要设）也从 #115 的第一批真机数据出发，**新开决策记录**。
- **实现票**：本文 D15 的九条外溢硬要求 + 环境要求，归 `/to-tickets` 与后续实现流程。**本票不开实现票、不产代码。**
