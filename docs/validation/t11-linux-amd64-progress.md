# T11 native Linux amd64 validation

Status: **T11 ACCEPTANCE COMPLETE**. The deterministic checks, native amd64 package build, default RT-C reproduction, and clean systemd first-start validation pass. Upgrade/rollback and the PG13–17 matrix are formally deferred to the R3 release gate.

Validation date: 2026-08-04 (Asia/Shanghai)
Repository revision: `e17a81d`

## Host and tools

The host is native Linux amd64, not a Windows x64 result or an emulated PostgreSQL run.

| Item | Observed value |
|---|---|
| OS | Kylin Linux Advanced Server V10 (Sword) |
| Kernel / machine | `4.19.90-24.4.v2101.ky10.x86_64`, `x86_64` |
| Host glibc | 2.28 |
| Go / Make | go1.25.5 linux/amd64 / GNU Make 4.3 |
| Node / npm | v24.19.0 / 11.17.0 |
| Docker / Compose | 29.6.2 / v2.25.0 |
| curl / Python | 7.71.1 / 3.13.7 |
| Host psql / pgbench | PostgreSQL client 10.5 |
| Development PostgreSQL | `postgres:17`, server 17.10, port 55432 |
| Package PostgreSQL builder | same-architecture `centos:7`, x86_64, glibc 2.17 |
| Root filesystem after expansion | 596 GiB total, 480 GiB free at the validation start |
| Init / service manager | systemd PID 1; `systemctl is-system-running` reported `degraded` because of unrelated host units |

The package builder image reported `arch=amd64`, and its container reported `uname -m=x86_64`. No qemu was used. The package build ran as non-root user `t11builder`.

## Generation and checks

The development PostgreSQL container was healthy before running the checks. Dependency caches were relocated to `/tmp` because the host root filesystem was nearly full.

| Command | Result | Elapsed |
|---|---|---:|
| `npm ci` in `web/` | pass, 342 packages | 15.625 s |
| `make gen` | pass: Redocly bundle, oapi-codegen server/client, openapi-typescript, sqlc | 7m13.366s |
| `sh scripts/check-generated.sh` | pass, no generated drift | 22.891 s |
| `make check` | pass: vet, Go tests, typecheck, ESLint, 21 Vitest tests | 1m54.378s |
| `make check-full` | pass: check, build, Playwright 1/1, amd64 and arm64 cross-builds | 1m20.510s |

> R2 收口复测（2026-08-05，本机 macOS arm64，缓存已热）：`make check` pass，real 29.35s。该行只用于当前护栏预算观察，不作为原生 Linux amd64 验收证据。

The production build emitted the existing large JavaScript chunk warning (1,578.79 kB, gzip 518.75 kB); it did not fail the build. The final E2E run used the installed Playwright Chromium 1234 browser.

The first `make check-full` attempt failed after 4m08.509s because the host `HTTPS_PROXY` intercepted the readiness request to `127.0.0.1`, and the required Playwright browser was not yet installed. The readiness probe now uses `curl --noproxy '*'`; the final check-full run is green with the proxy variables still present. This is the only tracked E2E fix in this validation.

## Native amd64 package

以下命令记录当时实际使用的 target 名称；[#102 处置](../design/superseded/21-v1-linux-release-disposition.md) 后，对应手动入口已改名为 `legacy-package-binaries-linux-amd64` 与 `legacy-package-linux-amd64`，且不再属于 v1 验收。

The standalone application binaries were built as the non-root `t11builder` user on the native host:

```text
make package-binaries-linux-amd64
elapsed: 1m14.443s
```

Artifacts in `/tmp/t11-package-work/dist/bin/linux-amd64/`:

| File | Size | SHA256 |
|---|---:|---|
| `dbs-monitor-server` | 18 MB | `3de0e4846638b6c6df1a3b5f134fd4e7c933fbd120396bb920778bbbfe7411d1` |
| `dbs-monitor-agent` | 8.5 MB | `58d50665e7fcd6e3939a16be9132d65a0da08fe9c23468cbf117c218252de3f8` |

Both are ELF x86-64, statically linked. Running `make package-linux-amd64` directly on the host was rejected as intended: `native amd64 package must be built on glibc 2.17 exactly (found 2.28)`.

The actual PostgreSQL package was built in the same-architecture CentOS 7 builder with PostgreSQL 17.6 source. The offline inputs were:

```text
PG_SOURCE_ARCHIVE=/inputs/postgresql-17.6.tar.bz2
PG_SOURCE_SHA256=e0630a3600aea27511715563259ec2111cd5f4353a4b040e0be827f94cd7a8b0
```

The package target completed as a non-root user in `2:22.08`. PostgreSQL `make check` reported all 222 regression tests passed. The target required two packaging fixes discovered by the native run:

1. PostgreSQL 17 generated headers are materialized with `make -C src/backend generated-headers` before the parallel build.
2. PostgreSQL check is invoked with `env -u MAKELEVEL make check` so the outer repository make does not suppress PostgreSQL's temporary test install.

The resulting archive is `/tmp/t11-package-work/dist/dbs-monitor-0.1.0-linux-amd64.tar.gz`:

```text
size: 24,025,391 bytes
sha256: 881d4b1e6eefa1e914c1dfdc78949b7461948a2ca3c9059938b2f5c0fc39a79b
```

The archive contains `ARCH=amd64`, the two static application binaries, PostgreSQL 17.6, `install.sh`, two systemd units, and `README-install.md`. The bundled PostgreSQL `postgres` is ELF x86-64, glibc 2.17-compatible, and dynamically links only the standard glibc 2.17-era libraries. The extracted bundle is 59 MB; the archive listing is available from the command log at `/tmp/t11-package-linux-amd64.log`.

## Installer rehearsal

The earlier installer rehearsal in the CentOS 7 builder used `--force` because that disposable filesystem was intentionally far below the production 200 GB requirement. The native host run used the expanded root filesystem and the normal installer path. The supplied first-start inputs were a disposable data directory and `127.0.0.1` as the public host. `initdb` completed with PostgreSQL 17.6 and the installer wrote:

- `listen_addresses = ''`;
- the Unix socket directory `/opt/dbs-monitor/run`;
- local `peer` authentication;
- TCP `reject` rules;
- `DATABASE_URL`, `PUBLIC_HOST`, and `CERT_DIR` in the environment file.

The native host first run created and started both systemd units, but its final readiness curl inherited the host `HTTPS_PROXY` and did not bypass `127.0.0.1`; the services were active, while the installer itself eventually timed out. A clean second run used `NO_PROXY='*' no_proxy='*'` and exited `0` without `--force`. It passed the 200 GB disk gate, started both units as `enabled` and `active`, passed HTTPS readiness with the generated CA, ran two migrations, seeded one administrator, and connected through the PostgreSQL Unix socket. PostgreSQL exposed no TCP listener; only the platform HTTPS listener on port 8443 was present. After restarting the server, the one-time-password journal count remained one. The generated administrator password was intentionally not recorded.

Evidence logs: `/tmp/t11-installer-partial.log` (builder rehearsal), `/tmp/t11-installer-host-20260804.log` (native host proxy-sensitive first run), and `/tmp/t11-installer-host-noproxy-20260805.log` (native host successful run). The extracted bundle used for the native run was `/tmp/t11-package-inspect-host-20260804/dbs-monitor-0.1.0-linux-amd64`.

The repository has no `upgrade.sh`, rollback script, or upgrade/rollback entries in the bundle. This is now an explicit scope boundary: the T11 archive is a walking-skeleton package, while the upgrade/backup/rollback lifecycle is an R3 release gate. The PG13–17 matrix is likewise an R3 release gate; T11 does not claim either as completed evidence.

## RT-C reference reproduction

A disposable, migrated `t11_rt_c` database was created on the PostgreSQL 17.10 container. The first post-expansion attempt used the existing development container and the exact reference script, but a PostgreSQL backend exited with code 2 during day 19 after 36:47.37. The container recovered, reported `OOMKilled=false`, and still had about 480 GiB free; no row or threshold evidence from that partial run was accepted. Its log is `/tmp/t11-rt-c-full.log`.

The root cause of that first day-19 backend exit code 2 was not isolated. It remains a known unresolved RT-C incident; the clean same-architecture retry below is the accepted evidence, and does not retroactively explain the first failure.

A clean retry used a same-architecture `postgres:17` container with a 1 GiB shared-memory mount, no qemu, a fresh migrated database, and no diagnostic SQL during loading. The exact reference script was run with `RT_C_CONFIRM=load-450m-points` and `RT_C_DATA_PATH` equal to the server's reported `/var/lib/postgresql/data`. The run started at `2026-08-04T14:59:06Z`, completed in `1:04:19`, and exited `0`. Its immutable result directory is `/tmp/t11-rt-c-results-20260804-isolated`; the host log and timing files are `/tmp/t11-rt-c-isolated-20260804.log` and `/tmp/t11-rt-c-isolated-time-20260804.txt`.

The earlier pre-expansion attempt refused before loading any rows:

```text
RT-C data filesystem has less than 200000000000 bytes free
rt_c_exit=2
```

Measured preflight values for that earlier attempt were PostgreSQL 17.10 (`server_version_num=170010`) and 348,360 KiB available on the actual data filesystem. The database remained at zero `metric_sample` and zero `metric_series` rows and was then dropped.

The successful expanded-disk result recorded `453600000` points, `1750` series, and `30` partitions. Query samples were 100 with P95 `25.379 ms` (threshold `500 ms`, PASS). The 30 partitions totaled `49,112,432,640` bytes, or `24.556%` of the 200,000,000,000-byte delivery disk (thresholds `100,000,000,000` bytes and `30%`, PASS). The 1,000 control transactions measured P95 `4.869 ms` at baseline and `7.844 ms` under the independent 120-second write pressure, a `1.611x` ratio (threshold `2x`, PASS). The missing-partition SQLSTATE was `23514` (PASS). PostgreSQL was 17.10 (`server_version_num=170010`); the pre-load filesystem evidence recorded 531,476,184 KiB available. The complete raw evidence includes `summary.json`, `counts.csv`, `capacity.csv`, `query-explain.txt`, pgbench logs, PostgreSQL settings, and the manifest in the result directory above.

## Acceptance status

| Requirement | Status |
|---|---|
| Native Linux amd64 tools and real PostgreSQL 17 | pass |
| `make gen` and generated-file drift gate | pass |
| Measured `make check` | pass |
| Measured `make check-full` | pass |
| Non-root amd64 application binaries | pass |
| Native glibc 2.17 PostgreSQL build, tests, and offline archive | pass |
| Clean systemd install and first-start end to end | pass on native host with expanded disk and localhost proxy bypass; first proxy-sensitive invocation timed out after services started |
| Upgrade and rollback scripts/rehearsal | deferred to R3 by scope decision; not a T11 acceptance item |
| Default RT-C reproduction and all four evidence groups | pass: full 453.6M-point run and threshold evidence |
| PG13–17 monitored-database integration matrix | deferred to R3 release gate; not a T11 acceptance item |

T11 acceptance is complete. Resolve issue 29 with the R3 deferrals above recorded explicitly; the native default RT-C thresholds are evidenced and must not be rerun with reduced parameters.
