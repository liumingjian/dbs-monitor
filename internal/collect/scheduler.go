package collect

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sort"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

type workClass uint8

const (
	workProbe workClass = iota
	workCollectionQuery
	workCapabilitySnapshot
)

const (
	capabilitySnapshotTaskID        metric.TaskID = "capability.snapshot"
	capabilitySnapshotReservedSlots               = 4
)

var capabilitySnapshotTask = metric.Task{
	ID:       capabilitySnapshotTaskID,
	Kind:     metric.TaskKindSQL,
	Interval: metric.CapabilitySnapshotTTL,
}

func isCapabilitySnapshotTask(task metric.Task) bool {
	return task.ID == capabilitySnapshotTaskID
}

type work struct {
	instanceID string
	class      workClass
}

type taskKey struct {
	instanceID string
	taskID     metric.TaskID
}

type scheduledRun struct {
	key       taskKey
	dueAt     time.Time
	startedAt time.Time
	target    instance.ListCollectionTargetsRow
	task      metric.Task
	interval  time.Duration
}

type pendingRuns struct {
	items map[taskKey]scheduledRun
}

func newPendingRuns() *pendingRuns {
	return &pendingRuns{items: map[taskKey]scheduledRun{}}
}

func (pending *pendingRuns) put(run scheduledRun) (scheduledRun, bool) {
	previous, replaced := pending.items[run.key]
	pending.items[run.key] = run
	return previous, replaced
}

func (pending *pendingRuns) take(key taskKey) (scheduledRun, bool) {
	run, exists := pending.items[key]
	if exists {
		delete(pending.items, key)
	}
	return run, exists
}

func (pending *pendingRuns) len() int { return len(pending.items) }

func (pending *pendingRuns) ordered() []scheduledRun {
	runs := make([]scheduledRun, 0, len(pending.items))
	for _, run := range pending.items {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(left, right int) bool {
		leftCapability := isCapabilitySnapshotTask(runs[left].task)
		rightCapability := isCapabilitySnapshotTask(runs[right].task)
		if leftCapability != rightCapability {
			return leftCapability
		}
		if !runs[left].dueAt.Equal(runs[right].dueAt) {
			return runs[left].dueAt.Before(runs[right].dueAt)
		}
		return taskKeyLess(runs[left].key, runs[right].key)
	})
	return runs
}

func (pending *pendingRuns) hasCapabilitySnapshot() bool {
	for _, run := range pending.items {
		if isCapabilitySnapshotTask(run.task) {
			return true
		}
	}
	return false
}

type dispatcher struct {
	probeLimit                int
	queryLimit                int
	activeProbes              int
	activeQueries             int
	activeCollectionQueries   int
	capabilitySnapshotWaiting bool
	instanceProbe             map[string]bool
	instanceQuery             map[string]bool
}

func newDispatcher(probeLimit, queryLimit int) *dispatcher {
	return &dispatcher{
		probeLimit:    probeLimit,
		queryLimit:    queryLimit,
		instanceProbe: map[string]bool{},
		instanceQuery: map[string]bool{},
	}
}

func (dispatcher *dispatcher) admit(item work) bool {
	if item.class == workProbe {
		if dispatcher.activeProbes >= dispatcher.probeLimit || dispatcher.instanceProbe[item.instanceID] {
			return false
		}
		dispatcher.activeProbes++
		dispatcher.instanceProbe[item.instanceID] = true
		return true
	}
	if dispatcher.activeQueries >= dispatcher.queryLimit || dispatcher.instanceQuery[item.instanceID] {
		return false
	}
	if item.class == workCollectionQuery && dispatcher.capabilitySnapshotWaiting {
		reserved := min(capabilitySnapshotReservedSlots, dispatcher.queryLimit)
		if dispatcher.activeCollectionQueries >= dispatcher.queryLimit-reserved {
			return false
		}
	}
	dispatcher.activeQueries++
	if item.class == workCollectionQuery {
		dispatcher.activeCollectionQueries++
	}
	dispatcher.instanceQuery[item.instanceID] = true
	return true
}

func (dispatcher *dispatcher) finish(item work) {
	if item.class == workProbe {
		if dispatcher.instanceProbe[item.instanceID] {
			delete(dispatcher.instanceProbe, item.instanceID)
			dispatcher.activeProbes--
		}
		return
	}
	if dispatcher.instanceQuery[item.instanceID] {
		delete(dispatcher.instanceQuery, item.instanceID)
		dispatcher.activeQueries--
		if item.class == workCollectionQuery {
			dispatcher.activeCollectionQueries--
		}
	}
}

func initialPhase(instanceID string, taskID metric.TaskID, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(instanceID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(taskID))
	return time.Duration(hash.Sum64() % uint64(interval))
}

func failureBackoff(kind metric.TaskKind, interval time.Duration, failures int) time.Duration {
	if kind == metric.TaskKindProbe || failures <= 0 {
		return 0
	}
	backoff := interval
	for count := 1; count < failures && backoff < 60*time.Second; count++ {
		backoff *= 2
	}
	if backoff > 60*time.Second {
		return 60 * time.Second
	}
	return backoff
}

type executionOutcome struct {
	run      scheduledRun
	result   taskResult
	duration time.Duration
	err      error
}

type scheduleEntry struct {
	template scheduledRun
	nextDue  time.Time
}

type schedulerCounts struct {
	success            int64
	failed             int64
	timedOut           int64
	skipped            int64
	backoff            int64
	dispatchDelayCount int64
	dispatchDelayTotal time.Duration
	dispatchDelayMax   time.Duration
	taskDurations      map[metric.TaskID]durationSummary
}

type durationSummary struct {
	count int64
	total time.Duration
	max   time.Duration
}

type centralScheduler struct {
	service    *Service
	dispatcher *dispatcher
	pending    *pendingRuns
	schedule   map[taskKey]scheduleEntry
	completed  chan executionOutcome
	counts     schedulerCounts
	lastLog    time.Time
}

func newCentralScheduler(service *Service) *centralScheduler {
	return &centralScheduler{
		service:    service,
		dispatcher: newDispatcher(service.config.ProbeConcurrency, service.config.QueryConcurrency),
		pending:    newPendingRuns(),
		schedule:   map[taskKey]scheduleEntry{},
		completed:  make(chan executionOutcome, service.config.ProbeConcurrency+service.config.QueryConcurrency),
		counts:     newSchedulerCounts(),
		lastLog:    service.clock.Now().UTC(),
	}
}

func (scheduler *centralScheduler) refresh(ctx context.Context, now time.Time) error {
	targets, err := instance.New(scheduler.service.platform).ListCollectionTargets(ctx)
	if err != nil {
		return fmt.Errorf("list collection targets: %w", err)
	}
	tasks := scheduledTasks()
	active := make(map[taskKey]struct{}, len(targets)*(len(tasks)+1))
	for _, target := range targets {
		if err := scheduler.service.ensureTaskStates(ctx, target.ID); err != nil {
			return err
		}
		intervals, err := scheduler.service.taskIntervals(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, task := range tasks {
			template := newScheduledRun(target, task, intervals[task.ID], time.Time{})
			active[template.key] = struct{}{}
			entry, exists := scheduler.schedule[template.key]
			if !exists || entry.template.interval != template.interval {
				entry.nextDue = nextDueAfter(now, initialPhase(template.key.instanceID, task.ID, template.interval), template.interval)
			}
			entry.template = template
			scheduler.schedule[template.key] = entry
		}
		template := newScheduledRun(target, capabilitySnapshotTask, 0, time.Time{})
		active[template.key] = struct{}{}
		entry, exists := scheduler.schedule[template.key]
		if !exists {
			entry.nextDue = nextDueAfter(now, initialPhase(template.key.instanceID, capabilitySnapshotTask.ID, capabilitySnapshotTask.Interval), capabilitySnapshotTask.Interval)
		}
		entry.template = template
		scheduler.schedule[template.key] = entry
	}
	for key := range scheduler.schedule {
		if _, exists := active[key]; !exists {
			delete(scheduler.schedule, key)
			_, _ = scheduler.pending.take(key)
		}
	}
	return nil
}

func (scheduler *centralScheduler) accrue(ctx context.Context, now time.Time) {
	keys := make([]taskKey, 0, len(scheduler.schedule))
	for key := range scheduler.schedule {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return taskKeyLess(keys[left], keys[right])
	})
	for _, key := range keys {
		entry := scheduler.schedule[key]
		for !entry.nextDue.After(now) {
			run := entry.template
			run.dueAt = entry.nextDue
			if replaced, exists := scheduler.pending.put(run); exists {
				if isCapabilitySnapshotTask(replaced.task) {
					_, err := scheduler.service.storeCapabilitySnapshot(
						ctx,
						replaced.target.ID,
						scheduler.service.clock.Now().UTC(),
						metric.UnknownCapabilityStates(),
					)
					if err != nil {
						log.Printf("record capability backpressure failed: instance_id=%s error=%v", replaced.key.instanceID, err)
					} else {
						scheduler.counts.skipped++
					}
				} else if err := scheduler.service.recordUnmet(ctx, replaced, resultSkippedBackpressure, time.Time{}); err != nil {
					log.Printf("record collection backpressure skip failed: instance_id=%s task_id=%s error=%v", replaced.key.instanceID, replaced.key.taskID, err)
				} else {
					scheduler.counts.skipped++
				}
			}
			entry.nextDue = entry.nextDue.Add(entry.template.interval)
		}
		scheduler.schedule[key] = entry
	}
}

func (scheduler *centralScheduler) dispatch(ctx context.Context) {
	scheduler.dispatcher.capabilitySnapshotWaiting = scheduler.pending.hasCapabilitySnapshot()
	for _, run := range scheduler.pending.ordered() {
		eligible, err := scheduler.service.nextEligible(ctx, run)
		if err != nil {
			log.Printf("read collection backoff failed: instance_id=%s task_id=%s error=%v", run.key.instanceID, run.key.taskID, err)
			continue
		}
		if !eligible.IsZero() && run.dueAt.Before(eligible) {
			_, _ = scheduler.pending.take(run.key)
			if err := scheduler.service.recordUnmet(ctx, run, resultBackoff, eligible); err != nil {
				log.Printf("record collection backoff failed: instance_id=%s task_id=%s error=%v", run.key.instanceID, run.key.taskID, err)
			} else {
				scheduler.counts.backoff++
			}
			continue
		}
		item := work{instanceID: run.key.instanceID, class: classFor(run.task)}
		if !scheduler.dispatcher.admit(item) {
			continue
		}
		delay := scheduler.service.clock.Now().UTC().Sub(run.dueAt)
		if delay < 0 {
			delay = 0
		}
		scheduler.counts.dispatchDelayCount++
		scheduler.counts.dispatchDelayTotal += delay
		if delay > scheduler.counts.dispatchDelayMax {
			scheduler.counts.dispatchDelayMax = delay
		}
		_, _ = scheduler.pending.take(run.key)
		scheduler.dispatcher.capabilitySnapshotWaiting = scheduler.pending.hasCapabilitySnapshot()
		go func(run scheduledRun) {
			outcome := scheduler.service.executeTask(ctx, run)
			select {
			case scheduler.completed <- outcome:
			case <-ctx.Done():
			}
		}(run)
	}
}

func (scheduler *centralScheduler) complete(outcome executionOutcome) {
	scheduler.dispatcher.finish(work{instanceID: outcome.run.key.instanceID, class: classFor(outcome.run.task)})
	if outcome.err != nil {
		log.Printf("collection task persistence failed: instance_id=%s task_id=%s error=%v", outcome.run.key.instanceID, outcome.run.key.taskID, outcome.err)
		return
	}
	switch outcome.result {
	case resultSuccess:
		scheduler.counts.success++
	case resultTimedOut:
		scheduler.counts.timedOut++
	default:
		scheduler.counts.failed++
	}
	duration := scheduler.counts.taskDurations[outcome.run.task.ID]
	duration.count++
	duration.total += outcome.duration
	if outcome.duration > duration.max {
		duration.max = outcome.duration
	}
	scheduler.counts.taskDurations[outcome.run.task.ID] = duration
	log.Printf("collection task result: instance_id=%s task_id=%s result=%s duration_ms=%d",
		outcome.run.key.instanceID, outcome.run.key.taskID, outcome.result, outcome.duration.Milliseconds())
}

func (scheduler *centralScheduler) logSummary(now time.Time) {
	if now.Sub(scheduler.lastLog) < time.Minute {
		return
	}
	scheduler.lastLog = now
	var averageDelay time.Duration
	if scheduler.counts.dispatchDelayCount > 0 {
		averageDelay = scheduler.counts.dispatchDelayTotal / time.Duration(scheduler.counts.dispatchDelayCount)
	}
	idle, rebuilds := scheduler.service.queryConnectionSummary(scheduler.dispatcher.activeQueries)
	log.Printf("collection scheduler summary: probe_capacity=%d probe_active=%d query_capacity=%d query_active=%d pending=%d dispatch_delay_avg_ms=%d dispatch_delay_max_ms=%d success=%d failed=%d timeout=%d skipped_backpressure=%d backoff=%d pool_active=%d pool_idle=%d pool_rebuild=%d",
		scheduler.dispatcher.probeLimit, scheduler.dispatcher.activeProbes,
		scheduler.dispatcher.queryLimit, scheduler.dispatcher.activeQueries, scheduler.pending.len(),
		averageDelay.Milliseconds(), scheduler.counts.dispatchDelayMax.Milliseconds(),
		scheduler.counts.success, scheduler.counts.failed, scheduler.counts.timedOut,
		scheduler.counts.skipped, scheduler.counts.backoff,
		scheduler.dispatcher.activeQueries, idle, rebuilds)
	for taskID, duration := range scheduler.counts.taskDurations {
		average := duration.total / time.Duration(duration.count)
		log.Printf("collection scheduler task summary: task_id=%s count=%d duration_avg_ms=%d duration_max_ms=%d",
			taskID, duration.count, average.Milliseconds(), duration.max.Milliseconds())
	}
	scheduler.counts = newSchedulerCounts()
}

func newSchedulerCounts() schedulerCounts {
	return schedulerCounts{taskDurations: map[metric.TaskID]durationSummary{}}
}

func classFor(task metric.Task) workClass {
	if isCapabilitySnapshotTask(task) {
		return workCapabilitySnapshot
	}
	if task.Kind == metric.TaskKindProbe {
		return workProbe
	}
	return workCollectionQuery
}

func taskKeyLess(left, right taskKey) bool {
	if left.instanceID == right.instanceID {
		return left.taskID < right.taskID
	}
	return left.instanceID < right.instanceID
}

func nextDueAfter(now time.Time, phase, interval time.Duration) time.Time {
	base := time.Unix(0, int64(phase)).UTC()
	if now.Before(base) {
		return base
	}
	return base.Add((now.Sub(base)/interval + 1) * interval)
}
