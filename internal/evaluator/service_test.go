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
		want                bool
	}{
		{
			name:                "role mismatch",
			metricID:            metric.MetricReplicationWALLagBytes,
			lastErrorCode:       pgtype.Text{String: "NOT_APPLICABLE_ROLE", Valid: true},
			agentMetricsEnabled: true,
			want:                true,
		},
		{
			name:                "unenrolled agent state",
			metricID:            metric.MetricAgentStatus,
			agentMetricsEnabled: false,
			want:                true,
		},
		{
			name:                "unenrolled agent metric",
			metricID:            metric.MetricHostCPUUsagePercent,
			agentMetricsEnabled: false,
			want:                true,
		},
		{
			name:                "server metric remains applicable",
			metricID:            metric.MetricConnectionTotal,
			agentMetricsEnabled: false,
			want:                false,
		},
		{
			name:                "ordinary missing data remains applicable",
			metricID:            metric.MetricConnectionTotal,
			lastErrorCode:       pgtype.Text{String: "COLLECTION_FAILED", Valid: true},
			agentMetricsEnabled: true,
			want:                false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := structurallyNotApplicable(test.metricID, test.lastErrorCode, test.agentMetricsEnabled); got != test.want {
				t.Fatalf("structurallyNotApplicable() = %t, want %t", got, test.want)
			}
		})
	}
}
