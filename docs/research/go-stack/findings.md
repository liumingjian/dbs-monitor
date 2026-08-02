# RT-D · Go 服务端与 Agent 基础库选型基线（调研产出）

> **本文件为 RT-D 调研产出，服务于 [issue #17](https://github.com/liumingjian/dbs-monitor/issues/17)，不构成决策。** 决策在 T5 / T6。
>
> 事实核对日期：**2026-08-02**。版本与维护状态均取自 GitHub API（releases / pushed_at）与官方文档原文。本文不含任何 benchmark 数字——没有一手实测，就不写。

## 0. 评估权重（承自 [#15](https://github.com/liumingjian/dbs-monitor/issues/15) 地图约束）

1. **AI agent 开发友好性是一等权重**：依赖树深度、错误能否在编译期暴露、生成物可读性、文档与惯例稳定性。
2. 契约优先，OpenAPI 为单一事实源，生成 Go 服务端接口。
3. 整包自带、无客户环境依赖；**Agent 必须单二进制、无运行时依赖**。

---

## 1. HTTP 与路由

| 维度 | `net/http` (Go 1.22+) | chi v5 | echo v5 | gin |
| --- | --- | --- | --- | --- |
| 最新版 / 活跃度 | 随 Go 发行版 | [v5.3.1 (2026-07-06)](https://github.com/go-chi/chi/releases) | [v5.3.1 (2026-07-21)](https://github.com/labstack/echo/releases) | [v1.12.0 (2026-02-28)](https://github.com/gin-gonic/gin/releases) |
| 许可证 | BSD-3（Go） | MIT | MIT | MIT |
| 直接依赖 | 0 | **0**（[go.mod 无 require 段](https://github.com/go-chi/chi/blob/master/go.mod)） | `golang.org/x/net`、`golang.org/x/time` | 较深（validator / 多种编解码 / protobuf 等） |
| 路径参数 | 有：`GET /items/{id}` + `Request.PathValue`；`{path...}`、`{$}` | 有 | 有 | 有 |
| Handler 签名 | 标准 `http.HandlerFunc` | 标准 `http.HandlerFunc` | 自有 `echo.Context` | 自有 `gin.Context` |
| 中间件形态 | 标准 `func(http.Handler) http.Handler` | 同左，且自带 `middleware` 包 | 自有类型 | 自有类型 |

**一手依据**：Go 1.22 release notes 原文——"The patterns used by `net/http.ServeMux` have been enhanced to accept methods and wildcards… Registering a handler with a method, like `\"POST /items/create\"`, restricts invocations of the handler to requests with the given method… Wildcards in patterns, like `/items/{id}`, match segments of the URL path. The actual segment value may be accessed by calling the `Request.PathValue` method."（<https://go.dev/doc/go1.22>）另有：更具体的 pattern 优先，注册顺序不影响匹配结果；破坏性变更由 `GODEBUG=httpmuxgo121=1` 兜底。

**在契约优先前提下框架还剩多少价值**：路由表由 OpenAPI 生成物写死，人/AI 不再手写路由字符串，因此「路由语法糖」这块价值几乎归零。框架剩下的价值只有三样：(a) 中间件成品（RequestID / Recoverer / Timeout / CORS / 压缩）；(b) 路由分组与嵌套挂载；(c) 参数绑定 —— 而 (c) 在 strict server 下也由生成代码接管。chi 的独特点是它**不引入新的 handler 类型**：`chi.Mux` 实现 `http.Handler`，handler 仍是 `http.HandlerFunc`，(a)(b) 是纯增量，标准库知识不作废；echo/gin 则要求整套代码写进各自的 `Context` 抽象。

**推荐基线**：`net/http` 为底，**chi v5 作为可选薄层**（零依赖、handler 类型不变、随时可退回标准库）。否决 gin/echo：引入自有 Context 类型，AI 生成代码时容易把两套惯例混写，依赖树更深，对「整包自带」无收益。

**被推翻条件**：
- 若最终选定的 OpenAPI 生成器只对某框架有一等支持而对 `net/http`/chi 是二等（当前不成立，见 §2）。
- 若需要大量成品中间件（限流、复杂 CORS、指标）且不愿自写——chi 从「可选」变「必选」，但仍不至于换 echo/gin。

---

## 2. OpenAPI 代码生成

| 维度 | oapi-codegen v2 | ogen |
| --- | --- | --- |
| 最新版 | [v2.8.0 (2026-07-17)](https://github.com/oapi-codegen/oapi-codegen/releases) | [v1.22.0 (2026-06-16)](https://github.com/ogen-go/ogen/releases) |
| 许可证 | Apache-2.0 | Apache-2.0 |
| 活跃度 | 高（2026-08-01 有提交） | 高（2026-08-01 有提交） |
| 生成器自身依赖树 | 中（仅构建期） | **深**：`fasthttp`、`zap`、`otel`（5 个模块）、`go-faster/jx`、`goldmark`、`regexp2`、`shopspring/decimal` 等（[go.mod](https://github.com/ogen-go/ogen/blob/main/go.mod)） |
| 路由绑定目标 | 多选：`std-http-server`、`chi-server`、`echo-server`、`echo5-server`、`gin-server`、fiber、iris 等（[README 支持表](https://github.com/oapi-codegen/oapi-codegen#supported-servers)） | 自带**代码生成的静态 radix router**，不可替换 |
| 生成风格 | 直白 verbose；README 自述 "fairly simple generated code, erring on the side of verbose code over complex modular code" | 高度优化：手写 JSON 编解码、生成校验、`Optional[T]`/`Nullable[T]` 包装、oneOf sum types |
| 生成物运行时依赖 | `oapi-codegen/runtime` + 所选路由 | ogen runtime（含 otel） |

**strict server 是本题关键**。oapi-codegen README 原文：strict server "takes inspiration from server-side code generation for RPC servers… This is the highest level of strictness that `oapi-codegen` supports right now, and it's a good idea to start with this if you want the most guardrails to simplify developing your APIs."

生成物形态（README 摘录）：

```go
// StrictServerInterface represents all server handlers.
type StrictServerInterface interface { ... }

func NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface

// 实现侧的编译期断言
var _ StrictServerInterface = (*PetStore)(nil)
```

对 AI 友好性的直接含义：接口方法签名是**每个操作一个具名请求类型 + 一个具名响应类型**，`var _ StrictServerInterface = (*Impl)(nil)` 这一行让「少实现一个 endpoint / 签名漂移 / 返回 spec 里没定义的状态码」全部变成 `go build` 红字，而不是运行时 404 或 JSON 形状不符。这正是地图约束 6「把接口漂移变成编译器必然拦下的错误」的落点。

配置形态（README）：strict server **必须搭配一个具体 server 生成**：

```yaml
generate:
  chi-server: true
  strict-server: true
```

**生成物是否入版本库**：README 设有 FAQ 条目 "Should I commit the generated code?" 与 "Should I lint the generated code?"，并明确其 SemVer 承诺主要是**对生成代码的兼容性承诺**："SemVer mostly applies to generated code. We strive to avoid breaking your code which depends on the generated code."；同时警告 `pkg/` 的导入面除 `Generate` 与 `Configuration` 外视为不稳定。工具本身推荐用 Go 1.24+ 的 `go tool` 机制固定版本（`//go:generate go tool oapi-codegen -config cfg.yaml ../../api.yaml`），对可复现生成很关键。

> 注意（README 明写的坑）：strict-server 下若用 `$ref` 跨 spec 引用 `components/responses/...`，目标 spec 也必须以 `strict-server: true` 生成，否则编译报 undefined（[issue #2010](https://github.com/oapi-codegen/oapi-codegen/issues/2010)）。单 spec 场景不受影响。

**推荐基线**：**oapi-codegen v2 + `strict-server` +（`std-http-server` 或 `chi-server`）**，生成物入库（可复现、code review 可见、CI 外无需装工具）。生成物 verbose 而可读，AI 读得懂；路由后端可选，与 §1 结论正交；工具版本可用 `go tool` 钉死。

**ogen 的位置**：技术上更"正确"（无反射、生成校验、类型级区分 optional/nullable），但 (a) 把路由与 runtime 焊死，与「标准库为底」冲突；(b) 生成物为性能做了大量特化，可读性低于 oapi-codegen，AI 定位问题更吃力；(c) 拉进 otel/zap/fasthttp 一大票传递依赖。在 50 实例、约 35 序列/实例的规模基线下，ogen 的性能优势不构成理由。

**被推翻条件**：
- 需要严格的 request/response 校验且不愿在 handler 里手写 → ogen 的生成校验成为硬需求。
- spec 大量使用 oneOf / discriminator，oapi-codegen 表达力不足以生成可用类型。
- Go 侧与 TS 侧生成类型口径出现不可调和的分歧。

---

## 3. 数据库访问层

核心问法（issue 原文）：**哪种让 AI 写错的查询在编译期就红。**

| 维度 | sqlc | pgx 裸用 | GORM |
| --- | --- | --- | --- |
| 最新版 | [v1.31.1 (2026-04-22)](https://github.com/sqlc-dev/sqlc/releases)，2026-08-02 仍有提交 | [v5.10.0](https://github.com/jackc/pgx/tags)，2026-08-01 有提交 | [v1.31.2 (2026-06-25)](https://github.com/go-gorm/gorm/releases) |
| 许可证 | MIT | MIT | MIT |
| 运行时依赖 | **无**（生成物只依赖所选 driver） | 直接依赖仅 `pgpassfile`/`pgservicefile`/`puddle`/`x/sync`/`x/text`（[go.mod](https://github.com/jackc/pgx/blob/master/go.mod)） | 较深，每个 driver 一个子模块 |
| SQL 拼写错误何时暴露 | **`sqlc generate` 阶段**（对 schema 解析 SQL，语法/未知列/未知表直接失败） | 运行时 | 运行时 |
| 列名/类型漂移何时暴露 | **编译期**（生成的 struct 字段与参数类型随 schema 变化，调用方立刻红） | 运行时（`Scan` 目标个数/类型不符） | 运行时（反射 + tag） |
| 参数个数/顺序错误 | **编译期**（生成具名 `XxxParams` 结构体） | 运行时（`$1/$2` 与可变参数对不上） | 运行时 |
| 动态查询（可变 WHERE / 排序） | 弱项，需变通 | 强 | 强（链式 API） |
| 对 pgx 的支持 | 一等：`sql_package: "pgx/v5"`（默认 `database/sql`），`sql_driver: "github.com/jackc/pgx/v5"`（仅 `:copyfrom` 需要）（[config 文档](https://docs.sqlc.dev/en/latest/reference/config.html)） | — | 经 driver 适配 |

**额外一手事实**：sqlc 提供 `sqlc vet`——官方文档载明内置规则 `sqlc/db-prepare` 会 "attempt to prepare each of your queries against the connected database and report any failures"，并可用 CEL 写自定义规则（含基于 `EXPLAIN` 的规则，例如禁止顺序扫描）（<https://docs.sqlc.dev/en/latest/howto/vet.html>）。这把「查询能不能在真库上 prepare」也变成 CI 阶段的确定性反馈。

**推荐基线**：**sqlc（`sql_package: pgx/v5`）为主 + pgx 直用作为逃生舱**。理由直指评估权重：sqlc 是三者中唯一把「SQL 写错」前移到**生成期**、把「schema 漂移」前移到**编译期**的方案；生成物是平铺直叙的 Go 函数，AI 读得懂；SQL 留在 `.sql` 文件里，是 AI 最擅长审阅的形态。GORM 反射 + 链式 API 意味着几乎所有错误都要跑到才发现，与「反馈闭环确定性决定 AI 自我验证能力」的立场正面冲突。

pgx 无论如何都在依赖里（sqlc 生成物的 driver），因此「逃生舱」不增加依赖成本：时序表批量写入用 `CopyFrom`、少数动态查询手写。

**被推翻条件**：
- 时间范围查询 API（地图 "Not yet specified" 一条）最终需大量运行期动态拼 SQL（可变聚合粒度/维度），使 sqlc 覆盖率跌破半数 → 退回 pgx + 一层薄查询构造器。
- 按月分区 / 动态表名导致 sqlc 无法静态解析 → **本次未取得一手验证，标注为待验**（见 §8）。

---

## 4. 数据库迁移

关键问法：**迁移文件能否随包分发并在启动时自动应用。**

| 维度 | goose | golang-migrate | atlas |
| --- | --- | --- | --- |
| 最新版 | [v3.27.3 (2026-07-22)](https://github.com/pressly/goose/releases) | [v4.19.1 (2025-11-29)](https://github.com/golang-migrate/migrate/releases) | [v1.2.0 (2026-04-10)](https://github.com/ariga/atlas/releases) |
| 活跃度 | 高（2026-08-01 有提交） | 中（2026-07-05 有提交，release 已约 8 个月未出） | 高（2026-07-29 有提交） |
| 许可证 | MIT（LICENSE 原文；GitHub 标 NOASSERTION 因含多份版权声明） | MIT（同上） | Apache-2.0 |
| 作为库嵌入 | **是**，README 首句 "Goose is a database migration tool. Both a CLI and a library." | 是，README "Use as CLI or import as library" | 主要是 CLI（有 Go SDK，但产品定位是 CLI + Cloud） |
| `go:embed` 迁移文件 | **一等支持**：`goose.SetBaseFS(embedMigrations)` 后 `goose.Up(db, "migrations")`（[README](https://github.com/pressly/goose#embedded-sql-migrations)） | 支持：[`source/iofs`](https://github.com/golang-migrate/migrate/tree/master/source/iofs) driver 读 `io/fs` | 需分发 migrations 目录 + atlas 二进制 |
| 启动时自动应用 | 直接调用库函数 | 直接调用库函数 | 需 exec 外部二进制或用 SDK |
| 版本支持策略 | — | — | 官方 README：**"the Atlas team will only support the two most recent minor versions of the CLI"** |

**推荐基线**：**goose 作为库嵌入 + `go:embed` 迁移文件 + 启动时自动 `Up`**。三条理由都对上硬约束：(a) 库形态 + embed 是文档里的一等用法，不是变通；(b) 迁移文件编译进主二进制，完全符合「整包自带、不依赖客户环境」；(c) 维护最活跃，release 节奏最密。

否决 atlas 作为运行时组件：其价值在 schema-as-code 与 diff/lint 工作流，但**引入第二个必须随包分发的二进制**，直接违反交付形态约束；且官方只支持最近两个 minor 版本，对长期私有化部署的整包是持续升级压力。（atlas 仍可作为**开发期**的 schema diff / migration lint 辅助工具，不进运行时——可选项，非基线。）

golang-migrate 功能上能做同样的事（`iofs` + 库调用），劣势只在维护节奏。

**被推翻条件**：
- 需要声明式 schema（写目标 schema、自动 diff 出迁移）而非手写增量 → 改为「atlas 开发期生成迁移文件 + goose 运行时应用」。
- 「升级与数据迁移路径」（地图 Not yet specified）最终要求迁移可回滚且有版本快照语义 → 需重新评估三者的 down migration 与并发安全（**本次未做一手验证**）。

---

## 5. 周边

### 5.1 结构化日志：`log/slog` 够用

一手依据（<https://pkg.go.dev/log/slog>）：Go 1.21 起进标准库；内置 `TextHandler`（key=value）与 `JSONHandler`（行分隔 JSON）；四档级别 `Debug(-4)/Info(0)/Warn(4)/Error(8)`，`LevelVar` 支持运行时动态调级（线程安全）；`Group`/`Attr` 结构化字段；`InfoContext` 等 context 感知方法；`HandlerOptions{AddSource, Level, ReplaceAttr}`；`LogValuer` 接口可让敏感类型自我脱敏（官方示例即 `Token` → `"REDACTED_TOKEN"`）；`slog.SetDefault` 可接管既有 `log.Printf`。Go 1.25 加 `Record.Source()` / `GroupAttrs`，Go 1.26 加 `MultiHandler`。

**推荐基线**：`log/slog` + `JSONHandler`，零依赖。`LogValuer` 正好用于地图约束 3「凭据永不回显」在日志侧的对应保证——PG 凭据类型实现 `LogValue()` 返回脱敏值，就把「AI 不小心把凭据打进日志」变成类型层面不可能。

**被推翻条件**：需要日志采样、专有格式、或与特定日志后端的高性能 sink → 才考虑 zap/zerolog（会引入依赖）。

### 5.2 配置加载

| | 标准库 flag + env + 单文件 | koanf | viper |
| --- | --- | --- | --- |
| 最新版 | — | [v2.3.5 (2026-05-30)](https://github.com/knadh/koanf/releases)，2026-07-31 有提交 | [v1.21.0 (2025-09-08)](https://github.com/spf13/viper/releases)，最近提交 2026-01-12 |
| 许可证 | — | MIT | MIT |
| 依赖 | 0 | 模块化（core + 各 provider/parser 独立子模块） | 较深（单体，拉进多种格式解析器） |

**推荐基线**：**先用标准库**（结构体 + `flag` + 环境变量 + 一个 YAML/JSON 文件），配置维度真的爆炸再上 koanf。整包交付意味着配置源本就少；viper 的「多源自动合并 + 远程配置 + watch」在这里是纯负债，且它是三者里维护最慢的。koanf 的模块化（只拉用到的 provider/parser）比 viper 更契合「依赖树浅」。

**被推翻条件**：配置源超过 3 种，或需要热加载。

### 5.3 周期调度

| | 自研 `time.Ticker` | gocron v2 | robfig/cron |
| --- | --- | --- | --- |
| 状态 | 标准库 | [v2.22.0 (2026-07-09)](https://github.com/go-co-op/gocron/releases)，活跃 | **最后 push 2024-07-08，事实停更** |
| 许可证 | — | MIT | MIT |

**推荐基线**：**自研 `time.Ticker` + `context`**。T1 已决策的调度需求是三个固定周期循环（采集、能力探测 5 分钟、告警评估与采集同频错相位），全是固定间隔，且**错相位**本身就要求手工控制起始时刻——用库反而绕。`for { select { case <-ticker.C: ...; case <-ctx.Done(): return } }` 是 AI 最不会写错的形态，且没有隐藏调度语义。

**被推翻条件**：出现用户可配置的 cron 表达式（例如保留期清理时间开放给用户配置）→ 引入 cron 解析器，选 gocron v2（robfig/cron 已停更，勿选）。

### 5.4 进程内并发控制

**推荐基线**：`golang.org/x/sync/errgroup`（`SetLimit`）+ `semaphore`。`x/sync` 已由 pgx 传递引入，边际依赖为零。50 实例量级下，「每轮采集并发上限 N + 单实例超时 + 整轮超时」用 `errgroup.WithContext` + `SetLimit` 就够，不需要 worker pool 库；并发度是显式数字而非隐式配置，AI 改动时看得见。

**被推翻条件**：需要任务持久化 / 跨进程队列（当前单二进制架构下不成立）。

---

## 6. Agent 侧

### 6.1 主机指标采集：gopsutil

| 事实 | 值 | 来源 |
| --- | --- | --- |
| 最新版 | **v4.26.7 (2026-08-01)**，约月度发版 | [releases](https://github.com/shirou/gopsutil/releases) |
| 许可证 | BSD-3-Clause（LICENSE 原文："gopsutil is distributed under BSD license"；GitHub 标 NOASSERTION 因含被移植代码的额外版权段） | [LICENSE](https://github.com/shirou/gopsutil/blob/master/LICENSE) |
| **CGO** | **不需要**。README 原文："All works are implemented without cgo by porting C structs to Go structs." | [README](https://github.com/shirou/gopsutil) |
| 直接依赖 | `purego`、`go-cmp`、`plan9stats`、`perfstat`、`go-sysconf`、`wmi`、`x/sys`（wmi/go-ole 仅 Windows 路径） | [go.mod](https://github.com/shirou/gopsutil/blob/master/go.mod) |
| 容器/chroot 适配 | 环境变量重定向：`HOST_PROC`、`HOST_SYS`、`HOST_ETC`、`HOST_VAR`、`HOST_RUN`、`HOST_DEV`、`HOST_ROOT`、`HOST_PROC_MOUNTINFO` | README |
| 平台能力差异 | README 有逐项 Linux/FreeBSD/OpenBSD/macOS/Windows/Solaris/AIX 支持矩阵；部分能力标 linux only（`docker/CgroupCPU`、`net_protocols`、`iptables nf_conntrack`），部分标 (linux, freebsd)（`cpu/CPUInfo`、`load/Avg`） | README 支持矩阵 |
| 缓存注意 | README 明确警告开启缓存可能导致不一致（例：Linux boottime 被 NTP 修改后返回意外值，[issue #1070](https://github.com/shirou/gopsutil/issues/1070)） | README |

**多发行版可靠性与权限**：gopsutil 在 Linux 上的数据源本质是 `/proc` 与 `/sys` 的文本解析，因此「跨发行版可靠性」的风险不在发行版品牌，而在 **kernel 版本、cgroup v1/v2 差异**、以及 `/proc` 是否被 `hidepid` 限制。CPU / 内存 / load / 磁盘用量 / 网络计数这类 P0 指标读的是全局 `/proc` 文件，**普通用户即可读，不需要 root**。需要提权的是**跨进程**信息（其它用户进程的 cmdline / open files / IO：`/proc/<pid>/io` 通常仅属主或 root 可读）与某些块设备/文件系统细节。

> **标注：本次未取得一手数据的项** —— gopsutil 在具体发行版矩阵（RHEL/CentOS 7/8/9、Ubuntu LTS、麒麟/统信等）上的实测差异、`hidepid` 环境下的降级行为、cgroup v2-only 主机的表现，均**未做实测验证**。若要作为选型依据，必须由 T5/T6 骨架阶段在目标发行版上实跑确认，不能从文档推断。

**推荐基线**：gopsutil v4。纯 Go 无 cgo（直接满足静态单二进制约束）、维护极活跃（月度发版）、支持矩阵逐函数公开（AI 查得到某指标在 Linux 上有没有，不用猜）、BSD 许可证无分发风险。P0 指标以「不需要 root」为设计红线；确需提权的指标另行论证。

**被推翻条件**：
- 目标发行版实测出关键指标缺失或口径错误 → 退回自解析 `/proc`（P0 的 CPU/内存/磁盘/网络其实不难，且口径完全可控）。
- 需要容器 / cgroup 感知口径 → 地图已划为 Out of scope（承自 T2），暂不成立。

### 6.2 跨 Linux 发行版静态编译

**核心事实**：Go 交叉编译不需要目标机工具链，但**一旦启用 cgo，二进制会动态链接 glibc**，从而绑定构建机的 glibc 版本下限——在更老发行版上运行会报 `GLIBC_2.xx not found`。规避方式是 `CGO_ENABLED=0`。

标准库里默认会用 cgo 的两处是 `net`（DNS 解析）与 `os/user`。`net` 包官方文档（<https://pkg.go.dev/net>）要点：

- Unix 上**默认偏好纯 Go resolver**，理由原文 "a blocked DNS request consumes only a goroutine, while a blocked C call consumes an operating system thread."
- 但在若干条件下回退到 cgo resolver：设置了 `LOCALDOMAIN` / `RES_OPTIONS` / `HOSTALIASES`，或 `/etc/resolv.conf`、`/etc/nsswitch.conf` 出现纯 Go resolver 不支持的特性。
- 构建标签：**`netgo` 完全禁用 cgo resolver，只保留纯 Go 实现**；`netcgo` 反之。
- 运行时可用 `GODEBUG=netdns=go` / `netdns=cgo` 覆盖，`netdns=1` 打调试信息。

**推荐基线（Agent 构建口径）**：

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" ./cmd/agent
```

- `CGO_ENABLED=0` 得到**完全静态、不链接 glibc/musl** 的二进制，跨发行版直接拷贝运行——「单二进制、无运行时依赖」约束最直接的实现。
- 需明确接受的副作用：DNS 走纯 Go resolver（不读 nsswitch 的 LDAP/NIS 等模块），`os/user` 只解析 `/etc/passwd`。对一个只需连服务端 HTTP 端点、读 `/proc` 的 Agent，这两条无实际影响。
- `-trimpath` 去掉构建机绝对路径（可复现 + 不泄漏路径）；`-ldflags="-s -w"` 去符号表减体积。
- 建议同时构建 `GOARCH=arm64`（国产化环境常见鲲鹏/飞腾）。
- gopsutil 无 cgo（§6.1），上述口径不会因指标采集而破功——这是选 gopsutil 而非任何 cgo 绑定库的硬理由。

**被推翻条件**：
- 出现必须 cgo 的依赖（当前基线中没有）→ 改用 musl 静态链接（zig cc / musl-gcc）或在最老目标 glibc 上构建。
- 目标环境 DNS 依赖 nsswitch 的非 files/dns 模块 → 部署文档需要求 Agent 用 IP 或 hosts 直连服务端。

---

## 7. 推荐基线汇总（**不是决策**）

| 维度 | 推荐基线 | 一句话理由 |
| --- | --- | --- |
| HTTP/路由 | `net/http` (Go 1.22+) 为底，chi v5 可选薄层 | 契约优先下框架的路由价值归零；chi 零依赖且不改 handler 类型 |
| OpenAPI 生成 | oapi-codegen v2 + `strict-server` + std-http/chi，生成物入库 | strict interface + `var _ I = (*Impl)(nil)` 把接口漂移变编译错误；生成物 verbose 可读 |
| DB 访问 | sqlc（`sql_package: pgx/v5`）为主，pgx 直用为逃生舱 | 唯一把 SQL 错误前移到生成期、schema 漂移前移到编译期的方案 |
| 迁移 | goose 作库 + `go:embed` + 启动时自动 Up | 库形态 + embed 是一等用法，迁移随主二进制分发，零外部二进制 |
| 日志 | `log/slog` + JSONHandler（`LogValuer` 脱敏凭据） | 标准库零依赖，脱敏可做成类型级保证 |
| 配置 | 标准库 flag/env/单文件；爆炸后再上 koanf | 整包交付下配置源本就少，viper 的复杂度是负债 |
| 调度 | 自研 `time.Ticker` + context | T1 的三个循环都是固定间隔且要求错相位，库反而绕 |
| 并发控制 | `x/sync/errgroup`（`SetLimit`）+ semaphore | 已在依赖树里，边际成本为零，并发度是显式数字 |
| Agent 指标 | gopsutil v4 | 纯 Go 无 cgo、月度发版、逐函数公开支持矩阵 |
| Agent 构建 | `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`，amd64 + arm64 | 完全静态、不绑 glibc 版本，跨发行版拷贝即用 |

## 8. 未覆盖 / 无一手数据（诚实清单）

以下项**本次调研未取得一手证据**，T5/T6 若要依赖，必须先补验证：

1. gopsutil 在具体目标发行版（RHEL 系 / Ubuntu / 国产系统）与 cgroup v2-only、`hidepid` 环境下的实测行为。
2. sqlc 对 PostgreSQL **声明式分区表**（保留 30 天原始数据的默认假设）的静态解析行为。
3. goose / golang-migrate 在**并发启动多实例**时的迁移锁语义与 down migration 可靠性。
4. 任何性能对比数字（路由吞吐、JSON 编解码、驱动性能）——**一律未测，本文不引用任何 benchmark**。
5. oapi-codegen 生成的 Go 类型与 TS 侧生成工具的类型口径一致性（TS 侧工具链不在本票范围）。
