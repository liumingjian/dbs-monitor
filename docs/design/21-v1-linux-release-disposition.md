# 21 · 现有 Linux 发布票 #92 的 v1 处置

> 出处：[现有 Linux 发布票 #92 的 v1 处置 #102](https://github.com/liumingjian/dbs-monitor/issues/102)（地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 子票）。
> 输入边界：[18](18-v1-macos-support-boundary.md) 已冻结 `darwin/arm64` 为唯一 v1 原生目标；[19](19-v1-macos-runtime-and-postgresql.md) 已冻结 macOS 运行契约；[20](20-v1-macos-build-validation-and-release.md) 已冻结 macOS 构建与发布图。
> 状态：2026-08-11 冻结。本文只处置 [#92](https://github.com/liumingjian/dbs-monitor/issues/92) 及其 Linux 资产，不实现 macOS 发布路线，也不承诺未来 Linux 目标。

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
| [09](09-packaging-and-deployment.md) 与 [15](15-ci-and-release-pipeline.md) 的 Linux 结论 | **保留历史决策** | 不原地改写；与 macOS v1 冲突时以 18–21 为准 |

现有脚本实际只强制 `amd64/glibc 2.17` 与 `arm64/glibc 2.28` 两个基线，并没有实现 #92 所写的四个「架构 × glibc」组合。不得把两个 legacy target 的存在描述为四组合已部分上线。

## 2. D2 · 延期必须在可执行入口上可见

- `make check-full` 只承担生成物、Go/Web、真实数据库、E2E 和宿主本机构建，不再显式执行 `GOOS=linux` 的双架构交叉编译。
- Linux 构建入口命名为 `legacy-package-*`，只允许人工调用；不保留无 `legacy` 前缀的兼容别名。
- 当前仓库没有 Linux release workflow，v1 不新增一个“默认禁用”或与 macOS 共用条件分支的 workflow。不存在的发布线比半启用发布线更不易误触发。
- legacy Linux 构建失败只形成 post-v1 欠账，不改变 v1 tag、审批或 Release 的结果。

`check-full` 在 Ubuntu 上运行仍然有效：它验证平台无关的软件行为，而不是声明该 Ubuntu 宿主或其架构为受支持交付目标。

## 3. D3 · #92 关闭为被取代，不迁移未完成验收

#92 的问题陈述内部一致，但它绑定了 v1 已否决的 Linux 四组合目标。继续保留为开放实现票，或只改一个 post-v1 标题，都会让其父票、验收清单和发布门的归属保持含混。

因此票务处置固定为：

1. 在 #102 合入后，由 issue 维护流程关闭 #92，理由记录为被 #98 的 macOS v1 路线与 #102 的延期决策取代。
2. 不重命名 #92，使其原始范围和历史讨论保持可追溯；不拆分四组合，因为未来是否仍需要这四个组合尚未决定。
3. 不把 #92 的验收框勾为完成，也不把现有两个基线包、交叉编译或 T11 证据冒充其真实 tag 演练。
4. #92 关闭只表示退出 v1 路线，不表示 Linux 发布已经交付或被永久取消。

## 4. D4 · post-v1 从新 PRD 重启

未来 Linux 工作不得直接 reopen #92。新的 Linux 发布 PRD 必须先回答当时的客户与部署事实，再生成实施票：

1. 重新确定 OS、架构、libc/最低版本、离线与进程管理支持边界，不默认继承四组合。
2. 重新审计 PostgreSQL 版本、CVE、动态依赖、原生 runner 可用性和 T11 证据时效。
3. 决定资产格式、命名、验证矩阵、tag/审批门和与 macOS Release 的隔离方式。
4. 从 `scripts/package-linux.sh`、`packaging/`、[09](09-packaging-and-deployment.md)、[15](15-ci-and-release-pipeline.md) 和 Linux T11 记录评估复用；不因保留它们而跳过重验。
5. 先以手动且独立命名的 workflow 验证候选；只有新 PRD 的支持证据闭合后，才允许接入对应版本的正式发布图。

## 5. 否决记录

| 被否决 | 为什么 |
|---|---|
| 删除 Linux 脚本、systemd 资产和验证记录 | 丢失可复用实现与历史证据，延期不要求销毁资产 |
| 保留无前缀 Make targets，靠文档说明“暂不发布” | 自动化入口看起来仍受支持，容易被脚本或操作员误用 |
| 新增禁用的 Linux release workflow | 形成半启用发布线，并暗示其矩阵与权限设计已经有效 |
| 让 legacy Linux job 非阻塞地挂在 macOS workflow | 仍把两个产品路线耦合在同一 workflow 图中 |
| 未来直接 reopen #92 | 四组合、runner 与兼容范围可能已过时，不能绕过重新决策 |
