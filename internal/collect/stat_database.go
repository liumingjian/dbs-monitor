package collect

import (
	"sort"
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

type statDatabaseCounters [statDatabaseCounterCount]float64

// statDatabaseSnapshot 是一次采集读到的 pg_stat_database，一库一行。
//
// 原来这里是一组已经跨库加总的计数器。加总掉的东西找不回来：一个实例下可以有几十个库，
// 「谁在产生这些事务」是这张表最先被问到的问题。实例级的总数改由读取侧按目录里的聚合方式收敛。
type statDatabaseSnapshot struct {
	observedAt time.Time
	databases  map[string]statDatabaseCounters
}

type statDatabaseRateState struct {
	mu       sync.Mutex
	previous map[string]statDatabaseSnapshot
}

func newStatDatabaseRateState() *statDatabaseRateState {
	return &statDatabaseRateState{previous: make(map[string]statDatabaseSnapshot)}
}

var statDatabaseCounterMetrics = [statDatabaseCounterCount]metric.MetricID{
	statDatabaseXactCommitIndex:   metric.MetricXactCommitPerS,
	statDatabaseXactRollbackIndex: metric.MetricXactRollbackPerS,
	statDatabaseTuplesReadIndex:   metric.MetricTuplesReadPerS,
	statDatabaseTuplesWriteIndex:  metric.MetricTuplesWritePerS,
	statDatabaseTempFilesIndex:    metric.MetricTempFilesPerS,
	statDatabaseTempBytesIndex:    metric.MetricTempBytesPerS,
}

// observe 把两次快照之间的计数差算成速率，一库一组样本。
//
// 计数器回绕仍然整批作废（counterReset）而不是逐库作废：pg_stat_database 的计数器由
// pg_stat_reset() 一起清零，一个库回绕通常意味着整台都回绕了；逐库放行会让实例级的和在一个
// 采集周期里凭空掉一截——那比缺一个点更难解释。
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

	// 新建的库这一轮只当基线：它上一轮不存在，没有可以求差的前值。删掉的库随快照一起消失，
	// previous 被整体替换，不会留下越积越多的残留。
	databases := make([]string, 0, len(current.databases))
	for name := range current.databases {
		if _, existed := previous.databases[name]; existed {
			databases = append(databases, name)
		}
	}
	sort.Strings(databases)

	samples := make([]collectedSample, 0, len(databases)*(statDatabaseCounterCount+1))
	for _, name := range databases {
		before, now := previous.databases[name], current.databases[name]
		rates := statDatabaseCounters{}
		for index := range now {
			value, ok, reason := metric.Rate(before[index], now[index], elapsed)
			if !ok {
				return collectedBatch{counterReset: reason == metric.ResetCounter}
			}
			rates[index] = value
		}
		samples = append(samples, collectedSample{
			metricID:     metric.MetricTPS,
			databaseName: name,
			value:        rates[statDatabaseXactCommitIndex] + rates[statDatabaseXactRollbackIndex],
		})
		for index, metricID := range statDatabaseCounterMetrics {
			samples = append(samples, collectedSample{metricID: metricID, databaseName: name, value: rates[index]})
		}
	}
	return collectedBatch{samples: samples}
}
