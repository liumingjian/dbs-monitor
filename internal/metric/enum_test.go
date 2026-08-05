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
