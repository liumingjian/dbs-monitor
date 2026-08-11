# 20 · v1 macOS 构建、验证与发布路线

> 出处：[v1 macOS 构建、验证与发布路线 #101](https://github.com/liumingjian/dbs-monitor/issues/101)（地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 子票）。
> 输入边界：[v1 macOS 支持边界](18-v1-macos-support-boundary.md) 已冻结唯一目标为 macOS 14.0 及以上版本的 Apple silicon；[v1 macOS 运行与 PostgreSQL 交付形态](19-v1-macos-runtime-and-postgresql.md) 已冻结随包 PostgreSQL 17、系统级 LaunchDaemon 和离线生命周期。
> 状态：2026-08-11 冻结。本文是对 [CI 与发布流水线](15-ci-and-release-pipeline.md) 中 Linux 四组合路线的 macOS 后续决策；v1 macOS 构建、验证与发布发生冲突时以本文为准。

---

## 0. 一句话结论

**GitHub 托管、固定 `macos-14` 大版本标签的 arm64 runner 负责唯一规范构建、签名、公证和自动验证，专用干净 Apple silicon Mac 只验证同一候选资产的断网安装、重启与完整生命周期；v1 把 server、Agent、PostgreSQL 17 和管理工具合成一个 Developer ID 签名、公证并 stapled 的 flat installer package（`.pkg`），以 GitHub Release 为唯一正式渠道；发布必须依次通过精确提交的 `check-full`、原生构建与 PostgreSQL 自测、最终包安装冒烟、校验和/元数据核对、Environment 人工审批，Linux 四组合从该发布图中拆出且不构成 v1 门。**

## 1. D1 · GitHub runner 产包，干净本机验最终包

| 执行环境 | 规范职责 | 明确不做 |
|---|---|---|
| GitHub 托管 `macos-14` arm64 runner | 在 v1 最低支持的 macOS 大版本上构建前端、`darwin/arm64` server/Agent 和 PostgreSQL 17；运行 PostgreSQL `make check`、原生测试、依赖审计、签名、公证和自动安装冒烟；产出唯一候选 `.pkg` | 不用 `macos-latest`，不把 Intel runner 或 Linux 交叉编译当 macOS 证据 |
| 对发布 workflow 隔离的专用 Apple silicon Mac | 下载并校验上述候选包，在恢复到干净基线后完成断网安装、首次启动、重启后自启、备份、同大版本升级/失败回滚、卸载和 purge；上传脱敏证据 | 不重新构建、不重签、不持有 Apple 签名凭据或 GitHub Release 写权限 |

规范产物只能来自 GitHub runner；本机不能以“最后修一下”产生第二份包。两边使用仓库内同一组脚本，本机验收按候选包 SHA-256 绑定证据，发布 job 只能提升这一个已验证字节流。

`macos-14` 固定规范构建的 macOS 大版本，但不代替最低部署目标设置：Go 构建与 PostgreSQL/C 依赖必须用各自工具链支持的方式将最低部署目标固定为 14.0，并检查所有 Mach-O 的架构和最低系统版本。发布时支持清单中每个高于 14 的 macOS 大版本，还要在对应的固定 arm64 runner label 上跑原生启动和核心端到端冒烟；label 不存在或证据不全时，该大版本不得进入支持清单。

专用 Mac 只能接收受保护 tag workflow 产生的候选资产，不运行 pull request 代码。其发布验收失败时阻止发布，但不允许在机器上修改候选包后继续。

## 2. D2 · 一个 flat `.pkg`，一个正式渠道

正式安装资产命名为 `dbs-monitor-<version>-macos-arm64.pkg`。它包含同一产品版本的以下内容，不把 PostgreSQL 或 Agent 另发成可混装资产：

- 已嵌入前端的 `dbs-monitor-server` 与原生 arm64 Agent；
- 项目从锁定源码构建的 PostgreSQL 17、客户端工具及全部非系统运行库；
- 平台 server 与 PostgreSQL 的两个 LaunchDaemon plist、版本化 payload、安装/升级/备份/恢复/诊断/卸载管理工具和离线文档。

`.pkg` 只把不可变 payload 和管理入口安全地落到版本目录；它不在无输入的 `postinstall` 中猜测证书 SAN、数据卷或直接切换正在运行的版本。首次安装由包内管理命令显式接收 `--public-address` 和可选 `--data-dir` 后完成 [运行契约](19-v1-macos-runtime-and-postgresql.md) §4；升级同样先落新 payload，再由受管命令执行备份、原子切换与回滚。这样 `.pkg` 的非交互约束不会破坏既有两项安装输入和升级前备份门。

GitHub Release 是 v1 唯一正式下载与长期归档渠道。客户可在联网机器下载后传入隔离网，目标 Mac 的安装和升级仍完全离线。v1 不上 Mac App Store，不提供 Homebrew formula/cask、DMG、裸二进制、独立 PostgreSQL 包或自动更新源；这些渠道都会形成第二套版本、权限或生命周期语义。

每个 Release 只发布以下四类资产：

| 资产 | 内容 |
|---|---|
| `dbs-monitor-<version>-macos-arm64.pkg` | 唯一可安装产品资产 |
| `SHA256SUMS` | 对最终 stapled `.pkg`、元数据和验证证据逐项计算 SHA-256；不自校验 |
| `dbs-monitor-<version>-macos-arm64.metadata.json` | 机器可读构建与来源元数据 |
| `dbs-monitor-<version>-macos-arm64-validation.tar.gz` | 脱敏的构建、签名、公证、自动冒烟和干净机验收记录 |

## 3. D3 · Developer ID 签名、公证和离线 Gatekeeper 证据是硬门

签名按嵌套顺序执行：逐个签所有 Mach-O 可执行文件和非系统动态库（Developer ID Application、hardened runtime、secure timestamp），再用 Developer ID Installer 签 flat `.pkg`。不得只给外层包签名，也不得用 ad-hoc 签名或 `codesign --deep` 掩盖漏签组件。

签名后用 `notarytool` 提交 Apple 公证并等待成功，把 ticket staple 到 `.pkg`。随后逐个对内层 Mach-O 执行 `codesign --verify --strict`，并对 `.pkg` 执行 `pkgutil --check-signature`、`spctl --assess --type install` 和 `stapler validate`。校验和只能在 stapling 和最终验证之后生成，因为 stapling 会改变包字节。干净机断网安装仍须通过 Gatekeeper 检查，证明 ticket 随包可用而非依赖在线查询。

Apple 签名证书和公证凭据只注入 tag workflow 的签名 job，不进入构建产物、日志或专用验收 Mac。GitHub Release 写权限只授予最终发布 job。

## 4. D4 · 从 tag 到 Release 的固定门序

1. **tag 门**：只有受保护 ruleset 允许的维护者可创建 `vMAJOR.MINOR.PATCH`；workflow 校验 tag 指向的精确 commit，不接受移动 tag、非 SemVer 或分支手工选择另一个 SHA。
2. **`check-full` 门**：精确 commit 必须已有默认分支 `check-full` workflow 的成功结论。该层继续在 Ubuntu 上承担平台无关的生成物、Go/Web、真实数据库与 E2E 回归，不冒充 macOS 支持证据。
3. **原生构建门**：固定 `macos-14` arm64 runner 从锁定且已校验 SHA-256 的 PostgreSQL 17 源码构建，运行其 `make check`；构建产品后检查全部 Mach-O 均为 arm64、最低系统为 14.0、非系统动态库全部随包且不存在 Homebrew/构建机绝对路径。
4. **候选资产门**：完成内层签名、`.pkg` 签名、公证与 stapling；此后冻结候选 SHA-256，任何后续步骤不得重打包。
5. **安装冒烟门**：每个受支持 macOS 大版本的托管 arm64 runner 对该候选包执行安装、LaunchDaemon、socket/peer、无 PG TCP listener、HTTPS health 和核心 E2E 冒烟；专用干净 Mac 对相同 SHA 执行 §1 的断网完整生命周期并产出证据。
6. **账目门**：先生成 metadata，记录 tag、精确 commit、runner image、macOS SDK/部署目标、Go/Node/Xcode/PostgreSQL 版本、PG 源码 SHA、签名 identity 指纹、公证 submission ID、构建时间以及 `.pkg`/验证证据 SHA；最后生成不包含自身的 `SHA256SUMS`，覆盖 `.pkg`、metadata 和验证证据并逐项复核。
7. **审批与发布门**：上述 job 全绿后进入受保护的 `production-release` GitHub Environment；禁止发起者自批。审批只提升已冻结资产，随后以最小 `contents: write` 权限创建 GitHub Release。拒绝、超时或发布失败均保留候选和证据供诊断，不得绕过门手工上传另一份包。

`check-full`、原生 macOS 构建和最终包验收是三个不同证据：任何一个通过都不能替代另外两个。Actions 中间日志和产物至少保留 90 天；正式 Release 的四类资产长期保留。

## 5. D5 · Linux 四组合拆出 v1 发布图

- 现有 Ubuntu `check` 与拆出 Linux 交叉编译后的平台无关 `check-full` 继续作为开发反馈与精确提交门；它们通过不代表 Linux 支持。
- 当前 `make check-full` 中显式的 `GOOS=linux` 双架构交叉编译要从 v1 必需路径拆出；Linux 打包目标和未来四组合原生构建只允许放在手动触发的 legacy workflow，且不加入 branch protection、`needs` 链、Environment 或 v1 Release 资产。
- v1 不启用 [#92](https://github.com/liumingjian/dbs-monitor/issues/92) 规划的 Linux 四组合发布 workflow，也不删除已有脚本、验证记录和历史产物。其重命名、禁用标记与后续重启入口已由 [#102 处置记录](21-v1-linux-release-disposition.md) 落地。
- legacy Linux job 失败可以形成后续版本欠账，但不得阻止 macOS v1 tag、审批或发布。

这选择的是“拆出并保留、但不作为发布门”，不是让半完成的 Linux 发布 job 与 macOS job 共用一条条件分支。后者会让旧矩阵的 runner 或资产失败继续隐式阻塞 v1。

## 6. 否决记录

| 被否决 | 为什么 |
|---|---|
| 本机直接构建并上传 Release | 环境和操作不可追溯，且会产生与 CI 验证对象不同的包 |
| 只用 GitHub 托管 runner | 不能覆盖断网、重启后自启和完整破坏性生命周期 |
| DMG 或 tar/zip 作为正式资产 | 产品是系统级 daemon 而非拖拽式 GUI app；flat `.pkg` 能表达管理员安装、Developer ID Installer 签名并 staple 公证票据 |
| App Store / Homebrew 分发 | 与系统级 LaunchDaemon、随包 PG 和完全离线版本闭环不匹配 |
| PostgreSQL 独立下载或独立发包 | 允许混装后，产品版本不再唯一决定数据库二进制、依赖和回滚对象 |
| 签名后重打包或验收后重建 | 校验和、签名、公证与安装证据不再指向发布的同一字节流 |
| `macos-latest` 作为规范构建标签 | 标签会漂移，无法固定规范构建的 macOS 大版本与构建证据 |
| 保留 Linux 四组合在 v1 `needs` 链中但口头声明“不阻塞” | workflow 依赖关系仍会让其失败阻止发布 |

## 7. 交给实现票

本票冻结路线，不声称当前仓库已有 macOS `.pkg`、签名、公证或干净机证据。后续实现必须落地 macOS 构建/签名/打包脚本、host-neutral `check-full` 拆分、tag release workflow、metadata schema、验收脚本和专用 Mac 接入；在这些产物走通一次真实候选前，不得宣称 v1 macOS 可发布。

## 8. 事实依据

- GitHub 的 [GitHub-hosted runners reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) 将公共仓库的 `macos-14` 列为 arm64 标准 runner，并明确 arm64 runner 不支持 nested virtualization；据此固定原生 runner，且不把容器当 macOS 构建前提。
- GitHub 的 [Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments) 说明 required reviewers、禁止 self-review 和审批前不释放 environment secrets 的语义；据此设置最终人工门与权限边界。
- Apple 的 [Notarizing macOS software before distribution](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) 明确 flat installer package 可公证、Developer ID/hardened runtime/secure timestamp 要求，以及 `notarytool`/`stapler` 自动化路径；据此确定签名、公证和离线 ticket 验证。
