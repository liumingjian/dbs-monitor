# 31 · 真 Linux 环境适配与最终验收

> 出处：[真 Linux 环境适配与最终验收 #115](https://github.com/liumingjian/dbs-monitor/issues/115)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> **载体补登**：#115 是地图上唯一一张 resolution 只留在票面、未落 `docs/design/` 的决策票。本文把该 resolution 固化进仓库，**不新增、不改写任何结论**；与票面原文的差异仅两处——编号引用改为文档链接、加入本注记。以本文与票面不一致处为疑时，以票面 resolution 为准并修本文。
> 定位：v1 真 Linux 最终验收规范。本文 supersede 以下既有双架构口径，不原地改写旧记录：[18](18-v1-delivery-boundary-bs-binary.md) D1/D6 的双架构交付目标；[19](19-agent-distribution-and-upgrade.md) D1/D7 的双架构下载允许值与双架构 plan B 备料；[27](27-v1-deliverables-and-candidate-provenance.md) 的四个二进制、双架构 `SHA256SUMS` 与 `agent_binary_dir` 必须同时含两架构的要求；`check-full` / 发布门中 arm64 交叉编译属于 v1 硬门的口径。[28](28-v1-go-no-go-gates.md) D10「不加 arm64 runner」结论仍成立，理由收紧为「arm64 已不在 v1 交付目标」。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

本票经 HITL grilling 收口；以下结论共同构成 v1 真 Linux 最终验收规范。地图只负责决策收口，**本文不表示当前仓库或某个真实候选已经通过验收**。

## D1 · v1 平台边界收窄为 Linux AMD64

- v1 构建目标从 `linux/amd64 + linux/arm64` 收窄为 **`linux/amd64`**。
- 正式兼容性证据只覆盖本票实查的参考环境：**Kylin Linux Advanced Server V10 (Sword)、Linux 4.19.90、x86_64、KVM**。
- KVM 真 Linux 已足够；不要求物理机，也不绑定云厂商实例。
- `linux/arm64` 的交付物、下载完整性要求、交叉编译门、runner 与真机验收全部移出 v1，归 post-v1 新路线。现存 ARM 代码或构建目标可以保留，但不构成支持承诺。
- 其他 Linux 发行版、cgroup v2、SELinux/AppArmor enforcing 形态均未验证，不进入 v1 承诺。

本条 supersede 的既有双架构口径见文首定位段；均不原地改写旧记录。

## D2 · 唯一正式验收主机

正式证据只能在上述 Kylin V10 AMD64 KVM 上产生。当前实查画像为：glibc 2.28、systemd 243、cgroup v1、Docker Linux/AMD64、NTP 已同步、SELinux disabled、AppArmor absent。

主机可以继续承担日常工作，但正式轮次必须取得**独占验收窗口**：暂停所有与 dbs-monitor 无关的容器与高负载服务。当前主机仍运行 `ati-site`、MySQL、TDSQL 等容器，可用内存约 450 MiB 且 swap 已满，**当前现场不满足正式轮次条件**；这是执行前置事实，不是候选的 NO-GO。

## D3 · 每个候选真机必跑范围

每个被正式评判的候选 SHA 都必须在真机重跑完整证据，CI 的绿或其他 SHA 的结果不得替代：

1. 从干净 checkout 构建 `linux/amd64` server/agent，校验 `--version`、40 位候选 SHA、产物 SHA-256 与 `SHA256SUMS`；
2. `make check-full`，包含 `make check`、真实构建、104 条验收矩阵以及既有漏洞检查归属；
3. 被监控 PG13/14/15/16/17 的采集与能力探测子集，产出每版本 × 每能力三态表；
4. RT-C 容量/延迟记录门：缺数据即 NO-GO，但 v1 不设性能通过阈值；
5. 宿主机原生 systemd 与 Linux 专属断言；
6. `AC-05-S4` AntD 页面观感与卡顿人工检查；
7. 汇总同一 SHA 的全部证据，由生成器给出唯一 `verdict`。

## D4 · 正式版本需要连续两轮 GO

- 每个 rc 候选至少执行一轮完整证据集；GO/NO-GO 都归档。
- 正式 `v1.0.0` 的同一候选 SHA 必须取得**连续两轮完整 GO**。
- 两轮之间重置平台库、目标库、恢复靶、配置、密钥目录、systemd 状态与测试数据，但不更换主机。
- 两轮都必须重跑 PG13–17 子集与 RT-C；昂贵证据不得只跑一次。
- 不允许只重跑失败项；任一有效轮次失败后连续计数归零。

## D5 · 同一轮采用 Docker 证据段 + 原生 systemd 段

- 104 条矩阵、PG13–17 子集和 RT-C 使用 Docker 依赖环境，以保持故障注入与状态重置可复现。
- server 与 agent 的 Linux 专属验收使用同一候选二进制，在宿主机通过真实 systemd unit、两个独立专用非 root 用户运行。
- systemd 段断首次启动、停止、自动重启、主机重启后的恢复、journal 结构化事件、配置/密钥/token 权限与进程无 root 权限。
- 不使用 systemd-in-container，也不把整套矩阵改造成 systemd 托管。

## D6 · 有效轮次、INVALID 与 NO-GO 的边界

每轮启动候选前先产出环境快照：OS/arch/虚拟化、CPU、内存、swap、磁盘、NTP、Docker、systemd、cgroup、端口、候选 SHA、工作树与测试目录状态。

- 预检不满足时，该轮为 **`INVALID`**，不产生 GO/NO-GO；清理环境后必须从头重跑。
- 一旦候选进程启动，任一门失败均为 **`NO-GO`**，不得用「环境抖动」豁免或局部重试。
- 主机上与 dbs-monitor 无关的 failed/degraded unit 只留痕；只有 dbs-monitor unit、依赖服务或证据环境异常影响 verdict。

## D7 · Linux 特有风险的验收口径

- **glibc**：server/agent 必须以 `CGO_ENABLED=0` 构建，并以 ELF/`ldd` 检查证明无动态 libc 依赖；不承诺 glibc 最低版本。
- **systemd/权限**：真实 unit、server/agent 两个独立非 root 用户、root 预建目录；配置、主密钥与 token 的 owner/mode 逐项断言。
- **时钟**：每轮预检必须 NTP 已同步；Agent 与 server 偏差超过 ±5 秒必须拒绝启动。
- **cgroup**：只验当前 cgroup v1；server/agent unit 不设 CPU/内存限额，资源分母按整台 KVM。cgroup v2、受限容器及容器化被监控 PG 不进入 v1。
- **安全模块**：报告必须明确记录 SELinux disabled、AppArmor absent；不设对应通过项，不宣称兼容 enforcing 模式。

## D8 · 证据结构与归档

每个候选只提交一份 `docs/validation/v1-go-no-go-<shortSHA>.json`，其中包含两个连续轮次。每轮至少记录环境指纹、起止时间和六类证据：

- `package`
- `check_full`
- `matrix`
- `pg_range`
- `rt_c`
- `linux_native`

`AC-05-S4` 由操作者提交结构化人工结果（操作者、时间、结论、截图索引、遗留风险），生成器读取它参与 verdict。薄壳 Markdown 只展示结果，不得覆盖 JSON。

原始 stdout、数据库转储与秘密不入库；JSON 保存结构化结果、摘要与产物 SHA-256，归档前必须通过秘密扫描。

## D9 · GO / NO-GO 判定

`GO` 必须同时满足：

- 两轮均有效且连续，候选 SHA 完全一致；
- 两轮的全部硬门通过；
- RT-C 数据存在；
- Linux 原生段通过；
- `AC-05-S4` 人工观感通过；
- 没有意外 `pending`、`n-a` 或 exception。

矩阵中已冻结的 5 条 `n-a` 与 2 条非基线 `pending` 允许保留，`exceptions` 必须仍为 `[]`；除此之外新增任一项即 NO-GO。

候选 SHA 不一致、证据缺失、门执行失败必须使用不同原因码，不能统一退化为「验收失败」。JSON 的机器 verdict 是唯一结论；Markdown、tag 或人工说明均不得把 NO-GO 覆盖为 GO。

## D10 · 后续执行边界

本票完成的是最终验收规范，不执行尚未实现的 104 条矩阵，也不为当前仓库声称投产通过。实现阶段须据此补齐验收生成器、环境预检、原生 systemd harness、双轮报告结构及 AMD64-only 交付口径。首批真实 RT-C 数据产生后，容量/延迟阈值如需成为硬门，另开新的决策记录；本票不预设阈值。

## 否决记录

- 保留 arm64 为 v1 交付目标、但不产 arm64 真机证据：会把未验证架构包装成已支持。
- 用 GitHub runner 或其他 Linux 发行版的绿替代参考 KVM：无法证明 systemd、权限、cgroup、时钟与客户形态。
- 正式版本只跑一轮：只证明偶然跑通，不能证明干净重建后可复现。
- 两轮只重跑便宜门、复用 PG 矩阵或 RT-C：违反候选 SHA 证据绑定，也让最贵的证据最容易陈旧。
- systemd-in-container：为一组原生断言引入更大的失真。
- 把预检失败算候选 NO-GO，或把候选失败算「环境抖动」：两者都会破坏 verdict 的可解释性。
- 在未启用 SELinux/AppArmor 的主机上宣称兼容 enforcing：没有证据。
