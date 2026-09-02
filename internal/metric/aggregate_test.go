package metric_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 这一个用例是加权平均存在的全部理由（规范 #213「库级指标怎么聚合成实例级」）：
// 一个 200GB 主库命中率崩到 60%，同实例下二十个空库各自 100%。算术平均给 98%——
// 一个没人经历过的数字。加权之后结果必须落在主库附近。
func TestWeightedAverageIsNotDilutedByEmptyDatabases(t *testing.T) {
	values := []metric.DatabaseValue{
		{DatabaseName: "primary", Value: 60, Weight: 2_000_000_000},
	}
	for index := 0; index < 20; index++ {
		values = append(values, metric.DatabaseValue{DatabaseName: "empty", Value: 100, Weight: 40})
	}

	got, err := metric.AggregateToInstance(metric.AggregationWeightedAverage, values)
	if err != nil {
		t.Fatalf("aggregate weighted average: %v", err)
	}
	if got < 60 || got > 60.001 {
		t.Fatalf("instance-level hit ratio = %v, want within 0.001 of the 60%% primary", got)
	}

	// 反证：算术平均在同一份输入上给出 98%，也就是这条规则挡住的那个答案。
	arithmetic := 0.0
	for _, value := range values {
		arithmetic += value.Value / float64(len(values))
	}
	if math.Abs(arithmetic-98.09) > 0.01 {
		t.Fatalf("arithmetic mean = %v, want ~98.09 (the diluted answer this rule rejects)", arithmetic)
	}
}

func TestAggregateToInstance(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		aggregation metric.MetricAggregation
		values      []metric.DatabaseValue
		want        float64
	}{
		{
			name:        "counts and volumes are summed",
			aggregation: metric.AggregationSum,
			values: []metric.DatabaseValue{
				{DatabaseName: "app", Value: 12},
				{DatabaseName: "reporting", Value: 30},
			},
			want: 42,
		},
		{
			name:        "one database sums to itself",
			aggregation: metric.AggregationSum,
			values:      []metric.DatabaseValue{{DatabaseName: "app", Value: 7.5}},
			want:        7.5,
		},
		{
			name:        "equal weights degrade to the arithmetic mean",
			aggregation: metric.AggregationWeightedAverage,
			values: []metric.DatabaseValue{
				{DatabaseName: "a", Value: 90, Weight: 100},
				{DatabaseName: "b", Value: 70, Weight: 100},
			},
			want: 80,
		},
		{
			name:        "zero-weight databases do not move the answer",
			aggregation: metric.AggregationWeightedAverage,
			values: []metric.DatabaseValue{
				{DatabaseName: "busy", Value: 60, Weight: 1_000},
				{DatabaseName: "idle", Value: 100, Weight: 0},
			},
			want: 60,
		},
		{
			name:        "non-finite values are dropped rather than poisoning the sum",
			aggregation: metric.AggregationSum,
			values: []metric.DatabaseValue{
				{DatabaseName: "app", Value: 5},
				{DatabaseName: "broken", Value: math.NaN()},
				{DatabaseName: "worse", Value: math.Inf(1)},
			},
			want: 5,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := metric.AggregateToInstance(testCase.aggregation, testCase.values)
			if err != nil {
				t.Fatalf("aggregate: %v", err)
			}
			if math.Abs(got-testCase.want) > 1e-9 {
				t.Fatalf("aggregate = %v, want %v", got, testCase.want)
			}
		})
	}
}

// 收敛不出值时必须说「没有值」，不能给 0：一个凭空出现的 0 在图上是一条真实的线，
// 在告警里是一次真实的判定。
func TestAggregateToInstanceRefusesToInventAValue(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		aggregation metric.MetricAggregation
		values      []metric.DatabaseValue
		wantErr     error
	}{
		{name: "no databases", aggregation: metric.AggregationSum, wantErr: metric.ErrNotAggregatable},
		{
			name:        "instance-level metrics have nothing to aggregate",
			aggregation: metric.AggregationNone,
			values:      []metric.DatabaseValue{{DatabaseName: "", Value: 1}},
			wantErr:     metric.ErrNotAggregatable,
		},
		{
			name:        "weighted average without weight",
			aggregation: metric.AggregationWeightedAverage,
			values:      []metric.DatabaseValue{{DatabaseName: "idle", Value: 100, Weight: 0}},
			wantErr:     metric.ErrNoWeight,
		},
		{
			name:        "every value is non-finite",
			aggregation: metric.AggregationSum,
			values:      []metric.DatabaseValue{{DatabaseName: "broken", Value: math.NaN()}},
			wantErr:     metric.ErrNotAggregatable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := metric.AggregateToInstance(testCase.aggregation, testCase.values)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("aggregate error = %v, want %v", err, testCase.wantErr)
			}
			if got != 0 {
				t.Fatalf("aggregate returned %v alongside an error", got)
			}
		})
	}
}

func TestAggregateSeriesToInstanceAlignsOnTimestamps(t *testing.T) {
	base := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	series := []metric.DatabaseSeries{
		{DatabaseName: "app", Points: []metric.DatabasePoint{
			{At: base, Value: 10},
			{At: base.Add(5 * time.Second), Value: 12},
			{At: base.Add(10 * time.Second), Value: 14},
		}},
		// 第二个库是中途建的，所以它的序列短一截——对齐必须按时刻，不能按下标。
		{DatabaseName: "reporting", Points: []metric.DatabasePoint{
			{At: base.Add(5 * time.Second), Value: 1},
			{At: base.Add(10 * time.Second), Value: 2},
		}},
	}

	got := metric.AggregateSeriesToInstance(metric.AggregationSum, series)
	want := []metric.InstancePoint{
		{At: base, Value: 10},
		{At: base.Add(5 * time.Second), Value: 13},
		{At: base.Add(10 * time.Second), Value: 16},
	}
	if len(got) != len(want) {
		t.Fatalf("aggregated %d points, want %d: %v", len(got), len(want), got)
	}
	for index, point := range got {
		if !point.At.Equal(want[index].At) || math.Abs(point.Value-want[index].Value) > 1e-9 {
			t.Fatalf("point %d = %v, want %v", index, point, want[index])
		}
	}
}

// 收敛不出值的时刻整个丢掉，而不是补一个 0。
func TestAggregateSeriesToInstanceDropsUnaggregatableTimestamps(t *testing.T) {
	base := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	got := metric.AggregateSeriesToInstance(metric.AggregationWeightedAverage, []metric.DatabaseSeries{
		{DatabaseName: "app", Points: []metric.DatabasePoint{
			{At: base, Value: 90, Weight: 0},
			{At: base.Add(5 * time.Second), Value: 90, Weight: 10},
		}},
	})
	if len(got) != 1 || !got[0].At.Equal(base.Add(5*time.Second)) {
		t.Fatalf("aggregated points = %v, want only the weighted timestamp", got)
	}
}
