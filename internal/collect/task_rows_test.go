package collect

import (
	"reflect"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestSamplesForTaskRow(t *testing.T) {
	tests := []struct {
		name   string
		taskID metric.TaskID
		row    map[string]any
		want   []collectedSample
	}{
		{
			name:   "role state is encoded",
			taskID: metric.TaskRole,
			row:    map[string]any{"role": "standalone"},
			want:   []collectedSample{{metricID: metric.MetricReplicationRole, value: 0}},
		},
		{
			name:   "replication row keeps labels and skips nullable replay lag",
			taskID: metric.TaskReplication,
			row: map[string]any{
				"replica": "standby-a", "connection_state": "streaming",
				"replay_lag_ms": nil, "wal_lag_bytes": float64(0),
			},
			want: []collectedSample{
				{metricID: metric.MetricReplicationConnectionState, value: 4, labels: map[string]string{"replica": "standby-a"}},
				{metricID: metric.MetricReplicationWALLagBytes, value: 0, labels: map[string]string{"replica": "standby-a"}},
			},
		},
		{
			name:   "caught-up slot persists a real zero",
			taskID: metric.TaskReplicationSlot,
			row:    map[string]any{"slot": "slot-a", "retained_wal_bytes": float64(0)},
			want:   []collectedSample{{metricID: metric.MetricReplicationSlotRetainedWAL, value: 0, labels: map[string]string{"slot": "slot-a"}}},
		},
		{
			name:   "prepared transactions retain database dimension",
			taskID: metric.TaskPreparedXacts,
			row:    map[string]any{"database": "app", "prepared_xacts_count": float64(2)},
			want:   []collectedSample{{metricID: metric.MetricPreparedXactsCount, value: 2, labels: map[string]string{"database": "app"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := samplesForTaskRow(taskByIDForTest(t, tt.taskID), tt.row)
			if err != nil {
				t.Fatalf("samplesForTaskRow() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("samplesForTaskRow() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestScheduledTasksIncludesIssue57Tasks(t *testing.T) {
	want := map[metric.TaskID]bool{
		metric.TaskReplication:     false,
		metric.TaskReplicationSlot: false,
		metric.TaskPreparedXacts:   false,
		metric.TaskRole:            false,
	}
	for _, task := range scheduledTasks() {
		if _, exists := want[task.ID]; exists {
			want[task.ID] = true
		}
	}
	for taskID, found := range want {
		if !found {
			t.Errorf("scheduled task %q is missing", taskID)
		}
	}
}

func taskByIDForTest(t *testing.T, id metric.TaskID) metric.Task {
	t.Helper()
	for _, task := range metric.Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q is missing", id)
	return metric.Task{}
}
