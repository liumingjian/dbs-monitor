package evaluator

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestStructurallyNotApplicable(t *testing.T) {
	tests := []struct {
		name                string
		metricID            metric.MetricID
		lastErrorCode       pgtype.Text
		agentMetricsEnabled bool
		agentExpected       bool
		want                bool
	}{
		{
			name:                "role mismatch",
			metricID:            metric.MetricReplicationWALLagBytes,
			lastErrorCode:       pgtype.Text{String: "NOT_APPLICABLE_ROLE", Valid: true},
			agentMetricsEnabled: true,
			agentExpected:       true,
			want:                true,
		},
		{
			name:                "unenrolled agent state",
			metricID:            metric.MetricAgentStatus,
			agentMetricsEnabled: false,
			agentExpected:       true,
			want:                true,
		},
		{
			name:                "unenrolled agent metric",
			metricID:            metric.MetricHostCPUUsagePercent,
			agentMetricsEnabled: false,
			agentExpected:       true,
			want:                true,
		},
		{
			name:                "server metric remains applicable",
			metricID:            metric.MetricConnectionTotal,
			agentMetricsEnabled: false,
			agentExpected:       true,
			want:                false,
		},
		{
			name:                "ordinary missing data remains applicable",
			metricID:            metric.MetricConnectionTotal,
			lastErrorCode:       pgtype.Text{String: "COLLECTION_FAILED", Valid: true},
			agentMetricsEnabled: true,
			agentExpected:       true,
			want:                false,
		},
		{
			name:                "disabled agent offline state",
			metricID:            metric.MetricAgentStatus,
			agentMetricsEnabled: true,
			agentExpected:       false,
			want:                true,
		},
		{
			name:                "revoked agent offline state remains applicable",
			metricID:            metric.MetricAgentStatus,
			agentMetricsEnabled: true,
			agentExpected:       true,
			want:                false,
		},
		{
			name:                "disabled agent host metric",
			metricID:            metric.MetricHostCPUUsagePercent,
			agentMetricsEnabled: true,
			agentExpected:       false,
			want:                true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := structurallyNotApplicable(test.metricID, test.lastErrorCode, test.agentMetricsEnabled, test.agentExpected); got != test.want {
				t.Fatalf("structurallyNotApplicable() = %t, want %t", got, test.want)
			}
		})
	}
}
