package httpapi

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestAgentMetricUnavailability(t *testing.T) {
	enabledFacts := metric.ControlPlaneFacts{AgentExpected: true, AgentMetricsEnabled: true}
	tests := []struct {
		name        string
		facts       metric.ControlPlaneFacts
		agentStatus string
		wantReason  api.Unavailability
		unavailable bool
	}{
		{
			name:        "unenrolled Agent is not applicable",
			agentStatus: metric.AgentStatusOffline,
			wantReason:  api.NOTAPPLICABLEROLE,
			unavailable: true,
		},
		{
			name:        "disabled feature takes precedence over Agent state",
			facts:       metric.ControlPlaneFacts{AgentExpected: true},
			agentStatus: metric.AgentStatusPermissionDenied,
			wantReason:  api.FEATUREDISABLED,
			unavailable: true,
		},
		{
			name:        "offline Agent",
			facts:       enabledFacts,
			agentStatus: metric.AgentStatusOffline,
			wantReason:  api.AGENTOFFLINE,
			unavailable: true,
		},
		{
			name:        "permission denied",
			facts:       enabledFacts,
			agentStatus: metric.AgentStatusPermissionDenied,
			wantReason:  api.PERMISSIONDENIED,
			unavailable: true,
		},
		{
			name:        "collection failed",
			facts:       enabledFacts,
			agentStatus: metric.AgentStatusError,
			wantReason:  api.COLLECTIONFAILED,
			unavailable: true,
		},
		{
			name:        "online Agent is available",
			facts:       enabledFacts,
			agentStatus: metric.AgentStatusOnline,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotReason, unavailable := agentMetricUnavailability(test.facts, test.agentStatus)
			if gotReason != test.wantReason || unavailable != test.unavailable {
				t.Fatalf(
					"unavailability = %q, %t; want %q, %t",
					gotReason, unavailable, test.wantReason, test.unavailable,
				)
			}
		})
	}
}
