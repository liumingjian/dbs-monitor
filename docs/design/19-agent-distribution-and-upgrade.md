# 19 · Agent 分发与升级形态（无安装器）

> 出处：[Agent 分发与升级形态（无安装器） #108](https://github.com/liumingjian/dbs-monitor/issues/108)，属地图 [Wayfinder 地图 · 从 walking skeleton 到可投产 B/S 系统 #105](https://github.com/liumingjian/dbs-monitor/issues/105)。
> 定位：**supersede 记录**。[T8](09-packaging-and-deployment.md) D7 与 §8.1 依附于「离线 tar 安装包 + 安装脚本自举」的形态，该形态已被 [18](18-v1-delivery-boundary-bs-binary.md) 作废。本文在「二进制直接运行」的交付边界下重定 Agent 的分发、信任根传递、升级与版本兼容，并接下 18 号 D8 移交的时钟自检执行形态。被推翻的原文档**不原地改写**，以本文为准。
> 输入边界（不重议）：[T1](https://github.com/liumingjian/dbs-monitor/issues/19)（PG 指标一律 server 直连、Agent 只采 OS + 心跳、Agent 与实例 1:1）、[T3](https://github.com/liumingjian/dbs-monitor/issues/21)（单上报端点、强制 TLS 自签 CA 无跳过开关、**无下行通道**、时间戳偏移 > ±30s 拒收并报 `error`、向后兼容一个大版本）、[T13](13-credential-encryption-rotation-and-revocation.md) D5/D6（Agent 显式登记、令牌只存哈希、轮换立即生效、不做机器绑定）、[T5](05-backend-code-structure.md)（Agent 与服务端编译期同源）、[17](17-user-role-and-instance-onboarding.md) D9（「接入设置」页 Agent 区的四态与一次性回显）、地图 #105 Notes 第 7 条（配置文件为规范来源、环境变量可覆盖、秘密不进命令行）。
> 状态：v1.0。要推翻其中任何一条，应新开决策记录，不在此原地改写。

---

## 0. 一句话结论

**平台自分发仍是主线，手工分发升格为有备料的 plan B；「绝不自升级」保留，但 Agent 通过上行响应得知自己落后并告警；信任根靠页面带外传递的 CA 指纹钉扎，全程无 `-k`。**

D7 的核心论据——**Agent → 平台的网络路径必然是通的，所以「从平台下载」不引入任何新的连通性假设**——依附的是 T3 的架构，不是安装器。安装器消失不削弱它，因此自分发保持主线。

---

## 1. 推翻 / 保留总表

| 原结论 | 处置 | 本文条目 |
|---|---|---|
| T8 D7.1 平台自分发 + 实例接入页一条安装命令 | **保留，载体改写**（安装脚本 → 接入设置页生成的安装命令 + 下载端点） | D1 |
| T8 D7.2 安装需 root、运行不需 root | **保留** | D4 |
| T8 D7.3 Agent 绝不自升级 | **保留，补一条落后告警** | D2 |
| T8 §7.1 编译期同源、运行期容一个大版本 | **保留，补运行期判定** | D3 |
| T8 §8.1 安装命令内嵌 CA 指纹 | **保留，载体改写** | D5 |
| T8 D11 时钟同步检查（执行者） | **归属落定**（安装脚本 → Agent 启动自检，复用上报端点） | D6 |
| T8 D7 中「整包 tar 内本就带两个架构的 Agent 二进制」这一 plan B 前提 | **作废**（无 tar 包），plan B 改由交付物备料 | D7 |

---

## 2. D1 · 分发路径：平台下载端点为主线，手工分发为备料 plan B

**结论**：两条路都要，主线是平台自分发。

1. **主线**：server 提供 `GET /api/v1/agent/download?arch=`（`linux/amd64` | `linux/arm64`）。「接入设置」页 Agent 区（[17](17-user-role-and-instance-onboarding.md) D9）在签发/轮换令牌成功后，除一次性回显令牌外，生成一条可复制的安装命令，其中内嵌 CA 指纹（D5）与令牌。
2. **plan B**：手工分发，备料见 D7。

**理由**

1. D7 的连通性论据未被削弱：Agent 存在的前提就是它能 push 到平台，从平台下载不新增任何连通性假设。
2. 目标规模 50 实例（[17](17-user-role-and-instance-onboarding.md) 已定逐个手工接入可接受）下，纯手工分发要把**架构匹配、版本对齐、令牌落盘**三件事全部推给客户的手工流程，每一件都是静默出错、只在「没数据」时才暴露的那类失败。
3. plan B 必须存在且**不依赖主线通路**——它的适用前提恰恰是主线通路走不通（网络策略隔离、下载端点被防火墙挡、批量装机走内部制品库）。

**推翻条件**：若客户环境普遍要求二进制只能经内部制品库分发、禁止从业务系统直接下载，则 D7 的备料升为主线，本条翻转，代价是文档与页面文案，不是架构。

---

## 3. D2 · 绝不自升级保留；落后由上行响应告警，不由平台推送

**结论**

1. **禁令保留**：Agent 不含任何自升级机制。这不是偏好，是 [T3](https://github.com/liumingjian/dbs-monitor/issues/21)「无下行通道」的结构性后果——自升级需要平台把「该升级了」**推**给 Agent，那就是下行通道。T3 在 [18](18-v1-delivery-boundary-bs-binary.md) §13 中「全部保持」，其依据未被交付边界触动。
2. **升级 = 重跑 D1 定的分发动作**（主线：重跑安装命令；plan B：替换二进制 + 重启 unit），可脚本化批量重跑。
3. **落后可见**：server 在**上报响应体**中回其自身版本；Agent 比对后若落后即打 `warn` 日志，**不做任何自动动作**。

**§3.1 「响应体带版本号」为何不是下行通道**

T3 禁止的是平台**主动发起**到 Agent 的连接或推送——「令牌只写边界不开口」。上报响应体是 Agent 自己发起的那次请求的返回值，不构成新的可达性要求、不需要 Agent 监听端口、不引入平台侧的调度语义。取消这条不会让任何东西更安全，只会让「50 台里有 3 台忘了升」从可见退回静默。

---

## 4. D3 · 版本不匹配：超窗拒收该次上报，且不静默

**结论**：server 按 Agent 上报的版本号判定，超出「向后兼容一个大版本」的窗口即**拒收该次上报**，并把该 Agent 置为可见的错误态。

- **编译期同源 / 运行期容差**的两个时刻不变（T8 §7.1、[T5](05-backend-code-structure.md)）：Agent 与服务端从同一次构建产出；已部署的旧 Agent 在服务端升级后仍被接收一个大版本。
- **不静默的落地**：「接入设置」页 Agent 区显示「版本过旧」。
- **归类**：这是**平台自身故障**语义（[T14](14-platform-observability-and-diagnostics.md)），**不进入目标告警、不产生 `NO_DATA`**。

**否决「超窗仍接收、只降级掉新增字段」**：它把一个明确的运维事实（该机器没升级）变成一份悄悄缺列的数据，与 T3「过老拒收但不静默」直接冲突。

---

## 5. D4 · Agent 侧托管与配置来源：照搬 server 规则；装要 root、跑不要 root

**结论**

1. **配置来源照搬地图 #105 Notes 第 7 条**：配置文件是规范来源，环境变量可覆盖，**秘密不进命令行**；交付 systemd unit 模板，验收只断言「Agent 以给定配置能被拉起、能重启恢复」。
2. **令牌落盘不变**（[T13](13-credential-encryption-rotation-and-revocation.md) D5.3）：`/etc/dbs-monitor-agent/token`，属 Agent 专用非 root 用户，`0600`；不进命令行参数、systemd 环境变量、日志或诊断包。
3. **安装需 root（写 systemd unit），运行不需要 root**（RT-D 已确认 P0 主机指标读全局 `/proc` 不需要 root）。**这个区分必须写进交付文档**——「监控 Agent 要 root」是客户安全评审最常卡住的一条；形态变了，这个事实没变。

**否决「Agent 简化为纯环境变量 / 命令行参数配置」**：命令行参数会让令牌出现在 `ps` 输出里，直接违反 T13 D5.3。

---

## 6. D5 · 信任根：页面带外传递的 CA 指纹钉扎，全程无 `-k`

**问题**：安装脚本要从平台 HTTPS 拉二进制，但此刻这台机器还不信任平台的自签 CA，而 T3 定死**无 `-k` / 无跳过开关**。

**结论**：**指纹钉扎**，不是跳过校验，是换一种信任根。

1. 「接入设置」页生成的安装命令内嵌平台 CA 的 **SHA-256 指纹**。
2. 脚本先取 CA，比对指纹；**不符即退出**，不做任何降级。
3. 比对通过后，该 CA 成为此后全部 TLS 校验的信任根。
4. **带外通道 = 运维用浏览器经 TLS 看到的那张页面**，指纹由人眼复制过来。

**plan B 的信任根**：CA 证书文件作为交付物之一由客户手工分发（D7），不经网络。

§8.1 的原则原样保留：**信任根靠带外传递，全程不出现 `-k` / `--insecure`。**

---

## 7. D6 · 时钟自检：Agent 启动自检，复用上报端点，超 ±5s 拒绝启动

**结论**（接下 [18](18-v1-delivery-boundary-bs-binary.md) D8 移交的执行形态）

1. Agent 启动后**立即发一次上报**；server 在响应体中回其自身时间（或直接回偏移判定结果）。
2. Agent 比对，偏移超 **±5s** 即**立即退出并打明确错误**，不进入常驻循环。
3. **不新开握手端点**——T3 定死单上报端点。时钟回执与 D2 的服务端版本号合用同一个响应结构：两者都是上行请求的返回值。
4. server 侧 **±30s 拒收并报 `error`** 的门槛（T3）不变。±5s 比它严是刻意的，留出运行期漂移余量。

**理由**：18 号 D8 保留的原则是把「装好了、服务起来了、就是没数据」这类模糊故障**前移成明确失败**。只 warn 或只靠 server 侧拒收，都会让一台时钟跑偏的机器安静地跑着却一条样本都进不来——正是要消灭的那类故障。

---

## 8. D7 · 分发物完整性与 plan B 备料

**完整性**

1. **主线**靠 D5 已被指纹钉住的 TLS 通道，通道本身即完整性保证。
2. **另发布二进制的 SHA-256 校验和清单**，随交付物提供，供 plan B 手工路径核对。

**否决代码签名（GPG / cosign）**：需建密钥保管与吊销流程，v1 无签名基础设施，成本与内网威胁模型不匹配。
**否决「只靠通道、不发校验和」**：plan B 的适用前提正是通道信任不成立，那时它将完全没有校验手段。

**plan B 备料清单**（交付物必须包含，是 [#110](https://github.com/liumingjian/dbs-monitor/issues/110) 的直接输入）

| 备料 | 说明 |
|---|---|
| `linux/amd64` + `linux/arm64` 两个 agent 二进制 | 交付目标双架构（[18](18-v1-delivery-boundary-bs-binary.md) D6） |
| SHA-256 校验和清单 | 覆盖上述二进制 |
| CA 证书文件 | plan B 的信任根，不经网络（D5） |
| agent 配置样例 | 规范来源是配置文件（D4） |
| systemd unit 模板 | 验收只断言能拉起、能重启恢复（D4） |
| 手工接入步骤文档 | 含令牌落盘路径与 `0600`、专用非 root 用户、**装要 root 跑不要 root**（D4） |

---

## 9. 保持有效（本文不触碰）

| 来源 | 内容 |
|---|---|
| [T1](https://github.com/liumingjian/dbs-monitor/issues/19) | PG 指标一律 server 直连、Agent 只采 OS 指标与心跳、Agent 与实例 1:1 |
| [T3](https://github.com/liumingjian/dbs-monitor/issues/21) | 单上报端点、强制 TLS 自签 CA 无跳过开关、无下行通道、±30s 拒收、向后兼容一个大版本 |
| [T13](13-credential-encryption-rotation-and-revocation.md) D5/D6 | Agent 显式登记（`agent_expected` 独立于实例）、令牌只存哈希、32 字节随机一次性回显、轮换立即生效不设宽限期、吊销 ≠ 停用、不做机器绑定 |
| [17](17-user-role-and-instance-onboarding.md) D9 | 「接入设置」页 Agent 区四态呈现与操作集；本文只改其中「安装指引」的**内容** |
| [18](18-v1-delivery-boundary-bs-binary.md) D4 | 平台对外访问地址为配置文件必填项（自签证书 SAN 无法自动推断） |

**enrollment 语义不新增。** 明确否决「已下载未上报」中间态：它记录的不是生命周期**事实**而是一次动作的痕迹，且不可靠（下载端点被打一次不代表脚本跑完）。真正要的信号是「有没有收到过上报」，已落在现有四态 + `NO_DATA` 里。

---

## 10. 交给下游

| 去向 | 内容 |
|---|---|
| [#110 v1 交付物与候选留痕](https://github.com/liumingjian/dbs-monitor/issues/110) | D7 的 plan B 备料清单进入 v1 交付物清单；SHA-256 校验和清单的产出与归档方式 |
| [#112 生产安全边界的具体断言集](https://github.com/liumingjian/dbs-monitor/issues/112) | CA 指纹钉扎、`-k` 禁用、令牌不进 `ps` 这三条如何写成自动断言 |
| [#115 真 Linux 环境适配与最终验收](https://github.com/liumingjian/dbs-monitor/issues/115) | 双架构 agent 二进制的真机验证、systemd 托管与专用非 root 用户、时钟同步 |
| 实现票（另开，不属本地图） | 下载端点 `GET /api/v1/agent/download?arch=` 及其 Agent token 鉴权；上报响应体新增服务端版本与时间字段；Agent 启动自检；接入设置页安装命令生成。**并须一并清掉 [T13](13-credential-encryption-rotation-and-revocation.md) D5.1 登记的欠账**——把 [T9](10-ai-guardrails-and-verification.md) B7 秘密禁名单的 `allowAgentToken` 例外从 `POST /api/v1/instances` 201 响应迁到登记/轮换端点，两者会碰同一处代码 |

---

## 11. 决策记录：下载端点为何要鉴权

`GET /api/v1/agent/download?arch=` **用刚签发的 Agent token 鉴权**。

安装命令本来就必须携带 token（脚本要把它写进 `/etc/dbs-monitor-agent/token`），复用零成本。匿名开放的代价是给平台开一个无鉴权、能确认「这里跑着 dbs-monitor 且版本是 X」的探测面，收益为零。要求登录会话 cookie 则脚本用不了，等于逼人先手工下载。

这与 D4「令牌不进命令行」不冲突：那条约束的是 **Agent 进程**的启动参数（防 `ps` 泄漏），一次性的安装命令携带 token 是 T8 D7 就有的形态。
