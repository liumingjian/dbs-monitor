#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tool_versions_path=${1:-"$project_root/.tool-versions"}
go_mod_path=${2:-"$project_root/go.mod"}

golang_version=$(awk '$1 == "golang" { print $2; exit }' "$tool_versions_path")
go_toolchain=$(awk '$1 == "toolchain" { sub(/^go/, "", $2); print $2; exit }' "$go_mod_path")

if [ -z "$golang_version" ]; then
	echo "toolchain mismatch: .tool-versions is missing golang" >&2
	exit 1
fi
if [ -z "$go_toolchain" ]; then
	echo "toolchain mismatch: go.mod is missing toolchain" >&2
	exit 1
fi
if [ "$golang_version" != "$go_toolchain" ]; then
	echo "toolchain mismatch: golang .tool-versions=$golang_version, go.mod toolchain=$go_toolchain" >&2
	exit 1
fi
