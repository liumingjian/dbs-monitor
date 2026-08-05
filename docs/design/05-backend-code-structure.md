# 后端代码结构与模块边界 v1.0

> 目标：定死后端代码的包划分、依赖方向、接缝清单、错误模型与启动形态。
> 适用范围：`monitor-server` 与 `monitor-agent` 两个二进制的全部 Go 代码。
> 决策票：[T5 · 后端代码结构与模块边界](https://github.com/liumingjian/dbs-monitor/issues/23)。
> 输入边界（不重议）：[T1 · 系统组件拓扑](https://github.com/liumingjian/dbs-monitor/issues/19)、[T2 · 时序存储选型与指标数据模型](https://github.com/liumingjian/dbs-monitor/issues/20)、[T3 · Agent 上报协议](https://github.com/liumingjian/dbs-monitor/issues/21)、[RT-D · Go 基础库选型基线](https://github.com/liumingjian/dbs-monitor/issues/17)。
> 本文档的结论是 [T9 · AI 开发护栏](https://github.com/liumingjian/dbs-monitor/issues/27) 所产 `CLAUDE.md` 的主体来源。

---

## 0. 本文档要解决的问题

后续 R3–R6 由几十个 Claude Code 会话增量实现。**边界不清则熵增无人拦截**——每个会话都只看到局部，没有人负责全局结构。因此本文档的每条规则都优先选择**结构性保证**（编译器或测试会拦下违规）而非**纪律性约定**（写在文档里靠人记住）：后者在第 20 个会话必然失效。

---

## 1. 目录树

```text
cmd/
  monitor-server/main.go     仅 signal 处理与退出码
  monitor-agent/main.go
internal/
  httpapi/     L2  实现生成的 StrictServerInterface；唯一懂 HTTP 语义的包
  collect/     L2  采集编排 + 采集执行（内含唯一预留接缝 Collector）
  capability/  L2  能力探测循环，维护三态能力表（server-direct / agent 两来源）
  evaluator/   L2  评估扫描循环：取样本 → 判定 → 驱动状态机 → 落库 → 排通知
  agent/       L2  Agent 全部逻辑（gopsutil 采集、上报循环、应用响应体配置）
  alerting/    L1  五状态机（纯函数）、去重、实例健康最坏归并
  metric/      L1  指标字典载体、样本读写、差分与 reset
  instance/    L1  实例与凭据
  notify/      L1  通知渠道适配
  db/          L0  pgxpool 持有 + InTx + sqlc DBTX
  api/         L0  oapi-codegen 生成物（类型 / StrictServerInterface / Agent 用 Go client）
  pgconn/      L0  Dialer 接缝 + PG 连接管理
  clock/       L0  Now + Ticker
  arch_test.go     层序、包白名单、interface 白名单的机器断言
```

### 1.1 切分主轴 = 领域，不是技术分层

**否决水平三层**（`handler/` + `service/` + `repo/` + `model/`）。

理由：

1. **Locality 是本票要买的东西。** 后续会话每次只改一件事。垂直切法下"改告警评估" = 打开一个目录；水平分层下同一改动要同时动四个目录，每个会话都得先重建这张散布图。
2. **水平三层全是浅模块。** `handler` 与 `repo` 是穿透层——删掉它们复杂度不会在调用方重现。而 `alerting`（五状态机 + `NO_DATA` + 冻结 + 去重）、`collect`（差分 reset + 权限降级 + 版本适配）天然是深模块：接口窄、实现厚。
3. **水平分层诱导跨领域通用抽象。** 一个 `service/` 目录待久了必然长出 `common`、`base`、`util`。

代价：领域间调用关系必须显式规定方向（§2），否则会变成互相 import 的网。

### 1.2 每个领域包自带存储访问

sqlc 支持多组 `sql` 配置，各自 package、共享同一份 schema。因此不存在全局 `repo/` 包，`metric` 的查询住在 `internal/metric`，`alerting` 的住在 `internal/alerting`。

### 1.3 单 go.mod，两个二进制

不拆第二个 module：单 module 下 `go build ./...`、`go vet ./...`、`go test ./...` 一条命令覆盖两个二进制，这是 R2「反馈闭环确定性」的直接兑现。拆 module 会让 Agent 与服务端的 API 版本靠 `replace` 或 tag 同步。

---

## 2. 依赖方向

### 2.1 四层偏序

```text
L3  cmd/monitor-server        cmd/monitor-agent
      ↓
L2  httpapi   collect   capability   evaluator   agent
      ↓
L1  alerting   metric   instance   notify
      ↓
L0  db   api   pgconn   clock
```

**只能向下 import。同层之间默认禁止**，例外逐条列举——当前只有一条：

| 例外 | 方向 | 原因 |
|---|---|---|
| `collect → capability` | 单向，无环 | 采集编排读能力表以决定本轮采哪些指标 |

### 2.2 层序即不变式

依赖方向不是整洁癖，它是 R1 不变式在代码层的执行机制：

| R1 不变式 | 代码层的结构性保证 |
|---|---|
| 不变式 2：实例健康单一来源 | 健康归并函数只住在 `alerting`；`instance` 同层够不着它，"实例列表自己算个健康"**写不出来** |
| 不变式 1：告警状态五档 | 状态类型定义在 `alerting`；`metric` / `instance` 够不着，长不出第二套状态表达 |

### 2.3 `evaluator` 与 `alerting` 分家

`alerting`（L1）是**纯的**：五状态机、去重、健康归并，不知道时间流逝、不知道数据库。
`evaluator`（L2）是那个**扫描循环**：取样本、算窗口聚合、判时效、算维护窗口命中、调用状态机、落库、排通知。

这是「状态机从哪读、往哪写、如何被驱动」的答案——**驱动在 L2，逻辑在 L1**。

`evaluator` 是编排层，本来就不该薄，但要提防它长成上帝包。规则：**凡是能写成纯函数的（窗口聚合、时效判定、维护窗口命中）就下沉成 L1 的纯函数**，`evaluator` 只留「取数—调用—落库—排队」这条编排骨架。

### 2.4 守卫方式：架构规则是一条 Go 测试

`internal/arch_test.go` 用 `golang.org/x/tools/go/packages` 加载全部包，断言 §2.1 的禁止边、§3 的包白名单、§5 的 interface 白名单。违规时错误信息写成人话并指向本文档。

**否决 golangci-lint + depguard**：它是第二个二进制、第二条命令、第二处配置。违规必须在**跑测试时**就炸，而不是等 CI 的另一个 job——`go test ./...` 本来就是验证闭环里那条命令。

**新增包默认拒绝**，必须先在 `arch_test.go` 里显式登记层级。这会小小地烦到每个加包的会话，这正是想要的摩擦。

---

## 3. 共享面：只有生成物

### 3.1 `monitor-server` 与 `monitor-agent` 共享 `internal/api` 一个包

Agent 只采 7 个 `host.*` + 心跳，走单端点 `POST /api/agent/v1/report`，不含 PG 指标、不持有 PG 凭据（T1 D2/D3、T3）。两个二进制在领域上几乎不重叠。

**生成物是最好的共享包，因为人写不进去。** 垃圾桶之所以形成，是因为共享包接受手写代码——一旦 `internal/common` 存在，"这个函数放哪都不合适"就有了收容所。`internal/api` 由 oapi-codegen 从 spec 生成、每次覆写，手写内容会被下一次生成抹掉。**这条物理性质比任何纪律都可靠。**

Agent 用**生成的 Go client** 调那个端点：上报字段改错 = 编译失败，而不是运行时 400。

### 3.2 指标字典不共享

Agent 的 7 个 `host.*` 是编译期固定的 P0 集合，不需要读字典；字典是服务端采集编排的输入（载体形态见 [T4](https://github.com/liumingjian/dbs-monitor/issues/22)）。让 Agent 依赖字典包，等于把一个会持续演化的服务端结构焊到必须长期兼容的 Agent 上。

### 3.3 禁止的包名

`internal/` 下**不得存在** `common` / `util` / `shared` / `base` / `helper`。需要被两处使用的东西，要么进 `internal/api`（生成物），要么就地复制——**两处 30 行重复远比一个共享包便宜**。由 `arch_test.go` 断言。

### 3.4 Agent 与服务端强制同版本

同一份 spec 定义 `host.*` 样本结构，改 spec 同时影响两侧编译（漂移被编译器拦下）。推论：**不支持「新服务端 + 旧 Agent」**，整包升级时 Agent 随包重装。

**代价（明确接受）**：若将来出现「服务端升了、几十台 Agent 没升」的现实场景，需反向要求 Agent 端点做版本协商——那是一次破坏性变更。

---

## 4. 事务归属

### 4.1 `internal/db` 是唯一基础设施包

只有三样东西：`pgxpool` 持有、`InTx(ctx, fn) error`、sqlc 的 `DBTX` 接口。内容物理上封顶为「连接与事务」，不会变成垃圾桶。所有领域包可 import 它。

### 4.2 领域包永不开事务，事务由编排层持有

每个领域包的 sqlc `Queries` 由 `New(dbtx DBTX)` 构造，因此能跑在池上或某个 `tx` 上。事务边界由 `collect` / `evaluator` / `httpapi` 决定：

```go
// evaluator：每实例一事务
db.InTx(ctx, func(tx pgx.Tx) error {
    samples, err := metric.New(tx).LatestFor(ctx, instanceID, ...)
    if err != nil { return err }
    // ... 判定与状态机 ...
    return alerting.New(tx).PersistTransitions(ctx, transitions)
})
```

理由：

1. **事务边界是业务语义，不是存储细节。** T2 的两条不变式——样本与 `last_success_at` 同事务、读样本与写告警状态同事务——主语都是「这一轮」，不是任何一张表。领域包自己 `BeginTx` 则这两条无从表达。
2. **它让不变式变成结构性的。** 领域包拿到 `DBTX`，**没有能力**开事务；想违反同事务约束的代码写不出来。
3. **不引入 Unit of Work / Repository 抽象层。** sqlc 的 `DBTX` 已是那个接缝，生成物自带，零成本。

### 4.3 通知发送必须在事务提交之后

评估事务内只写状态，把待发通知落表或入内存队列，提交后再发。否则会出现「持事务发网络请求」这类最难查的长事务问题。

### 4.4 代价

编排层代码会显式看到事务和多个领域包，一个函数里同时出现 `metric.` 与 `alerting.`。这是刻意的：**事务边界应当在代码里可见**，藏起来的事务边界是这类系统最难查的一类 bug。

---

## 5. 接缝白名单（封闭）

「AI 会话写 Go」最典型的熵增，是为每个包造一个 interface + 一个 mock。判据：**一个实现只是假想接缝，两个实现才是真接缝。**

允许存在的 interface **当前是四个手写接缝，加上生成的 `DBTX`**：

| 接缝 | 第二个实现 | 依据 |
|---|---|---|
| `pgconn.Dialer` | 未来「经 Agent 隧道拨号」 | T1 D3 明文预留（隧道强制端到端 TLS） |
| `collect.Collector` | 未来下沉到 Agent 侧 | T1 D1 **唯一**预留接缝 |
| `clock.Clock` | 测试用 fake | §6 |
| `notify.Channel` | 邮件 / Webhook / … | `02-alert-rule-model-draft.md` §3.7；**随 R4 落地** |
| sqlc `DBTX` | `pgxpool` 与 `pgx.Tx` | 生成物自带，§4 |

**其余领域包之间一律直接 import 具体类型，不定义 interface。** 由 `arch_test.go` 断言 `internal/` 下的 interface 声明集合等于该白名单；新增必须先改白名单（强制一次显式论证）。当前 `notify/` 尚未落地，因此守卫实际为 4 个手写接缝 + `DBTX`。

理由：

1. **T1 D1 已在架构层做过同一判断**——「API / 评估 / 通知之间焊死，不假装可拆」，理由是「此后每个会话都要绕着一层没人使用的抽象写代码」。本条是它在类型层的翻译；不写死，D1 会在代码层被悄悄推翻。
2. **测试不需要那些接口。** 数据库有真的（测试库跑 goose 迁移）；时间有 fake clock；状态机是纯函数不需要替身。
3. **`collect.Collector` 是接口但不是「为测试」的接口**，它的第二实现是未来的进程边界。这个区别必须写清楚，否则会话会照猫画虎地给 `capability`、`evaluator` 也各造一个。

### 5.1 代价：测试依赖真库

部分单测会变成需要真 PG 的集成测。这笔交易划算——它顺带保证 sqlc 生成的查询真的被执行过（RT-D 点名的缺口之一即「sqlc 对分区表的解析」无一手数据），假 mock 永远测不出这个。

**对 T9 的硬要求**：必须有**一条命令**能起测试用 PG。否则会话跑不动测试就会开始造 mock 绕过白名单。

---

## 6. 时间的接缝

### 6.1 状态机是无时间的纯函数

R1 **只支持 `consecutive_count`，明确否决 `for_duration`**（`02` §3.2）；`NO_DATA` 门槛是「连续 2 个评估周期」，也是计数。因此持续判定完全不依赖 wall clock：

```go
// internal/alerting —— 不 import clock，也不用 time 表达「现在」
func Step(cur State, counters Counters, input Evaluation) (State, Counters, []Event)
```

`Evaluation` 携带的是**本次评估的判定结论**：满足 / 不满足 / 缺数（+ 原因码）。

### 6.2 `clock` 止步于 L2

`internal/clock` 只有 `Now() time.Time` 与 `Ticker(d) (<-chan time.Time, stop func())`。时间的全部出现点：

| 出现点 | 归属 | 处理 |
|---|---|---|
| `window` 回看窗口 | `evaluator` | 算出 `[from, to]` 传给 `metric` |
| 样本时效性（缺数判定） | `evaluator` | 用 `now` 与样本 `ts` 比，**结论**传给 `Step` |
| 维护窗口 / 心跳超时 | `evaluator` / `capability` | 注入 `clock.Now()` |
| 循环节拍 | L2 各循环 | 注入 `clock.Ticker`，测试手动推进 |

理由：

1. **状态机是这套系统里最深的模块，纯函数是它能达到的最深形态。** R1 那张 5×N 流转表可直接翻译成表驱动单测——喂一串「满足 / 不满足 / 缺数」，断言状态与计数序列。与 T2 对差分 reset 采用的形状一致。
2. **时间注入若全局传染，代价是每个会话都要学它。** 一旦 `alerting` 接受 `Clock`，后续会话会顺手调 `clock.Now()`，纯函数性质一次性丢失且不可恢复。
3. **判定与迁移分离是 `NO_DATA` 正确性的关键。** 缺数的**原因**（不可达 / 无权限 / 扩展未装 / 超时…）只有 L2 知道；状态机只需知道「这次是缺数」，原样把原因带进 `Event`（规格要求告警详情显示 No Data 原因）。

### 6.3 代价

告警的时间感知依赖评估周期均匀——某轮评估若延迟 3 个周期，状态机看到的仍是「1 次评估」。规格已接受（UI 显示「连续 3 次 × 30 秒 ≈ 1 分 30 秒」是**约合**）。

---

## 7. 错误模型

### 7.1 12 种空状态不是错误，是一等的值

`03` §1.4 的 12 种空状态里绝大多数**不是失败**——「扩展未安装」「当前实例角色不适用」「暂无样本」「当前时间范围无数据」都是正确执行后的**正常结论**，需要落库、随 API 返回、在前端渲染成解释性文案。

用 Go `error` 表达它们，等于把正常业务结果塞进异常通道，每个调用方都得 `errors.Is` 一遍才能还原——**这正是「塌缩成一个通用 error」的成因**。

### 7.2 两套东西，严格分开

**`Unavailability`（不可用原因码）—— 值**
在 **OpenAPI spec 里定义 enum**，生成到 `internal/api`（L0），前后端共用同一份码表。它出现在三处且是同一个类型：

- 能力三态表的原因（`capability`）
- `NO_DATA` 原因（`evaluator` / `alerting`）
- API 响应里图表 / 表格的空状态（`httpapi` → 前端空状态壳组件）

采集与查询的返回形态因此区分「没有数据」与「操作失败」——二者在类型上就是两件事。

**Go `error` —— 只表示「操作失败了」**
DB 挂了、序列化炸了、bug。用 sentinel + `errors.Is`；**唯一一处到 HTTP 状态码的映射表放在 `httpapi`**，其余任何包不得碰 HTTP 语义（由 §2.1 层序保证：L1 不知道 HTTP 存在）。

### 7.3 红利

- 与 T2 已定的形状一致——「不可计算 = 无行 + 控制面 `COUNTER_RESET` 码」，本条是它的全局推广。
- 兑现 RT-E 的发现——「12 种空状态任何图表库都表达不了，必须由图表外的空状态壳组件承担」。码进了 spec，前端那个壳组件就有了**穷尽的、编译器保证的** switch（TS union 漏一支即报错）。
- 结构性堵死「缺数当 0」：数据通道里缺数是 `null` + 一个码，不是零值。

### 7.4 约束与代价

- **码表只增不改**：码进了 spec 也就进了库（能力表、告警实例存的是 spec 定义的字符串码），改码名会碰存量数据。与 T2 对枚举码的处理一致。
- **后端码表只收后端能判定的项**：「加载中」是纯前端状态，不进后端码表。12 项的最终裁定归 [T6](https://github.com/liumingjian/dbs-monitor/issues/24)。
- 函数签名变啰嗦（不是 `(T, error)` 而要同时表达不可用原因）。`CLAUDE.md` 需给出范例形状。

---

## 8. 配置与启动

### 8.1 配置分两类，各有唯一的家

| | 启动期静态配置 | 运行期可变配置 |
|---|---|---|
| 例子 | 监听地址与端口、自带 PG 连接与数据目录、TLS 证书路径、日志级别 | 采样周期、增强监控开关、保留期、评估周期、通知渠道、维护窗口、暂停采集 |
| 来源 | **单个配置文件 + 环境变量覆盖**（仅密钥类） | **数据库，由 UI 修改** |
| 生效 | 重启 | 立即（Agent 侧最坏延迟一个上报周期，T3 D1） |

**判据：UI 上能改的，不进文件。** R1 已把采样周期、增强监控、暂停采集定成产品功能，它们就不能同时出现在配置文件里——否则必须回答「文件写 30s、库里写 10s，听谁的」。**任何一项配置只有一个家。**

### 8.2 启动顺序

```text
加载配置 → 全量校验（fail fast）
  → 连 DB → goose 迁移（go:embed，启动时自动执行）
  → 自签 CA / 服务端证书自举（T3 D4，首次启动生成）
  → 构造 L0/L1/L2 各模块（显式 new，依赖显式传，无 DI 框架）
  → 起 HTTP server → 起采集编排 / 能力探测 / 评估三个循环
```

- **配置校验一次性全跑完并汇总报错**，不是遇到第一个错就退——交付团队改配置时应一次看到全部问题。
- **迁移启动时自动执行**（RT-D 基线）：整包交付下不存在「运维手动跑迁移」这一步。

### 8.3 `main` 的形状

```go
func main() { /* signal */ ; if err := run(ctx, cfg); err != nil { os.Exit(1) } }
```

`main` 只管信号与退出码，**全部组装在可测的 `run` 里**——walking skeleton（T11）的端到端测试可直接起一个真实实例，而不是靠外部进程。

### 8.4 优雅退出

1. HTTP server 停止接受新连接，in-flight 请求给有界超时（建议 30s）
2. 三个循环停止节拍，当前那一轮跑完或被 `ctx` 取消
3. 关闭连接池

**循环被取消时绝不能提交半个事务**——`ctx` 取消让事务回滚，这是 §4.2「事务由编排层持有」的直接红利（编排层同时持有 `ctx` 与 `tx`）。

关停期间 Agent 的 push 被拒，**Agent 下一轮自然重试**（无下行通道、无长连接，T3 D1）。写死一条：**服务端不得为了「优雅」而接受一个自己无法完整落库的 push——宁可拒。**

### 8.5 代价

运行期配置全在库里，意味着**数据库不可用时平台完全不可运行**。整包自带 PG 的形态下可接受——DB 挂了平台本来也没有任何数据可展示。

---

## 9. 否决记录

| 方案 | 否决理由 |
|---|---|
| 水平三层 `handler/service/repo/model` | 毁掉 locality；三层全是浅模块；诱导 `common` 垃圾桶（§1.1） |
| 拆第二个 go.mod 给 Agent | API 版本要靠 `replace`/tag 同步，毁掉一条命令的验证闭环（§1.3） |
| `internal/common` / `util` / `shared` | 为「放哪都不合适」提供收容所（§3.3） |
| Agent 依赖指标字典包 | 把演化中的服务端结构焊到需长期兼容的 Agent 上（§3.2） |
| 领域包自持事务 / Unit of Work 抽象层 | T2 两条同事务不变式无从表达；sqlc `DBTX` 已是该接缝（§4.2） |
| 为每个领域包定义 interface + mock | 一个实现只是假想接缝；违反 T1 D1「不假装可拆」（§5） |
| 给 `alerting` 注入 `Clock` | 纯函数性质一次性丢失且不可恢复；R1 无 `for_duration`，状态机本就不需要时间（§6.2） |
| 12 种空状态用 Go `error` 表达 | 把正常业务结果塞进异常通道，即塌缩的成因（§7.1） |
| golangci-lint + depguard 守依赖方向 | 第二个二进制 / 命令 / 配置；违规须在 `go test` 时就炸（§2.4） |
| 采样周期等同时可在配置文件与 UI 配置 | 必然要回答「听谁的」（§8.1） |
