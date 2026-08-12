# 21 · 现有 Linux 发布票 #92 的 v1 处置

> 出处：[现有 Linux 发布票 #92 的 v1 处置 #102](https://github.com/liumingjian/dbs-monitor/issues/102)（地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 子票）。
> 输入边界：[18](18-v1-macos-support-boundary.md) 已冻结 `darwin/arm64` 为唯一 v1 原生目标；[19](19-v1-macos-runtime-and-postgresql.md) 已冻结 macOS 运行契约；[20](20-v1-macos-build-validation-and-release.md) 已冻结 macOS 构建与发布图。
> 状态：2026-08-11 冻结；2026-08-12 补记其下游 [#95](https://github.com/liumingjian/dbs-monitor/issues/95)、[#96](https://github.com/liumingjian/dbs-monitor/issues/96)、[#97](https://github.com/liumingjian/dbs-monitor/issues/97) 与父 spec [#50](https://github.com/liumingjian/dbs-monitor/issues/50) 的同源处置。本文只处置 #50、#92、#95、#96、#97 及其 Linux 发布前提，不实现 macOS 发布路线，也不承诺未来 Linux 目标。

---

## 0. 一句话结论

**#92 不再作为 v1 实现票：由 issue 维护流程关闭为被 #98/#102 取代，不改名、不拆分，也不勾选其未完成验收项；现有 Linux 脚本、systemd 安装资产、验证证据和发布门的通用设计继续保留，但统一降为手动 `legacy` 参考，绝不进入 `check-full`、branch protection、macOS release workflow 或 v1 Release assets。macOS v1 收口后如仍需 Linux，必须新开 PRD，重新确认支持元组和构建矩阵，再从本文的资产清单开始。**

## 1. D1 · 保留的是工程资产，不是 v1 支持承诺

| 资产 | 处置 | 可复用边界 |
|---|---|---|
| `.github/workflows/check.yml` 与 `check-full.yml` | **继续作为 v1 基础** | PR 快层、默认分支慢层与精确提交证据；Ubuntu 宿主不等于 Linux 交付支持 |
| 精确 tag SHA、Environment 审批、最小权限、SHA-256、构建元数据与留痕规则 | **继续作为 v1 基础** | 由 [20](20-v1-macos-build-validation-and-release.md) 映射到 macOS 资产与门序 |
| `scripts/package-linux.sh`、`packaging/bundle/`、`packaging/systemd/` 与 Linux Make targets | **保留为 legacy 参考** | 可用于 post-v1 调研；不是 macOS `.pkg` 的实现基础或可发布资产 |
| `docs/validation/t11-linux-amd64-progress.md` | **保留历史证据** | 证明当时的 Linux walking skeleton，不证明当前 v1 支持，也不替代未来重验 |
| `scripts/rt-c/` 与 T11 的 453.6M 点 amd64 完整参数结果 | **保留历史证据** | 证明当时的 Linux amd64 存储选型基线；不是 macOS v1 发布门，也不替代未来 Linux 重验 |
| [09](09-packaging-and-deployment.md) 与 [15](15-ci-and-release-pipeline.md) 的 Linux 结论 | **保留历史决策** | 不原地改写；与 macOS v1 冲突时以 18–21 为准 |

现有脚本实际只强制 `amd64/glibc 2.17` 与 `arm64/glibc 2.28` 两个基线，并没有实现 #92 所写的四个「架构 × glibc」组合。不得把两个 legacy target 的存在描述为四组合已部分上线。

## 2. D2 · 延期必须在可执行入口上可见

- `make check-full` 只承担生成物、Go/Web、真实数据库、E2E 和宿主本机构建，不再显式执行 `GOOS=linux` 的双架构交叉编译。
- Linux 构建入口命名为 `legacy-package-*`，只允许人工调用；不保留无 `legacy` 前缀的兼容别名。
- 当前仓库没有 Linux release workflow，v1 不新增一个“默认禁用”或与 macOS 共用条件分支的 workflow。不存在的发布线比半启用发布线更不易误触发。
- `scripts/rt-c/run.sh` 继续是人工历史复现入口，不接入 `check-full` 或 v1 release workflow。历史基线只认默认完整参数；不得以缩减参数的排练结果替代或更新它。
- legacy Linux 构建失败只形成 post-v1 欠账，不改变 v1 tag、审批或 Release 的结果。

`check-full` 在 Ubuntu 上运行仍然有效：它验证平台无关的软件行为，而不是声明该 Ubuntu 宿主或其架构为受支持交付目标。

## 3. D3 · #92 关闭为被取代，不迁移未完成验收

#92 的问题陈述内部一致，但它绑定了 v1 已否决的 Linux 四组合目标。继续保留为开放实现票，或只改一个 post-v1 标题，都会让其父票、验收清单和发布门的归属保持含混。

因此票务处置固定为：

1. 在 #102 合入后，由 issue 维护流程关闭 #92，理由记录为被 #98 的 macOS v1 路线与 #102 的延期决策取代。
2. 不重命名 #92，使其原始范围和历史讨论保持可追溯；不拆分四组合，因为未来是否仍需要这四个组合尚未决定。
3. 不把 #92 的验收框勾为完成，也不把现有两个基线包、交叉编译或 T11 证据冒充其真实 tag 演练。
4. #92 关闭只表示退出 v1 路线，不表示 Linux 发布已经交付或被永久取消。

## 3.1 · #96 随其 Linux 前提一并退出 v1

#96 被 #92 阻塞，要求按 [09](09-packaging-and-deployment.md) D3.1 为原生 Linux arm64 补做端到端冒烟和 `pg_total_relation_size` 容量抽样，再与 Linux amd64 基线比较；它还要求把完整参数 RT-C 作为同一发布门留档。#92 退出 v1 后，这组证据已没有当前发布候选或发布门可以绑定，不能脱离其前提单独实现。

处置固定为：

1. #96 由 issue 维护流程记录为被 #98/#102 取代；不勾选其两项未执行的验收标准，不编造 arm64 容量数字或对比结论。
2. #96 的 Linux arm64 不是 macOS v1 的 `darwin/arm64`。macOS 原生冒烟和容量证据必须由 [20](20-v1-macos-build-validation-and-release.md) 的下游实现票绑定最终 `.pkg` 另行定义和执行，不能复用 #96 充数。
3. T11 已留档的 Linux amd64 453.6M 点完整 RT-C 结果仍是历史证据；缩减 `RT_C_DAYS`、`RT_C_SERIES`、`RT_C_QUERY_RUNS` 或 `RT_C_CONTROL_RUNS` 的运行只算排练，不得以缩减参数结果冒充基线。
4. 未来 Linux 新 PRD 若重新确认 arm64 支持，须重新规定候选、宿主矩阵、抽样口径和数量级升级规则，并对当时的软件提交与原生机器重做证据；不得直接把 #96 或历史 amd64 结果列为发布门已通过。

## 4. D4 · post-v1 从新 PRD 重启

未来 Linux 工作不得直接 reopen #92。新的 Linux 发布 PRD 必须先回答当时的客户与部署事实，再生成实施票：

1. 重新确定 OS、架构、libc/最低版本、离线与进程管理支持边界，不默认继承四组合。
2. 重新审计 PostgreSQL 版本、CVE、动态依赖、原生 runner 可用性和 T11 证据时效。
3. 决定资产格式、命名、验证矩阵、tag/审批门和与 macOS Release 的隔离方式。
4. 从 `scripts/package-linux.sh`、`packaging/`、[09](09-packaging-and-deployment.md)、[15](15-ci-and-release-pipeline.md) 和 Linux T11 记录评估复用；不因保留它们而跳过重验。
5. 先以手动且独立命名的 workflow 验证候选；只有新 PRD 的支持证据闭合后，才允许接入对应版本的正式发布图。
6. 重新决定是否需要 arm64 冒烟、容量抽样和完整参数 RT-C；如需要，为新候选留档原始结果，不继承 #96 的未执行验收。

## 5. 否决记录

| 被否决 | 为什么 |
|---|---|
| 删除 Linux 脚本、systemd 资产和验证记录 | 丢失可复用实现与历史证据，延期不要求销毁资产 |
| 保留无前缀 Make targets，靠文档说明“暂不发布” | 自动化入口看起来仍受支持，容易被脚本或操作员误用 |
| 新增禁用的 Linux release workflow | 形成半启用发布线，并暗示其矩阵与权限设计已经有效 |
| 让 legacy Linux job 非阻塞地挂在 macOS workflow | 仍把两个产品路线耦合在同一 workflow 图中 |
| 未来直接 reopen #92 | 四组合、runner 与兼容范围可能已过时，不能绕过重新决策 |
| 用 macOS `darwin/arm64` 验收完成 #96 | #96 的比较基线和被阻塞发布线都是 Linux；同名 CPU 架构不等于同一产品目标 |
| 把缩参 RT-C 或历史 amd64 结果写成当前发布门通过 | 没有绑定当前候选的完整参数运行，且会掩盖 #96 的 arm64 证据从未执行 |

## 6. 对依赖票 #95 的直接推论

[#95](https://github.com/liumingjian/dbs-monitor/issues/95) 写定的生命周期演练以 #92 的 Linux 离线 tar 为输入，并要求在 root、systemd 与自建 `--without-icu` PostgreSQL 环境中执行。因此 #92 退出 v1 后，这条演练不能继续作为 v1 实现票，也不能把 `packaging/bundle/` 或 `packaging/systemd/` 接入 macOS release workflow 来冒充完成。#95 的三项未执行验收不得勾选；保留现有 Linux 安装、升级与迁移锁资产仅代表 post-v1 可复用参考。

生命周期门本身没有取消。macOS v1 的下游实现票必须按 [19](19-v1-macos-runtime-and-postgresql.md) 与 [20](20-v1-macos-build-validation-and-release.md) 重新落位：对同一份签名、公证并 stapled 的 `.pkg`，在专用干净 Mac 上验证 LaunchDaemon、Unix socket、安装、同大版本升级、控制面与独立 keyring 恢复、回滚和 HTTPS health。并发首启的 advisory lock、缺 key 明确失败、无明文 `password` 列以及夹具/日志无可恢复实例密码仍是必须消费的既有行为，但不能用未执行的 Linux #95 清单充当 macOS 证据。

## 7. 对发布收口终票 #97 的直接推论

[#97](https://github.com/liumingjian/dbs-monitor/issues/97) 把平台无关的账目项与片⑩的 Linux 发布终判据绑在同一张票里：它要求 Unavailability 13 码全表验收、生成 appendix 和补齐交付文档，同时把 #95 的干净机生命周期演练与 #92 的发布流水线出包作为整体完成条件。#92、#95 与 #96 退出 v1 后，#97 已不能按原验收对象完成，应由 issue 维护流程记录为被 #98/#102 取代；四项验收均不得勾选，也不得用既有 Linux 资产、历史证据或尚未执行的 macOS 路线冒充完成。

退出原票不取消其中仍适用于 v1 的账目。macOS 发布路线的下游实现票必须重新落位并验收：

1. Unavailability 13 码仍须逐码驱动真实产生条件并经 API 断言；不得 mock 或直插库表伪造原因码。`VERSION_UNSUPPORTED` 仍按 PG12 接入即拒与码表存在性验收，码表只增不改。
2. `01-appendix-implemented.md` 仍须从 Go 声明生成并入库，漂移检查继续复用 `make gen` 与统一 diff 门，不另造生成入口。
3. macOS 安装与交付文档仍须明确被监控 PostgreSQL 前置为 PG13–17、升级窗口内时序数据丢失是有意代价，以及整机宕机仍需客户的外部探测。
4. 片⑩整体发布证据须改为 [20](20-v1-macos-build-validation-and-release.md) 定义的同一候选：专用干净 Mac 验证最终 `.pkg`，发布门全绿后归档 Release assets。未执行的 Linux 装升回滚演练和不存在的 Linux release workflow 都不能计入该证据。

以上四项必须绑定实际 macOS 候选重新执行并留痕；本文只记录归属迁移，不实现这些交付物，也不声明 v1 已可发布。未来 Linux 新 PRD 也不得继承 #97 的未执行验收为已完成。

## 8. 对父 spec #50 的最终处置

[#50](https://github.com/liumingjian/dbs-monitor/issues/50) 来自已收口的 MVP 切片地图 [#36](https://github.com/liumingjian/dbs-monitor/issues/36)，其十片规划和发布收口要求继续作为历史规格依据。但 #50 的最终判据把 v1 发布绑定到 Linux 离线 tar、四组合构建和原生 Linux 证据；这些产品边界已被 #98 的 macOS v1 路线与 #102 的延期决策推翻。因此 #50 由 issue 维护流程记录为被 #98/#102 取代，不改写原 issue，也不得勾选其中未执行的 Linux 验收。

这项处置不回退已经交付的平台无关基础：#93 的控制面备份与 advisory lock 行为继续保留，#94 接入的 PG13–17 矩阵与 sqlc vet 继续作为 host-neutral `check-full` 门。关闭 #50 只清除已经失效的 Linux 发布阻塞关系，不表示片⑩验收完成、Linux 已发布或 macOS v1 已可发布。

片⑩仍有效的生命周期、账目和发布要求转由 [20](20-v1-macos-build-validation-and-release.md) 的下游实现票承接。它们必须绑定同一份最终 `.pkg` 和真实候选重新实现、执行并留痕；#92、#95、#96、#97 的未执行验收、legacy Linux 资产与历史证据均不能替代该证据。未来恢复 Linux 时仍须按 §4 从新 PRD 重启。
