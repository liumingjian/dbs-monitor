package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 库维度的读取侧。
//
// 一个实例是一条连接，连接下可以有几十个库，所以库级指标（TPS、提交回滚、元组读写、临时文件……）
// 一库一条序列。列表与总览要的是**实例级**的一个数——按目录里逐指标记的聚合方式收敛；
// 逐库明细只在实例工作台里展开（by_database=true）。行数不能因为库多而膨胀，这是规范
// 「库降级为指标的一个维度」的直接后果。

type metricPoint struct {
	ts    time.Time
	value float64
}

// fetchedSeries 是一条已经取到点的序列。databaseName 为空表示实例级。
type fetchedSeries struct {
	databaseName string
	labels       map[string]string
	points       []metricPoint
}

// readMetricSeries 取出这个指标在这个实例上的所有序列及其时间范围内的点。
func readMetricSeries(
	ctx context.Context,
	queries *metric.Queries,
	instanceID pgtype.UUID,
	metricID metric.MetricID,
	step metricStep,
	from, to time.Time,
) ([]fetchedSeries, error) {
	rows, err := queries.SeriesForMetric(ctx, metric.SeriesForMetricParams{
		InstanceID: instanceID, MetricID: metricID.String(),
	})
	if err != nil {
		return nil, err
	}
	series := make([]fetchedSeries, 0, len(rows))
	for _, row := range rows {
		points, err := readSeriesPoints(ctx, queries, row.SeriesID, step, from, to)
		if err != nil {
			return nil, err
		}
		labels := map[string]string{}
		_ = json.Unmarshal(row.Labels, &labels)
		series = append(series, fetchedSeries{databaseName: row.DatabaseName, labels: labels, points: points})
	}
	return series, nil
}

func readSeriesPoints(
	ctx context.Context,
	queries *metric.Queries,
	seriesID int64,
	step metricStep,
	from, to time.Time,
) ([]metricPoint, error) {
	start := pgtype.Timestamptz{Time: from, Valid: true}
	end := pgtype.Timestamptz{Time: to, Valid: true}
	if step.raw {
		raw, err := queries.PointsInRange(ctx, metric.PointsInRangeParams{SeriesID: seriesID, Ts: start, Ts_2: end})
		if err != nil {
			return nil, err
		}
		points := make([]metricPoint, 0, len(raw))
		for _, point := range raw {
			points = append(points, metricPoint{ts: point.Ts.Time, value: point.Value})
		}
		return points, nil
	}
	bucketed, err := queries.BucketedPointsInRange(ctx, metric.BucketedPointsInRangeParams{
		SeriesID: seriesID, Ts: start, Ts_2: end, Bucket: step.bucket,
	})
	if err != nil {
		return nil, err
	}
	points := make([]metricPoint, 0, len(bucketed))
	for _, point := range bucketed {
		points = append(points, metricPoint{ts: point.Ts.Time, value: point.Value})
	}
	return points, nil
}

// aggregateSeriesToInstance 把逐库序列收敛成一条实例级序列。
//
// 权重取自目录里为这个指标登记的权重指标（比率型才有），按「同一个库、同一个时刻」配对。
// 权重序列缺了某个点，那个点就不参与——加权平均少一个库好过悄悄退化成算术平均。
func aggregateSeriesToInstance(
	ctx context.Context,
	queries *metric.Queries,
	instanceID pgtype.UUID,
	metricID metric.MetricID,
	step metricStep,
	from, to time.Time,
	series []fetchedSeries,
) ([]metricPoint, error) {
	aggregation := metric.AggregationFor(metricID)
	weights := map[string]map[time.Time]float64{}
	if aggregation == metric.AggregationWeightedAverage {
		weightMetric, hasWeight := metric.WeightMetricFor(metricID)
		if !hasWeight {
			// 目录说加权，却没登记权重。宁可什么都不给，也不能退回算术平均：
			// 那正是这条规则要挡住的答案。catalog_test 上的成对约束就是防这一步的。
			return nil, nil
		}
		weightSeries, err := readMetricSeries(ctx, queries, instanceID, weightMetric, step, from, to)
		if err != nil {
			return nil, err
		}
		for _, item := range weightSeries {
			byTime := make(map[time.Time]float64, len(item.points))
			for _, point := range item.points {
				byTime[point.ts.UTC()] = point.value
			}
			weights[item.databaseName] = byTime
		}
	}

	converted := make([]metric.DatabaseSeries, 0, len(series))
	for _, item := range series {
		points := make([]metric.DatabasePoint, 0, len(item.points))
		for _, point := range item.points {
			weight := 1.0
			if aggregation == metric.AggregationWeightedAverage {
				known, exists := weights[item.databaseName][point.ts.UTC()]
				if !exists {
					continue
				}
				weight = known
			}
			points = append(points, metric.DatabasePoint{At: point.ts, Value: point.value, Weight: weight})
		}
		converted = append(converted, metric.DatabaseSeries{DatabaseName: item.databaseName, Points: points})
	}

	aggregated := metric.AggregateSeriesToInstance(aggregation, converted)
	points := make([]metricPoint, 0, len(aggregated))
	for _, point := range aggregated {
		points = append(points, metricPoint{ts: point.At, value: point.Value})
	}
	return points, nil
}

// byDatabase 说这次请求要不要逐库明细。缺省是不要：默认口径是实例级，
// 库维度只在实例工作台里显式展开。
func byDatabase(requested *bool) bool {
	return requested != nil && *requested
}
