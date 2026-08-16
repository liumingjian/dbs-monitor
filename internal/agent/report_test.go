package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestCalculateCounterRates(t *testing.T) {
	tests := []struct {
		name     string
		previous counterSnapshot
		current  counterSnapshot
		want     rateSnapshot
	}{
		{
			name:     "rates use the actual sample interval",
			previous: counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			current:  counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 130, diskBytes: 1_500, networkBytes: 2_700},
			want: rateSnapshot{
				diskCountersValid: true, networkCountersValid: true,
				diskIOPS: 3, diskBytesPerSecond: 50, networkBytesPerSecond: 70,
			},
		},
		{
			name:     "disk reset does not produce a negative spike",
			previous: counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			current:  counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 10, diskBytes: 100, networkBytes: 2_700},
			want:     rateSnapshot{networkCountersValid: true, networkBytesPerSecond: 70},
		},
		{
			name:     "out of order counters are not treated as reset",
			previous: counterSnapshot{sampledAt: time.Unix(110, 0), diskOps: 130, diskBytes: 1_500, networkBytes: 2_700},
			current:  counterSnapshot{sampledAt: time.Unix(100, 0), diskOps: 100, diskBytes: 1_000, networkBytes: 2_000},
			want:     rateSnapshot{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateCounterRates(tt.previous, tt.current); got != tt.want {
				t.Fatalf("calculateCounterRates() = %+v, want %+v", got, tt.want)
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

func TestClockSkewExceedsStartupLimit(t *testing.T) {
	serverTime := time.Unix(1_000, 0)
	tests := []struct {
		name      string
		agentTime time.Time
		want      bool
	}{
		{name: "same time", agentTime: serverTime, want: false},
		{name: "positive limit is accepted", agentTime: serverTime.Add(startupClockSkewLimit), want: false},
		{name: "negative limit is accepted", agentTime: serverTime.Add(-startupClockSkewLimit), want: false},
		{name: "positive skew exceeds limit", agentTime: serverTime.Add(startupClockSkewLimit + time.Nanosecond), want: true},
		{name: "negative skew exceeds limit", agentTime: serverTime.Add(-startupClockSkewLimit - time.Nanosecond), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clockSkewExceedsStartupLimit(tt.agentTime, serverTime); got != tt.want {
				t.Fatalf("clockSkewExceedsStartupLimit(%s, %s) = %v, want %v", tt.agentTime, serverTime, got, tt.want)
			}
		})
	}
}

func TestAgentMajorVersionBehind(t *testing.T) {
	tests := []struct {
		name   string
		agent  string
		server string
		want   bool
	}{
		{name: "server major is newer", agent: "1.2.3", server: "2.0.0", want: true},
		{name: "same major", agent: "2.4.0", server: "2.0.0", want: false},
		{name: "Agent major is newer", agent: "3.0.0", server: "2.4.0", want: false},
		{name: "optional v prefix", agent: "v1.2.3", server: "v2.0.0", want: true},
		{name: "invalid Agent version", agent: "dev", server: "2.0.0", want: false},
		{name: "invalid server version", agent: "1.2.3", server: "dev", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentMajorVersionBehind(tt.agent, tt.server); got != tt.want {
				t.Fatalf("agentMajorVersionBehind(%q, %q) = %v, want %v", tt.agent, tt.server, got, tt.want)
			}
		})
	}
}

func TestServiceBackfillsUnacknowledgedSampleAndWarnsAfterReconnect(t *testing.T) {
	requests := 0
	var received api.AgentReport
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
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
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"server_version":       "2.0.0",
			"server_time":          received.Timestamp,
			"unknown_future_field": true,
		}); err != nil {
			t.Errorf("encode Agent report response: %v", err)
		}
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
		t.Fatal("first report succeeded, want server error response")
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
	if !strings.Contains(logs.String(), "Agent version 1.2.3 is behind server version 2.0.0") {
		t.Fatalf("Agent logs = %q, want version upgrade warning", logs.String())
	}
}

func TestServiceRejectsStartupClockSkewOverFiveSeconds(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"server_version": "1.2.3",
			"server_time":    now.Add(6 * time.Second),
		}); err != nil {
			t.Errorf("encode Agent report response: %v", err)
		}
	}))
	defer server.Close()

	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create Agent API client: %v", err)
	}
	service := NewService(client, Config{Instance: uuid.New()}, "1.2.3", "/")
	err = service.RunOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "clock differs from the platform by more than 5 seconds") {
		t.Fatalf("startup report error = %v, want explicit clock skew failure", err)
	}
}
