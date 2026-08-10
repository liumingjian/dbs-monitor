package alerting

import (
	"reflect"
	"testing"
	"time"
)

func TestAggregateWindow(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	points := []Point{
		{Timestamp: now.Add(-70 * time.Second), Value: 100},
		{Timestamp: now.Add(-30 * time.Second), Value: 4},
		{Timestamp: now.Add(-10 * time.Second), Value: 8},
	}
	tests := []struct {
		aggregation string
		want        float64
	}{
		{aggregation: "latest", want: 8},
		{aggregation: "avg", want: 6},
		{aggregation: "max", want: 8},
		{aggregation: "min", want: 4},
		{aggregation: "sum", want: 12},
		{aggregation: "count", want: 2},
	}
	for _, test := range tests {
		t.Run(test.aggregation, func(t *testing.T) {
			got, ok := AggregateWindow(points, now, time.Minute, test.aggregation)
			if !ok || got != test.want {
				t.Fatalf("AggregateWindow() = %v, %t, want %v, true", got, ok, test.want)
			}
		})
	}
}

func TestHysteresisBandPreservesStateAndClearsCounters(t *testing.T) {
	evaluation := Evaluate(17, ">=", 20, "<", 15)
	if evaluation != Stable {
		t.Fatalf("evaluation = %v, want Stable", evaluation)
	}
	got := Step(Snapshot{State: FIRING, BreachCount: 1, RecoveryCount: 1, NoDataCount: 1}, evaluation, 2, 2)
	want := Snapshot{State: FIRING}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestStateEvents(t *testing.T) {
	tests := []struct {
		name          string
		before, after State
		want          []EventKind
	}{
		{name: "pending", before: OK, after: PENDING, want: []EventKind{PENDING_STARTED}},
		{name: "fired", before: PENDING, after: FIRING, want: []EventKind{FIRED}},
		{name: "updated", before: FIRING, after: FIRING, want: []EventKind{UPDATED}},
		{name: "recovered", before: FIRING, after: RECOVERED, want: []EventKind{RECOVERED_EVENT}},
		{name: "no data entered", before: FIRING, after: NO_DATA, want: []EventKind{NO_DATA_ENTERED}},
		{name: "no data exited", before: NO_DATA, after: FIRING, want: []EventKind{NO_DATA_EXITED}},
		{name: "no data recovered", before: NO_DATA, after: RECOVERED, want: []EventKind{NO_DATA_EXITED, RECOVERED_EVENT}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StateEvents(test.before, test.after); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("events = %v, want %v", got, test.want)
			}
		})
	}
}
