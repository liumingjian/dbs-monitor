# 29 · 生产安全边界的具体断言集

> 出处：[生产安全边界的具体断言集 #112](https://github.com/liumingjian/dbs-monitor/issues/112)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：地图 Notes 第 6 条「HTTP 安全头由 server 无条件输出、server 自行终结 TLS、允许但不依赖前置反向代理」只定了边界的**存在**，没定它的**内容**。本文把这条边界写成可断言的具体取值，并逐条交代哪些能自动判定、哪些只能是文档承诺。
> **本文不原地改写 20 / 21 / 22 / 23 / 24 / 26 / 28 任何一条**，只在矩阵上新开第三个横切组 `SEC-1..10`。
> 输入边界（不重议）：[17](17-user-role-and-instance-onboarding.md) D1–D4（本地账号、只停用不删除、口令随机生成一次性回显、角色守卫）、D6；[13](13-credential-encryption-rotation-and-revocation.md)（凭据加密与 Agent 令牌）；[14](14-platform-observability-and-diagnostics.md) D2（平台健康四态）、D4（磁盘分级保护）、D5（诊断出口秘密禁区）；[18](18-v1-delivery-boundary-bs-binary.md) §6 D5；[19](19-agent-distribution-and-upgrade.md)（CA 指纹钉扎、全程无 `-k`）；[25](25-master-key-provenance-and-startup-failure.md) D1/D4（配置文件是规范来源、启动失败语义）；[26](26-data-and-recovery-gate.md) D5/D7/D9（启动失败按性质两分、执行序全序、参数化不是模拟）；[20](20-v1-acceptance-matrix.md) D4/D5/D6/D8、[21](21-v1-acceptance-entries-a.md) D1/D7/D8、[24](24-v1-acceptance-entries-d.md) D7/D14；[00](00-decision-index.md) M1（目标库监控账号权限）。平台库权限与前置校验归 [外部前置 PostgreSQL 的版本要求与部署前置条件 #116](https://github.com/liumingjian/dbs-monitor/issues/116)，本文只引用不复写。
> 状态：v1.0，2026-08-14 HITL 拍板。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**安全头六项由 server 在两条 handler 链上无条件输出，取值以 B 栏 golden 快照钉死，CSP 的 `script-src` 严格、`style-src` 放行 `'unsafe-inline'`（AntD cssinjs 的代价，显式记账）；HSTS 只发 `max-age`，永不发 `preload`、也不发 `includeSubDomains`；TLS 最低版本抬到 1.3、密码套件不列举，自签证书有效期一年，过期不拒启动而是 `tls` 子系统降级——`tls` 成为 `14` 的第九个健康子系统且刻意没有 `FAILED` 档；会话是服务端会话表 + `__Host-` cookie，绝对 12 小时 / 空闲 2 小时且两者必须可配，停用用户与口令重置即时吊销，不引入 CSRF token 并把推理写下来；审计不建 `audit_log` 第二份真相，落成 A 栏第十二条登记表 + 端到端三项；最小权限本票只出「server 以非 root 专用系统用户运行」一条，平台库那一半显式引用 #116；`govulncheck` 进 `check-full` 且阻断、`npm audit` 只报告不阻断，两者都不进矩阵。矩阵新开第三个横切组 `SEC-1..10` 全基线，硬底 91 → 101、条目 94 → 104。**

---

## 1. D1 · 安全头六项：取值与输出面

**结论：下列六项由 server 无条件输出，与是否存在前置反向代理无关。**

| 头 | 取值 |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'` |
| `Strict-Transport-Security` | `max-age=31536000` |
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `no-referrer` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=(), interest-cohort=()` |

### 1.1 CSP：脚本严格、样式放行

一手事实：`web/package.json` 的 `antd` 为 **6.5.3**，AntD 6 经 `@ant-design/cssinjs` 在**运行时**注入 `<style>` 标签；`web/index.html` 自身无内联脚本，Vite 构建产物的脚本全部是外链文件。

因此 `script-src 'self'` 可以不带任何放行项，而 `style-src` 必须带 `'unsafe-inline'`。

**否决全 nonce 化**（`StyleProvider nonce` + 关掉 Vite 的 modulePreload polyfill 内联）：它要求 server 对 `go:embed` 出来的 `index.html` 逐请求改写并注入 nonce，而当前形态是 `http.FileServer(http.FS(static))` 直发（`cmd/monitor-server/main.go:99-108`）。为了样式侧的 nonce 去拆掉静态直发这一层，是拿结构复杂度换一个**杀伤面在 script 不在 style** 的收益。

**已接受代价**：注入型攻击若能落到样式，可做布局欺骗与部分数据外带（如 `background-image` 侧信道）。本平台无用户生成的富文本面，注入入口本就窄。这条代价必须留在文档里，不得因为「CSP 看起来是绿的」而被遗忘。

### 1.2 HSTS：只发 `max-age`

- **永不发 `preload`**。preload 是提交给浏览器厂商的**全局、跨版本、实质不可逆**的名单。私有化部署的域名是客户的，替客户把域名钉进浏览器内置表是事故，不是加固。
- **不发 `includeSubDomains`**。平台不拥有部署域名的兄弟子域，替客户的其他子域下命令超出交付边界——那些子域上可能跑着必须走 HTTP 的老系统。

**「无条件输出」≠「取最激进值」。** 地图 Notes 第 6 条定的是输出面无条件，本条定的是取值有克制，两者不冲突。

### 1.3 输出面：两条 handler 链都要发

`cmd/monitor-server/main.go:100-109` 现在按 `/api/` 前缀分叉成两条 handler 链（API 与静态资源）。**只在其中一条上挂中间件是本条最典型的漏法**，且症状极隐蔽：页面能开、接口能通、扫描器只扫 API 就报绿。

因此安全头中间件必须包在**分叉之前**的最外层，`SEC-1` 对两条链各断一次。Agent 上报端点（`/api/v1/agent/*`）同样在覆盖内——它不是浏览器客户端，多发几个头没有代价，而「按客户端类型分别决定发不发」会立刻制造出一张需要维护的例外表。

---

## 2. D2 · TLS 承诺面

**结论：最低 TLS 1.3；密码套件不列举；自签证书默认有效期一年；证书来源两种（自动生成的自签 / 客户自备）走同一加载路径。**

- **最低版本抬到 1.3**。现状 `cmd/monitor-server/main.go:121` 的 `MinVersion: tls.VersionTLS12` 是 Go 的保守默认值，不是一条决策。交付目标只有 `linux/amd64` + `linux/arm64`，客户端只有现代浏览器与我方 agent（`cmd/monitor-agent/main.go:78` 由我方控制），没有任何需要迁就的老客户端。
- **密码套件不显式列举**。TLS 1.3 的套件集合本就极小且由 Go 运行时随版本维护；写下一份清单等于承诺一份**会过时**的清单，且把「Go 升级修了套件」变成「我们的文档说谎了」。
- **证书轮换**：换文件 + 重启进程。**不做热重载**——热重载要引入文件监视与半旧半新的连接态，而私有化部署一年一次的换证操作，重启是可接受代价。

---

## 3. D3 · 证书过期：不拒启动，`tls` 子系统降级

**结论：证书过期不拒绝启动；`tls` 成为 [14](14-platform-observability-and-diagnostics.md) 的第九个健康子系统，取值只有 `OK` / `DEGRADED`。**

- 证书已过期 → 继续监听、`tls` = `DEGRADED`、平台事件显式记一条。
- 剩余有效期 < 30 天 → `tls` = `DEGRADED`，与已过期**状态可区分**（事件内容与健康详情不同）。

**为什么不拒启动**：与 [25](25-master-key-provenance-and-startup-failure.md) D4 同构。证书过期时服务本身完全健康，拒启动会把「证书到期」这件计划内的事直接升级成「监控平台整个看不见」——而看不见的第一个后果就是没人知道证书到期了。

**为什么 `tls` 没有 `FAILED` 档**：`FAILED` 会被实例健康最坏归并（不变式②）判死整台平台。服务还在跑却报最坏态，与「不拒启动」自相矛盾。这是刻意的缺档，不是漏了。

**与 [21](21-v1-acceptance-entries-a.md) 片⑨ `F7` 的分工**：`AC-09-F7` 断的是「证书过期这类平台自身故障不污染目标库告警与 `NO_DATA`」；本文 `SEC-3` 断的是「平台自身的健康事实源如实降级」。同一个注入手段、两个断言点，**不合并**——合并会让「平台知道自己病了」和「平台没把病传染给目标库」退化成一句话。

---

## 4. D4 · 会话语义

**结论：服务端会话表 + `__Host-` cookie；绝对 12 小时、空闲 2 小时；并发会话不限制；不引入 CSRF token。**

一手事实：`api/openapi.bundled.yaml` 当前只有 `agent_token` 一种 security scheme，**用户会话在 API 契约上还不存在**。本条是从零起的决策，不是修订。

### 4.1 服务端会话表，不是无状态 JWT

[17](17-user-role-and-instance-onboarding.md) D2 硬要求「停用立即生效：不可登录，现有会话全部失效」。无状态 JWT 做不到即时失效，除非再加一张吊销表——那已经是会话表，还多背一层令牌解析。**否决 JWT。**

### 4.2 cookie 与时限

| 项 | 取值 |
|---|---|
| cookie 名 | `__Host-` 前缀（强制 `Secure`、`Path=/`、无 `Domain`，正好匹配单 host 部署形态） |
| 属性 | `HttpOnly`、`Secure`、`SameSite=Strict` |
| 绝对有效期 | 12 小时（`session_absolute_ttl`，**必须可配**） |
| 空闲有效期 | 2 小时（`session_idle_ttl`，**必须可配**） |
| 并发会话 | **不限制** |
| 登出 | 删行，立即生效 |
| 口令自改 / 被管理员重置 | **吊销该用户全部其它会话** |
| 用户被停用 | 删该用户全部会话行，立即生效 |

**并发会话不限制的理由**：限制并发会在双人值班、或同一个人在办公室与家里两处盯屏时把人锁在门外。这是监控平台——它存在的意义就是有人能看见。

**两个 TTL 必须可配**，因为矩阵单轮 ≤10 分钟（[21](21-v1-acceptance-entries-a.md) D7），不可配就等于「过期」这条腿永远测不到。做法与 [22](22-v1-acceptance-entries-b.md) 对 `repeat_interval` 的处置一致：**参数化不是模拟**，验收调成 90 秒 / 30 秒跑的是同一段代码。

### 4.3 不引入 CSRF token（推理写下来，不留白）

两条独立防线同时成立：`SameSite=Strict` 使跨站请求根本带不上会话 cookie；全部写操作是 `application/json`，属非简单请求，必过 CORS 预检而预检不会被放行。

**把这段推理写进文档而不是留空**，是因为「没有 CSRF token」和「忘了做 CSRF」在代码上长得一模一样。日后若引入表单提交或放宽 `SameSite`，本条即刻失效，必须新开决策记录。

---

## 5. D5 · 审计：不建第二份真相

**结论：不新建 `audit_log` 表。审计 = 既有业务表的 actor 列 + [14](14-platform-observability-and-diagnostics.md) 的平台事件流；本票产出的是一张「必须可归因的写操作登记表」。**

**否决新建统一审计表**：告警确认人、忽略理由、凭据更新人等留痕字段已在各片冻结，再加一张全量审计表就是双写，而**双写必然漂移**——到那时「哪份是真的」没有答案。
**否决用 journal 承载**：journal 按 [14](14-platform-observability-and-diagnostics.md) D4 有磁盘分级保护，是**可丢载体**，扛不起 `CONTEXT.md`「使记录可归因」这条硬约束。

### 5.1 必须可归因的写操作登记表（十一项）

| # | 操作 | 归因落点 |
|---|---|---|
| 1 | 登录成功 | 平台事件（主体 = 用户名） |
| 2 | 登录失败 | 平台事件（主体 = 尝试的用户名，**不记口令任何形式**） |
| 3 | 创建用户 | 用户表 + 事件 |
| 4 | 停用 / 启用用户 | 用户表 + 事件 |
| 5 | 变更用户角色 | 用户表 + 事件 |
| 6 | 重置他人口令 | 事件（**不记口令**） |
| 7 | 实例接入（创建） | 实例表 |
| 8 | 凭据更新 | 实例表 + 事件 |
| 9 | 实例移除 | 事件 |
| 10 | 告警规则改动 / 告警确认 / 忽略 | 既有留痕字段 |
| 11 | 采集暂停 / 恢复、主密钥轮换 | 事件 |

**可读者 = 全部登录用户只读。** 与 [00](00-decision-index.md) 已作出的让步（实例级授权不做，读取面跟随全局只读）保持一致，不为审计新造角色——新造一个「审计员」角色会立刻要求实例级授权来配套。

### 5.2 守卫形态：A 栏新增 `A12`

「有没有漏登记一项」本质是**表驱动判定**，不是执行判定。十一项跨片①⑦⑧，端到端逐项跑会让本组成为全矩阵最贵的一条，且十一次执行也证明不了「第十二项被忘了」。

- **`A12`**（[10](10-ai-guardrails-and-verification.md) §3.2 A 栏新增）：「必须可归因的写操作登记表」表驱动单测，断每一项都有 actor 落点。
- **端到端只跑三项**（`SEC-8`）：**登录失败**（无 actor 主体，最容易被实现成匿名丢弃）、**用户停用**（有守卫，容易在守卫拒绝路径上漏留痕）、**凭据更新**（接触秘密，容易连带把秘密写进留痕）。三种最容易漏的形状各取其一。

**这是第二次动 `10` §3.2 那张写着「不要往里加」的表**（第一次是 [24](24-v1-acceptance-entries-d.md) D10 加 `A10`/`A11`）。A 栏从十一条变十二条。理由同上；若日后认为 `A12` 名不副实，应回到本文重议，**不得悄悄删表行**。

---

## 6. D6 · 最小权限：本票只出一条

**结论：server 以非 root 专用系统用户 `dbsmon` 运行。平台库权限归 #116，目标库权限已由 [00](00-decision-index.md) M1 冻结，本文都只引用。**

- [25](25-master-key-provenance-and-startup-failure.md) 已定「`/etc/dbs-monitor/credentials/` 由 root 预建、server 不 `mkdir` 父目录」。本条正好接上：**root 建目录并 `chown dbsmon`，进程本身不以 root 跑**。
- 断言面 = 端到端断 server 进程 **uid ≠ 0**（`SEC-9`）。
- **不由本票统一收口三个权限面**：#116 已在写「专属 database + 独立 schema `dbsmon` + 不需要 superuser、不需要扩展」，两票同写平台库权限就是第二份真相。本文显式记账「平台库那一半在 28 号」，而不是假装它不存在。

---

## 7. D7 · 依赖漏洞扫描

**结论：`govulncheck` 进 `check-full` 且阻断；`npm audit` 进 `check-full` 只报告不阻断；两者都不进矩阵。**

一手事实：当前 `Makefile` 与 `.github/` 中**没有任何漏洞扫描**（`govulncheck` / `npm audit` / trivy 均无命中）。

- **`govulncheck` 阻断**：它做可达性分析，报的是「你真的调到了那条路径」，误报率低到可以当门。
- **`npm audit` 只报告**：传递依赖噪音大，SPA 打包后大量 devDependency 告警不构成运行时暴露面。一道天天红的门等于没有门。
- **都不进矩阵**：矩阵是**运行期验收**，构建期门禁不在它的射程。结果进 `acceptance-report.json` 的显式清单（承 [27](27-v1-deliverables-and-candidate-provenance.md)）。

**已接受代价（联网张力）**：漏洞库需要联网，而 [27](27-v1-deliverables-and-candidate-provenance.md) 把校验和信任根移到了交付团队自建构建机，那台机器未必联网。处置：**`check-full` 允许联网；`release-gate` 不重跑扫描、只读报告**。否则这道门在离线构建机上会退化成「跳过即绿」——一条被绕过却看起来生效的规则，比没有规则更坏。

---

## 8. D8 · 矩阵：新开第三个横切组 `SEC-1..10`

**结论：新开 `SEC-*` 组，10 条全基线，`rides_on: []` 全独立执行。硬底 91 → 101、条目 94 → 104。**

**否决并进 `REC` 组**：`REC` 的组名语义是**恢复门**（[26](26-data-and-recovery-gate.md) D1 立组理由是备份 / 重启 / 分区一条轴）。#116 把启动前置校验并进去尚属同轴；安全边界不同轴，并进去会让组名说谎。
**否决拆散挂进片⑧ `AC-08-*`**：安全头与会话跨全部九片，挂片⑧等于宣称它只是凭据页的事。

### 8.1 条目草案

| ID | 层 | 断什么 |
|---|---|---|
| `SEC-1` | api | 六个安全头在**两条 handler 链**（`/api/*` 与静态资源）都输出，取值逐字对齐 `B13` golden |
| `SEC-2` | api | 真握手：TLS 1.2 被拒、1.3 通过；自签证书开箱可用；**不断套件** |
| `SEC-3` | api | 放已过期证书：不拒启动、`tls` = `DEGRADED`、平台事件显式 |
| `SEC-4` | api | 放 20 天后到期证书：`DEGRADED` 预警，与 `SEC-3` 状态可区分 |
| `SEC-5` | api | 会话绝对 / 空闲过期（TTL 参数化为 90s / 30s） |
| `SEC-6` | api | 即时失效三面：登出、停用用户、口令重置吊销其它会话 |
| `SEC-7` | api | cookie 四属性（`__Host-` / `HttpOnly` / `Secure` / `SameSite=Strict`）+ 响应体**全文正则**无会话令牌 |
| `SEC-8` | api | 审计可归因端到端三项（登录失败 / 用户停用 / 凭据更新） |
| `SEC-9` | api | server 进程 **uid ≠ 0** |
| `SEC-10` | browser | 跑一条关键路径，**console 无 CSP violation** |

`SEC-10` 是本组唯一浏览器条目，也是唯一能证明 D1.1 那个取舍**没有把 AntD 打碎**的面——CSP 在真实世界的失败模式是砸自己的页面，不是被绕过。

### 8.2 执行序

全序改写为：`AC-08-S1` → 片条目 → `REC-*` → **`SEC-*`** → `AC-08-S7`。

组内用 `after` 字段把 **`SEC-3` / `SEC-4` / `SEC-5`** 压到组尾——三条都要求 server 以不同证书或配置重启，是破坏性的；`SEC-1/2/6/7/8/9/10` 在前，共用一次常规启动。

### 8.3 `exceptions` 仍为 `[]`

证书 fixture 不是业务表数据，不触 [20](20-v1-acceptance-matrix.md) D4 的 `B11` 禁令。全部会话与审计数据经业务 API 真实产生。

---

## 9. D9 · 新增守卫 `B13`

| # | 内容 | 出处 | 兑现时机 |
|---|---|---|---|
| `B13` | **安全头 golden 快照**：六项头的取值逐字入 golden 文件 | 本文 D1 | 片⑧实现期 |

与 `A9`（枚举码表 golden）同构。理由：运行时断言若只断「有 CSP 头」，有人手滑删掉 `object-src 'none'` 也照绿；golden 把「改头」升级成一次**有意识的行为**。分工——`B13` 断**取值逐字**，`SEC-1` 断**真的发出来了、且两条链都发**。

---

## 10. 只能是文档承诺的六条（逐条给理由）

| # | 承诺 | 为什么不可自动判定 |
|---|---|---|
| 1 | 密码套件不列举 | D2 已定不承诺清单；断了就等于承诺了 |
| 2 | HSTS 的浏览器端实际行为 | 只能断头发出；浏览器的 HSTS 缓存态不在我方射程 |
| 3 | `npm audit` 只报告不阻断 | D7；报告本身不构成判定 |
| 4 | 目标库监控账号最小权限 | 已由片①能力四态覆盖（[21](21-v1-acceptance-entries-a.md) D3），本票不重复断 |
| 5 | 平台库账号最小权限 | 归 #116，本票只引用 |
| 6 | 口令强度由生成器保证 | 归片⑧单测（[17](17-user-role-and-instance-onboarding.md) D3），不进矩阵 |

---

## 11. 外溢到实现的硬要求

1. **安全头中间件必须包在 `/api/` 前缀分叉之前的最外层**（D1.3）。
2. `session_absolute_ttl` / `session_idle_ttl` **必须是部署期配置项**（D4.2）。
3. **`tls` 追加为 `14` 的第九个健康子系统**，取值只有 `OK` / `DEGRADED`（D3）。
4. `10` §3.2 **A 栏登记 `A12`、B 栏登记 `B13`**（D5.2 / D9）。按 [10](10-ai-guardrails-and-verification.md) §3.4「一经登记即有约束力」，登记动作归片⑧实现票。
5. **登录失败事件与口令重置事件绝不记录口令任何形式**，口令类字段进秘密扫描禁名单（承 [25](25-master-key-provenance-and-startup-failure.md) 对诊断包的同类要求）。
6. `MinVersion` 由 `tls.VersionTLS12` 改为 `tls.VersionTLS13`（`cmd/monitor-server/main.go:121`）。
7. 自签证书生成器的有效期定为 **1 年**，并在启动时计算剩余有效期供健康快照读取。

## 12. 环境要求

1. **compose 中 server 以非 root 用户运行**——否则 `SEC-9` 恒绿，是一条自欺条目。
2. **`test/acceptance/fixtures/` 预置两张证书**：一张已过期、一张 20 天后到期（供 `SEC-3` / `SEC-4`）。

---

## 13. 已接受的代价

1. **`style-src` 带 `'unsafe-inline'`**（D1.1）：样式侧注入面未关。换的是不拆掉 `go:embed` 静态直发这一层。
2. **矩阵从此有三个横切组**：`20` 号 D5 的「独立计分」规则要对第三组同样适用。
3. **第二次动 `10` §3.2 的登记表**（`A12`）：那张表自己写着「不要往里加」，本文加了。
4. **TLS 1.3 硬下限**排除了任何老客户端。交付目标下无已知受害者，但这是一条不可静默降级的承诺。
5. **换证需重启**（D2）：私有化部署一年一次，可接受。

---

## 14. 交给下游

| 内容 | 去处 |
|---|---|
| 会话表 schema、登录/登出端点、cookie 下发、中间件落位 | 片⑧「凭据与接入」spec |
| `SEC-1..10` 条目写入 `test/acceptance/matrix.yaml` | 本票后续提交（待 #116 的 `matrix.yaml` 改动先合入） |
| `A12` / `B13` 登记进 `10` §3.2 | 片⑧实现票 |
| `tls` 子系统进 `14` 的健康模型 | 片⑧实现票 |
| Go/No-Go 报告如何呈现 `govulncheck` / `npm audit` 结果 | [Go/No-Go 质量门组成 #114](https://github.com/liumingjian/dbs-monitor/issues/114) |
