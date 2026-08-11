package collect

import (
	"sync"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

type statDatabaseSnapshot struct {
	observedAt time.Time
	counters   [6]float64
}

type statDatabaseRateState struct {
	mu       sync.Mutex
	previous map[string]statDatabaseSnapshot
}

func newStatDatabaseRateState() *statDatabaseRateState {
	return &statDatabaseRateState{previous: make(map[string]statDatabaseSnapshot)}
}

func (state *statDatabaseRateState) observe(instanceID string, current statDatabaseSnapshot) ([]collectedSample, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()

	previous, exists := state.previous[instanceID]
	if !exists {
		state.previous[instanceID] = current
		return nil, false
	}
	if !current.observedAt.After(previous.observedAt) {
		return nil, false
	}

	elapsed := current.observedAt.Sub(previous.observedAt)
	state.previous[instanceID] = current

	rates := [6]float64{}
	for index := range current.counters {
		value, ok, reason := metric.Rate(previous.counters[index], current.counters[index], elapsed)
		if !ok {
			return nil, reason == metric.ResetCounter
		}
		rates[index] = value
	}

	return []collectedSample{
		{metricID: metric.MetricTPS, value: rates[0] + rates[1]},
		{metricID: metric.MetricXactCommitPerS, value: rates[0]},
		{metricID: metric.MetricXactRollbackPerS, value: rates[1]},
		{metricID: metric.MetricTuplesReadPerS, value: rates[2]},
		{metricID: metric.MetricTuplesWritePerS, value: rates[3]},
		{metricID: metric.MetricTempFilesPerS, value: rates[4]},
		{metricID: metric.MetricTempBytesPerS, value: rates[5]},
	}, false
}
