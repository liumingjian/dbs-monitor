# CI 与发布流水线 v1.0

> 目标：把 R2 已冻结的验证命令（[T9](10-ai-guardrails-and-verification.md) 两层闭环）与交付形态（[T8](09-packaging-and-deployment.md) 离线 tar、双架构）落成可执行的 CI / 发布决策：触发与职责矩阵、合并门定义、发布流程和产物留痕规则。
> 决策票：[T15 · CI 与发布流水线](https://github.com/liumingjian/dbs-monitor/issues/33)。
> 输入边界：[T8 · 打包、部署与运行形态](09-packaging-and-deployment.md)（离线 tar、amd64+arm64、glibc 下限、原生构建否决 qemu）、[T9 · AI 开发护栏与验证闭环](10-ai-guardrails-and-verification.md)（`make check` / `make check-full` 两层闭环、「CI 只定接口不定流水线」的接口即本票的输入、否决 pre-commit hook 改为 CI PR 门）。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。
> 当前适用性：PR `check`、宿主中立 `check-full`、精确提交校验、审批、最小权限和发布留痕继续有效；本文的 Linux 四组合发布矩阵已由 [20](20-v1-macos-build-validation-and-release.md) 与 [21](21-v1-linux-release-disposition.md) 从 macOS v1 发布图中移除。
> **本票只冻结决策。** workflow、构建 runner、打包脚本和发布配置的实现留给后续执行路线。
> 落盘说明：本票 2026-08-05 在 [#33 关票评论](https://github.com/liumingjian/dbs-monitor/issues/33) 中冻结结论，文件未随票落盘；本文档为该冻结结论的仓库落盘，内容以关票评论为准，未新增决策。

---

## 0. 一句话结论

**GitHub Actions 是唯一规范 CI 执行者，本地与交付团队复用同一组 `make` 命令；PR 合并门是 `make check`，默认分支合并后跑 `make check-full`；只有维护者创建的语义化版本 tag 触发发布，且 tag 指向的精确提交必须已有成功的 `check-full` 结果；四个「架构 × glibc 下限」组合全部产出可发布离线 tar，经 GitHub Environment 人工审批后归档为 Release assets；Actions 留痕 90 天，Release assets 长期保留。**

---

## 1. D1 · 谁来跑：GitHub Actions 是唯一规范执行者

- GitHub Actions 是**唯一规范 CI 执行者**。本地开发与交付团队环境**复用同一组 `make` 命令**（`make check` / `make check-full`），不另建第二套验证体系——验证语义只有一份，宿主不同而已（承 [T9](10-ai-guardrails-and-verification.md)「完成 = `make check` 全绿」，CI 只是把同一定义挂到 PR 上）。

## 2. D2 · 何时跑、怎么阻断

| 触发 | 跑什么 | 失败语义 |
|---|---|---|
| Pull Request | `make check` | **合并门：失败即阻断合并** |
| 合并到默认分支 | `make check-full`（自动） | **不回溯阻断已完成的合并，但阻止发布** |
| 手动重跑 | `make check-full` | 提供手动重跑入口，语义同上 |
| 语义化版本 tag | 发布流程（§3） | 前置校验不过即拒绝发布 |

## 3. D3 · 怎么发：tag + 精确提交校验 + 人工审批

1. **只有维护者创建的语义化版本 tag** 才能触发正式发布。
2. 发布前必须确认**该 tag 指向的精确提交已有成功的 `make check-full` 结果**；提交不匹配或结果缺失即拒绝发布——不存在「tag 一打就发」的通路。
3. 构建与验证完成后，经过 **GitHub Environment 人工审批**，才将产物发布为 GitHub Release assets。
4. **权限**：发布 workflow 使用最小权限，仅在人工审批通过后获得 Release 写权限，**不使用个人长期 token**。

## 4. D4 · Runner 分工与构建矩阵

- **GitHub 托管 runner** 执行快层检查（PR 门 `make check`）。
- **团队维护的原生 amd64 / arm64 runner** 执行构建与安装验证；**禁止用 QEMU 替代原生 arm64 构建**（承 [T8](09-packaging-and-deployment.md) §15 对 qemu 交叉构建的否决）。
- **构建矩阵四组合**：`amd64 × glibc 2.17/2.28` 与 `arm64 × glibc 2.17/2.28`，全部产出可发布离线 tar。
  > 与 [T8](09-packaging-and-deployment.md) 的关系：T8 §12 交付物清单列的是每架构一个 tar（amd64=2.17、arm64=2.28 构建）；本票把构建线扩为四组合，**以本票为准**。T8 D3「每架构各一条 glibc **下限**承诺」不变——扩的是构建产物覆盖，不是兼容性承诺。

## 5. D5 · 怎么留痕：命名、校验与保留

- **文件名**包含版本、操作系统、架构和 glibc 下限。
- 每次发布生成**统一 SHA-256 校验清单**。
- 归档**构建元数据**：tag、精确 commit SHA、构建矩阵、Go/Node/PG 工具链版本、构建时间——交付团队凭此复现并定位一次发布。
- **保留期**：Actions 日志、测试报告和中间产物保留 **90 天**；正式 Release assets（tar + 校验清单 + 构建元数据）**长期保留**。

## 6. 否决记录

| 被否决 | 为什么 |
|---|---|
| 本地 / 交付团队另建验证体系 | 验证语义分叉后必然漂移；`make` 命令是唯一语义源，CI 只是宿主（D1） |
| `check-full` 失败回溯阻断已合并 PR | 慢层失败的正确出口是阻止发布，不是回滚合并历史（D2） |
| tag 直接触发发布、无提交校验 | tag 可指向任意提交；不校验精确 commit 的 `check-full` 结果等于发布未验证产物（D3） |
| QEMU 替代原生 arm64 构建 | 承 [T8](09-packaging-and-deployment.md) §15：慢到不可接受且掩盖真机差异（D4） |
| 个人长期 token 做发布凭据 | 权限过宽且不可审计；改为审批后短时最小权限（D3） |

## 7. 交给下游

| 去向 | 内容 |
|---|---|
| 后续执行路线（CI 落地） | 两个 workflow（PR 门 `make check`；默认分支 `check-full` + 发布线）、团队 runner 接入、四组合构建脚本、Environment 审批与 Release 归档配置 |

> **收口增补 2026-08-05：P1 拆分。** 本轮先落地 PR 门与默认分支 / 手动入口的 `check-full` workflow；发布线的四组合原生 runner、Environment 审批和 Release 归档仍待团队 runner 就绪后由下游接手。
