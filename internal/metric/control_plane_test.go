package metric

import (
	"reflect"
	"testing"
	"time"
)

func TestProjectControlPlaneMetric(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	watermark := now.Add(-45 * time.Second)
	recentReport := now.Add(-AgentOfflineAfter)

	tests := []struct {
		name     string
		metricID MetricID
		facts    ControlPlaneFacts
		want     ControlPlaneProjection
		ok       bool
	}{
		{
			name:     "collector projects only the completeness watermark",
			metricID: MetricCollectorLastSuccessTime,
			facts:    ControlPlaneFacts{CollectorLastSuccessAt: watermark},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      float64(watermark.Unix()),
				Labels:     map[string]string{"source_type": "SERVER_DIRECT"},
			},
			ok: true,
		},
		{
			name:     "collector without a watermark has no projection",
			metricID: MetricCollectorLastSuccessTime,
			facts:    ControlPlaneFacts{},
		},
		{
			name:     "accepted heartbeat at the offline boundary is online",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: recentReport},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusOnline],
				State:      AgentStatusOnline,
				Labels:     map[string]string{"node": "agent"},
			},
			ok: true,
		},
		{
			name:     "missing heartbeat past two periods is offline",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: recentReport.Add(-time.Nanosecond)},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusOffline],
				State:      AgentStatusOffline,
				Labels:     map[string]string{"node": "agent"},
			},
			ok: true,
		},
		{
			name:     "recorded permission failure is a state",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{AgentExpected: true, AgentLastErrorCode: "PERMISSION_DENIED"},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusPermissionDenied],
				State:      AgentStatusPermissionDenied,
				Labels:     map[string]string{"node": "agent"},
			},
			ok: true,
		},
		{
			name:     "other recorded Agent failure is error",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{AgentExpected: true, AgentLastErrorCode: "CLOCK_SKEW"},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusError],
				State:      AgentStatusError,
				Labels:     map[string]string{"node": "agent"},
			},
			ok: true,
		},
		{
			name:     "unenrolled Agent is not installed",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusNotInstalled],
				State:      AgentStatusNotInstalled,
				Labels:     map[string]string{"node": "agent"},
			},
			ok: true,
		},
		{name: "sample metric is not a control-plane projection", metricID: MetricConnectionTotal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ProjectControlPlaneMetric(test.metricID, test.facts, now)
			if ok != test.ok || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("projection = %+v, %t; want %+v, %t", got, ok, test.want, test.ok)
			}
		})
	}
}
