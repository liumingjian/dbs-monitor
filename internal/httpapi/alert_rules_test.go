package httpapi

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestValidateAlertRuleRequiresNumericHysteresis(t *testing.T) {
	rule := api.AlertRuleInput{
		Name: "High connections", MetricId: "pg.connection.active",
		Aggregation: api.Latest, Operator: api.GreaterThanEqual, Threshold: 10,
		RecoveryOperator: api.LessThan, RecoveryThreshold: 10,
		WindowSeconds: 60, ConsecutiveCount: 2, RecoveryConsecutiveCount: 2,
		Severity: api.Warning, NoDataPolicy: api.MarkNoData, Enabled: true,
	}
	errors := validateAlertRule(rule)
	if len(errors) != 1 || errors[0].field != "recovery_threshold" {
		t.Fatalf("validation errors = %+v, want recovery_threshold error", errors)
	}
}

func TestValidateAlertRuleUsesMetricDictionaryAlertability(t *testing.T) {
	rule := api.AlertRuleInput{
		Name: "Unsupported metric", MetricId: "pg.replication.role",
		Aggregation: api.Latest, Operator: api.Equal, Threshold: 1,
		RecoveryOperator: api.Equal, RecoveryThreshold: 0,
		WindowSeconds: 60, ConsecutiveCount: 1, RecoveryConsecutiveCount: 1,
		Severity: api.Warning, NoDataPolicy: api.MarkNoData, Enabled: true,
	}
	errors := validateAlertRule(rule)
	if len(errors) != 1 || errors[0].field != "metric_id" {
		t.Fatalf("validation errors = %+v, want metric_id error", errors)
	}
}
