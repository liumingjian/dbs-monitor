# 19 · v1 macOS 运行与 PostgreSQL 交付形态

> 出处：[v1 macOS 运行与 PostgreSQL 交付形态 #100](https://github.com/liumingjian/dbs-monitor/issues/100)（地图 [#98](https://github.com/liumingjian/dbs-monitor/issues/98) 子票）。
> 输入边界：[v1 macOS 支持边界](18-v1-macos-support-boundary.md) 已冻结唯一目标为 macOS 14.0 及以上版本的 Apple silicon（`darwin/arm64`）。
> 状态：2026-08-10 冻结。本文是对 [打包、部署与运行形态](09-packaging-and-deployment.md) 中 Linux/systemd 路线的 macOS 后续决策；v1 macOS 运行与 PostgreSQL 交付发生冲突时以本文为准。

---

## 0. 一句话结论

**v1 在安装资产中随包交付由项目维护的 PostgreSQL 17 arm64 运行时，不依赖 Homebrew、系统 PostgreSQL、外部数据库或容器；平台 server 与 PostgreSQL 作为两个系统级 LaunchDaemon，以同一个专用非登录用户运行，通过 peer 认证的 Unix socket 通信。安装、初始化、运行、日志、备份、同大版本升级和安全卸载均由随包工具闭环，目标 Mac 在安装和升级时不需要联网。**

发布资产的外层格式、签名、公证、下载渠道和构建验证由 #101 决定；本文只规定无论采用何种外层资产都必须满足的运行契约。

## 1. D1 · PostgreSQL 17 随包交付

v1 交付物必须包含原生 `darwin/arm64` 的 PostgreSQL 17 server、客户端工具和运行所需的非系统动态库。PostgreSQL 与 dbs-monitor 作为一个产品版本一起构建、验证和升级；客户不单独选择或维护 PostgreSQL 版本。

| 方案 | v1 结论 | 原因 |
|---|---|---|
| 随包 PostgreSQL 17 | **采用** | 与现有 schema、迁移、socket 默认值及离线交付一致；版本和依赖面可由发布证据锁定 |
| Homebrew PostgreSQL | 否决 | 引入联网/镜像源、formula 版本漂移、独立升级与 service 管理边界，干净机不再自包含 |
| 复用系统或用户已有 PostgreSQL | 否决 | macOS 不提供可依赖的内置 PostgreSQL；已有实例的版本、配置、端口、扩展和权限也不受产品控制 |
| 外部 PostgreSQL | 否决为生产形态 | 增加网络、TLS、凭据、兼容版本和备份责任，无法形成单机最小闭环 |
| Docker/Podman | 否决 | 引入额外运行时并破坏完全离线干净机的依赖边界 |

源码开发和测试仍可显式设置 `DATABASE_URL` 连接开发数据库；这是开发入口，不构成受支持的生产部署形态。

PostgreSQL 保持 17 大版本。17.x 小版本随 dbs-monitor 整包升级；17 到未来大版本的迁移不隐含在普通升级中，必须另开路线并选择 `pg_upgrade` 或 dump/restore。安装器和启动脚本必须拒绝用不匹配的大版本二进制直接启动现有数据目录。

## 2. D2 · 系统级 launchd 管理两个进程

平台机安装两个独立的 `/Library/LaunchDaemons` job：

| Label | 进程 | 运行身份 | 运行策略 |
|---|---|---|---|
| `com.dbs-monitor.postgres` | 随包 PostgreSQL | `_dbsmonitor` | 开机启动，异常退出后重启 |
| `com.dbs-monitor.server` | `dbs-monitor-server` | `_dbsmonitor` | 开机启动，异常退出后重启 |

选择 LaunchDaemon 而不是 LaunchAgent，是因为平台必须在无人登录时启动并持续运行。两个 job 不合并到 shell supervisor 中：`launchd` 是唯一进程管理者，操作员可以分别查看、停止、启动和诊断数据库与 server。生产环境不以终端前台进程、`nohup`、Homebrew services 或第三方进程管理器运行。

`launchd` 不提供这两个普通进程之间的启动顺序契约，因此正确性不得依赖 plist 加载顺序：安装器首次启动时先等待 PostgreSQL ready 再启动 server；正常开机时 server 按既有平台自举约束容忍数据库尚未 ready，并以有上限的退避重试。两个进程都必须响应 `SIGTERM` 并完成有界优雅退出。若 Agent 安装在受支持的 Mac 上，它同样使用独立的系统级 LaunchDaemon，但不安装第二份 PostgreSQL。

## 3. D3 · 文件、数据和权限边界

默认系统级布局如下；可执行文件与可变状态必须分开，以便升级替换程序而不触碰数据：

| 路径 | 所有者 / 模式 | 内容与生命周期 |
|---|---|---|
| `/usr/local/libexec/dbs-monitor/releases/<version>/` | `root:wheel`，运行用户只读 | server、Agent、PostgreSQL 和随包动态库；按版本安装 |
| `/usr/local/libexec/dbs-monitor/current` | `root:wheel` | 指向当前版本的原子切换链接 |
| `/Library/Application Support/dbs-monitor/postgres/` | `_dbsmonitor`，`0700` | PostgreSQL 数据目录；升级与普通卸载均保留 |
| `/Library/Application Support/dbs-monitor/etc/` | `_dbsmonitor`，目录 `0700` | server 配置、凭据 keyring、CA 与服务端私钥；密钥文件 `0600` |
| `/Library/Application Support/dbs-monitor/run/` | `_dbsmonitor`，`0700` | PostgreSQL Unix socket 和 pid 等运行文件 |
| `/Library/Application Support/dbs-monitor/backups/` | `_dbsmonitor`，`0700` | 本机升级前备份集 |
| `/Library/Logs/dbs-monitor/` | `_dbsmonitor`，目录 `0750` | server 与 PostgreSQL 日志 |
| `/Library/LaunchDaemons/com.dbs-monitor.*.plist` | `root:wheel`，`0644` | 系统级 job 定义 |

安装需要管理员权限，以创建专用非登录用户、系统目录和 LaunchDaemon；server 与 PostgreSQL 日常运行不得使用 root。两者继续使用同一个 `_dbsmonitor` 身份，使本地 peer 认证无需数据库密码。自带 PostgreSQL 只监听上述 Unix socket，`listen_addresses` 为空，不占用 5432 或其他 TCP 端口；`pg_hba.conf` 只允许产品需要的本地 peer 映射，其余拒绝。

默认数据目录固定在 Application Support 下。为了延续独立数据盘要求，安装时可以一次性选择另一个本地 APFS 卷上的绝对数据目录；该选择写入受管配置，升级不得重问或迁移它。网络文件系统不进入 v1 支持范围。

## 4. D4 · 安装与首次初始化闭环

正式安装入口必须幂等地区分“全新安装”和“已安装版本”：已安装版本不得重复执行首次初始化；全新安装按以下顺序完成：

1. 接收数据目录和平台对外访问地址；后者用于 TLS 证书 SAN，不允许猜测。
2. 硬检查 macOS 版本、原生 arm64、管理员权限、目标数据盘空间和资产完整性；失败时不创建半套服务。
3. 创建 `_dbsmonitor` 与 §3 的目录和权限，将版本化只读 payload 落盘。
4. 仅在空数据目录执行一次 `initdb`（`UTF8`、`C` locale），写入 socket-only 与 peer 配置；发现未知或不匹配的现有目录必须停止，不得覆盖。
5. 生成并持久保存自签 CA、CA 私钥和服务端证书/私钥；安装 server 与 PostgreSQL plist。
6. 启动 PostgreSQL 并验证 ready，再启动 server；server 自动执行向上 schema 迁移，并仅在不存在管理员时生成一次初始管理员密码。
7. 使用生成的 CA 验证 HTTPS 健康、数据库没有 TCP listener、两个 job 由 `launchd` 托管，然后打印访问地址、本次生成的一次性管理员密码和诊断入口。

整个过程不得从网络下载 Homebrew、PostgreSQL、动态库、Go、Node.js 或前端资源。允许且必须使用 macOS 14 自带的基础系统能力；浏览器、运行期访问远端被监控 PostgreSQL 的网络，以及用户主动把资产传到目标机，不属于“安装时依赖”。

## 5. D5 · 证书与日志

- CA、CA 私钥、服务端私钥和凭据 keyring 都是持久状态，不随版本 payload 替换。升级不得静默重建 CA。
- 服务端证书续期或访问地址变更必须使用原 CA 显式重签并原子替换证书；只有明确执行 CA 轮换时才要求 Agent 重新建立信任。
- 私钥与 keyring 不写日志；一次性管理员密码只允许在首次成功初始化时输出一次。
- server 日志写入 `/Library/Logs/dbs-monitor/server.log`，由产品自管的轮转机制限制大小和保留数；PostgreSQL 使用自身 logging collector 的按大小/时间轮转。轮转后进程必须继续写入当前日志文件，不得要求常驻第三方组件。
- 正式运维入口必须覆盖两个 job 的状态、最近日志、版本、数据目录、socket ready 和 HTTPS health；不得要求操作员进入前台手工运行服务才能诊断。

## 6. D6 · 备份、升级与回滚

v1 的升级备份仍采用“控制面可恢复、时序样本可丢弃”的既有边界：数据库逻辑备份保存升级前的完整 schema 和控制面数据，只排除时序样本数据。备份集还必须包含凭据 keyring、CA/证书及受管配置，否则恢复后的凭据密文或 Agent 信任链不可用。备份目录权限为 `0700`，文件为 `0600`；备份成功且可读取是升级的硬前置条件。

同大版本升级流程固定为：

1. 将新 payload 解包到新的版本目录，不覆盖当前目录；校验版本、架构和完整性。
2. 停 server，保持 PostgreSQL 运行并创建带版本元数据的升级前备份。
3. 若 PostgreSQL 17.x 小版本发生变化，停止 PostgreSQL；原子切换 `current` 后重新启动 PostgreSQL。仅应用升级则不必停止数据库。
4. 启动 server 执行向上迁移，再验证数据库、HTTPS、版本和两个 LaunchDaemon。
5. 验收失败则停止新 server；若切换过 PostgreSQL 二进制，也停止 PostgreSQL。切回旧 payload，确保旧版 PostgreSQL 运行，从升级前备份集恢复后再启动旧 server；不执行 down migration。

恢复必须重建产品数据库后再导入备份，不得把旧数据叠加到已部分迁移的 schema 上。恢复失败时不得启动旧 server，也不得删除升级前备份、旧 payload 或失败现场；诊断并重试恢复后才能恢复服务。

旧 payload 与升级前备份至少保留到本次升级验收完成。PostgreSQL 17.x 的数据格式在小版本间兼容，但切换二进制时仍必须停库；PostgreSQL 大版本升级明确不属于以上流程。

日常手工备份复用同一个受管命令和备份集格式。时序样本不在 v1 自动备份与回滚承诺内，交付文档必须明确升级/回滚窗口可能形成监控数据缺口；需要完整灾备时必须另开范围，不能把文件系统复制冒充在线 PostgreSQL 备份。

## 7. D7 · 卸载默认可恢复

普通卸载执行 `launchctl bootout` 停止两个 job，移除 plist、随 payload 安装的 server 日志轮转配置、`current` 链接和版本化 payload，但保留 PostgreSQL 数据、配置/密钥、备份、日志及 `_dbsmonitor` 用户，并打印这些保留路径。这样重装或人工恢复仍有明确入口。

永久清除必须是单独的显式 purge 动作：再次列出将删除的绝对路径，要求管理员确认，先停止 job，再删除 §3 的可变状态，最后在确认没有残留文件归属后移除专用用户。升级脚本不得调用卸载或 purge；卸载也不得删除所选数据目录以外的父卷内容。

## 8. D8 · 验收与下游移交

干净的受支持 Mac 必须在断网条件下完成安装、首次启动、重启后自启、日志轮转、手工备份、同大版本升级/失败回滚、普通卸载后数据保留和显式 purge。运行验收还必须证明所有随包进程均为原生 arm64、没有 Homebrew/容器/外部 PostgreSQL、PG 无 TCP listener、peer 连接成立、服务不依赖用户登录会话。

| 下游 | 必须承接的内容 |
|---|---|
| #101 macOS 构建、验证与发布路线 | 决定外层资产、签名/公证和 CI；构建并审计原生 PG 17 与非系统动态库，保存上述干净机证据 |
| 后续安装/升级实现票 | 落地目录、专用用户、两个 plist、初始化、备份/恢复、证书续期、日志轮转和卸载/purge 命令 |
| server 运行实现票 | 用 macOS 路径覆盖当前 Linux 默认值，确保开机无顺序依赖、数据库未 ready 时有界退避，并保持 SIGTERM 优雅退出 |
| Agent 交付实现票 | 在受支持 Mac 上使用独立 LaunchDaemon，并复用本票的离线、日志、升级和卸载边界 |

## 9. 事实依据

- Apple 的 [Creating Launch Daemons and Agents](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) 说明系统级 daemon 的 plist 位于 `/Library/LaunchDaemons`，`launchd` 负责启动、管理并在关机时发送 `SIGTERM`；本文据此选择 LaunchDaemon，但重启策略与双进程拆分是产品决策。
- PostgreSQL 17 的 [Peer Authentication](https://www.postgresql.org/docs/17/auth-peer.html) 明确 peer 认证只用于本地连接，并列出 macOS 支持所需的 `getpeereid()` 机制；本文据此保留 socket + peer。
- PostgreSQL 17 的 [Upgrading a PostgreSQL Cluster](https://www.postgresql.org/docs/17/upgrading.html) 明确同一大版本的小版本数据格式兼容，而大版本需要 dump/restore、`pg_upgrade` 或复制迁移；本文据此划分普通升级与未来大版本迁移。
