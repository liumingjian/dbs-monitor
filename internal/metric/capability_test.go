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
		{name: "snapshot older than five minutes becomes unknown", states: complete, observedAt: observedAt, now: now.Add(time.Nanosecond), want: UnknownCapabilityStates()},
		{name: "missing snapshot becomes unknown", now: now, want: UnknownCapabilityStates()},
		{name: "partial snapshot becomes entirely unknown", states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent}, observedAt: now, now: now, want: UnknownCapabilityStates()},
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
		{CapabilityRolePGMonitor, 24},
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

func TestTaskCapabilityBlockReason(t *testing.T) {
	tests := []struct {
		name    string
		taskID  TaskID
		states  map[CapabilityID]CapabilityStatus
		want    CapabilityBlockReason
		blocked bool
	}{
		{name: "present requirement", taskID: TaskStatActivity, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent}},
		{name: "missing role", taskID: TaskStatActivity, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityMissing}, want: CapabilityBlockPermissionDenied, blocked: true},
		{name: "missing extension", taskID: TaskQueryStatistics, states: map[CapabilityID]CapabilityStatus{CapabilityExtensionPGStatStatements: CapabilityMissing}, want: CapabilityBlockExtensionMissing, blocked: true},
		{name: "replication is structurally absent", taskID: TaskReplication, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent, CapabilityTopologyHasReplication: CapabilityNotApplicable}, want: CapabilityBlockNotApplicableRole, blocked: true},
		{name: "slot is structurally absent", taskID: TaskReplicationSlot, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityPresent, CapabilityTopologyHasSlot: CapabilityNotApplicable}, want: CapabilityBlockNotApplicableRole, blocked: true},
		{name: "unknown capability", taskID: TaskStatActivity, states: map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityUnknown}, want: CapabilityBlockCollectionFailed, blocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, blocked := TaskCapabilityBlockReason(taskForTest(t, tt.taskID), tt.states)
			if got != tt.want || blocked != tt.blocked {
				t.Fatalf("block reason = %q, %t; want %q, %t", got, blocked, tt.want, tt.blocked)
			}
		})
	}
}

func TestMetricCapabilityBlockReasonUsesProducingTaskRequirements(t *testing.T) {
	states := map[CapabilityID]CapabilityStatus{CapabilityRolePGMonitor: CapabilityMissing}
	got, blocked := MetricCapabilityBlockReason(MetricConnectionTotal, states)
	if got != CapabilityBlockPermissionDenied || !blocked {
		t.Fatalf("block reason = %q, %t; want %q, true", got, blocked, CapabilityBlockPermissionDenied)
	}
}

func TestTaskForMetric(t *testing.T) {
	task, exists := TaskForMetric(MetricTPS)
	if !exists || task.ID != TaskStatDatabase {
		t.Fatalf("TaskForMetric(%q) = %q, %t; want %q, true", MetricTPS, task.ID, exists, TaskStatDatabase)
	}
	if task, exists := TaskForMetric(MetricID("unknown")); exists {
		t.Fatalf("TaskForMetric(unknown) = %q, true; want no task", task.ID)
	}
}

func taskForTest(t *testing.T, id TaskID) Task {
	t.Helper()
	for _, task := range Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q is missing", id)
	return Task{}
}
