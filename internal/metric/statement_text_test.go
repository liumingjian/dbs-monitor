package metric_test

import (
	"regexp"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 只有 pg_stat_statements 的文本可以被采集，pg_stat_activity 的原文一个字都不许取。
//
// 这不是风格偏好：pg_stat_activity.query 是带真实字面量的原始语句，里面可能有身份证号、
// 手机号、口令；pg_stat_statements 的 query 里字面量已经是 $1 占位符，这是该扩展的设计保证。
// 采集端一旦把前者取回来，它就会顺着现有的落库路径进平台库，而那一步是不可逆的。
//
// 这个用例守的是**采集 SQL 本身**，也就是最上游的那道口子：查询里没有这一列，
// 后面的结构体、快照表、接口再怎么改都变不出原文来。所以有人日后想「顺手把语句也带上」时，
// 第一个变红的是这里，而不是某次安全评审。
func TestOnlyPgStatStatementsMayCollectStatementText(t *testing.T) {
	// `\bquery\b` 只匹配裸的 query 列，不匹配 query_start / query_duration_ms 这类
	// 名字里带 query 的时间与计数列——后者是长查询采样要显示的耗时，与语句文本无关。
	bareQueryColumn := regexp.MustCompile(`\bquery\b`)
	for _, task := range metric.Tasks {
		if task.Kind != metric.TaskKindSQL {
			continue
		}
		matched := bareQueryColumn.MatchString(task.SQL)
		if task.ID == metric.TaskQueryStatistics {
			if !matched {
				t.Errorf("task %q no longer collects the normalised statement text", task.ID)
			}
			continue
		}
		if matched {
			t.Errorf("task %q selects a raw statement text column; only %q may collect statement text, "+
				"because only pg_stat_statements guarantees the literals are already placeholders",
				task.ID, metric.TaskQueryStatistics)
		}
	}
}
