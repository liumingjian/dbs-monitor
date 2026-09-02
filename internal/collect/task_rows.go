package collect

import (
	"context"
	"fmt"

	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

func collectDeclaredTask(ctx context.Context, conn *monitorpg.TargetConn, task metric.Task) (collectedBatch, error) {
	rows, err := conn.Query(ctx, task.SQL)
	if err != nil {
		return collectedBatch{}, err
	}
	defer rows.Close()

	fieldDescriptions := rows.FieldDescriptions()
	samples := make([]collectedSample, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return collectedBatch{}, err
		}
		row := make(map[string]any, len(fieldDescriptions))
		for index, field := range fieldDescriptions {
			row[field.Name] = values[index]
		}
		rowSamples, err := samplesForTaskRow(task, row)
		if err != nil {
			return collectedBatch{}, err
		}
		samples = append(samples, rowSamples...)
	}
	if err := rows.Err(); err != nil {
		return collectedBatch{}, err
	}
	return collectedBatch{samples: samples}, nil
}

func samplesForTaskRow(task metric.Task, row map[string]any) ([]collectedSample, error) {
	samples := make([]collectedSample, 0, len(task.Yields))
	for _, yield := range task.Yields {
		valueColumn, err := valueColumnForYield(yield)
		if err != nil {
			return nil, fmt.Errorf("decode task %q metric %q: %w", task.ID, yield.Metric, err)
		}
		rawValue, exists := row[valueColumn]
		if !exists {
			return nil, fmt.Errorf("decode task %q metric %q: column %q is missing", task.ID, yield.Metric, valueColumn)
		}
		if rawValue == nil {
			continue
		}
		var labels map[string]string
		databaseName := ""
		for _, dimension := range yield.Dimensions {
			rawLabel, exists := row[dimension]
			if !exists || rawLabel == nil {
				return nil, fmt.Errorf("decode task %q metric %q: dimension %q is missing", task.ID, yield.Metric, dimension)
			}
			label, ok := rawLabel.(string)
			if !ok {
				return nil, fmt.Errorf("decode task %q metric %q: dimension %q has type %T", task.ID, yield.Metric, dimension, rawLabel)
			}
			// 库这一维走 metric_series.database_name，不进 labels：它是时序表上唯一的具名维度，
			// 读取侧要靠它逐库展开、按目录里的聚合方式收敛成实例级值。其余维度（replica、slot）
			// 与库正交，继续走通用 labels。
			if dimension == metric.DimensionDatabase {
				databaseName = label
				continue
			}
			if labels == nil {
				labels = make(map[string]string, len(yield.Dimensions))
			}
			labels[dimension] = label
		}
		value, err := metricValue(yield.Metric, rawValue)
		if err != nil {
			return nil, fmt.Errorf("decode task %q metric %q: %w", task.ID, yield.Metric, err)
		}
		samples = append(samples, collectedSample{metricID: yield.Metric, value: value, databaseName: databaseName, labels: labels})
	}
	return samples, nil
}

func valueColumnForYield(yield metric.MetricYield) (string, error) {
	for index := len(yield.Columns) - 1; index >= 0; index-- {
		column := yield.Columns[index]
		isDimension := false
		for _, dimension := range yield.Dimensions {
			if column == dimension {
				isDimension = true
				break
			}
		}
		if !isDimension {
			return column, nil
		}
	}
	return "", fmt.Errorf("no value column is declared")
}

func metricValue(metricID metric.MetricID, raw any) (float64, error) {
	if encodings, exists := metric.NonNumericMetricEncodings[metricID.String()]; exists {
		state, ok := raw.(string)
		if !ok {
			return 0, fmt.Errorf("state value has type %T", raw)
		}
		value, exists := encodings[state]
		if !exists {
			return 0, fmt.Errorf("unknown state %q", state)
		}
		return value, nil
	}
	value, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("numeric value has type %T", raw)
	}
	return value, nil
}
