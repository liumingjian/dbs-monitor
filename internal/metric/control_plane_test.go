package metric

import (
	"reflect"
	"testing"
	"time"
)

func TestProjectControlPlaneMetric(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	watermark := now.Add(-45 * time.Second)

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
			name:     "agent status includes its stable encoding",
			metricID: MetricAgentStatus,
			facts:    ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: now},
			want: ControlPlaneProjection{
				ObservedAt: now,
				Value:      AgentStatusEncodings[AgentStatusOnline],
				State:      AgentStatusOnline,
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

func TestAgentStatusAt(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	offlineBoundary := now.Add(-AgentOfflineAfter)
	tests := []struct {
		name  string
		facts ControlPlaneFacts
		want  string
	}{
		{
			name:  "accepted heartbeat at the offline boundary is online",
			facts: ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: offlineBoundary},
			want:  AgentStatusOnline,
		},
		{
			name:  "heartbeat past the offline boundary is offline",
			facts: ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: offlineBoundary.Add(-time.Nanosecond)},
			want:  AgentStatusOffline,
		},
		{
			name:  "permission failure takes precedence over a recent heartbeat",
			facts: ControlPlaneFacts{AgentExpected: true, AgentLastReportAt: now, AgentLastErrorCode: "PERMISSION_DENIED"},
			want:  AgentStatusPermissionDenied,
		},
		{
			name:  "other recorded Agent failure is error",
			facts: ControlPlaneFacts{AgentExpected: true, AgentLastErrorCode: "CLOCK_SKEW"},
			want:  AgentStatusError,
		},
		{name: "unenrolled Agent is not installed", want: AgentStatusNotInstalled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AgentStatusAt(test.facts, now); got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}
