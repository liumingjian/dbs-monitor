# 从指定 SHA 构建交付二进制

本仓库公开，v1 不发布二进制，也不产出 GitHub Release assets。交付团队在自己的受控构建机上，从指定的 40 位 commit SHA 构建产物，并把该 SHA 作为候选身份留档。

本流程承诺：给定 SHA 和仓库 `.tool-versions` 中的精确 Go、Node.js 版本，可以构建出功能等价的产物。由于前端依赖和构建环境尚未形成完整的可复现构建链，本项目不承诺 bit-for-bit 一致；不同构建机生成的 SHA-256 可以不同。

## 构建机准备

构建机需要 Git、`sha256sum`，以及能读取 `.tool-versions` 的工具链管理器。以下命令以 `asdf` 为例；使用其他管理器时，仍须安装文件中记录的精确版本。

```bash
git clone https://github.com/liumingjian/dbs-monitor.git
cd dbs-monitor
git fetch origin
git checkout --detach <40位候选SHA>
test "$(git rev-parse HEAD)" = "<40位候选SHA>"
test -z "$(git status --porcelain)"

cat .tool-versions
asdf install
go version
node --version

cd web
npm ci
cd ..
make build
```

`make build` 先构建前端，再把前端静态资源嵌入 `dbs-monitor-server`，同时构建同源的 `dbs-monitor-agent`。两个文件输出到仓库根目录，适用于构建机当前的操作系统和架构。

## 构建 Linux 交付目录

下列命令先构建一次前端，再显式生成 Linux 二进制。`linux/amd64` 是 v1 的正式投产目标；`linux/arm64` Agent 作为手工分发 plan B 的跨架构备料构建，不替代正式环境验收。

```bash
candidate_sha=$(git rev-parse HEAD)
candidate_tag=$(git describe --exact-match HEAD 2>/dev/null || true)
if [ -n "$candidate_tag" ]; then
  build_version=${candidate_tag#v}
else
  build_version=0.0.0-dev+$candidate_sha
fi
build_ldflags="-X main.version=$build_version -X main.commitSHA=$candidate_sha"

(cd web && npm run build)

install -d dist/bin/linux-amd64 dist/bin/linux-arm64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$build_ldflags" -tags embed_web \
  -o dist/bin/linux-amd64/dbs-monitor-server ./cmd/monitor-server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$build_ldflags" \
  -o dist/bin/linux-amd64/dbs-monitor-agent-linux-amd64 ./cmd/monitor-agent
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$build_ldflags" \
  -o dist/bin/linux-arm64/dbs-monitor-agent-linux-arm64 ./cmd/monitor-agent

(cd dist/bin/linux-amd64 && sha256sum dbs-monitor-server dbs-monitor-agent-linux-amd64 > SHA256SUMS)
(cd dist/bin/linux-arm64 && sha256sum dbs-monitor-agent-linux-arm64 > SHA256SUMS)

install -d dist/plan-b
cp dist/bin/linux-amd64/dbs-monitor-agent-linux-amd64 dist/plan-b/
cp dist/bin/linux-arm64/dbs-monitor-agent-linux-arm64 dist/plan-b/
(cd dist/plan-b && sha256sum dbs-monitor-agent-linux-amd64 dbs-monitor-agent-linux-arm64 > SHA256SUMS)
```

在 `linux/amd64` 主机上核对候选身份；arm64 文件的运行验证只能在 arm64 主机上完成。

```bash
dist/bin/linux-amd64/dbs-monitor-server --version
dist/bin/linux-amd64/dbs-monitor-agent-linux-amd64 --version
(cd dist/bin/linux-amd64 && sha256sum -c SHA256SUMS)
(cd dist/bin/linux-arm64 && sha256sum -c SHA256SUMS)
(cd dist/plan-b && sha256sum -c SHA256SUMS)
```

版本输出必须包含指定的 40 位候选 SHA。`SHA256SUMS` 是本次构建的副产物，信任根在交付团队的构建机；它只证明手工分发的文件来自同一次受控构建，不证明不同构建机能生成逐字节相同的文件。
