package collect

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestScheduledTasksIncludesQueryStatistics(t *testing.T) {
	for _, task := range scheduledTasks() {
		if task.ID == metric.TaskQueryStatistics {
			return
		}
	}
	t.Fatalf("scheduled task %q is missing", metric.TaskQueryStatistics)
}
