# Linux package build

Run each target as a non-root build user on a native Linux builder matching the delivery baseline; PostgreSQL is never QEMU-cross-built. PostgreSQL's own `make check` rejects root, and the packaging script enforces that requirement before starting the expensive build.

| Package | Native builder | glibc baseline |
|---|---|---|
| `amd64` | `x86_64` | 2.17 |
| `arm64` | `aarch64` | 2.28 |

The builder's glibc version must equal the baseline exactly so a newer host cannot silently raise the package floor. `ZLIB_STATIC_PREFIX` must contain `include/zlib.h` and `lib/libz.a`; PostgreSQL is linked against that archive.

The PostgreSQL source build is deliberately offline and audit-friendly. Supply a PostgreSQL 17 source archive and its independently recorded SHA256; the script never downloads source. On builders where current Node.js or Go cannot run on the delivery glibc baseline, build the static application binaries separately and pass them through `PACKAGE_BIN_DIR`:

```sh
make package-binaries-linux-arm64

VERSION=0.1.0 \
PG_SOURCE_ARCHIVE=/build-input/postgresql-17.x.tar.bz2 \
PG_SOURCE_SHA256='<sha256>' \
ZLIB_STATIC_PREFIX=/build-input/zlib-static \
PACKAGE_BIN_DIR="$PWD/dist/bin/linux-arm64" \
make package-linux-arm64
```

Without `PACKAGE_BIN_DIR`, the packaging script runs `npm ci`, builds the embedded web application, and builds both Go binaries on the native builder. Dependency caches or an approved dependency-fetching stage must already be available; the script itself only mechanically guarantees that PostgreSQL source is never downloaded implicitly.

A release is not accepted from package construction alone. Extract it on a clean, offline, systemd-based VM and follow `README-install.md`; record native architecture/glibc, `ldd` output for PostgreSQL binaries, PostgreSQL test output, tar/server sizes, socket/peer checks, absence of a PostgreSQL TCP listener, HTTPS verification with the generated CA, and one-time administrator-password behavior.
