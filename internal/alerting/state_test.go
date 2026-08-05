package alerting

import "testing"

func TestStepWalkingSkeletonLifecycle(t *testing.T) {
	current := Snapshot{State: OK}
	steps := []struct {
		name string
		in   Evaluation
		want State
	}{
		{"first breach", Breaching, PENDING},
		{"second breach", Breaching, FIRING},
		{"first missing evaluation", Missing, FIRING},
		{"second missing evaluation", Missing, NO_DATA},
		{"data returns", Breaching, FIRING},
		{"first recovery", Recovering, FIRING},
		{"second recovery", Recovering, RECOVERED},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			current = Step(current, step.in, 2, 2)
			if current.State != step.want {
				t.Fatalf("state = %s, want %s", current.State, step.want)
			}
		})
	}
}

func TestStepRecoversFromNoDataWithoutPriorFiring(t *testing.T) {
	current := Snapshot{State: OK}
	current = Step(current, Missing, 2, 2)
	current = Step(current, Missing, 2, 2)
	if current.State != NO_DATA {
		t.Fatalf("state after missing evaluations = %s, want %s", current.State, NO_DATA)
	}
	current = Step(current, Recovering, 2, 2)
	if current.State != RECOVERED {
		t.Fatalf("state after data returns = %s, want %s", current.State, RECOVERED)
	}
}

func TestMissingEvaluationClearsIncompleteCounters(t *testing.T) {
	current := Step(Snapshot{State: OK}, Breaching, 3, 3)
	if current.BreachCount != 1 {
		t.Fatalf("breach count = %d, want 1", current.BreachCount)
	}
	current = Step(current, Missing, 3, 3)
	if current.BreachCount != 0 || current.RecoveryCount != 0 {
		t.Fatalf("missing evaluation left incomplete counters: %+v", current)
	}
}
