package alerting

import (
	"reflect"
	"testing"
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
