#!/bin/sh
set -eu

# 代码生成必须是幂等的：跑一遍 `make gen`，工作树不该有任何变化。
#
# 判断变化不走 `git diff`：这个脚本也要能在没有 .git 的目录里跑通——rexec 把代码 rsync
# 到 mac 上时不带仓库，那里 `git diff` 只会报 "not a git repository" 并以 129 退出，
# 于是整条 `make check` 在第二步就断了。改成自己给文件树做前后两次校验和快照。
# 两次快照之间只跑 `make gen`，所以任何差异都是它造成的。

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

if command -v sha256sum >/dev/null 2>&1; then
	sum='sha256sum'
else
	sum='shasum -a 256' # macOS 自带的是这个
fi

# 只跳过不属于源码的目录；剩下的全都进快照，免得漏掉某个新增的生成物。
snapshot() {
	find . \
		\( -name .git -o -name node_modules -o -name dist -o -name .venv -o -name results \) -prune -o \
		-type f -print0 \
		| xargs -0 $sum \
		| LC_ALL=C sort
}

before=$(mktemp)
after=$(mktemp)
trap 'rm -f "$before" "$after"' EXIT

snapshot > "$before"
make gen
snapshot > "$after"

if ! diff "$before" "$after" > /dev/null; then
	echo "generated files are out of date; run 'make gen' and commit the result:" >&2
	diff "$before" "$after" | sed -n 's/^[<>] [0-9a-f]\{64\}  //p' | LC_ALL=C sort -u >&2
	exit 1
fi
