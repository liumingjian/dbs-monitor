#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
arch=${TARGET_ARCH:-}
unset TARGET_ARCH
version=${VERSION:-dev}
pg_archive=${PG_SOURCE_ARCHIVE:-}
pg_sha256=${PG_SOURCE_SHA256:-}
zlib_prefix=${ZLIB_STATIC_PREFIX:-}
prebuilt_bin_dir=${PACKAGE_BIN_DIR:-}
output=${OUTPUT_DIR:-"$root/dist"}

if [ "$(id -u)" -eq 0 ]; then
  echo "package build must run as a non-root user because PostgreSQL make check rejects root" >&2
  exit 2
fi
case "$arch" in
  amd64|arm64) ;;
  *) echo "TARGET_ARCH must be amd64 or arm64" >&2; exit 2 ;;
esac
if [ "$(uname -s)" != Linux ]; then
  echo "PostgreSQL must be built on native Linux, not cross-built from $(uname -s)" >&2
  exit 2
fi
case "$arch:$(uname -m)" in
  amd64:x86_64) required_glibc=2.17 ;;
  arm64:aarch64) required_glibc=2.28 ;;
  *) echo "TARGET_ARCH=$arch does not match native host $(uname -m)" >&2; exit 2 ;;
esac
host_glibc=$(ldd --version 2>&1 | sed -n '1s/.* //p')
if [ "$host_glibc" != "$required_glibc" ]; then
  echo "native $arch package must be built on glibc $required_glibc exactly (found $host_glibc)" >&2
  exit 2
fi
if [ -z "$pg_archive" ] || [ -z "$pg_sha256" ]; then
  echo "PG_SOURCE_ARCHIVE and PG_SOURCE_SHA256 are required; the build never downloads source implicitly" >&2
  exit 2
fi
if [ -z "$zlib_prefix" ] || [ ! -f "$zlib_prefix/include/zlib.h" ] || [ ! -f "$zlib_prefix/lib/libz.a" ]; then
  echo "ZLIB_STATIC_PREFIX must contain lib/libz.a and include/zlib.h" >&2
  exit 2
fi
printf '%s  %s\n' "$pg_sha256" "$pg_archive" | sha256sum -c -

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
install_prefix=/opt/dbs-monitor/pgsql
mkdir -p "$work/source" "$work/stage" "$work/bundle/bin" "$output"
tar -xf "$pg_archive" -C "$work/source" --strip-components=1
(
  cd "$work/source"
  CPPFLAGS="-I$zlib_prefix/include" LDFLAGS="-L$zlib_prefix/lib" LIBS="$zlib_prefix/lib/libz.a" \
    ./configure --prefix="$install_prefix" --without-icu --without-openssl --without-readline --without-perl --without-python --without-tcl
  # PostgreSQL 17's generated headers must exist before parallel submakes start.
  make -C src/backend generated-headers
  make -j"$(getconf _NPROCESSORS_ONLN)"
  env -u MAKELEVEL make check
  make install DESTDIR="$work/stage"
)

if [ -n "$prebuilt_bin_dir" ]; then
  if [ ! -x "$prebuilt_bin_dir/dbs-monitor-server" ] || [ ! -x "$prebuilt_bin_dir/dbs-monitor-agent" ]; then
    echo "PACKAGE_BIN_DIR must contain executable dbs-monitor-server and dbs-monitor-agent" >&2
    exit 2
  fi
  cp "$prebuilt_bin_dir/dbs-monitor-server" "$prebuilt_bin_dir/dbs-monitor-agent" "$work/bundle/bin/"
else
  cd "$root/web"
  npm ci
  npm run build
  cd "$root"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags embed_web -trimpath -o "$work/bundle/bin/dbs-monitor-server" ./cmd/monitor-server
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$work/bundle/bin/dbs-monitor-agent" ./cmd/monitor-agent
fi
cp -a "$work/stage$install_prefix" "$work/bundle/pgsql"
cp -a packaging/systemd "$work/bundle/systemd"
cp packaging/bundle/install.sh packaging/bundle/README-install.md "$work/bundle/"
printf '%s\n' "$arch" >"$work/bundle/ARCH"
chmod 0755 "$work/bundle/install.sh" "$work/bundle/bin/"*

archive_name="dbs-monitor-$version-linux-$arch.tar.gz"
archive="$output/$archive_name"
tar -C "$work" -czf "$archive" --transform "s,^bundle,dbs-monitor-$version-linux-$arch," bundle
(cd "$output" && sha256sum "$archive_name" >"$archive_name.sha256")
ls -lh "$archive" "$archive.sha256"
