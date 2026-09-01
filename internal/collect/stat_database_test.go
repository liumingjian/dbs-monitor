package collect

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

type databaseMetric struct {
	database string
	metricID metric.MetricID
}

func counters(values ...float64) statDatabaseCounters {
	result := statDatabaseCounters{}
	copy(result[:], values)
	return result
}

func TestStatDatabaseRateState(t *testing.T) {
	base := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		observations []statDatabaseSnapshot
		want         []statDatabaseExpectation
	}{
		{
			name: "baseline then rates",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app": counters(100, 20, 1_000, 500, 10, 1_000),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(130, 30, 1_200, 550, 12, 1_500),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[databaseMetric]float64{
					{"app", metric.MetricTPS}:              4,
					{"app", metric.MetricXactCommitPerS}:   3,
					{"app", metric.MetricXactRollbackPerS}: 1,
					{"app", metric.MetricTuplesReadPerS}:   20,
					{"app", metric.MetricTuplesWritePerS}:  5,
					{"app", metric.MetricTempFilesPerS}:    0.2,
					{"app", metric.MetricTempBytesPerS}:    50,
				}},
			},
		},
		{
			// 一个连接下的两个库各自成一条序列。这是本票的核心：加总掉的东西找不回来。
			name: "two databases under one connection land as two series",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app":       counters(100, 20, 1_000, 500, 10, 1_000),
					"reporting": counters(0, 0, 0, 0, 0, 0),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app":       counters(130, 30, 1_200, 550, 12, 1_500),
					"reporting": counters(10, 0, 100, 0, 0, 0),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 14, values: map[databaseMetric]float64{
					{"app", metric.MetricTPS}:                  4,
					{"app", metric.MetricXactCommitPerS}:       3,
					{"reporting", metric.MetricTPS}:            1,
					{"reporting", metric.MetricTuplesReadPerS}: 10,
					{"reporting", metric.MetricTempBytesPerS}:  0,
				}},
			},
		},
		{
			// 新建的库这一轮只当基线：没有前值就没有速率，不能当成「从 0 涨上来」。
			name: "a database created between two observations starts as a baseline",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app": counters(100, 20, 1_000, 500, 10, 1_000),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(130, 30, 1_200, 550, 12, 1_500),
					"new": counters(900, 100, 9_000, 900, 90, 9_000),
				}},
				{observedAt: base.Add(20 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(160, 40, 1_400, 600, 14, 2_000),
					"new": counters(910, 100, 9_100, 900, 90, 9_000),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[databaseMetric]float64{{"app", metric.MetricTPS}: 4}},
				{sampleCount: 14, values: map[databaseMetric]float64{
					{"app", metric.MetricTPS}: 4,
					{"new", metric.MetricTPS}: 1,
				}},
			},
		},
		{
			// 删掉的库随快照消失，previous 整体替换，不留残留。
			name: "a dropped database stops producing series",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app":  counters(100, 20, 1_000, 500, 10, 1_000),
					"temp": counters(10, 0, 100, 0, 0, 0),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(130, 30, 1_200, 550, 12, 1_500),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[databaseMetric]float64{{"app", metric.MetricTPS}: 4}},
			},
		},
		{
			name: "counter reset reestablishes baseline",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app": counters(100, 20, 1_000, 500, 10, 1_000),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(5, 2, 100, 50, 1, 100),
				}},
				{observedAt: base.Add(20 * time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(15, 4, 120, 60, 2, 200),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{counterReset: true},
				{sampleCount: 7, values: map[databaseMetric]float64{
					{"app", metric.MetricTPS}:              1.2,
					{"app", metric.MetricXactCommitPerS}:   1,
					{"app", metric.MetricXactRollbackPerS}: 0.2,
					{"app", metric.MetricTuplesReadPerS}:   2,
					{"app", metric.MetricTuplesWritePerS}:  1,
					{"app", metric.MetricTempFilesPerS}:    0.1,
					{"app", metric.MetricTempBytesPerS}:    10,
				}},
			},
		},
		{
			// 一个库回绕，整批作废：计数器是被 pg_stat_reset() 一起清零的。
			name: "one database resetting discards the whole batch",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app":       counters(100, 20, 1_000, 500, 10, 1_000),
					"reporting": counters(50, 5, 500, 50, 5, 500),
				}},
				{observedAt: base.Add(10 * time.Second), databases: map[string]statDatabaseCounters{
					"app":       counters(130, 30, 1_200, 550, 12, 1_500),
					"reporting": counters(1, 0, 10, 0, 0, 0),
				}},
			},
			want: []statDatabaseExpectation{{}, {counterReset: true}},
		},
		{
			name: "recent out of order observation is ignored",
			observations: []statDatabaseSnapshot{
				{observedAt: base, databases: map[string]statDatabaseCounters{
					"app": counters(100, 20, 1_000, 500, 10, 1_000),
				}},
				{observedAt: base.Add(5 * time.Minute), databases: map[string]statDatabaseCounters{
					"app": counters(400, 80, 4_000, 2_000, 40, 4_000),
				}},
				{observedAt: base.Add(time.Minute), databases: map[string]statDatabaseCounters{
					"app": counters(150, 30, 1_500, 750, 15, 1_500),
				}},
				{observedAt: base.Add(5*time.Minute + 10*time.Second), databases: map[string]statDatabaseCounters{
					"app": counters(410, 82, 4_100, 2_050, 41, 4_100),
				}},
			},
			want: []statDatabaseExpectation{
				{},
				{sampleCount: 7, values: map[databaseMetric]float64{{"app", metric.MetricTPS}: 1.2}},
				{},
				{sampleCount: 7, values: map[databaseMetric]float64{{"app", metric.MetricTPS}: 1.2}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newStatDatabaseRateState()
			for index, observation := range tt.observations {
				batch := state.observe("instance-1", observation)
				want := tt.want[index]
				if batch.counterReset != want.counterReset {
					t.Errorf("observation %d counter reset = %t, want %t", index, batch.counterReset, want.counterReset)
				}
				got := sampleValues(batch.samples)
				for key, wantValue := range want.values {
					if value, exists := got[key]; !exists || value != wantValue {
						t.Errorf("observation %d %s on %q = %v, %t; want %v", index, key.metricID, key.database, value, exists, wantValue)
					}
				}
				if len(got) != want.sampleCount {
					t.Errorf("observation %d samples = %v, want %d", index, got, want.sampleCount)
				}
			}
		})
	}
}

type statDatabaseExpectation struct {
	sampleCount  int
	values       map[databaseMetric]float64
	counterReset bool
}

func sampleValues(samples []collectedSample) map[databaseMetric]float64 {
	values := make(map[databaseMetric]float64, len(samples))
	for _, sample := range samples {
		values[databaseMetric{database: sample.databaseName, metricID: sample.metricID}] = sample.value
	}
	return values
}
