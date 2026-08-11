package metric

import (
	"reflect"
	"testing"
)

func TestNonNumericMetricEncodingGolden(t *testing.T) {
	tests := []struct {
		metricID string
		want     map[string]float64
	}{
		{"pg.availability.reachable", map[string]float64{
			"unreachable": 0,
			"reachable":   1,
		}},
		{"pg.replication.role", map[string]float64{
			"standalone": 0,
			"primary":    1,
			"replica":    2,
		}},
		{"pg.replication.connection_state", map[string]float64{
			"stopped":    0,
			"starting":   1,
			"startup":    2,
			"catchup":    3,
			"streaming":  4,
			"backup":     5,
			"stopping":   6,
			"waiting":    7,
			"restarting": 8,
		}},
	}

	if len(NonNumericMetricEncodings) != len(tests) {
		t.Fatalf("registered non-numeric metrics = %d, want %d", len(NonNumericMetricEncodings), len(tests))
	}
	for _, tt := range tests {
		got, ok := NonNumericMetricEncodings[tt.metricID]
		if !ok {
			t.Fatalf("metric %q is not registered", tt.metricID)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("metric %q encoding = %v, want %v", tt.metricID, got, tt.want)
		}
	}
}
