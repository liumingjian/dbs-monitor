package collect

import (
	"reflect"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestDecodeQueryStatisticsEntry(t *testing.T) {
	want := queryStatisticsEntry{
		QueryID:         -9223372036854775807,
		DatabaseOID:     16384,
		UserOID:         16385,
		Calls:           42,
		TotalExecTimeMS: 1250.5,
	}
	got, err := decodeQueryStatisticsEntry([]any{
		want.QueryID, want.DatabaseOID, want.UserOID, want.Calls, want.TotalExecTimeMS,
	})
	if err != nil {
		t.Fatalf("decodeQueryStatisticsEntry() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeQueryStatisticsEntry() = %#v, want %#v", got, want)
	}
}

func TestScheduledTasksIncludesQueryStatistics(t *testing.T) {
	for _, task := range scheduledTasks() {
		if task.ID == metric.TaskQueryStatistics {
			return
		}
	}
	t.Fatalf("scheduled task %q is missing", metric.TaskQueryStatistics)
}
