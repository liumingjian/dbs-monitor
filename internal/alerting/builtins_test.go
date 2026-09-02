package alerting

import (
	"reflect"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/dbengine"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestBuiltinCollectionRulesGolden(t *testing.T) {
	want := []BuiltinRule{
		{
			ID: "00000000-0000-0000-0000-000000063001", Identifier: "database_unreachable", Name: "数据库不可达",
			MetricID: "pg.availability.reachable", Aggregation: "latest", Operator: "=", Threshold: 0,
			RecoveryOperator: "=", RecoveryThreshold: 1, WindowSeconds: 30, ConsecutiveCount: 3,
			RecoveryConsecutiveCount: 3, Severity: "critical", NoDataPolicy: "mark_no_data",
			EvaluationIntervalSeconds: 30, Enabled: true,
		},
		{
			ID: "00000000-0000-0000-0000-000000063002", Identifier: "agent_offline", Name: "Agent 离线",
			MetricID: "agent.status", Aggregation: "latest", Operator: "=", Threshold: 0,
			RecoveryOperator: "=", RecoveryThreshold: 1, WindowSeconds: 30, ConsecutiveCount: 3,
			RecoveryConsecutiveCount: 3, Severity: "critical", NoDataPolicy: "mark_no_data",
			EvaluationIntervalSeconds: 30, Enabled: true,
		},
		{
			ID: "00000000-0000-0000-0000-000000063003", Identifier: "data_stale", Name: "数据过期",
			MetricID: "collector.last_success_time", Aggregation: "latest", Operator: ">", Threshold: 600,
			RecoveryOperator: "<", RecoveryThreshold: 450, WindowSeconds: 60, ConsecutiveCount: 2,
			RecoveryConsecutiveCount: 2, Severity: "warning", NoDataPolicy: "mark_no_data",
			EvaluationIntervalSeconds: 60, Enabled: true,
		},
	}
	if !reflect.DeepEqual(BuiltinCollectionRules, want) {
		t.Fatalf("built-in collection rules changed:\n got: %+v\nwant: %+v", BuiltinCollectionRules, want)
	}
	for _, rule := range BuiltinCollectionRules {
		if !rule.Enabled {
			t.Errorf("built-in rule %q must be enabled", rule.Name)
		}
		if rule.Severity != "warning" && rule.Severity != "critical" {
			t.Errorf("built-in rule %q severity %q is below warning", rule.Name, rule.Severity)
		}
	}
}

func TestBuiltinRuleRestriction(t *testing.T) {
	info := api.Info
	warning := api.Warning
	critical := api.Critical
	disabled := false
	enabled := true
	tests := []struct {
		name      string
		isBuiltin bool
		change    BuiltinRuleChange
		want      api.ErrorErrorCode
	}{
		{name: "delete", isBuiltin: true, change: BuiltinRuleChange{Delete: true}, want: api.BUILTINRULEDELETEFORBIDDEN},
		{name: "disable", isBuiltin: true, change: BuiltinRuleChange{Enabled: &disabled}, want: api.BUILTINRULEDISABLEFORBIDDEN},
		{name: "severity below warning", isBuiltin: true, change: BuiltinRuleChange{Severity: &info}, want: api.BUILTINRULESEVERITYTOOLOW},
		{name: "keep enabled", isBuiltin: true, change: BuiltinRuleChange{Enabled: &enabled}},
		{name: "warning severity", isBuiltin: true, change: BuiltinRuleChange{Severity: &warning}},
		{name: "critical severity", isBuiltin: true, change: BuiltinRuleChange{Severity: &critical}},
		{name: "user rule is unrestricted", change: BuiltinRuleChange{Delete: true, Enabled: &disabled, Severity: &info}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := BuiltinRuleRestriction(test.isBuiltin, test.change); got != test.want {
				t.Fatalf("restriction = %q, want %q", got, test.want)
			}
		})
	}
}

// 模板的引擎归属是它引用的那个指标在目录里的归属的转写，不是另立的事实。
// 写歪了这里就红——目录改了归属而模板没跟上，同样红。
func TestBuiltinRuleTemplatesCarryTheirMetricsEngineOwnership(t *testing.T) {
	for _, template := range BuiltinRuleTemplates {
		item, exists := metric.Lookup(metric.MetricID(template.MetricID))
		if !exists {
			t.Errorf("template %q references %q, which is not in the catalogue", template.Identifier, template.MetricID)
			continue
		}
		if template.Engine != item.Engine {
			t.Errorf("template %q declares engine %q, catalogue says %q", template.Identifier, template.Engine, item.Engine)
		}
		// 引擎无关的指标不占位：它们本来就处处可用，再给一个位只会让「容量水位」
		// 在同一台 PostgreSQL 实例上同时指向磁盘使用率和数据库体积。
		// alert_rule_template 上的 alert_rule_template_agnostic_has_no_slot 是同一条约束。
		wantSlot := item.Slot.String()
		if item.Engine == metric.EngineAgnostic {
			wantSlot = ""
		}
		if template.Slot != wantSlot {
			t.Errorf("template %q declares slot %q, want %q", template.Identifier, template.Slot, wantSlot)
		}
	}
}

// 可见性：带位的模板一份两用，引擎私有的模板只在本引擎露面，引擎无关的处处露面。
func TestBuiltinRuleTemplateVisibility(t *testing.T) {
	const engineUnderTest = dbengine.Engine("ENGINE_UNDER_TEST")
	visibleElsewhere := make(map[string]bool, len(BuiltinRuleTemplates))
	for _, template := range BuiltinRuleTemplates {
		if !metric.AppliesToEngine(metric.MetricID(template.MetricID), dbengine.PostgreSQL) {
			t.Errorf("template %q is invisible on PostgreSQL", template.Identifier)
		}
		visibleElsewhere[template.Identifier] = metric.AppliesToEngine(metric.MetricID(template.MetricID), engineUnderTest)
	}
	// 第二个引擎还没有绑定任何语义位，所以这一刻只有引擎无关的三条越得过去。
	// 一旦 MySQL 把 connections 这个位绑上，connections_high 会自动跟着越过去——
	// 那正是「一份两用」，而 Slot 积压这类模板永远越不过去。
	for identifier, wantVisible := range map[string]bool{
		"cpu_high": true, "memory_high": true, "disk_usage_high": true,
		"connections_high": false, "probe_latency_high": false,
		"replication_slot_backlog": false, "prepared_xacts": false, "temp_bytes_high": false,
	} {
		if visibleElsewhere[identifier] != wantVisible {
			t.Errorf("template %q visible on an engine that binds nothing = %v, want %v", identifier, visibleElsewhere[identifier], wantVisible)
		}
	}
}
