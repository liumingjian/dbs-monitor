package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestCounterRates(t *testing.T) {
	tests := []struct {
		name     string
		previous counterSnapshot
		current  counterSnapshot
		want     rateSnapshot
		wantDisk bool
		wantNet  bool
	}{
		{
			name:     "rates use the actual sample interval",
			previous: counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			current:  counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 130, diskBytes: 1_500, networkBytes: 2_700},
			want:     rateSnapshot{diskIOPS: 3, diskThroughput: 50, networkThroughput: 70},
			wantDisk: true, wantNet: true,
		},
		{
			name:     "disk reset does not produce a negative spike",
			previous: counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			current:  counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 10, diskBytes: 100, networkBytes: 2_700},
			want:     rateSnapshot{networkThroughput: 70},
			wantDisk: false, wantNet: true,
		},
		{
			name:     "out of order counters are not treated as reset",
			previous: counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 130, diskBytes: 1_500, networkBytes: 2_700},
			current:  counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			want:     rateSnapshot{}, wantDisk: false, wantNet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diskOK, networkOK := counterRates(tt.previous, tt.current)
			if got != tt.want || diskOK != tt.wantDisk || networkOK != tt.wantNet {
				t.Fatalf("counterRates() = (%+v, %v, %v), want (%+v, %v, %v)", got, diskOK, networkOK, tt.want, tt.wantDisk, tt.wantNet)
			}
		})
	}
}

func TestCollectorProducesAllHostMetricsAfterRateBaseline(t *testing.T) {
	collector := NewCollector("/")
	now := time.Now().UTC()
	if _, err := collector.Collect(context.Background(), now); err != nil {
		t.Fatalf("collect baseline: %v", err)
	}
	collected, err := collector.Collect(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatalf("collect host metrics: %v", err)
	}
	got := make(map[string]bool, len(collected.metrics))
	for _, metric := range collected.metrics {
		got[string(metric.Metric)] = true
	}
	for _, metricID := range []string{
		"host.cpu.usage_percent", "host.memory.usage_percent", "host.disk.usage_percent",
		"host.disk.free_bytes", "host.disk.iops", "host.disk.throughput_bytes_per_sec",
		"host.network.bytes_per_sec",
	} {
		if !got[metricID] {
			t.Errorf("collector omitted %s: got %v", metricID, got)
		}
	}
}

func TestReportBufferKeepsOnlyFiveMinutesInMemory(t *testing.T) {
	now := time.Unix(1_000, 0)
	buffer := reportBuffer{}
	buffer.add(sample{sampledAt: now.Add(-5 * time.Minute)})
	buffer.add(sample{sampledAt: now.Add(-5*time.Minute - time.Nanosecond)})
	buffer.add(sample{sampledAt: now})

	pending := buffer.pending(now)
	if len(pending) != 2 || !pending[0].sampledAt.Equal(now.Add(-5*time.Minute)) || !pending[1].sampledAt.Equal(now) {
		t.Fatalf("pending samples = %+v, want inclusive five-minute window", pending)
	}

	buffer.acknowledge()
	if pending := buffer.pending(now); len(pending) != 0 {
		t.Fatalf("acknowledged buffer retained %d samples", len(pending))
	}
}

func TestServiceBackfillsUnacknowledgedSampleAfterReconnect(t *testing.T) {
	requests := 0
	var received api.AgentReport
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode Agent report: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create Agent API client: %v", err)
	}
	instanceID := uuid.New()
	service := NewService(client, Config{Instance: instanceID}, "1.2.3", "/")
	now := time.Now().UTC()
	if err := service.RunOnce(context.Background(), now); err == nil {
		t.Fatal("first report succeeded, want connection failure response")
	}
	if err := service.RunOnce(context.Background(), now.Add(10*time.Second)); err != nil {
		t.Fatalf("second report: %v", err)
	}
	if received.AgentVersion != "1.2.3" || received.InstanceId != instanceID {
		t.Fatalf("Agent identity = (%q, %s), want (1.2.3, %s)", received.AgentVersion, received.InstanceId, instanceID)
	}
	if received.Backfill == nil || len(*received.Backfill) != 1 || !(*received.Backfill)[0].Timestamp.Equal(now) {
		t.Fatalf("backfill = %+v, want failed sample at %s", received.Backfill, now)
	}
}
