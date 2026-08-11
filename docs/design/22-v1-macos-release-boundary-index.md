# v1 macOS 首发适配与发布边界索引 v1.0

> 本文把 Wayfinder 地图 [#98 · v1 macOS 首发适配与发布边界](https://github.com/liumingjian/dbs-monitor/issues/98) 的 `Decisions so far` 固化到仓库。后续实现以本文索引和对应决策文档为输入，不依赖 issue 正文仍保持可见。
> 状态：地图的决策路线已收口；macOS 打包、签名、公证、发布 workflow 与真实候选演练仍须由下游实现票交付。

## 1. 已冻结决策

| 票 | 仓库文档 | 决策 gist |
|---|---|---|
| [#99](https://github.com/liumingjian/dbs-monitor/issues/99) | [18 · v1 macOS 支持边界](18-v1-macos-support-boundary.md) | v1 唯一原生交付目标是 macOS 14.0 及以上版本的 Apple silicon（`darwin/arm64`）；不承诺 Intel、universal binary、Rosetta 2、Linux 或任何 amd64 发布。 |
| [#100](https://github.com/liumingjian/dbs-monitor/issues/100) | [19 · v1 macOS 运行与 PostgreSQL 交付形态](19-v1-macos-runtime-and-postgresql.md) | 随包交付项目维护的 PostgreSQL 17；server 与 PostgreSQL 由系统级 LaunchDaemon 管理，以专用用户通过 peer 认证的 Unix socket 通信，离线闭环安装、备份、升级、回滚和卸载。 |
| [#101](https://github.com/liumingjian/dbs-monitor/issues/101) | [20 · v1 macOS 构建、验证与发布路线](20-v1-macos-build-validation-and-release.md) | 固定 `macos-14` arm64 runner 产出 Developer ID 签名、公证并 stapled 的 `.pkg`，专用干净 Mac 验证同一候选；精确提交、`check-full`、原生构建、安装验收、账目和 Environment 审批依次把关，GitHub Release 是唯一正式渠道。 |
| [#102](https://github.com/liumingjian/dbs-monitor/issues/102) | [21 · 现有 Linux 发布票 #92 的 v1 处置](21-v1-linux-release-disposition.md) | #92 退出 v1；现有 Linux 脚本和证据只作为手动 `legacy` 参考，不进入 `check-full`、branch protection、macOS release workflow 或 v1 Release assets，post-v1 Linux 必须从新 PRD 重启。 |

## 2. v1 发布边界

v1 的唯一正式可安装产品资产是 `dbs-monitor-<version>-macos-arm64.pkg`，并随 Release 提供 `SHA256SUMS`、构建元数据和验证证据。只有同一份最终 stapled 字节流依次通过原生构建、签名公证、受支持 macOS 冒烟、专用干净机断网生命周期验收和人工审批，才可作为 GitHub Release 发布。

以下内容不属于地图完成的证明：交叉编译成功、历史 Linux 包、尚未运行的 workflow、未签名或未公证的 `.pkg`、在构建机上通过但未在干净机验证的资产。地图收口只冻结路线，不表示仓库已经具备 macOS 发布能力。

## 3. 交给实现流程

下游实现票必须按 [20](20-v1-macos-build-validation-and-release.md) §7 落地 macOS 构建/签名/打包脚本、host-neutral `check-full`、tag release workflow、metadata schema、验收脚本和专用 Mac 接入，并按 [19](19-v1-macos-runtime-and-postgresql.md) 实现 LaunchDaemon 与完整离线生命周期。在真实候选完成一次全门演练前，不得宣称 v1 macOS 可发布。

Linux 与 amd64 不再是 v1 的实现依赖或验收阻塞项。未来是否恢复、支持哪些元组以及采用什么构建矩阵，均由新的 post-v1 PRD 决定，不从 #92 的旧四组合范围自动继承。
