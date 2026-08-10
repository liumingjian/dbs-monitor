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
	workRegular
	workCapability
)

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

func (pending *pendingRuns) put(run scheduledRun) *scheduledRun {
	var replaced *scheduledRun
	if previous, exists := pending.items[run.key]; exists {
		copy := previous
		replaced = &copy
	}
	pending.items[run.key] = run
	return replaced
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
		if runs[left].dueAt.Equal(runs[right].dueAt) {
			if runs[left].key.instanceID == runs[right].key.instanceID {
				return runs[left].key.taskID < runs[right].key.taskID
			}
			return runs[left].key.instanceID < runs[right].key.instanceID
		}
		return runs[left].dueAt.Before(runs[right].dueAt)
	})
	return runs
}

type dispatcher struct {
	probeLimit, normalLimit       int
	activeProbes, activeNormal    int
	activeRegular                 int
	capabilityWaiting             bool
	instanceProbe, instanceNormal map[string]bool
}

func newDispatcher(probeLimit, normalLimit int) *dispatcher {
	return &dispatcher{
		probeLimit: probeLimit, normalLimit: normalLimit,
		instanceProbe: map[string]bool{}, instanceNormal: map[string]bool{},
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
	if dispatcher.activeNormal >= dispatcher.normalLimit || dispatcher.instanceNormal[item.instanceID] {
		return false
	}
	if item.class == workRegular && dispatcher.capabilityWaiting {
		reserved := min(4, dispatcher.normalLimit)
		if dispatcher.activeRegular >= dispatcher.normalLimit-reserved {
			return false
		}
	}
	dispatcher.activeNormal++
	if item.class == workRegular {
		dispatcher.activeRegular++
	}
	dispatcher.instanceNormal[item.instanceID] = true
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
	if dispatcher.instanceNormal[item.instanceID] {
		delete(dispatcher.instanceNormal, item.instanceID)
		dispatcher.activeNormal--
		if item.class == workRegular {
			dispatcher.activeRegular--
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

func safeErrorMessage(code string, _ error) string {
	switch code {
	case "CONNECTION_FAILED":
		return "target connection failed"
	case "QUERY_FAILED":
		return "collection query failed"
	case "TIMEOUT":
		return "collection deadline exceeded"
	case "SKIPPED_BACKPRESSURE":
		return "collection skipped because scheduler capacity was unavailable"
	case "BACKOFF":
		return "collection deferred by failure backoff"
	default:
		return "collection failed"
	}
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
	success, failed, timedOut, skipped, backoff int64
	dispatchCount                               int64
	dispatchTotal, dispatchMax                  time.Duration
	durations                                   map[metric.TaskID]durationSummary
}

type durationSummary struct {
	count      int64
	total, max time.Duration
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
		pending:    newPendingRuns(), schedule: map[taskKey]scheduleEntry{},
		completed: make(chan executionOutcome, service.config.ProbeConcurrency+service.config.QueryConcurrency),
		counts:    newSchedulerCounts(),
		lastLog:   service.clock.Now().UTC(),
	}
}

func (scheduler *centralScheduler) refresh(ctx context.Context, now time.Time) error {
	targets, err := instance.New(scheduler.service.platform).ListCollectionTargets(ctx)
	if err != nil {
		return fmt.Errorf("list collection targets: %w", err)
	}
	active := make(map[taskKey]bool, len(targets)*len(scheduledTasks()))
	for _, target := range targets {
		if err := scheduler.service.ensureTaskStates(ctx, target.ID); err != nil {
			return err
		}
		intervals, err := scheduler.service.taskIntervals(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, task := range scheduledTasks() {
			template := newScheduledRun(target, task, intervals[task.ID], time.Time{})
			active[template.key] = true
			entry, exists := scheduler.schedule[template.key]
			if !exists || entry.template.interval != template.interval {
				entry.nextDue = nextDueAfter(now, initialPhase(template.key.instanceID, task.ID, template.interval), template.interval)
			}
			entry.template = template
			scheduler.schedule[template.key] = entry
		}
	}
	for key := range scheduler.schedule {
		if !active[key] {
			delete(scheduler.schedule, key)
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
		if keys[left].instanceID == keys[right].instanceID {
			return keys[left].taskID < keys[right].taskID
		}
		return keys[left].instanceID < keys[right].instanceID
	})
	for _, key := range keys {
		entry := scheduler.schedule[key]
		for !entry.nextDue.After(now) {
			run := entry.template
			run.dueAt = entry.nextDue
			if replaced := scheduler.pending.put(run); replaced != nil {
				if err := scheduler.service.recordUnmet(ctx, *replaced, resultSkippedBackpressure, time.Time{}); err != nil {
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
		scheduler.counts.dispatchCount++
		scheduler.counts.dispatchTotal += delay
		if delay > scheduler.counts.dispatchMax {
			scheduler.counts.dispatchMax = delay
		}
		_, _ = scheduler.pending.take(run.key)
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
	duration := scheduler.counts.durations[outcome.run.task.ID]
	duration.count++
	duration.total += outcome.duration
	if outcome.duration > duration.max {
		duration.max = outcome.duration
	}
	scheduler.counts.durations[outcome.run.task.ID] = duration
	log.Printf("collection task result: instance_id=%s task_id=%s result=%s duration_ms=%d",
		outcome.run.key.instanceID, outcome.run.key.taskID, outcome.result, outcome.duration.Milliseconds())
}

func (scheduler *centralScheduler) logSummary(now time.Time) {
	if now.Sub(scheduler.lastLog) < time.Minute {
		return
	}
	scheduler.lastLog = now
	var averageDelay time.Duration
	if scheduler.counts.dispatchCount > 0 {
		averageDelay = scheduler.counts.dispatchTotal / time.Duration(scheduler.counts.dispatchCount)
	}
	idle, rebuilds := scheduler.service.poolSummary(scheduler.dispatcher.activeNormal)
	log.Printf("collection scheduler summary: probe_capacity=%d probe_active=%d query_capacity=%d query_active=%d pending=%d dispatch_delay_avg_ms=%d dispatch_delay_max_ms=%d success=%d failed=%d timeout=%d skipped_backpressure=%d backoff=%d pool_active=%d pool_idle=%d pool_rebuild=%d",
		scheduler.dispatcher.probeLimit, scheduler.dispatcher.activeProbes,
		scheduler.dispatcher.normalLimit, scheduler.dispatcher.activeNormal, scheduler.pending.len(),
		averageDelay.Milliseconds(), scheduler.counts.dispatchMax.Milliseconds(),
		scheduler.counts.success, scheduler.counts.failed, scheduler.counts.timedOut,
		scheduler.counts.skipped, scheduler.counts.backoff,
		scheduler.dispatcher.activeNormal, idle, rebuilds)
	for taskID, duration := range scheduler.counts.durations {
		average := duration.total / time.Duration(duration.count)
		log.Printf("collection scheduler task summary: task_id=%s count=%d duration_avg_ms=%d duration_max_ms=%d",
			taskID, duration.count, average.Milliseconds(), duration.max.Milliseconds())
	}
	scheduler.counts = newSchedulerCounts()
}

func newSchedulerCounts() schedulerCounts {
	return schedulerCounts{durations: map[metric.TaskID]durationSummary{}}
}

func classFor(task metric.Task) workClass {
	if task.Kind == metric.TaskKindProbe {
		return workProbe
	}
	return workRegular
}

func nextDueAfter(now time.Time, phase, interval time.Duration) time.Time {
	base := time.Unix(0, int64(phase)).UTC()
	if now.Before(base) {
		return base
	}
	return base.Add((now.Sub(base)/interval + 1) * interval)
}
