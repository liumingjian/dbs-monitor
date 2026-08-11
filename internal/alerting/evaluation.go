package alerting

import "time"

type Point struct {
	Timestamp time.Time
	Value     float64
}

func AggregateWindow(points []Point, now time.Time, window time.Duration, aggregation string) (float64, bool) {
	windowStart := now.Add(-window)
	var value float64
	var latestTimestamp time.Time
	sampleCount := 0
	for _, point := range points {
		if !point.Timestamp.After(windowStart) || point.Timestamp.After(now) {
			continue
		}
		sampleCount++
		switch aggregation {
		case "latest":
			if latestTimestamp.IsZero() || point.Timestamp.After(latestTimestamp) {
				latestTimestamp = point.Timestamp
				value = point.Value
			}
		case "avg", "sum":
			value += point.Value
		case "max":
			if sampleCount == 1 || point.Value > value {
				value = point.Value
			}
		case "min":
			if sampleCount == 1 || point.Value < value {
				value = point.Value
			}
		case "count":
			value = float64(sampleCount)
		default:
			return 0, false
		}
	}
	if sampleCount == 0 {
		return 0, false
	}
	if aggregation == "avg" {
		value /= float64(sampleCount)
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

func Evaluate(
	value float64,
	operator string,
	threshold float64,
	recoveryOperator string,
	recoveryThreshold float64,
) Evaluation {
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
	EventPendingStarted EventKind = "PENDING_STARTED"
	EventFired          EventKind = "FIRED"
	EventUpdated        EventKind = "UPDATED"
	EventRecovered      EventKind = "RECOVERED"
	EventNoDataEntered  EventKind = "NO_DATA_ENTERED"
	EventNoDataExited   EventKind = "NO_DATA_EXITED"
	EventFrozen         EventKind = "FROZEN"
	EventUnfrozen       EventKind = "UNFROZEN"
)

func StateEvents(before, after State) []EventKind {
	if before == NO_DATA && after != NO_DATA {
		events := []EventKind{EventNoDataExited}
		if after == RECOVERED {
			events = append(events, EventRecovered)
		}
		return events
	}
	events := make([]EventKind, 0, 1)
	switch {
	case before != PENDING && after == PENDING:
		events = append(events, EventPendingStarted)
	case before != FIRING && after == FIRING:
		events = append(events, EventFired)
	case before == FIRING && after == FIRING:
		events = append(events, EventUpdated)
	case before != RECOVERED && after == RECOVERED:
		events = append(events, EventRecovered)
	case before != NO_DATA && after == NO_DATA:
		events = append(events, EventNoDataEntered)
	}
	return events
}
