package collect

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestInitialPhaseIsStableAndDistributed(t *testing.T) {
	interval := 5 * time.Second
	first := initialPhase("00000000-0000-0000-0000-000000000001", metric.TaskProbe, interval)
	if repeated := initialPhase("00000000-0000-0000-0000-000000000001", metric.TaskProbe, interval); repeated != first {
		t.Fatalf("repeated phase = %s, want %s", repeated, first)
	}
	if first < 0 || first >= interval {
		t.Fatalf("phase = %s, want within [0, %s)", first, interval)
	}

	distinct := map[time.Duration]struct{}{first: {}}
	for _, key := range []string{
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000004",
	} {
		distinct[initialPhase(key, metric.TaskProbe, interval)] = struct{}{}
	}
	if len(distinct) < 2 {
		t.Fatalf("phases were not distributed: %v", distinct)
	}
}

func TestFailureBackoffCapsAtSixtySecondsAndExcludesProbe(t *testing.T) {
	tests := []struct {
		name     string
		kind     metric.TaskKind
		interval time.Duration
		failures int
		want     time.Duration
	}{
		{name: "probe never backs off", kind: metric.TaskKindProbe, interval: 5 * time.Second, failures: 8, want: 0},
		{name: "first failure", kind: metric.TaskKindSQL, interval: 5 * time.Second, failures: 1, want: 5 * time.Second},
		{name: "second failure", kind: metric.TaskKindSQL, interval: 5 * time.Second, failures: 2, want: 10 * time.Second},
		{name: "third failure", kind: metric.TaskKindSQL, interval: 5 * time.Second, failures: 3, want: 20 * time.Second},
		{name: "capped", kind: metric.TaskKindSQL, interval: 30 * time.Second, failures: 8, want: 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failureBackoff(tt.kind, tt.interval, tt.failures); got != tt.want {
				t.Fatalf("failureBackoff() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDispatcherEnforcesDualSlotsGlobalLimitsAndCapabilityReserve(t *testing.T) {
	dispatcher := newDispatcher(2, 4)
	if !dispatcher.admit(work{instanceID: "one", class: workCollectionQuery}) {
		t.Fatal("first collection query was not admitted")
	}
	if dispatcher.admit(work{instanceID: "one", class: workCollectionQuery}) {
		t.Fatal("same-instance collection queries overlapped")
	}
	if !dispatcher.admit(work{instanceID: "one", class: workProbe}) {
		t.Fatal("probe did not run alongside same-instance collection query")
	}
	if !dispatcher.admit(work{instanceID: "two", class: workProbe}) {
		t.Fatal("second probe was not admitted")
	}
	if dispatcher.admit(work{instanceID: "three", class: workProbe}) {
		t.Fatal("probe global limit was exceeded")
	}
	if !dispatcher.admit(work{instanceID: "two", class: workCollectionQuery}) {
		t.Fatal("second collection query was not admitted")
	}

	dispatcher.capabilityWaiting = true
	if dispatcher.admit(work{instanceID: "three", class: workCollectionQuery}) {
		t.Fatal("collection query consumed a capability-reserved slot")
	}
	for _, instanceID := range []string{"three", "four"} {
		if !dispatcher.admit(work{instanceID: instanceID, class: workCapability}) {
			t.Fatalf("capability task for %s did not receive its reserved slot", instanceID)
		}
	}
	if dispatcher.admit(work{instanceID: "five", class: workCapability}) {
		t.Fatal("query-channel global limit was exceeded")
	}
}

func TestPendingRunsKeepOnlyLatestDueIntent(t *testing.T) {
	pending := newPendingRuns()
	base := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	key := taskKey{instanceID: "one", taskID: metric.TaskProbe}

	if replaced, exists := pending.put(scheduledRun{key: key, dueAt: base}); exists {
		t.Fatalf("first due replaced %+v", replaced)
	}
	if replaced, exists := pending.put(scheduledRun{key: key, dueAt: base.Add(5 * time.Second)}); !exists || !replaced.dueAt.Equal(base) {
		t.Fatalf("second due replaced %+v, want %s", replaced, base)
	}
	second := base.Add(5 * time.Second)
	latest := base.Add(10 * time.Second)
	if replaced, exists := pending.put(scheduledRun{key: key, dueAt: latest}); !exists || !replaced.dueAt.Equal(second) {
		t.Fatalf("third due replaced %+v, want %s", replaced, second)
	}
	if pending.len() != 1 {
		t.Fatalf("pending count = %d, want 1", pending.len())
	}
	if got, ok := pending.take(key); !ok || !got.dueAt.Equal(latest) {
		t.Fatalf("latest pending run = %+v, %t; want due %s", got, ok, latest)
	}
}

func TestQueryTaskTimeoutUsesEightyPercentWithTenSecondCap(t *testing.T) {
	tests := []struct {
		interval time.Duration
		want     time.Duration
	}{
		{interval: 5 * time.Second, want: 4 * time.Second},
		{interval: 10 * time.Second, want: 8 * time.Second},
		{interval: 30 * time.Second, want: 10 * time.Second},
	}
	for _, tt := range tests {
		if got := taskTimeout(tt.interval); got != tt.want {
			t.Errorf("taskTimeout(%s) = %s, want %s", tt.interval, got, tt.want)
		}
	}
}
