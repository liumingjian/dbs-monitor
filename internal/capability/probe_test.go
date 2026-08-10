package capability

import (
	"errors"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestStatusesForProbeResults(t *testing.T) {
	tests := []struct {
		name    string
		results []probeResult
		want    map[metric.CapabilityID]metric.CapabilityStatus
	}{
		{
			name: "successful probes distinguish fixable and structural absence",
			results: []probeResult{
				{capability: metric.Capabilities[0], present: true},
				{capability: metric.Capabilities[1], present: false},
				{capability: metric.Capabilities[2], present: false},
				{capability: metric.Capabilities[3], present: true},
			},
			want: map[metric.CapabilityID]metric.CapabilityStatus{
				metric.CapabilityRolePGMonitor:             metric.CapabilityPresent,
				metric.CapabilityExtensionPGStatStatements: metric.CapabilityMissing,
				metric.CapabilityTopologyHasReplication:    metric.CapabilityNotApplicable,
				metric.CapabilityTopologyHasSlot:           metric.CapabilityPresent,
			},
		},
		{
			name: "one failed probe makes the entire snapshot unknown",
			results: []probeResult{
				{capability: metric.Capabilities[0], present: true},
				{capability: metric.Capabilities[1], err: errors.New("permission denied")},
			},
			want: unknownStates(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusesForProbeResults(tt.results)
			for _, declaration := range metric.Capabilities {
				if got[declaration.ID] != tt.want[declaration.ID] {
					t.Errorf("status for %s = %s, want %s", declaration.ID, got[declaration.ID], tt.want[declaration.ID])
				}
			}
		})
	}
}
