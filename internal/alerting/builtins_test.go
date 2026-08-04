package alerting

import (
	"reflect"
	"testing"
)

func TestBuiltinCollectionRulesGolden(t *testing.T) {
	want := []BuiltinRule{
		{Name: "数据库不可达", MetricID: "pg.availability.reachable", Severity: "critical", Enabled: true, Deletable: false},
		{Name: "Agent 离线", MetricID: "agent.status", Severity: "critical", Enabled: true, Deletable: false},
		{Name: "数据过期", MetricID: "collector.last_success_time", Severity: "warning", Enabled: true, Deletable: false},
	}
	if !reflect.DeepEqual(BuiltinCollectionRules, want) {
		t.Fatalf("built-in collection rules changed:\n got: %+v\nwant: %+v", BuiltinCollectionRules, want)
	}
	for _, rule := range BuiltinCollectionRules {
		if !rule.Enabled || rule.Deletable {
			t.Errorf("built-in rule %q must be enabled and non-deletable", rule.Name)
		}
		if rule.Severity != "warning" && rule.Severity != "critical" {
			t.Errorf("built-in rule %q severity %q is below warning", rule.Name, rule.Severity)
		}
	}
}
