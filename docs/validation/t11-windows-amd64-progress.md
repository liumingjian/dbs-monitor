# T11 Windows amd64 验证进度

> 适用范围：[T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29)。
> 路线地图：[Wayfinder 地图 · R2 系统架构骨架与技术选型](https://github.com/liumingjian/dbs-monitor/issues/15)。
> 更新时间：2026-08-04（Asia/Shanghai）。

## 结论

当前 Windows 环境只完成了前端和环境诊断；后续原生 Linux amd64 验证已在提交 [e17a81d](https://github.com/liumingjian/dbs-monitor/commit/e17a81d) 完成 T11 验收。升级/回滚和 PG13–17 矩阵已正式延期到 R3，详细结论见 [Linux 验证记录](t11-linux-amd64-progress.md) 和 [T11 resolution 评论](https://github.com/liumingjian/dbs-monitor/issues/29)。

后续目标环境应是原生 Linux amd64。当前 Windows 的 `PROCESSOR_ARCHITECTURE=AMD64` 只证明 x86-64/amd64 ABI；虚拟 CPU 报告为 Intel Broadwell，因此不应将本次结果称为 AMD 实体 CPU 验证。

**范围决定：Windows 支持与真实 AMD 物理 CPU 验证均放弃，不是递延。** 本路线的交付目标是原生 Linux amd64 / arm64；Windows 环境不进入支持矩阵，虚拟化 Intel CPU 也不替代真实 AMD 物理机证据。若未来要支持 Windows 或要求 AMD 物理机认证，须另开路线与验收标准。

## 当前代码状态

- 分支：`feat/t11-walking-skeleton`。
- 基线提交：[ec3a317 · 实现 T11 walking skeleton 并完成 ARM 验证](https://github.com/liumingjian/dbs-monitor/commit/ec3a317ba3b286b608ed47cd1e6ab731605d645c)。
- 工作区在本次记录时干净；没有修改生成物或业务代码。

## Windows 已通过

- Windows Server 2019 Datacenter，build 17763，x64。
- Node.js `v24.19.0`、npm `11.17.0`。
- `npm ci` 使用锁文件完成；下载通过本机代理进行。
- Vitest：5 个测试文件、21 个测试全部通过。
- TypeScript typecheck 通过。
- ESLint 通过。
- Vite production build 通过；仅有既有的 large chunk warning。
- `git diff --check` 通过。

## Windows 未完成与原因

- `make check`、`go vet ./...`、`go test ./...`、`make gen`、服务端 E2E 和 RT-C 尚未执行。
- Go、GNU Make、Docker/Compose 和本地 PostgreSQL 均未成为当前 shell 可用工具。
- Go 1.23.0 Windows amd64 便携包经代理下载多次超时；临时归档最新只有 36,155,392 / 81,889,852 字节，没有可用的 `go.exe`。
- Docker Desktop 4.85.0 安装器在 Windows Server 2019 上失败。安装日志位于系统的 `C:\ProgramData\DockerDesktop\install-log-admin.txt`，失败原因为该 Windows 版本不在 Docker Desktop 支持的 Windows 10/11 版本范围内。
- Redocly/openapi-typescript 的首次 npx 下载也因代理下载链路超时而停止；仓库内已有生成文件未被覆盖。

## Linux amd64 后续清单（历史交接清单，已完成）

以下清单是 Windows 环境交接时的原始计划；原生 Linux amd64 已完成其中的 T11 范围。升级/回滚脚本与 PG13–17 矩阵不在 T11 范围内，按上述决定延期到 R3。

在原生 Linux amd64 机器上继续，不要用 qemu 代替原生 PG 构建：

1. 阅读 `docs/design/09-packaging-and-deployment.md`、`docs/design/10-ai-guardrails-and-verification.md` 和 `docs/design/11-walking-skeleton-slice.md`，确认验收边界。
2. 确认 Go 1.23+、GNU Make、Node/npm、Docker/Compose 或可用的 PostgreSQL 17，以及 `curl`、`psql`、`python3` 等脚本依赖。
3. 使用真 PostgreSQL 17 启动平台库，执行 `make gen`，确认跨文件 `$ref`、oapi-codegen、openapi-typescript、sqlc 生成物和漂移门全部通过。
4. 执行 `make check`，记录完整耗时；按 T9 要求，未全绿不能宣称 T11 完成。
5. 执行 `make check-full`，覆盖 T11 范围内的真实构建、Playwright smoke 和 Linux amd64/arm64 交叉编译；PG13–17 矩阵与生命周期发布验证延期到 R3。
6. 在原生 Linux amd64 上执行 `make package-binaries-linux-amd64` 和 `make package-linux-amd64`，验证离线包和首启；升级/回滚脚本延期到 R3。打包脚本要求非 root 构建用户。
7. 按 `scripts/rt-c/README.md` 准备一次 disposable PostgreSQL 17 RT-C 环境，执行 T1 查询延迟、T2 分区容量、T3 控制面劣化和缺失分区 SQLSTATE 实测；不要用小样本结果替代参考基线。
8. 将命令、版本、耗时、P95、磁盘占用、归档路径和失败日志追加到 [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29)；T11 范围已完成，R3 延期项单独记录。

## 参考

- [T11 · Walking skeleton 实现](https://github.com/liumingjian/dbs-monitor/issues/29)
- [T10 · Walking skeleton 切片定义与验收标准](https://github.com/liumingjian/dbs-monitor/issues/28)
- [T9 · AI 开发护栏与验证闭环](https://github.com/liumingjian/dbs-monitor/issues/27)
- [`Makefile`](../../Makefile)
- [`scripts/rt-c/README.md`](../../scripts/rt-c/README.md)
