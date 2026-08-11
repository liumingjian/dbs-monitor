package evaluator

import (
	"testing"
	"time"

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

func TestControlPlaneRuleValue(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		metricID   metric.MetricID
		projection metric.ControlPlaneProjection
		want       float64
	}{
		{name: "agent status keeps encoded value", metricID: metric.MetricAgentStatus, projection: metric.ControlPlaneProjection{Value: 0}, want: 0},
		{name: "collector watermark becomes age", metricID: metric.MetricCollectorLastSuccessTime, projection: metric.ControlPlaneProjection{Value: float64(now.Add(-601 * time.Second).Unix())}, want: 601},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := controlPlaneRuleValue(test.metricID, test.projection, now); got != test.want {
				t.Fatalf("controlPlaneRuleValue() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCollectionFailureDoesNotHideReachability(t *testing.T) {
	if collectionFailureBlocksSamples(metric.MetricAvailabilityReachable, pgtype.Text{String: "DB_UNREACHABLE", Valid: true}) {
		t.Fatal("DB_UNREACHABLE must not hide the reachability sample")
	}
	if !collectionFailureBlocksSamples(metric.MetricConnectionTotal, pgtype.Text{String: "DB_UNREACHABLE", Valid: true}) {
		t.Fatal("DB_UNREACHABLE must still block ordinary PostgreSQL metric samples")
	}
}
