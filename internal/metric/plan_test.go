package metric_test

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestTaskIntervalValidation(t *testing.T) {
	tests := []struct {
		name     string
		taskID   metric.TaskID
		interval time.Duration
		wantErr  bool
	}{
		{name: "unknown task", taskID: "unknown", interval: 5 * time.Second, wantErr: true},
		{name: "below floor", taskID: metric.TaskStatDatabase, interval: 4 * time.Second, wantErr: true},
		{name: "valid floor", taskID: metric.TaskStatDatabase, interval: 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := metric.ValidateTaskInterval(tt.taskID, tt.interval); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTaskInterval(%q, %s) error = %v, wantErr %t", tt.taskID, tt.interval, err, tt.wantErr)
			}
		})
	}
}

func TestDeclaredRowTaskDefaultIntervals(t *testing.T) {
	tests := []struct {
		taskID metric.TaskID
		want   time.Duration
	}{
		{metric.TaskReplication, 5 * time.Second},
		{metric.TaskReplicationSlot, 5 * time.Second},
		{metric.TaskPreparedXacts, 5 * time.Minute},
		{metric.TaskRole, 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(string(tt.taskID), func(t *testing.T) {
			task := requiredTask(t, tt.taskID)
			if task.Interval != tt.want {
				t.Fatalf("interval = %s, want %s", task.Interval, tt.want)
			}
		})
	}
}
