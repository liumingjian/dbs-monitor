package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestValidateAlertRule(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*api.AlertRuleInput)
		wantField string
	}{
		{name: "valid numeric rule", mutate: func(*api.AlertRuleInput) {}},
		{
			name:      "blank name",
			mutate:    func(rule *api.AlertRuleInput) { rule.Name = " " },
			wantField: "name",
		},
		{
			name:      "metric must be alertable",
			mutate:    func(rule *api.AlertRuleInput) { rule.MetricId = metric.MetricReplicationRole.String() },
			wantField: "metric_id",
		},
		{
			name:      "aggregation is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.Aggregation = "median" },
			wantField: "aggregation",
		},
		{
			name:      "operator is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.Operator = "contains" },
			wantField: "operator",
		},
		{
			name:      "recovery operator is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.RecoveryOperator = "contains" },
			wantField: "recovery_operator",
		},
		{
			name:      "numeric recovery threshold is required",
			mutate:    func(rule *api.AlertRuleInput) { rule.RecoveryThreshold = nil },
			wantField: "recovery_threshold",
		},
		{
			name: "numeric recovery range must be separate",
			mutate: func(rule *api.AlertRuleInput) {
				value := 10.0
				rule.RecoveryThreshold = &value
			},
			wantField: "recovery_threshold",
		},
		{
			name:      "window must be positive",
			mutate:    func(rule *api.AlertRuleInput) { rule.WindowSeconds = 0 },
			wantField: "window_seconds",
		},
		{
			name:      "trigger count must be positive",
			mutate:    func(rule *api.AlertRuleInput) { rule.ConsecutiveCount = 0 },
			wantField: "consecutive_count",
		},
		{
			name: "explicit recovery count must be positive",
			mutate: func(rule *api.AlertRuleInput) {
				value := 0
				rule.RecoveryConsecutiveCount = &value
			},
			wantField: "recovery_consecutive_count",
		},
		{
			name:      "severity is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.Severity = "fatal" },
			wantField: "severity",
		},
		{
			name:      "no data policy is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.NoDataPolicy = "fire" },
			wantField: "no_data_policy",
		},
		{
			name:      "evaluation interval has floor",
			mutate:    func(rule *api.AlertRuleInput) { rule.EvaluationIntervalSeconds = 4 },
			wantField: "evaluation_interval_seconds",
		},
		{
			name:      "all scope has no explicit instances",
			mutate:    func(rule *api.AlertRuleInput) { rule.InstanceIds = []uuid.UUID{uuid.New()} },
			wantField: "instance_ids",
		},
		{
			name:      "instances scope requires targets",
			mutate:    func(rule *api.AlertRuleInput) { rule.Scope = api.INSTANCES },
			wantField: "instance_ids",
		},
		{
			name:      "scope is closed",
			mutate:    func(rule *api.AlertRuleInput) { rule.Scope = "GROUP" },
			wantField: "scope",
		},
		{
			name: "categorical condition must use latest",
			mutate: func(rule *api.AlertRuleInput) {
				setCategoricalRule(rule)
				rule.Aggregation = api.Avg
			},
			wantField: "aggregation",
		},
		{
			name: "categorical recovery must be opposite",
			mutate: func(rule *api.AlertRuleInput) {
				setCategoricalRule(rule)
				value := 0.0
				rule.RecoveryThreshold = &value
			},
			wantField: "recovery_threshold",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := validNumericRule()
			test.mutate(&rule)
			fieldErrors := validateAlertRule(rule)
			if test.wantField == "" {
				if len(fieldErrors) != 0 {
					t.Fatalf("validation errors = %+v, want none", fieldErrors)
				}
				return
			}
			for _, item := range fieldErrors {
				if item.field == test.wantField {
					return
				}
			}
			t.Fatalf("validation errors = %+v, want field %q", fieldErrors, test.wantField)
		})
	}
}

func TestRecoveryCountDefaultsToTriggerCount(t *testing.T) {
	rule := validNumericRule()
	rule.RecoveryConsecutiveCount = nil
	if got := recoveryConsecutiveCount(rule); got != rule.ConsecutiveCount {
		t.Fatalf("recovery count = %d, want trigger count %d", got, rule.ConsecutiveCount)
	}

	explicit := 4
	rule.RecoveryConsecutiveCount = &explicit
	if got := recoveryConsecutiveCount(rule); got != explicit {
		t.Fatalf("recovery count = %d, want explicit count %d", got, explicit)
	}
}

func validNumericRule() api.AlertRuleInput {
	recoveryThreshold := 5.0
	recoveryCount := 2
	return api.AlertRuleInput{
		Name:                      "High connections",
		MetricId:                  metric.MetricConnectionActive.String(),
		Aggregation:               api.Latest,
		Operator:                  api.GreaterThanEqual,
		Threshold:                 10,
		RecoveryOperator:          api.LessThan,
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             60,
		ConsecutiveCount:          2,
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  api.Warning,
		NoDataPolicy:              api.MarkNoData,
		Scope:                     api.ALL,
		InstanceIds:               []uuid.UUID{},
		EvaluationIntervalSeconds: 30,
		Enabled:                   true,
	}
}

func setCategoricalRule(rule *api.AlertRuleInput) {
	recoveryThreshold := 1.0
	rule.MetricId = metric.MetricAvailabilityReachable.String()
	rule.Operator = api.Equal
	rule.Threshold = 0
	rule.RecoveryOperator = api.Equal
	rule.RecoveryThreshold = &recoveryThreshold
}
