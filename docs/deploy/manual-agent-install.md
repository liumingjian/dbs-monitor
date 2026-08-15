# Agent 手工分发 plan B

实例“接入设置”页生成的安装命令和平台下载端点是主线。本流程只用于网络隔离、下载端点被策略阻断或必须经客户内部制品库分发的环境。Agent 不自升级；升级仍是替换二进制并重启 unit 的显式运维动作。

## 六项备料

1. 两架构 Agent 二进制：`linux/amd64` 的 `dbs-monitor-agent-linux-amd64` 与 `linux/arm64` 的 `dbs-monitor-agent-linux-arm64`；目标主机只安装与 `uname -m` 匹配的一份。
2. `SHA256SUMS`：由交付团队的受控构建机生成，覆盖上述二进制。
3. CA 证书文件：从本次部署运行中的 server 取出 `ca.crt`，经批准的带外通道交给目标主机。
4. 配置样例：包含 `SERVER_URL`、`INSTANCE_ID`、`AGENT_TOKEN_FILE` 与 `CA_FILE`，令牌材料本身不写入配置。
5. unit 模板：仓库中的 [`packaging/systemd/dbs-monitor-agent.service`](../../packaging/systemd/dbs-monitor-agent.service)。
6. 手工步骤：本页，包括校验和、CA 指纹、专用用户、令牌权限、启动与验证命令。

CA 证书不进交付物。它是每套部署运行期生成的实例私有信任根；规范层交付物只包含取用和校验方法，不得把某套环境的 CA 随仓库、tag 或通用二进制发布。plan B 现场备料时，必须从目标部署取得 CA。

## 在交付构建机上准备

按 [`build.md`](build.md) 从指定 SHA 构建。把 `dist/plan-b/` 中的两架构 Agent 和统一 `SHA256SUMS`，连同 unit 模板和本文送入客户批准的制品通道。复制前先在构建目录校验：

```bash
(cd dist/plan-b && sha256sum -c SHA256SUMS)
```

从运行中的目标 server 取得 `/etc/dbs-monitor/tls/ca.crt`。在接入设置页记录该 CA 的 DER SHA-256 指纹，并通过独立通道交给实施人员。不得使用 `curl -k`、`--insecure` 或任何跳过证书校验的开关。

## 在 Agent 主机上安装

以下手工步骤需要 root；安装后的常驻进程使用专用非 root 用户。即“装要 root，跑不要 root”。

1. 核对主机架构，选择匹配文件，并在改名或安装前使用随构建产物提供的清单验证它。

   ```bash
   uname -m
   sha256sum -c SHA256SUMS
   ```

2. 创建运行用户和目录，安装匹配的二进制。amd64 示例如下；arm64 主机将源文件名替换为 `dbs-monitor-agent-linux-arm64`。

   ```bash
   useradd --system --home-dir /opt/dbs-monitor-agent --shell /usr/sbin/nologin dbs-monitor-agent
   install -d -m 0755 -o root -g root /opt/dbs-monitor-agent /opt/dbs-monitor-agent/bin
   install -d -m 0750 -o root -g dbs-monitor-agent /etc/dbs-monitor-agent
   install -m 0755 dbs-monitor-agent-linux-amd64 /opt/dbs-monitor-agent/bin/dbs-monitor-agent
   ```

3. 安装现场取得的 CA，并核对 DER 指纹与接入设置页显示值完全一致。

   ```bash
   install -m 0644 ca.crt /etc/dbs-monitor-agent/ca.crt
   openssl x509 -in /etc/dbs-monitor-agent/ca.crt -outform DER | sha256sum
   ```

   指纹不一致时立即停止，不得降级 TLS 校验。

4. 在平台接入设置页登记 Agent，取得一次性令牌。通过秘密文件通道把令牌写入 `/etc/dbs-monitor-agent/token`，属于 `dbs-monitor-agent:dbs-monitor-agent`，权限为 `0600`。令牌不得放在命令行参数、`agent.env`、日志或诊断包中。

   ```bash
   install -m 0600 -o dbs-monitor-agent -g dbs-monitor-agent /secure/path/agent-token /etc/dbs-monitor-agent/token
   ```

5. 创建 `/etc/dbs-monitor-agent/agent.env`。`SERVER_URL` 必须使用 HTTPS，主机名须能被 CA 验证；`INSTANCE_ID` 使用接入设置页给出的 UUID。

   ```text
   SERVER_URL=https://monitor.example.com:8443
   INSTANCE_ID=00000000-0000-0000-0000-000000000000
   AGENT_TOKEN_FILE=/etc/dbs-monitor-agent/token
   CA_FILE=/etc/dbs-monitor-agent/ca.crt
   ```

   ```bash
   chown root:dbs-monitor-agent /etc/dbs-monitor-agent/agent.env
   chmod 0640 /etc/dbs-monitor-agent/agent.env
   ```

6. 安装 unit，启动并检查状态。

   ```bash
   install -m 0644 dbs-monitor-agent.service /etc/systemd/system/dbs-monitor-agent.service
   systemctl daemon-reload
   systemctl enable --now dbs-monitor-agent.service
   systemctl status dbs-monitor-agent.service
   journalctl -u dbs-monitor-agent.service --since today
   ```

Agent 主机与平台时钟偏差超过 5 秒时安装或启动必须失败。启动成功后，在接入设置页确认 Agent 已上报；不要用补 0 或手工写库掩盖缺数。

## 手工升级

先升级 server，再对每台 Agent 重复二进制校验与安装动作，最后执行 `systemctl restart dbs-monitor-agent.service`。不要修改 token、CA 或 `INSTANCE_ID`，除非平台上明确执行了轮换或重新登记。版本超出一个 MAJOR 兼容窗口时，server 会明确拒收该次上报。
