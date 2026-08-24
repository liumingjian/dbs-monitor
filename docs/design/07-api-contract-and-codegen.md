---
status: active
kind: decision
---
# API 契约组织与代码生成流水线 v1.0

> 目标：定死 OpenAPI spec 的物理组织、代码生成流水线与漂移门、资源与 URL 模型、时间/分页/排序约定、错误与空状态响应模型、枚举表达、认证与授权落点、以及前端实时性方案。
> 适用范围：`api/` 下的全部契约文件，`monitor-server` 的 `internal/api` 与 `internal/httpapi`，`monitor-agent` 的客户端，以及前端的全部数据获取代码。
> 决策票：[T6 · API 契约组织与代码生成流水线](https://github.com/liumingjian/dbs-monitor/issues/24)。
> 输入边界（不重议）：[T1 · 系统组件拓扑](https://github.com/liumingjian/dbs-monitor/issues/19)、[T2 · 时序存储选型与指标数据模型](04-metric-storage-model.md)、[T3 · Agent 上报协议](https://github.com/liumingjian/dbs-monitor/issues/21)、[T5 · 后端代码结构与模块边界](05-backend-code-structure.md)、[RT-D · Go 基础库选型基线](https://github.com/liumingjian/dbs-monitor/issues/17)。
> 上游规格：[`03-monitor-platform-ia-draft.md`](03-monitor-platform-ia-draft.md)（页面树、§1.4 空状态、§7 状态模型、§8 角色）、[`02-alert-rule-model-draft.md`](02-alert-rule-model-draft.md) §7、[`00-decision-index.md`](00-decision-index.md) §4（四条不变式）。
> 状态：v1.0。后续路线要推翻其中任何一条，应新开决策记录，不在此原地改写结论。

---

## 0. 一句话结论

**spec 按域拆分、生成物入库、`make gen` 唯一入口、`git diff --exit-code` 作漂移门；UI 面 `/api/v1`，有全局视图的对象扁平、只在实例语境下存在的对象嵌套；空状态一律 200 + 封闭 `Unavailability` 码；服务端会话 cookie + spec 上声明 `x-required-role`；轮询，MVP 内禁推送通道。**

贯穿全文的取向与 [T4](06-metric-dictionary-and-collection-plan.md) / [T5](05-backend-code-structure.md) 一致：**凡「必须记住的纪律」都换成一条 `go test` 时就炸的测试**。本票产出三条这样的测试——枚举穷尽、`operationId` 角色全覆盖、生成物漂移。

---

## 1. D1 · spec 的物理组织

**结论**：**按域拆分，`$ref` 跨文件引用，不引 bundler。**

```text
api/
  openapi.yaml                 root：info / servers / tags / paths 的 $ref 汇总，不含内联定义
  paths/
    instances.yaml
    metrics.yaml
    sessions.yaml
    alerting.yaml              规则 / 告警实例 / 事件
    notification.yaml          渠道 / 联系人 / 策略 / 维护窗口
    collection.yaml            采集状态 / 能力 / 配置
    auth.yaml
    agent.yaml                 Agent 上报面（T3）
  components/
    schemas/<同名分域>.yaml
    enums.yaml                 全部封闭枚举，单文件（见 D9）
    errors.yaml                统一错误体与 Unavailability 码表
```

**理由**：

1. **与 T5 的切分主轴同轴。** T5 已按领域垂直切包（`alerting` / `metric` / `collect`…）。spec 按同一条轴切，「改告警 = 打开一个目录」这条 locality 才在契约层也成立。
2. **单文件是并行会话的冲突热点。** 粗估 spec 达 2000–4000 行；R3–R6 由几十个会话增量实现，单文件意味着几乎每个会话都在改同一个文件。
3. **不引 bundler** 是为了少一个生成物、少一步——`make gen` 的每一步都要有人维护。

**否决了什么**：

| 被否决 | 为什么 |
|---|---|
| **单文件 `openapi.yaml`** | 好处（一次读全契约）真实存在，但被「并行会话冲突」与「与 T5 切分轴不一致」压过 |
| **拆分 + 强制 bundle 出单文件入库** | 两头都要，代价是多一个生成物、多一个工具（redocly = 又一个 Node 依赖）、以及「改哪个文件」的持续歧义 |

**代价**：agent 要跳文件才能读全契约；`$ref` 跨文件时 IDE 与 lint 支持普遍弱于单文件。

**待钉死的事实（本票无法实测）**：**oapi-codegen v2 与 `openapi-typescript` 对跨文件 `$ref` 的解析能力**。决策会话所在环境无 Go 工具链。此项作为 [T11](https://github.com/liumingjian/dbs-monitor/issues/29) 首次 `make gen` 的显式验证项。**退路**：任一侧不支持 ⇒ 在 `make gen` 前加一步 redocly bundle，产出 `api/openapi.bundled.yaml` 作为生成器输入并入库；拆分的源文件布局不变。

**T11 实测回写（2026-08-03）**：`oapi-codegen v2.5.0` 对当前拆分 spec 的外部 `$ref` 报 `unrecognized external reference ... please provide --import-mapping`；`openapi-typescript 7.13.0` 可消费 bundle。已按本节预授权退路在 `make gen` 前加入 `@redocly/cli 2.20.3 bundle`，入库 `api/openapi.bundled.yaml`；拆分源文件布局不变。重复生成已做逐字节稳定性验证。

---

## 2. D2 · 生成器与生成物

**结论**：

| 侧 | 生成器 | 产物 |
|---|---|---|
| Go 服务端 | **oapi-codegen v2，`strict-server`** | `internal/api`：类型 + `StrictServerInterface` + 每操作具名请求/响应类型 |
| Go Agent | **oapi-codegen v2，`client`** | 同包内的 Go client |
| TS 前端 | **`openapi-typescript`（只生成类型）+ `openapi-fetch`（薄运行时）** | `web/src/api/schema.d.ts` |

Go 侧承 RT-D 基线。`var _ api.StrictServerInterface = (*handler)(nil)` 让接口漂移必然编译报错——这是地图 Notes 第 6 条要买的唯一东西。

**TS 侧只生成类型，不生成客户端与 hooks。**

**否决了什么**：

| 被否决 | 为什么 |
|---|---|
| **orval / hey-api（生成 TanStack Query hooks）** | 把「数据获取层怎么组织」交给生成器决定，而那是 [T7](https://github.com/liumingjian/dbs-monitor/issues/25) 的决策范围；产物体量大、diff 噪声高；**漂移时报错点落在生成代码里而不在调用点**，恰好毁掉生成的唯一目的 |
| **ogen** | RT-D 已降备选：路由与 runtime 焊死，并拉进 fasthttp/zap/otel/jx |

`openapi-typescript` 的产物是一个 `.d.ts`，类型对不上时报错落在**调用点**。RT-E 亦倾向「只生成类型的 OpenAPI 客户端」。

---

## 3. D3 · 触发方式与漂移门

**结论**：

1. **`make gen` 是唯一入口**，一次产出三份生成物，**永不单独跑其中一个**。
2. **生成物入版本库。**
3. **漂移门**：`make gen && git diff --exit-code` 是验证闭环的一环——改了 spec 没重新生成即为红。

**理由**：

- **`make` 而非 `go generate`**：`go generate` 跑不了 TS 侧，会退化成两个入口。
- **`make` 而非 npm script**：Go 侧是主体，主入口不该住在前端目录里。
- **入库**的三条理由：① 后续会话读得到具体类型名，不必先跑生成器才能写代码；② diff 可审，契约变更在 PR 里看得见影响面；③ 不入库则 clone 后 `go build` 直接失败，与 T5「反馈闭环确定性」冲突。

**代价**：引入 Makefile 作为跨 Go/TS 的编排层。这是有意的——[T9](https://github.com/liumingjian/dbs-monitor/issues/27) 的「一条命令的验证闭环」大概率长在同一处，本票把它的宿主先定了。

---

## 4. D4 · URL 前缀与版本位

**结论**：UI 面 **`/api/v1/...`**；Agent 面沿用 T3 已冻结的 **`/api/agent/v1/report`**。两个面靠 `agent` 段区分。**版本段保留但 MVP 内永不递增。**

不追求 `/api/ui/v1` 的对称：`agent` 是特例面（无 cookie、令牌鉴权、只写），UI 面是主干，主干不该背一个 `ui/` 段。

版本段冻结的理由：整包交付、前后端同版本发布（T5 §3.4 已对 Agent 定了强制同版本），`v2` 只在未来出现**外部消费者**时才有意义。保留段位是为了那一天不必改全部路径。

---

## 5. D5 · 嵌套统一律

**结论**：

> **有全局视图的对象走扁平集合 + `instance_id` 过滤；只在单实例语境下存在的对象走嵌套。**

| 扁平（有全局视图） | 嵌套（只在实例语境下存在） |
|---|---|
| `/api/v1/instances` | `/api/v1/instances/{id}/sessions`（活跃会话 / 长事务 / 锁等待 / 阻塞链） |
| `/api/v1/alerts`（当前告警） | `/api/v1/instances/{id}/metrics/series`（图表数据，D6） |
| `/api/v1/alert-events`（历史） | `/api/v1/instances/{id}/collection`（采集状态 / 能力三态 / 配置缺失待办） |
| `/api/v1/alert-rules` | `/api/v1/instances/{id}/query-stats`（`pg_stat_statements` 排行） |
| `/api/v1/performance-events` | |
| `/api/v1/notification-channels`、`/contacts`、`/contact-groups`、`/notification-policies`、`/maintenance-windows` | |

**理由**：

1. **「全局告警」与「实例级告警」是同一份数据的两个视角**——IA §3 明说告警详情页在两处复用。给它两条路径就是两个 handler、两处分页排序、前端两个 query key，以及「哪条权威」的持续歧义。
2. `sessions` 没有全局视图，扁平化只会逼出一个**永远必填**的 `instance_id`，那是假的集合。

**保证**：每个对象恰好一条路径。

**代价**：读 URL 已不能一眼看出是否实例级，得查本表。接受。

**否决了什么**：**全扁平（一律 `?instance_id=`）**——统一性是真的，但把只在实例语境下存在的对象伪装成全局集合，会诱导出「跨实例查会话」这种做不到也不该做的读法。

### 5.1 实例写端在 R2 的范围

`/api/v1/instances` 的**读端 + 最小写端**（新建 / 改凭据 / 删除）进 R2——骨架必须能接一个真 PG 进来，否则 [T10](https://github.com/liumingjian/dbs-monitor/issues/28) 的验收标准无法成立。**接入向导、批量导入、连接测试留 R6**（承 R1）。

---

## 6. D6 · 指标数据端点

**结论**：**批量 GET**，一次取一页所需的全部指标。

```
GET /api/v1/instances/{id}/metrics/series
      ?metric=pg.connection.total
      &metric=pg.tup.fetched
      &from=2026-08-02T00:00:00Z
      &to=2026-08-02T06:00:00Z
      &step=auto            # auto | 15s | 1m | 5m | ... | raw
```

```jsonc
{
  "from": "2026-08-02T00:00:00Z",
  "to":   "2026-08-02T06:00:00Z",
  "step": "1m",                          // 后端实际采用的粒度，回传（T2）
  "metrics": [
    {
      "metric": "pg.connection.total",
      "unit": "count",
      "unavailability": null,            // 指标级；null = 可用
      "series": [
        { "labels": {"database": "app"},
          "points": [[1754092800, 42], [1754092860, null], [1754092980, 44]] }
      ]
    },
    {
      "metric": "pg.replication.lag_bytes",
      "unavailability": "NOT_APPLICABLE_ROLE",
      "series": []
    }
  ]
}
```

**理由与要点**：

1. **批量而非一图一请求。** 标准监控一页有资源 / 数据库 / 复制三组共十几张图。一图一请求 = 十几个并发请求、十几份重复的 `from/to/step` 协商，且「多图时间范围联动」（RT-E 指出这是 ECharts 的一等能力）会退化成十几次不同步的刷新。
2. **GET 而非 POST。** 读就该是 GET——可缓存、**可分享链接**（R1 硬要求）、幂等。POST 读会让「URL 承载时间范围与筛选」这条前端状态归属规则在唯一一个高频端点上破例。参数用重复 `metric=` 表达，长度可控。
3. **`unavailability` 挂指标级，不挂 series 级。** 能力缺失（扩展未装、版本不支持、角色不适用、无权限）都是**指标**的属性；series 是「发现结果」（T2：维度消失属结构性不适用），它存在即代表采到过。
4. **`points` 用 `[ts, value]` pair，不用列存 `{t:[],v:[]}`。** 粒度由后端收敛到图宽量级（数百点 / series，`raw` 逃生舱 ≤6h 亦仅数千点），列存省的字节买不回可读性；且 pair 是 ECharts 原生入参。
5. **两种缺数表达都保留**：
   - `value = null` ⇒ **该桶采集了但值不可计算**（如 T2 定义的 `COUNTER_RESET`）。
   - **桶根本不出现在数组里** ⇒ 没采到（T2：空桶不补 0）。

   视觉上前端两者都画断点，但**排障时能分清「采集正常但值不可信」与「压根没采到」**——这正是 R1 §1.4 的精神。代价是前端处理两条分支；简化成单一表达会永久丢掉这个区分，不划算。

---

## 7. D7 · 时间、分页、排序的统一约定

**a) 时间一律绝对。** 参数固定为 `from` / `to`，**RFC 3339 UTC**，出现在任何带时间窗的端点（指标序列、告警历史、性能事件、长查询采样）。

**不提供 `?last=1h`。** 理由：R1 要求监控链接**可分享**，相对窗口分享出去指向的是另一段数据，等于分享了一个会变的东西。自动刷新的滚动窗口由前端每次重算 `from/to`（与 T7 的「时间范围归 URL」咬合）。代价：URL 变长，且「最近 1 小时」预设在每次刷新时 URL 会变。接受。

**b) 分页统一 `limit` + `offset`，响应带 `total`，不上 cursor。** 默认 `limit=50`，**硬上限 `limit ≤ 200`**（防止后续会话写出 `limit=100000`）。理由：50 实例量级下最大的表是告警事件历史，规模远够不上 keyset 分页的门槛；而 UI 需要「共 N 条」与跳页，cursor 给不了 `total`。统一一种形状，避免各端点各选各的。

**c) 排序单参数白名单。** `sort=-started_at`（`-` 前缀为降序）。每个端点在 spec 里列出**允许的排序字段枚举**，不是自由字符串——否则等于把列名暴露成 API，并诱发注入面。

**d) 实时快照类端点豁免 b/c。** 活跃会话、锁等待、阻塞链是**当下快照**，没有 `from/to`，也不分页——阻塞链分页会把一条链切断。这类端点返回整个快照 + **服务端硬上限**（如活跃会话最多 500 行），并在响应里带 `"truncated": true`。

**e) 请求跨度与 30 天保留边界。**（收口增补 2026-08-05，承 [T2](04-metric-storage-model.md) §10 移交「请求跨度超出 30 天保留边界时如何表现由 T6 定」，本票 v1.0 漏接。）请求窗口**允许**超出保留边界：不校验、不报错、不静默收窄 `from`/`to`。超出部分没有样本，自然表现为 D6 第 5 条的「桶不出现在数组里」，与其他「没采到」共用同一语义；响应回传的 `from`/`to` 仍是请求值——可分享链接要求 URL 语义稳定，后端不得改写窗口。**不为此新增 HTTP 错误码或 `Unavailability` 码**：「原始数据只保留 30 天」是交付文档声明的产品事实（地图约束 5），不是每次响应都要解释的异常；且 `Unavailability` 是封闭 13 码（D8.3），为一条时间边界开口不值得。`granularity=raw` 的 ≤6h 上限是另一条独立约束，不受本条影响。

---

## 8. D8 · 错误模型与空状态

### 8.1 空状态永远是 200

这是本节最硬的一条。「扩展未安装」「角色不适用」「暂无样本」是**正确执行后的正常结论**（承 T5 §7.1）。用 404 / 503 表达它们，等于：

- 逼前端从 HTTP 状态码反推业务语义；
- 让 TanStack Query 把它当 error，走重试与「保留上次成功数据」路径（RT-E 点名过这个坑）。

HTTP 错误码**只留给操作失败**：`400` 参数不合法、`401` 未认证、`403` 角色不足、`404` 资源真的不存在、`409` 冲突、`500` 内部故障。唯一的 Go `error` → HTTP 映射表住在 `httpapi`（T5 §7.2）。

### 8.2 错误体：极简自定义

```jsonc
{ "error": {
    "code": "VALIDATION_FAILED",
    "message": "阈值必须大于 0",
    "field_errors": [ { "field": "threshold", "message": "必须大于 0" } ]
} }
```

`code` 是封闭枚举；`message` 面向用户、可直接展示；`field_errors` 仅在 `VALIDATION_FAILED` 时出现。

**否决 RFC 9457（`application/problem+json`）**：它的 `type` URI 需要一个文档站点来解析，私有化交付下没有；`title` / `detail` / `instance` 三个字段在本项目会退化成「两个地方都写同一句 message」。

### 8.3 `Unavailability` 封闭码表（13 码）

IA §1.4 的 13 项减去「加载中」（纯前端状态，不进后端码表），加上 T2 已定的 `COUNTER_RESET`：

| 码 | IA 对应 | 备注 |
|---|---|---|
| `NO_SAMPLES_YET` | 暂无样本 | 指标可用但从未采到 |
| `NO_DATA_IN_RANGE` | 当前时间范围无数据 | 与上一条**必须分开**：补救动作不同（等 vs 换范围） |
| `STALE` | 数据过期 | 超出新鲜度阈值 |
| `COLLECTION_PAUSED` | 已暂停 | **不得**渲染为「采集失败」或「数据库不可达」（IA §7.2 硬性） |
| `COLLECTION_FAILED` | 采集失败 / 采集异常 | 失败原因文本走控制面（T2） |
| `DB_UNREACHABLE` | 数据库不可达 | |
| `AGENT_OFFLINE` | Agent 离线 | 与 `agent.status=offline` 同源同门槛（T3） |
| `PERMISSION_DENIED` | 无权限 | → 配置缺失待办（IA §4.8.1） |
| `EXTENSION_MISSING` | 扩展未安装 | → 配置缺失待办 |
| `FEATURE_DISABLED` | 功能未启用 | → 配置缺失待办 |
| `VERSION_UNSUPPORTED` | 版本不支持 | 不产生告警实例、不影响健康 |
| `NOT_APPLICABLE_ROLE` | 角色 / 拓扑不适用 | 同上；措辞须明确区别于「缺失」 |
| `COUNTER_RESET` | —（承 T2） | 点级：值不可计算 |

**对 R1 的一处修订**：IA §7.2 的表把「扩展未安装」与「功能未启用」合并为一行「功能未启用」，本票**按 §1.4 拆回两码**——待办清单里的修复动作不同（装扩展 vs 改参数），合并会让配置缺失待办无法给出可执行的下一步。

**码表只增不改**（承 T5 §7.4）：码进 spec 也就进了库（能力表、告警实例存的是这些字符串），改码名会碰存量数据。**允许的变更只有新增。**

---

## 9. D9 · 枚举的表达与穷尽性

**a) 一律 `SCREAMING_SNAKE` 字符串，不用整数码。** 承 T2 对枚举码的处理：码会落库、只增不改；整数码在库里不可读，且改序即灾难。

**b) 状态与压制标记在类型上分家。**

```yaml
AlertInstance:
  status:        # 恰好五值
    enum: [OK, PENDING, FIRING, NO_DATA, RECOVERED]
  suppressions:  # 独立数组，正交轴
    type: array
    items: { enum: [MAINTENANCE, ACKED, PAUSED] }
```

这样**「第六档状态」在契约层写不出来**——R1 不变式 1（五档 + 压制是正交轴）从一句文档变成一个类型约束。本票认为这是全票收益最大的一条。

**c) 穷尽性两侧不对称，分别处理。**

- **TS 侧天然成立**：`openapi-typescript` 生成字面量联合，`switch` 的 `default` 写 `assertNever(x)` 后漏一支即 `tsc` 报错。
  **规定**（进 `CLAUDE.md`）：凡对枚举做映射（状态 → 颜色、码 → 文案），必须用带 `assertNever` 的穷尽 `switch`；**禁止 `default:` 兜底成 fallback 文案**——兜底会把「漏了一档」伪装成正常渲染。
- **Go 侧天然不成立**：oapi-codegen 生成 `type AlertStatus string` + 常量，漏一个 `case` 编译器不会报错。
  **规定**：`internal/api/enum_test.go` 解析 `api/**.yaml`，对每个**登记**的枚举断言「spec 取值集合 == 该枚举在 Go 侧映射表的 key 集合」。新增一档而未更新映射表 ⇒ `go test` 红。

**否决 `exhaustive` linter**：与 T5 §2.4 否决 depguard 是**同一条理由**——违规必须在 `go test` 时炸，而不是在 lint 时炸；仓库里只保留一种守卫做法。且本方案与 T4 §3「解析字典文档总览表的一致性测试」是同一个套路。

**已知洞**：需要维护一份「哪些枚举必须有 Go 侧映射表」的登记清单（在测试里就是一个表）。**漏登记一个枚举 ⇒ 这条保护对它不生效。** 接受：登记是一行代码，且新增枚举必然经过 spec review。

---

## 10. D10 · 认证与授权

### 10.1 服务端会话 cookie，否决 JWT

**结论**：服务端会话，会话记录落库；cookie 属性 `HttpOnly; Secure; SameSite=Strict; Path=/`。

**否决 JWT**：其唯一真实好处是无状态横向扩展，而本项目是**整包单实例私有化交付**，该好处为零；代价却是**吊销做不到**——停用账号、降级角色必须立刻生效，JWT 只能靠短过期 + 续签做成「最多延迟 N 分钟生效」。纯负债。

前端是 `go:embed` 进同一二进制的**同源** SPA，cookie 天然可用、无 CORS。

Agent 面不受影响：T3 已定它走 bearer 令牌、无 cookie、不建会话。

### 10.2 CSRF：依赖 `SameSite=Strict`，不上 token

同源 + `SameSite=Strict` 已挡住跨站表单与跨站 fetch。**不再单独引入 CSRF token。**

**这条依赖的前提写死**：前端与 API **同源部署**。若未来出现跨源部署形态（独立前端域名、反代拆分），`SameSite=Strict` 失效，**必须补 CSRF token**——这是本决策的推翻条件。

### 10.3 授权：角色声明在 spec，检查落 `httpapi`

每个 operation 挂扩展字段：

```yaml
x-required-role: PLATFORM_ADMIN     # READONLY | ALERT_ADMIN | PLATFORM_ADMIN
```

解析成 `operationId → 最低角色` 表，`httpapi`（L2）中间件按 `operationId` 查表。配一条测试：

> **断言每个 `operationId` 都在表里**，漏登记 ⇒ `go test` 红。

**理由**：路径前缀式中间件（「`/api/v1/alert-rules/*` 需要告警管理员」）的默认行为是**新端点默认放行**——一个漏配就是越权，且没有任何东西会报错。声明在 spec + 全覆盖测试，把默认从「放行」翻成「拒绝」，且**角色要求与端点定义在同一处，读契约就能看见权限**。

角色**不下沉到领域包**：L1 不知道有「角色」这回事（T5 §2.1 层序）。

R1 §8.1「可见性不收窄，写能力收窄」因此在 API 层的落法是：**读端点一律 `READONLY`**，写端点按 §8.2 矩阵标注。

### 10.4 不变式 3 做成类型约束

「凭据永不回显明文」**不靠 `writeOnly: true`**（生成器支持参差），而是**请求体与响应体用两个不同的 schema**：

```yaml
NotificationChannelInput:   # 请求体
  properties: { name, type, endpoint, password }
NotificationChannel:        # 响应体
  properties: { name, type, endpoint, has_password }   # 根本没有 password
```

这样「回显明文」在契约层就**写不出来**，不需要靠人复核。同一形状适用于 PG 连接凭据、SMTP 密码、Webhook 密钥 / 签名头。

---

## 11. D11 · 实时性：轮询，MVP 内禁推送通道

**结论**：全部页面走**轮询**。MVP 内**不引入 WebSocket / SSE**。

**理由**：

1. **契约完整性。** OpenAPI 表达不了 WebSocket；SSE 只能勉强描述成 `text/event-stream`，生成器基本不管，等于要手写一套契约外的消息类型。**T3 已因同一条理由拒绝了 remote_write 作为第二套契约语言**——这里放行 WS/SSE 就是自我推翻。
2. **数据本身没有那么新。** 采样周期 10–60s（增强监控 5s），推送相对轮询能省的延迟**上限不到一个采样周期**。为此引入长连接、心跳、断线重连、反压，收益/复杂度严重不对称。
3. **告警送达不靠 UI。** R1 已定通知走邮件 + Webhook 双通道，且 IA §3.2 **显式接受了**「角标仅在有人登录时起作用」这条残留风险。UI 不是告警送达通道，因此 UI 实时性没有硬门槛。
4. 50 实例 × 个位数并发用户，轮询开销可忽略。

**配套约定**：

- 轮询周期**按页面定，不设全局值**，且**写在一处**不散落。参考量级：实例列表 / 标准监控 30s、增强监控 5s、当前告警 15s、会话与阻塞 10s。**具体值归 [T7](https://github.com/liumingjian/dbs-monitor/issues/25)**，本票只定「按页面定、集中声明」。
- **必须用 `dataUpdatedAt` 判新鲜度**（进 `CLAUDE.md`）。RT-E 的发现：TanStack Query 轮询失败会保留上次成功数据；不加判断就会把陈旧数据画成实时数据——那正好是 R1 最在意的那类谎言。
- **不做 ETag / 304 协商缓存**：本规模下省的带宽买不回「每个端点都要正确算 ETag」的持续成本。

**推翻条件**：若出现「秒级告警必须在 UI 内送达」的需求，**先改需求归属**（那本该是通知通道的职责），而不是先加 WebSocket。

---

## 12. 本票产出的三条机器守卫

本票不留纪律，只留测试。R3–R6 的会话违反下列任一条，`go test` / `tsc` 会红：

| 守卫 | 位置 | 拦下什么 |
|---|---|---|
| **生成物漂移** | `make gen && git diff --exit-code` | 改了 spec 没重新生成 |
| **枚举穷尽** | `internal/api/enum_test.go`；TS 侧 `assertNever` | 新增一档而映射表 / switch 未更新 |
| **`operationId` 角色全覆盖** | `internal/httpapi/authz_test.go` | 新端点漏声明 `x-required-role`（默认拒绝而非放行） |

---

## 13. 交付边界与未决事实

- **本票只产决策文档。** 实际的 `api/*.yaml`、`Makefile` 与三条守卫测试随 [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29) 落地——决策会话所在环境无 Go 工具链，写出的 YAML 与 Makefile 跑不了一次生成，等于未经验证的纸面产物，与 R2「纸面选型未经运行验证只是偏好」的立论冲突。
- **须在 T11 首次 `make gen` 时钉死的事实**：oapi-codegen v2 与 `openapi-typescript` 对**跨文件 `$ref`** 的解析能力（D1）。不支持 ⇒ 按 D1 的退路加 bundle 步骤，并回写本文档。
- **依赖本票的下游票**：[T9 · AI 开发护栏](https://github.com/liumingjian/dbs-monitor/issues/27)（`CLAUDE.md` 需收录 D9 的 `assertNever` 规定、D11 的 `dataUpdatedAt` 规定；「一条命令」需含 D3 漂移门）、[T10 · 骨架切片定义](https://github.com/liumingjian/dbs-monitor/issues/28)（切片必须穿过 D6 的批量端点）。

---

## 14. 否决记录汇总

| 被否决 | 出处 | 一句话理由 |
|---|---|---|
| 单文件 spec | D1 | 并行会话冲突热点；与 T5 切分轴不一致 |
| 拆分 + 强制 bundle | D1 | 多一个生成物、多一个工具、多一处歧义 |
| orval / hey-api 生成 hooks | D2 | 越界替 T7 决定数据获取层；漂移报错点落在生成代码里 |
| ogen | D2 | 承 RT-D：路由与 runtime 焊死，拉进一串重依赖 |
| `go generate` / npm script 作主入口 | D3 | 各自只覆盖一侧，必然退化成两个入口 |
| 生成物不入库 | D3 | clone 后构建即失败；类型名对会话不可见 |
| `/api/ui/v1` 对称前缀 | D4 | 主干不该背特例段 |
| 全扁平 `?instance_id=` | D5 | 把实例语境内的对象伪装成全局集合 |
| 一图一请求 | D6 | 十几个并发请求 + 多图联动失同步 |
| POST 读指标 | D6 | 破坏可分享链接与幂等，且只为这一个端点破例 |
| 列存 `{t:[],v:[]}` | D6 | 本粒度下省的字节买不回可读性；非 ECharts 原生形状 |
| 单一缺数表达 | D6 | 永久丢掉「值不可信」与「没采到」的区分 |
| 相对时间参数 `?last=` | D7 | 分享出去的链接指向另一段数据 |
| cursor 分页 | D7 | 本规模够不上门槛，且给不了 `total` |
| RFC 9457 | D8 | `type` URI 需文档站点；三字段会退化成重复 message |
| 空状态用 4xx / 5xx | D8 | 逼前端从状态码反推业务语义；被 Query 当 error 重试 |
| 整数枚举码 | D9 | 落库不可读，改序即灾难 |
| `exhaustive` linter | D9 | 承 T5 否决 depguard 同一理由：必须在 `go test` 时炸 |
| JWT | D10 | 吊销做不到；无状态扩展的好处在整包单实例下为零 |
| CSRF token | D10 | 同源 + `SameSite=Strict` 已覆盖；推翻条件已写死 |
| `writeOnly: true` 表达凭据 | D10 | 生成器支持参差；改用双 schema 让明文回显写不出来 |
| 路径前缀式授权中间件 | D10 | 默认放行，漏配即越权且无人报错 |
| WebSocket / SSE | D11 | 契约外的第二套消息语言；省下的延迟不到一个采样周期 |
| ETag / 304 | D11 | 省的带宽买不回每端点正确算 ETag 的成本 |
