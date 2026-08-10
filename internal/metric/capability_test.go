package metric

import (
	"testing"
	"time"
)

func TestProjectCapabilitySnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-CapabilitySnapshotTTL)
	complete := map[CapabilityID]CapabilityStatus{
		CapabilityRolePGMonitor:             CapabilityPresent,
		CapabilityExtensionPGStatStatements: CapabilityMissing,
		CapabilityTopologyHasReplication:    CapabilityNotApplicable,
		CapabilityTopologyHasSlot:           CapabilityPresent,
	}
	tests := []struct {
		name       string
		states     map[CapabilityID]CapabilityStatus
		observedAt time.Time
		now        time.Time
		want       map[CapabilityID]CapabilityStatus
	}{
		{name: "fresh snapshot preserves all four-state facts", states: complete, observedAt: observedAt, now: now, want: complete},
		{name: "snapshot older than five minutes becomes unknown", states: complete, observedAt: observedAt, now: now.Add(time.Nanosecond), want: unknownCapabilityStates()},
		{name: "missing snapshot becomes unknown", now: now, want: unknownCapabilityStates()},
		{name: "partial snapshot becomes entirely unknown", states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent}, observedAt: now, now: now, want: unknownCapabilityStates()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectCapabilitySnapshot(tt.states, tt.observedAt, tt.now)
			for _, capability := range Capabilities {
				if got[capability.ID] != tt.want[capability.ID] {
					t.Errorf("status for %s = %s, want %s", capability.ID, got[capability.ID], tt.want[capability.ID])
				}
			}
		})
	}
}

func TestCapabilityAffectedMetricCountUsesTaskYields(t *testing.T) {
	tests := []struct {
		capability CapabilityID
		want       int
	}{
		{CapabilityRolePGMonitor, 19},
		{CapabilityExtensionPGStatStatements, 0},
		{CapabilityTopologyHasReplication, 3},
		{CapabilityTopologyHasSlot, 1},
	}
	for _, tt := range tests {
		if got := CapabilityAffectedMetricCount(tt.capability); got != tt.want {
			t.Errorf("CapabilityAffectedMetricCount(%s) = %d, want %d", tt.capability, got, tt.want)
		}
	}
}

func TestMetricCapabilityBlockReasonUsesProducingTaskRequirements(t *testing.T) {
	tests := []struct {
		name     string
		metricID MetricID
		states   map[CapabilityID]CapabilityStatus
		want     string
		blocked  bool
	}{
		{name: "present requirement", metricID: MetricConnectionTotal, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent}},
		{name: "missing role", metricID: MetricConnectionTotal, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityMissing}, want: "PERMISSION_DENIED", blocked: true},
		{name: "missing extension", metricID: MetricID("opportunity.query_statistics"), states: map[CapabilityID]CapabilityStatus{CapabilityExtensionPGStatStatements: CapabilityMissing}, want: "EXTENSION_MISSING", blocked: true},
		{name: "replication is structurally absent", metricID: MetricReplicationReplayLagMS, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent, CapabilityTopologyHasReplication: CapabilityNotApplicable}, want: "NOT_APPLICABLE_ROLE", blocked: true},
		{name: "slot is structurally absent", metricID: MetricReplicationSlotRetainedWAL, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent, CapabilityTopologyHasSlot: CapabilityNotApplicable}, want: "FEATURE_DISABLED", blocked: true},
		{name: "unknown capability", metricID: MetricConnectionTotal, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityUnknown}, want: "COLLECTION_FAILED", blocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			var blocked bool
			if tt.metricID == MetricID("opportunity.query_statistics") {
				got, blocked = TaskCapabilityBlockReason(taskForTest(TaskQueryStatistics), tt.states)
			} else {
				got, blocked = MetricCapabilityBlockReason(tt.metricID, tt.states)
			}
			if got != tt.want || blocked != tt.blocked {
				t.Fatalf("block reason = %q, %t; want %q, %t", got, blocked, tt.want, tt.blocked)
			}
		})
	}
}

func taskForTest(id TaskID) Task {
	for _, task := range Tasks {
		if task.ID == id {
			return task
		}
	}
	return Task{}
}

func unknownCapabilityStates() map[CapabilityID]CapabilityStatus {
	result := make(map[CapabilityID]CapabilityStatus, len(Capabilities))
	for _, capability := range Capabilities {
		result[capability.ID] = CapabilityUnknown
	}
	return result
}
