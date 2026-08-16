# DBS Monitor v1 部署前置条件与运维承诺

本文是面向客户 DBA 与运维团队的正式部署边界。实施前应按本文准备 PostgreSQL、主机目录、配置与备份方案。投产结论由机器生成的验收报告承载，不另建交付前人工勾选表。

## 平台数据库

平台数据库与被监控的 PostgreSQL 实例不是同一个概念。平台数据库必须满足以下条件：

- 客户自管的专属 PostgreSQL 17 实例，支持任意 17.x 小版本；
- 平台独占的 database，编码为 `UTF8`；
- 独立 schema `dbsmon`，不用 `public`；
- 平台角色是 `dbsmon` 的 schema owner；不需要 superuser、`CREATEDB` 或 `CREATEROLE`，零扩展；
- PostgreSQL 服务端启用 TLS，证书 SAN 与连接串主机名匹配；
- `platform_database_url` 同时包含 `search_path=dbsmon` 与 `sslmode=verify-full`，并提供验证服务端证书所需的 CA 信任链。

平台启动时会在迁移前检查主版本、编码、schema owner 的 `CREATE` 权限、schema 洁净度和连接是否实际使用 TLS。任一硬条件不满足都会拒绝启动并指名具体项目。locale 不是 `C`/`POSIX`、时区不是 UTC、同实例存在其他 database 或 PG 小版本落后只会产生告警和 `DEGRADED` 健康状态，不会阻止启动。

一个最小的初始化示例如下。角色密码应由客户的秘密管理流程生成，不要直接沿用示例值。

```sql
CREATE ROLE dbs_monitor LOGIN PASSWORD 'replace-with-customer-secret';
CREATE DATABASE dbs_monitor OWNER dbs_monitor ENCODING 'UTF8' TEMPLATE template0;
\connect dbs_monitor
CREATE SCHEMA dbsmon AUTHORIZATION dbs_monitor;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
```

连接串示例见 [`config/server-minimal.yaml`](../../config/server-minimal.yaml)。主机名必须与数据库证书匹配；不得把 `sslmode` 降为 `verify-ca`、`require`、`prefer` 或 `disable`。

## Server 主机与文件权限

server 以专用非 root 用户 `dbsmon` 运行。安装动作需要 root，常驻进程不需要 root。以下命令创建约定目录；尤其是 keyring 目录必须由 root 预建，server 不会创建父目录，也不会自动修复权限。

```bash
useradd --system --home-dir /opt/dbs-monitor --shell /usr/sbin/nologin dbsmon
install -d -m 0755 -o root -g root /opt/dbs-monitor /opt/dbs-monitor/bin
install -d -m 0750 -o root -g dbsmon /etc/dbs-monitor
install -d -m 0700 -o dbsmon -g dbsmon /etc/dbs-monitor/credentials
install -d -m 0750 -o dbsmon -g dbsmon /var/lib/dbs-monitor/diagnostics
install -d -m 0700 -o dbsmon -g dbsmon /etc/dbs-monitor/tls
```

安装二进制、配置样例和 unit：

```bash
install -m 0755 dist/bin/linux-amd64/dbs-monitor-server /opt/dbs-monitor/bin/dbs-monitor-server
install -m 0600 -o dbsmon -g dbsmon config/server-minimal.yaml /etc/dbs-monitor/config.yaml
install -m 0644 packaging/systemd/dbs-monitor-server.service /etc/systemd/system/dbs-monitor-server.service
```

`/etc/dbs-monitor/config.yaml` 含平台库明文密码，必须属于 `dbsmon:dbsmon` 且为 `0600`。权限过宽时平台会告警并降级健康状态，但不会替客户执行 `chmod 0600`。完整默认值与注释见 [`config/server-full.yaml`](../../config/server-full.yaml)。

unit 还需要 `/etc/dbs-monitor/server.env`。此文件不承载主密钥材料；`PUBLIC_HOST` 必须是客户访问 server 时使用、且将写入 TLS 证书 SAN 的 DNS 名或 IP。

```text
PUBLIC_HOST=monitor.example.com
CERT_DIR=/etc/dbs-monitor/tls
LISTEN_ADDR=:8443
DBS_MONITOR_CONFIG_FILE=/etc/dbs-monitor/config.yaml
# Optional independent overrides; defaults are 80/90/95 with 2 percentage points of hysteresis.
DISK_WARNING_PERCENT=80
DISK_CRITICAL_PERCENT=90
DISK_EMERGENCY_PERCENT=95
DISK_HYSTERESIS_POINTS=2
PLATFORM_DATABASE_CAPACITY_WARNING_PERCENT=80
PLATFORM_DATABASE_CAPACITY_CRITICAL_PERCENT=90
PLATFORM_DATABASE_CAPACITY_EMERGENCY_PERCENT=95
PLATFORM_DATABASE_CAPACITY_HYSTERESIS_POINTS=2
```

```bash
chown root:dbsmon /etc/dbs-monitor/server.env
chmod 0640 /etc/dbs-monitor/server.env
systemctl daemon-reload
systemctl enable --now dbs-monitor-server.service
```

首次启动仅在 keyring 中没有任何版本化密钥、平台库中没有任何密文行、且目录存在可写时生成 `master-key-v1`。主密钥材料不得放入环境变量、命令行、数据库或日志。

## 换机恢复与 keyring 搬迁

数据库备份和 keyring 是两个独立制品，但必须标记为同一个恢复点。换机恢复按以下顺序执行：

1. 停止旧 server，完成平台数据库备份，并单独复制整个 `/etc/dbs-monitor/credentials/`；不要把 keyring 打进数据库备份。
2. 在新机安装同一候选或目标版本的 server，先由 root 执行 `install -d -m 0700 -o dbsmon -g dbsmon /etc/dbs-monitor/credentials`。
3. 把与该数据库备份匹配的 `current` 和全部 `master-key-v*` 安全搬到新目录，不得启动 server 生成替代密钥。
4. 执行 `chown -R dbsmon:dbsmon /etc/dbs-monitor/credentials`、`chmod 0700 /etc/dbs-monitor/credentials` 和 `chmod 0600 /etc/dbs-monitor/credentials/*`。
5. 将数据库恢复到新的空 PostgreSQL 17 实例，更新 `platform_database_url`，确认 CA、主机名、`dbsmon` 与 `sslmode=verify-full` 正确后再启动 server。
6. 登录后检查平台健康、凭据连接测试和采集恢复情况；保留旧机与原备份，直到恢复验证完成。

keyring 遗失、版本不匹配或文件损坏时，已有密文数据不可恢复，平台无后门，只能重新录入全部受保护凭据。不要用新生成的 keyring 覆盖故障现场。

## 客户责任清单（十二条）

1. 备份频率、保留与验证由客户决定并执行；平台不做任何自动备份。
2. keyring 必须与平台数据库分开备份；遗失即密文数据不可恢复，平台无后门。
3. 恢复顺序由客户执行：先灌库，再放回与备份匹配的 keyring、恢复属主与 `0600`，最后启动 server。
4. 回滚由客户执行：装回旧二进制并恢复升级前备份；不存在 down 迁移。
5. PostgreSQL 大版本升级是客户的独立工程，不包含在平台启动时执行的 schema 迁移中。
6. 客户按容量基线准备磁盘：30 天全量 30 个分区的实测约为 49.1 GB，并为增长、备份和维护操作留出余量。
7. 客户在升级前手工备份控制面；这不是平台会自动执行的动作。
8. 客户提供专属 PostgreSQL 17 实例（任意 17.x 小版本）；平台只能验证到 database 级，实例级独占性由客户保证。
9. 客户负责 PostgreSQL 实例的 initdb 参数、备份、监控、补丁与升级节奏；平台只观察并报告编码、locale 与时区。
10. 客户保证 PostgreSQL 主机可用空间；外部数据库形态下平台无法可靠检查主机剩余空间。
11. 升级到 PG 18+ 会导致平台拒绝启动。这是版本硬门，不是“建议不要升级”，也没有跳过开关。
12. 客户创建的最小权限平台角色只需成为 `dbsmon` 的 schema owner；平台不需要 superuser，也不安装任何 PostgreSQL 扩展。

## 升级与降级承诺（四条）

1. v1.x 内任意版本可直接升级到最新 v1.x；跨 MAJOR 不承诺跳版，必须逐 MAJOR 升级。
2. 降级不受支持。唯一回退路径是装回旧二进制并恢复升级前备份；未备份即升级 = 不可回退。
3. 版本偏斜期间先升级 server，再升级 Agent。旧 Agent 在一个 MAJOR 兼容窗口内继续工作并告警；反序先升级 Agent 不在承诺范围内。
4. v1.0.0 是首发版本，不存在从 0.x 升级的路径；v1 之前的 walking skeleton 部署必须重装。

客户计划升级平台数据库大版本时，还必须先停止平台 server。升级后主版本只要离开 17，平台就会直接拒绝启动。
