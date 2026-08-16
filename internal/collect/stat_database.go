package collect

import (
	"sync"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	statDatabaseXactCommitIndex = iota
	statDatabaseXactRollbackIndex
	statDatabaseTuplesReadIndex
	statDatabaseTuplesWriteIndex
	statDatabaseTempFilesIndex
	statDatabaseTempBytesIndex
	statDatabaseCounterCount
)

type statDatabaseSnapshot struct {
	observedAt time.Time
	counters   [statDatabaseCounterCount]float64
}

type statDatabaseRateState struct {
	mu       sync.Mutex
	previous map[string]statDatabaseSnapshot
}

func newStatDatabaseRateState() *statDatabaseRateState {
	return &statDatabaseRateState{previous: make(map[string]statDatabaseSnapshot)}
}

func (state *statDatabaseRateState) observe(instanceID string, current statDatabaseSnapshot) collectedBatch {
	state.mu.Lock()
	defer state.mu.Unlock()

	previous, exists := state.previous[instanceID]
	if !exists {
		state.previous[instanceID] = current
		return collectedBatch{}
	}
	if !current.observedAt.After(previous.observedAt) {
		return collectedBatch{}
	}

	elapsed := current.observedAt.Sub(previous.observedAt)
	state.previous[instanceID] = current

	rates := [statDatabaseCounterCount]float64{}
	for index := range current.counters {
		value, ok, reason := metric.Rate(previous.counters[index], current.counters[index], elapsed)
		if !ok {
			return collectedBatch{counterReset: reason == metric.ResetCounter}
		}
		rates[index] = value
	}

	return collectedBatch{samples: []collectedSample{
		{metricID: metric.MetricTPS, value: rates[statDatabaseXactCommitIndex] + rates[statDatabaseXactRollbackIndex]},
		{metricID: metric.MetricXactCommitPerS, value: rates[statDatabaseXactCommitIndex]},
		{metricID: metric.MetricXactRollbackPerS, value: rates[statDatabaseXactRollbackIndex]},
		{metricID: metric.MetricTuplesReadPerS, value: rates[statDatabaseTuplesReadIndex]},
		{metricID: metric.MetricTuplesWritePerS, value: rates[statDatabaseTuplesWriteIndex]},
		{metricID: metric.MetricTempFilesPerS, value: rates[statDatabaseTempFilesIndex]},
		{metricID: metric.MetricTempBytesPerS, value: rates[statDatabaseTempBytesIndex]},
	}}
}
