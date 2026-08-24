# dbs-monitor

PostgreSQL 私有化监控平台。Go 后端 + Agent，TS/React SPA（`go:embed` 进主二进制）。
本文件预算 150 行，只写违反后 `make check` 不会红的规则。

## Agent skills

### Issue tracker

GitHub Issues 是本仓库的 issue tracker，`ready-for-agent` 表示可交给无人值守实现。见 `docs/agents/issue-tracker.md`。

### Domain docs

本仓库是 single-context。开工前必读两份，且通常只读这两份：
`CONTEXT.md`（术语）与 `docs/design/LIVE.md`（当前真值索引，一行一条决策）。

`docs/design/` 全量约 25 万 token，是 smart zone 的一倍半——**不要 glob 决策正文**。
由 `LIVE.md` 指到哪读哪；确需通读时派 subagent 只取结论。
每份文档的 frontmatter `status` 自报死活，`superseded/` 下的一律作废、不得据以行事。
推翻任何一条须新开决策记录，不原地改写；约定见 `docs/design/README.md`。

## 完成的定义

`make check` 全绿才算完成。没跑绿不要报告完成。
`make dev-up` 起开发用 PG。`make check-full` 是 CI / 发版慢层。
改了 `api/*.yaml` 必须 `make gen`；生成物入库。

## 强制测试登记表

见 `docs/design/10-ai-guardrails-and-verification.md` §3.2。
实现该表任一语义时，必须有对应表驱动测试，且先红后绿。
表中没有的东西不必表驱动。

## 依赖方向

`internal/` 四层偏序，只许上层依赖下层：L3 `cmd` → L2 编排/循环 → L1 领域 → L0 基础设施。
同层禁止互相依赖，唯一例外 `collect → capability`。
新增包默认拒绝，须先在 `internal/arch_test.go` 登记。
禁止 `common` / `util` / `utils` / `shared`。
共享面只有生成物 `internal/api`。
理由与目录树：`docs/design/05-backend-code-structure.md`。

## 后端禁令

不造 mock。接缝白名单见 `internal/arch_test.go`；其余直接 import 具体类型。
领域包不开事务，只接 DBTX。事务由 L2 编排层持有。
`clock` 止步 L2。状态机是无时间纯函数。
空状态是值不是 error。Go error 只表操作失败。
新增枚举码只许追加，禁止修改或复用既有码值。
不补 0；桶内无样本就不出现在结果里。
最新值查询必须带时间下界。
采集 SQL 走 pgx 直连，不走 sqlc。
不新增指标、不改口径、不改采集 SQL、不加采集任务；先改决策文档。
不做用户自定义 SQL 探针。
不写版本分支或动态拼接采集 SQL。
`migrations/` 只写 up，不写 down。

## 先例

后端采集→存储：`internal/collect/service.go`。
DBTX 事务编排：`internal/evaluator/service.go`。
表驱动状态机测试：`internal/alerting/state_test.go`。
