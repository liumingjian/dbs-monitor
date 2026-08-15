package collect

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
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

func TestInitialDueDistinguishesCapabilityAndCollectionTasks(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	instanceID := "00000000-0000-0000-0000-000000000001"
	collectionTask := metric.Task{ID: metric.TaskProbe, Interval: 5 * time.Second}
	collectionRun := scheduledRun{
		key:      taskKey{instanceID: instanceID, taskID: metric.TaskProbe},
		task:     collectionTask,
		interval: collectionTask.Interval,
	}

	if got := initialDue(now, scheduledRun{task: capabilitySnapshotTask}); !got.Equal(now) {
		t.Fatalf("initial capability due = %s, want %s", got, now)
	}
	if got := initialDue(now, collectionRun); !got.After(now) {
		t.Fatalf("initial collection due = %s, want staggered after %s", got, now)
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

	dispatcher.capabilitySnapshotWaiting = true
	if dispatcher.admit(work{instanceID: "three", class: workCollectionQuery}) {
		t.Fatal("collection query consumed a capability-reserved slot")
	}
	for _, instanceID := range []string{"three", "four"} {
		if !dispatcher.admit(work{instanceID: instanceID, class: workCapabilitySnapshot}) {
			t.Fatalf("capability task for %s did not receive its reserved slot", instanceID)
		}
	}
	if dispatcher.admit(work{instanceID: "five", class: workCapabilitySnapshot}) {
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

func TestPendingRunsPrioritizeCapabilitySnapshot(t *testing.T) {
	pending := newPendingRuns()
	due := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	query := metric.Task{ID: metric.TaskStatActivity, Kind: metric.TaskKindSQL}
	pending.put(scheduledRun{key: taskKey{instanceID: "one", taskID: query.ID}, task: query, dueAt: due.Add(-time.Second)})
	pending.put(scheduledRun{key: taskKey{instanceID: "one", taskID: capabilitySnapshotTask.ID}, task: capabilitySnapshotTask, dueAt: due})

	ordered := pending.ordered()
	if len(ordered) != 2 || ordered[0].task.ID != capabilitySnapshotTask.ID {
		t.Fatalf("pending order = %+v, want capability snapshot first", ordered)
	}
	if got := classFor(capabilitySnapshotTask); got != workCapabilitySnapshot {
		t.Fatalf("capability work class = %d, want %d", got, workCapabilitySnapshot)
	}
}

func TestSchedulerSummaryUpdatesPlatformHealthWithBackpressureDetail(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), log.New(io.Discard, "", 0))
	expiresAt := now.Add(90 * 24 * time.Hour)
	health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{PrebuildDaysRemaining: 7}))
	health.Update(now, platformhealth.CertificateSource(now, &expiresAt))
	health.Update(now, platformhealth.SourceSnapshot{Source: platformhealth.SourceAgentIngress, Status: platformhealth.StatusOK, Code: "AGENT_INGRESS_READY"})
	health.Update(now, platformhealth.CredentialSource(platformhealth.CredentialFacts{Available: true}))
	health.Update(now, platformhealth.SourceSnapshot{Source: platformhealth.SourceTLSCertificate, Status: platformhealth.StatusOK, Code: "FACT_AVAILABLE"})
	health.Update(now, platformhealth.SourceSnapshot{Source: platformhealth.SourcePlatformDatabaseCapacity, Status: platformhealth.StatusOK, Code: "FACT_AVAILABLE"})
	service := &Service{health: health}
	if err := service.SetDiskMonitor(t.TempDir(), platformhealth.DefaultDiskThresholds()); err != nil {
		t.Fatalf("configure disk monitor: %v", err)
	}
	scheduler := &centralScheduler{
		service:    service,
		dispatcher: newDispatcher(1, 2),
		pending:    newPendingRuns(),
		counts:     newSchedulerCounts(),
	}
	scheduler.dispatcher.activeProbes = 1
	scheduler.pending.put(scheduledRun{key: taskKey{instanceID: "one", taskID: metric.TaskProbe}})
	scheduler.counts.skipped = 7

	scheduler.refreshDiskHealth(now)
	scheduler.publishPlatformHealth(now, nil, nil)

	source := health.Source(platformhealth.SourceCollectionScheduler)
	if source.Status != platformhealth.StatusDegraded || source.Pending == nil || *source.Pending != 1 ||
		source.SkippedBackpressure == nil || *source.SkippedBackpressure != 7 {
		t.Fatalf("scheduler platform health = %+v, want DEGRADED pending=1 skipped_backpressure=7", source)
	}
	diskSource := health.Source(platformhealth.SourceDisk)
	if diskSource.DiskLevel == nil || diskSource.DiskUsagePercent == nil || diskSource.Code == "DISK_USAGE_UNAVAILABLE" {
		t.Fatalf("scheduler disk health = %+v, want sampled filesystem facts", diskSource)
	}
	if got := health.Current().Status; got != platformhealth.StatusDegraded {
		t.Fatalf("aggregate platform health = %s, want DEGRADED", got)
	}
}
