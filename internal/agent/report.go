package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

const maxBackfillAge = 5 * time.Minute

type sample struct {
	sampledAt time.Time
	metrics   []api.AgentMetric
}

type reportBuffer struct {
	samples []sample
}

func (buffer *reportBuffer) add(value sample) {
	buffer.samples = append(buffer.samples, value)
}

func (buffer *reportBuffer) pending(now time.Time) []sample {
	cutoff := now.Add(-maxBackfillAge)
	kept := buffer.samples[:0]
	for _, value := range buffer.samples {
		if !value.sampledAt.Before(cutoff) {
			kept = append(kept, value)
		}
	}
	buffer.samples = kept
	return buffer.samples
}

func (buffer *reportBuffer) acknowledge() {
	buffer.samples = nil
}

type counterSnapshot struct {
	sampledAt    time.Time
	diskOps      uint64
	diskBytes    uint64
	networkBytes uint64
}

type rateSnapshot struct {
	diskCountersValid     bool
	networkCountersValid  bool
	diskIOPS              float64
	diskBytesPerSecond    float64
	networkBytesPerSecond float64
}

func calculateCounterRates(previous, current counterSnapshot) rateSnapshot {
	elapsed := current.sampledAt.Sub(previous.sampledAt).Seconds()
	if elapsed <= 0 {
		return rateSnapshot{}
	}
	rates := rateSnapshot{
		diskCountersValid:    current.diskOps >= previous.diskOps && current.diskBytes >= previous.diskBytes,
		networkCountersValid: current.networkBytes >= previous.networkBytes,
	}
	if rates.diskCountersValid {
		rates.diskIOPS = float64(current.diskOps-previous.diskOps) / elapsed
		rates.diskBytesPerSecond = float64(current.diskBytes-previous.diskBytes) / elapsed
	}
	if rates.networkCountersValid {
		rates.networkBytesPerSecond = float64(current.networkBytes-previous.networkBytes) / elapsed
	}
	return rates
}

type Collector struct {
	dataPath string
	previous *counterSnapshot
}

func NewCollector(dataPath string) *Collector {
	return &Collector{dataPath: dataPath}
}

func (collector *Collector) Collect(ctx context.Context, sampledAt time.Time) (sample, error) {
	cpuPercent, err := cpu.PercentWithContext(ctx, 100*time.Millisecond, false)
	if err != nil {
		return sample{}, fmt.Errorf("collect CPU usage: %w", err)
	}
	if len(cpuPercent) != 1 {
		return sample{}, fmt.Errorf("collect CPU usage: got %d aggregate values", len(cpuPercent))
	}
	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return sample{}, fmt.Errorf("collect memory usage: %w", err)
	}
	diskUsage, err := disk.UsageWithContext(ctx, collector.dataPath)
	if err != nil {
		return sample{}, fmt.Errorf("collect disk usage for %s: %w", collector.dataPath, err)
	}
	diskCounters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return sample{}, fmt.Errorf("collect disk counters: %w", err)
	}
	networkCounters, err := gopsutilnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return sample{}, fmt.Errorf("collect network counters: %w", err)
	}

	current := counterSnapshot{sampledAt: sampledAt}
	for name, counter := range diskCounters {
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		current.diskOps += counter.ReadCount + counter.WriteCount
		current.diskBytes += counter.ReadBytes + counter.WriteBytes
	}
	for _, counter := range networkCounters {
		if counter.Name != "lo" {
			current.networkBytes += counter.BytesRecv + counter.BytesSent
		}
	}

	result := sample{sampledAt: sampledAt, metrics: []api.AgentMetric{
		{Metric: api.AgentMetricMetricHostCpuUsagePercent, Value: cpuPercent[0]},
		{Metric: api.AgentMetricMetricHostMemoryUsagePercent, Value: memory.UsedPercent},
		{Metric: api.AgentMetricMetricHostDiskUsagePercent, Value: diskUsage.UsedPercent},
		{Metric: api.AgentMetricMetricHostDiskFreeBytes, Value: float64(diskUsage.Free)},
	}}
	if collector.previous != nil {
		rates := calculateCounterRates(*collector.previous, current)
		if rates.diskCountersValid {
			result.metrics = append(result.metrics,
				api.AgentMetric{Metric: api.AgentMetricMetricHostDiskIops, Value: rates.diskIOPS},
				api.AgentMetric{Metric: api.AgentMetricMetricHostDiskThroughputBytesPerSec, Value: rates.diskBytesPerSecond},
			)
		}
		if rates.networkCountersValid {
			result.metrics = append(result.metrics, api.AgentMetric{Metric: api.AgentMetricMetricHostNetworkBytesPerSec, Value: rates.networkBytesPerSecond})
		}
	}
	collector.previous = &current
	return result, nil
}

type Service struct {
	client    *api.ClientWithResponses
	config    Config
	version   string
	collector *Collector
	buffer    reportBuffer
}

func NewService(client *api.ClientWithResponses, config Config, version, dataPath string) *Service {
	return &Service{client: client, config: config, version: version, collector: NewCollector(dataPath)}
}

func (service *Service) RunOnce(ctx context.Context, now time.Time) error {
	now = now.UTC()
	collected, err := service.collector.Collect(ctx, now)
	if err != nil {
		return err
	}
	service.buffer.add(collected)
	pending := service.buffer.pending(now)
	body := api.AgentReport{
		AgentVersion: service.version,
		InstanceId:   service.config.Instance,
		Timestamp:    collected.sampledAt,
		Metrics:      collected.metrics,
	}
	if len(pending) > 1 {
		backfill := make([]api.AgentSample, 0, len(pending)-1)
		for _, pendingSample := range pending[:len(pending)-1] {
			backfill = append(backfill, api.AgentSample{Timestamp: pendingSample.sampledAt, Metrics: pendingSample.metrics})
		}
		body.Backfill = &backfill
	}
	response, err := service.client.ReportAgentMetricsWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("report Agent metrics: %w", err)
	}
	if response.StatusCode() != http.StatusNoContent {
		if response.JSON400 != nil {
			return fmt.Errorf("report Agent metrics: %s", response.JSON400.Error.Message)
		}
		return fmt.Errorf("report Agent metrics: server returned %s", response.Status())
	}
	service.buffer.acknowledge()
	return nil
}
