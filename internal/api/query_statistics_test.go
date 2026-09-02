package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

// 查询统计条目暴露的是**归一化**语句文本，绝不是原始语句。
//
// 这条守卫原本禁掉的是全部四种文本字段名（issue 58：那时平台还没有归一化文本表，
// 于是「一个字段都不给」是当时唯一安全的形状）。#213 引入了 `query_statement_text` ——
// 只收 pg_stat_statements 的归一化文本，字面量已经是 `$1`，并且这份文本已经由
// `/api/v1/top-sql` 对外给出。继续把 `query_text` 也禁掉，禁的就不再是原始语句，
// 而是同一份已公开的文本换个端点出现，代价是实例工作台上永远只有一个 queryid ——
// 那正是 SPEC-metrics-v2.md 开篇点名的问题。
//
// 所以守卫改成两面：`query_text` **必须**在（否则工作台又退回只有 queryid），
// 而带真实字面量的那三种名字一个都不许有。真正拦住原始语句落库的是
// `internal/metric/statement_text_test.go` 的字典扫描——它盯的是采集侧的 SQL。
func TestQueryStatisticsContractPreservesQueryIDAndExposesOnlyNormalisedText(t *testing.T) {
	entryType := reflect.TypeOf(api.QueryStatisticsEntry{})
	queryIDField, exists := entryType.FieldByName("Queryid")
	if !exists || queryIDField.Type.Kind() != reflect.String {
		t.Fatalf("queryid field = %+v, want string identifier", queryIDField)
	}
	normalised := false
	for index := 0; index < entryType.NumField(); index++ {
		name := strings.Split(entryType.Field(index).Tag.Get("json"), ",")[0]
		switch name {
		case "query", "sql", "sql_text":
			t.Fatalf("query statistics entry exposes raw SQL text field %q", name)
		case "query_text":
			normalised = true
			// 缺席与空串是两件事：没采到文本的条目不该带一个空字符串上榜。
			if entryType.Field(index).Type.Kind() != reflect.Pointer {
				t.Fatalf("query_text field = %v, want pointer so absent stays distinguishable from empty",
					entryType.Field(index).Type)
			}
		}
	}
	if !normalised {
		t.Fatal("query statistics entry has no query_text; the workbench would be back to showing only a queryid")
	}

	snapshotType := reflect.TypeOf(api.QueryStatisticsSnapshot{})
	unavailabilityField, exists := snapshotType.FieldByName("Unavailability")
	if !exists || unavailabilityField.Type != reflect.TypeOf((*api.Unavailability)(nil)) {
		t.Fatalf("unavailability field = %+v, want *api.Unavailability", unavailabilityField)
	}
}
