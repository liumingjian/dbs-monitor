package collect

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestStatDatabaseRateState(t *testing.T) {
	base := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		observations []statDatabaseSnapshot
		want         []statDatabaseExpectation
		wantReset    []bool
	}{
		{
			name: "baseline then rates",
			observations: []statDatabaseSnapshot{
				{observedAt: base, counters: [6]float64{100, 20, 1_000, 500, 10, 1_000}},
				{observedAt: base.Add(10 * time.Second), counters: [6]float64{130, 30, 1_200, 550, 12, 1_500}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[metric.MetricID]float64{
					metric.MetricTPS:              4,
					metric.MetricXactCommitPerS:   3,
					metric.MetricXactRollbackPerS: 1,
					metric.MetricTuplesReadPerS:   20,
					metric.MetricTuplesWritePerS:  5,
					metric.MetricTempFilesPerS:    0.2,
					metric.MetricTempBytesPerS:    50,
				}},
			},
			wantReset: []bool{false, false},
		},
		{
			name: "counter reset reestablishes baseline",
			observations: []statDatabaseSnapshot{
				{observedAt: base, counters: [6]float64{100, 20, 1_000, 500, 10, 1_000}},
				{observedAt: base.Add(10 * time.Second), counters: [6]float64{5, 2, 100, 50, 1, 100}},
				{observedAt: base.Add(20 * time.Second), counters: [6]float64{15, 4, 120, 60, 2, 200}},
			},
			want: []statDatabaseExpectation{
				{},
				{},
				{sampleCount: 7, values: map[metric.MetricID]float64{
					metric.MetricTPS:              1.2,
					metric.MetricXactCommitPerS:   1,
					metric.MetricXactRollbackPerS: 0.2,
					metric.MetricTuplesReadPerS:   2,
					metric.MetricTuplesWritePerS:  1,
					metric.MetricTempFilesPerS:    0.1,
					metric.MetricTempBytesPerS:    10,
				}},
			},
			wantReset: []bool{false, true, false},
		},
		{
			name: "recent out of order observation is ignored",
			observations: []statDatabaseSnapshot{
				{observedAt: base, counters: [6]float64{100, 20, 1_000, 500, 10, 1_000}},
				{observedAt: base.Add(5 * time.Minute), counters: [6]float64{400, 80, 4_000, 2_000, 40, 4_000}},
				{observedAt: base.Add(time.Minute), counters: [6]float64{150, 30, 1_500, 750, 15, 1_500}},
				{observedAt: base.Add(5*time.Minute + 10*time.Second), counters: [6]float64{410, 82, 4_100, 2_050, 41, 4_100}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[metric.MetricID]float64{
					metric.MetricTPS: 1.2,
				}},
				{},
				{sampleCount: 7, values: map[metric.MetricID]float64{
					metric.MetricTPS: 1.2,
				}},
			},
			wantReset: []bool{false, false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newStatDatabaseRateState()
			for index, observation := range tt.observations {
				samples, reset := state.observe("instance-1", observation)
				if reset != tt.wantReset[index] {
					t.Errorf("observation %d reset = %t, want %t", index, reset, tt.wantReset[index])
				}
				got := sampleValues(samples)
				for metricID, want := range tt.want[index].values {
					if value, exists := got[metricID]; !exists || value != want {
						t.Errorf("observation %d metric %s = %v, %t; want %v", index, metricID, value, exists, want)
					}
				}
				if len(got) != tt.want[index].sampleCount {
					t.Errorf("observation %d samples = %v, want %v", index, got, tt.want[index])
				}
			}
		})
	}
}

type statDatabaseExpectation struct {
	sampleCount int
	values      map[metric.MetricID]float64
}

func sampleValues(samples []collectedSample) map[metric.MetricID]float64 {
	values := make(map[metric.MetricID]float64, len(samples))
	for _, sample := range samples {
		values[sample.metricID] = sample.value
	}
	return values
}
