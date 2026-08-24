#!/bin/sh
set -eu

# 决策文档卫生检查。见 docs/design/README.md。
# 五条不变式，全部可执行；违反即红。

fail=0
report() {
	fail=1
	printf 'check-docs: %s\n' "$1" >&2
}

live_docs() {
	find docs/design -maxdepth 1 -name '[0-9][0-9]-*.md' | sort
}

dead_docs() {
	find docs/design/superseded -maxdepth 1 -name '[0-9][0-9]-*.md' 2>/dev/null | sort
}

frontmatter() {
	awk 'NR==1 && $0!="---" {exit} NR>1 && $0=="---" {exit} NR>1' "$1"
}

# 生成物用 Go 惯例的首行标记自报身份，不需要也不该带手写 frontmatter
is_generated() {
	head -1 "$1" | grep -q 'Code generated'
}

# 1 · 每份决策文档都带机器可读的 status
for f in $(live_docs) $(dead_docs); do
	is_generated "$f" && continue
	status=$(frontmatter "$f" | sed -n 's/^status:[[:space:]]*//p' | head -1)
	case "$status" in
	active | partially-superseded | superseded | historical) ;;
	'') report "$f 缺少 frontmatter status；决策文档必须自报死活" ;;
	*) report "$f status 取值非法: '$status'" ;;
	esac
done

# 2 · superseded/ 下的文档必须指名推翻者，且不得留在顶层
for f in $(dead_docs); do
	status=$(frontmatter "$f" | sed -n 's/^status:[[:space:]]*//p' | head -1)
	[ "$status" = superseded ] ||
		report "$f 在 superseded/ 下却标 status: $status"
	frontmatter "$f" | grep -q '^superseded_by:[[:space:]]*[^[:space:]]' ||
		report "$f 缺少 superseded_by；作废必须指名推翻它的文档"
done
for f in $(live_docs); do
	status=$(frontmatter "$f" | sed -n 's/^status:[[:space:]]*//p' | head -1)
	[ "$status" != superseded ] ||
		report "$f 标为 superseded 却仍在 docs/design/ 顶层，应移入 superseded/"
done

# 3 · 活文档不得把读者指向已作废文档
for f in $(live_docs) CLAUDE.md docs/README.md docs/agents/domain.md; do
	[ -f "$f" ] || continue
	# 只看真正的 markdown 链接目标，散文里提到 superseded/ 不算
	hits=$(grep -n '](\([^)]*/\)\?superseded/' "$f" | grep -cv 'allow-superseded-link' || true)
	if [ "${hits:-0}" -gt 0 ]; then
		report "$f 有 $hits 处链接指向 superseded/；活文档不得为死文档背书（确需引用请在同行加 <!-- allow-superseded-link --> ）"
	fi
done

# 4 · 决策编号在顶层唯一（撞号会让「见 18」这类简写无法解析）
dup=$(live_docs | while read -r f; do
	is_generated "$f" && continue
	printf '%s\n' "$f"
done | sed 's#.*/##; s/-.*//' | sort | uniq -d)
[ -z "$dup" ] || report "docs/design/ 编号撞号: $(echo "$dup" | tr '\n' ' ')"

# 5 · 相对 markdown 链接必须解析得到
for f in $(find docs -name '*.md') CLAUDE.md web/CLAUDE.md; do
	[ -f "$f" ] || continue
	dir=$(dirname "$f")
	grep -o '](\([^)]*\.md\)[^)]*)' "$f" 2>/dev/null |
		sed 's/^](//; s/[)#].*$//' |
		while read -r target; do
			case "$target" in
			http* | '') continue ;;
			esac
			[ -e "$dir/$target" ] || printf '%s -> %s\n' "$f" "$target"
		done
done >/tmp/check-docs-brokenlinks.$$ || true
if [ -s /tmp/check-docs-brokenlinks.$$ ]; then
	report "以下相对链接指向不存在的文件:"
	sed 's/^/  /' /tmp/check-docs-brokenlinks.$$ >&2
fi
rm -f /tmp/check-docs-brokenlinks.$$

# 6 · 当前真值索引必须保持可全量载入的体量
budget=16000
if [ -f docs/design/LIVE.md ]; then
	size=$(wc -c <docs/design/LIVE.md)
	[ "$size" -le "$budget" ] ||
		report "docs/design/LIVE.md 已 $size 字节，超出 $budget 预算；它是索引不是仓库，把细节推回决策文档"
else
	report "缺少 docs/design/LIVE.md（当前真值索引）"
fi

if [ "$fail" -ne 0 ]; then
	printf '\ncheck-docs: 决策文档卫生检查未通过\n' >&2
	exit 1
fi
printf 'check-docs: ok\n'
