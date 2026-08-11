package alerting

import (
	"reflect"
	"testing"
)

func TestStepStateTransitions(t *testing.T) {
	tests := []struct {
		name         string
		current      Snapshot
		input        Evaluation
		triggerCount int
		want         Snapshot
	}{
		{name: "ok breach starts pending", current: Snapshot{State: OK}, input: Breaching, want: Snapshot{State: PENDING, BreachCount: 1}},
		{name: "ok breach fires immediately", current: Snapshot{State: OK}, input: Breaching, triggerCount: 1, want: Snapshot{State: FIRING, BreachCount: 1}},
		{name: "ok recovery stays ok", current: Snapshot{State: OK}, input: Recovering, want: Snapshot{State: OK}},
		{name: "ok stable stays ok", current: Snapshot{State: OK}, input: Stable, want: Snapshot{State: OK}},
		{name: "ok first missing stays ok", current: Snapshot{State: OK}, input: Missing, want: Snapshot{State: OK, NoDataCount: 1}},
		{name: "ok second missing enters no data", current: Snapshot{State: OK, NoDataCount: 1}, input: Missing, want: Snapshot{State: NO_DATA, StateBeforeNoData: OK, NoDataCount: 2}},
		{name: "pending breach fires", current: Snapshot{State: PENDING, BreachCount: 1}, input: Breaching, want: Snapshot{State: FIRING, BreachCount: 2}},
		{name: "pending recovery returns ok", current: Snapshot{State: PENDING, BreachCount: 1}, input: Recovering, want: Snapshot{State: OK}},
		{name: "pending hysteresis band returns ok", current: Snapshot{State: PENDING, BreachCount: 1}, input: Stable, want: Snapshot{State: OK}},
		{name: "pending first missing clears breach", current: Snapshot{State: PENDING, BreachCount: 1}, input: Missing, want: Snapshot{State: PENDING, NoDataCount: 1}},
		{name: "pending second missing enters no data", current: Snapshot{State: PENDING, NoDataCount: 1}, input: Missing, want: Snapshot{State: NO_DATA, StateBeforeNoData: PENDING, NoDataCount: 2}},
		{name: "firing breach stays firing", current: Snapshot{State: FIRING, RecoveryCount: 1}, input: Breaching, want: Snapshot{State: FIRING}},
		{name: "firing recovery accumulates", current: Snapshot{State: FIRING}, input: Recovering, want: Snapshot{State: FIRING, RecoveryCount: 1}},
		{name: "firing recovery completes", current: Snapshot{State: FIRING, RecoveryCount: 1}, input: Recovering, want: Snapshot{State: RECOVERED, RecoveryCount: 2}},
		{name: "firing hysteresis band stays firing", current: Snapshot{State: FIRING, RecoveryCount: 1}, input: Stable, want: Snapshot{State: FIRING}},
		{name: "no data returns to firing before counting", current: Snapshot{State: NO_DATA, StateBeforeNoData: FIRING}, input: Recovering, want: Snapshot{State: FIRING}},
		{name: "no data returns to pending before counting", current: Snapshot{State: NO_DATA, StateBeforeNoData: PENDING}, input: Breaching, want: Snapshot{State: PENDING}},
		{name: "no data returns to ok", current: Snapshot{State: NO_DATA, StateBeforeNoData: OK}, input: Recovering, want: Snapshot{State: OK}},
		{name: "no data returns on stable input", current: Snapshot{State: NO_DATA, StateBeforeNoData: FIRING, NoDataCount: 2}, input: Stable, want: Snapshot{State: FIRING}},
		{name: "no data remains while missing", current: Snapshot{State: NO_DATA, StateBeforeNoData: FIRING, NoDataCount: 2}, input: Missing, want: Snapshot{State: NO_DATA, StateBeforeNoData: FIRING, NoDataCount: 3}},
		{name: "recovered breach starts new pending lifecycle", current: Snapshot{State: RECOVERED}, input: Breaching, want: Snapshot{State: PENDING, BreachCount: 1}},
		{name: "recovered breach fires immediate lifecycle", current: Snapshot{State: RECOVERED}, input: Breaching, triggerCount: 1, want: Snapshot{State: FIRING, BreachCount: 1}},
		{name: "recovered recovery stays recovered", current: Snapshot{State: RECOVERED}, input: Recovering, want: Snapshot{State: RECOVERED}},
		{name: "recovered stable stays recovered", current: Snapshot{State: RECOVERED}, input: Stable, want: Snapshot{State: RECOVERED}},
		{name: "recovered missing stays recovered", current: Snapshot{State: RECOVERED}, input: Missing, want: Snapshot{State: RECOVERED}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			triggerCount := test.triggerCount
			if triggerCount == 0 {
				triggerCount = 2
			}
			got := Step(test.current, test.input, triggerCount, 2)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Step() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestStepNoDataPolicy(t *testing.T) {
	tests := []struct {
		name    string
		current Snapshot
		input   Evaluation
		want    Snapshot
	}{
		{
			name:    "first missing evaluation preserves firing and clears recovery",
			current: Snapshot{State: FIRING, RecoveryCount: 1},
			input:   Missing,
			want:    Snapshot{State: FIRING, NoDataCount: 1},
		},
		{
			name:    "second missing evaluation enters no data",
			current: Snapshot{State: FIRING, NoDataCount: 1},
			input:   Missing,
			want:    Snapshot{State: NO_DATA, StateBeforeNoData: FIRING, NoDataCount: 2},
		},
		{
			name:    "ignored missing clears incomplete counters without accumulating no data",
			current: Snapshot{State: PENDING, BreachCount: 1, RecoveryCount: 1, NoDataCount: 1},
			input:   MissingIgnored,
			want:    Snapshot{State: PENDING},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Step(test.current, test.input, 2, 2)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Step() = %+v, want %+v", got, test.want)
			}
		})
	}
}
