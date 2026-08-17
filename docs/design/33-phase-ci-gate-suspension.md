# 33 · 阶段性停用合入 main 的 CI 门禁

- 状态:生效中(2026-08-17 起)
- 决策人:项目所有者(会话内口头拍板)
- 取代面:见 §3;恢复条件见 §4

## 1. 决策

在**第一个可验收版本完成之前**,合入 main 的 CI 门禁整体停用:

1. `check` workflow(PR 门)停用——`gh workflow disable check.yml`。
2. `check-full` workflow(main push 门)停用——`gh workflow disable check-full.yml`。
3. `acceptance` workflow(main push)停用——`gh workflow disable acceptance.yml`。
4. main 分支保护的 required status check(`check`)摘除——
   `DELETE /repos/liumingjian/dbs-monitor/branches/main/protection/required_status_checks`。

以上均为 GitHub 侧状态操作,**不改 workflow YAML**;`release-evidence` / `release-gate`
(dispatch / tag 触发)保持原样不动。

## 2. 理由与验证责任转移

- 当前阶段以「第一个可验收版本」为唯一目标,CI 门在修复通道上反复制造摩擦
  (BEHIND 重跑、每票两轮等待),收益低于成本。
- 停用期间,「完成的定义」不变:`make check` 全绿才算完成;E2E / 矩阵等慢层验证
  照跑,只是执行地点从 CI 转移到开发侧真实环境(rexec → mac)红绿闭环,
  证据回填对应票/PR。
- 本决策**不改变** 28 号 D9:开发侧本地绿依旧不构成 Go/No-Go 证据。发版前必须
  先恢复 CI 门(§4)并在 CI 上重新取证。

## 3. 取代了什么

- **[15](15-ci-and-release-pipeline.md) T15**「PR 门为 `make check`、默认分支为
  `make check-full`」——停用期内两条均不执行(workflow 禁用)。
- **[10](10-ai-guardrails-and-verification.md) T9** 的「CI PR 门」执行者——停用期内
  由开发侧 `make check`(rexec → mac)承担,规范本身(两层闭环、快层预算)不变。
- **[28](28-v1-go-no-go-gates.md) D11** 的「main 红态只有修复类 PR 允许合入」——
  该条以 CI 门在岗为前提;停用期内合入范围由所有者按阶段目标裁量,
  单票单 PR 的过程纪律保留。

## 4. 恢复条件与恢复动作

**条件**:第一个可验收版本实现完成(所有者判定)。届时:

```sh
gh workflow enable check.yml
gh workflow enable check-full.yml
gh workflow enable acceptance.yml
# 恢复 required status check
gh api -X PATCH repos/liumingjian/dbs-monitor/branches/main/protection/required_status_checks \
  -f strict=true -f 'contexts[]=check'
```

恢复后,停用期内合入 main 的全部提交须在 CI 上完整跑通 `check-full` 取证,
再进入 28 号门的正常序。
