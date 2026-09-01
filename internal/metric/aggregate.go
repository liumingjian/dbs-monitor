package metric

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// 库级指标怎么收敛成实例级。
//
// 一条连接下可以有几十个库，列表与总览一律显示实例级的一个数——收敛方式逐指标记在
// metric_catalog 里，这里是执行它的地方。规则只有两条，但第二条是这一整个文件存在的理由：
//
//   - 计数与体积求和（TPS、回滚、死锁、体积）；
//   - 比率取加权平均，绝不取算术平均。一个 200GB 主库命中率崩到 60%，旁边二十个空库各自
//     100%，算术平均给出 98%——一个既没人经历过、也不会触发告警的数。加权之后结果落在主库
//     附近，因为权重（blks_hit + blks_read）本来就是「这个比率覆盖了多少真实工作量」。
//
// 这里是纯函数，不碰数据库：聚合规则是本项目少数几处「算错了不会报错、只会安静地骗人」的
// 逻辑之一，必须能被单元测试直接钉住（先例见 rate.go / rate_test.go）。

var (
	// ErrNotAggregatable 表示这个聚合方式收敛不出实例级值：实例级指标本来就没有可聚合的东西。
	ErrNotAggregatable = errors.New("metric: metric level value is not aggregatable")
	// ErrNoWeight 表示加权平均拿到的总权重是零或负——没有权重就没有加权平均。
	ErrNoWeight = errors.New("metric: weighted average has no weight")
)

// DatabaseValue 是某一时刻、某一个库上的一个取值。Weight 只有加权平均用得上。
type DatabaseValue struct {
	DatabaseName string
	Value        float64
	Weight       float64
}

// AggregateToInstance 把同一时刻的若干库级取值收敛成一个实例级取值。
//
// 空输入没有实例级值（返回 false），而不是 0：一个都没采到和真的是 0 是两件事，
// 后者会让「零 TPS」这种结论凭空出现。非有限值（NaN / Inf）整条丢掉，同理。
func AggregateToInstance(aggregation MetricAggregation, values []DatabaseValue) (float64, error) {
	usable := make([]DatabaseValue, 0, len(values))
	for _, value := range values {
		if math.IsNaN(value.Value) || math.IsInf(value.Value, 0) {
			continue
		}
		usable = append(usable, value)
	}
	if len(usable) == 0 {
		return 0, fmt.Errorf("%w: no database-level values", ErrNotAggregatable)
	}

	switch aggregation {
	case AggregationSum:
		total := 0.0
		for _, value := range usable {
			total += value.Value
		}
		return total, nil
	case AggregationWeightedAverage:
		weighted, totalWeight := 0.0, 0.0
		for _, value := range usable {
			weight := value.Weight
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
				continue
			}
			weighted += value.Value * weight
			totalWeight += weight
		}
		if totalWeight <= 0 {
			// 全实例一个块都没读过：比率此刻没有意义，不编一个出来。
			return 0, fmt.Errorf("%w: total weight is %v", ErrNoWeight, totalWeight)
		}
		return weighted / totalWeight, nil
	case AggregationNone:
		return 0, fmt.Errorf("%w: aggregation is NONE", ErrNotAggregatable)
	default:
		return 0, fmt.Errorf("%w: unknown aggregation %q", ErrNotAggregatable, aggregation)
	}
}

// DatabasePoint 是一条库级序列上的一个点。
type DatabasePoint struct {
	At     time.Time
	Value  float64
	Weight float64
}

// DatabaseSeries 是一个库在一段时间上的取值。
type DatabaseSeries struct {
	DatabaseName string
	Points       []DatabasePoint
}

// InstancePoint 是收敛之后的实例级点。
type InstancePoint struct {
	At    time.Time
	Value float64
}

// AggregateSeriesToInstance 逐时刻收敛：同一个采集时刻上的各库取值合成一个点，按时间排序返回。
//
// 按时刻对齐而不是按下标对齐：库是会新建和删除的，两条序列的点数根本不必相等；同一批采集
// 里所有库共用一个 observedAt（见 internal/collect/state.go 的写入事务），读取侧分桶时也共用
// 一个 date_bin 边界，所以时刻是可靠的对齐键。收敛不出值的时刻整个丢掉——图上那里是断的，
// 而不是一条掉到 0 的线。
func AggregateSeriesToInstance(aggregation MetricAggregation, series []DatabaseSeries) []InstancePoint {
	byTime := make(map[time.Time][]DatabaseValue)
	for _, item := range series {
		for _, point := range item.Points {
			at := point.At.UTC()
			byTime[at] = append(byTime[at], DatabaseValue{
				DatabaseName: item.DatabaseName, Value: point.Value, Weight: point.Weight,
			})
		}
	}

	points := make([]InstancePoint, 0, len(byTime))
	for at, values := range byTime {
		value, err := AggregateToInstance(aggregation, values)
		if err != nil {
			continue
		}
		points = append(points, InstancePoint{At: at, Value: value})
	}
	sort.Slice(points, func(left, right int) bool { return points[left].At.Before(points[right].At) })
	return points
}
