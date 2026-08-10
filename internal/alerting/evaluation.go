package alerting

import "time"

type Point struct {
	Timestamp time.Time
	Value     float64
}

func AggregateWindow(points []Point, now time.Time, window time.Duration, aggregation string) (float64, bool) {
	lower := now.Add(-window)
	var value float64
	var latest time.Time
	count := 0
	for _, point := range points {
		if !point.Timestamp.After(lower) || point.Timestamp.After(now) {
			continue
		}
		count++
		switch aggregation {
		case "latest":
			if latest.IsZero() || point.Timestamp.After(latest) {
				latest = point.Timestamp
				value = point.Value
			}
		case "avg", "sum":
			value += point.Value
		case "max":
			if count == 1 || point.Value > value {
				value = point.Value
			}
		case "min":
			if count == 1 || point.Value < value {
				value = point.Value
			}
		case "count":
			value = float64(count)
		default:
			return 0, false
		}
	}
	if count == 0 {
		return 0, false
	}
	if aggregation == "avg" {
		value /= float64(count)
	}
	return value, true
}

func Compare(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "=":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

func Evaluate(value float64, operator string, threshold float64, recoveryOperator string, recoveryThreshold float64) Evaluation {
	if Compare(value, operator, threshold) {
		return Breaching
	}
	if Compare(value, recoveryOperator, recoveryThreshold) {
		return Recovering
	}
	return Stable
}

type EventKind string

const (
	PENDING_STARTED EventKind = "PENDING_STARTED"
	FIRED           EventKind = "FIRED"
	UPDATED         EventKind = "UPDATED"
	RECOVERED_EVENT EventKind = "RECOVERED"
	NO_DATA_ENTERED EventKind = "NO_DATA_ENTERED"
	NO_DATA_EXITED  EventKind = "NO_DATA_EXITED"
)

func StateEvents(before, after State) []EventKind {
	if before == NO_DATA && after != NO_DATA {
		events := []EventKind{NO_DATA_EXITED}
		if after == RECOVERED {
			events = append(events, RECOVERED_EVENT)
		}
		return events
	}
	events := make([]EventKind, 0, 1)
	switch {
	case before != PENDING && after == PENDING:
		events = append(events, PENDING_STARTED)
	case before != FIRING && after == FIRING:
		events = append(events, FIRED)
	case before == FIRING && after == FIRING:
		events = append(events, UPDATED)
	case before != RECOVERED && after == RECOVERED:
		events = append(events, RECOVERED_EVENT)
	case before != NO_DATA && after == NO_DATA:
		events = append(events, NO_DATA_ENTERED)
	}
	return events
}
