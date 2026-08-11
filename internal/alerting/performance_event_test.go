package alerting

import (
	"strings"
	"testing"
)

func TestPerformanceEventTypeForMetric(t *testing.T) {
	tests := []struct {
		metricID string
		want     PerformanceEventType
		derived  bool
	}{
		{metricID: "pg.lock.waiting_count", want: PerformanceEventLockBlocking, derived: true},
		{metricID: "pg.session.blocked_count", want: PerformanceEventLockBlocking, derived: true},
		{metricID: "pg.transaction.long_count", want: PerformanceEventLongTransaction, derived: true},
		{metricID: "pg.transaction.max_duration_sec", want: PerformanceEventLongTransaction, derived: true},
		{metricID: "pg.connection.idle_in_transaction", want: PerformanceEventIdleInTransaction, derived: true},
		{metricID: "pg.connection.active", want: PerformanceEventActiveSessionsHigh, derived: true},
		{metricID: "pg.replication.wal_lag_bytes", want: PerformanceEventReplicationLag, derived: true},
		{metricID: "pg.temp.files_per_sec", want: PerformanceEventTempFilesSurge, derived: true},
		{metricID: "pg.temp.bytes_per_sec", want: PerformanceEventTempFilesSurge, derived: true},
		{metricID: "pg.connection.total"},
		{metricID: "host.cpu.usage_percent"},
		{metricID: "pg.availability.reachable"},
		{metricID: "agent.status"},
		{metricID: "collector.last_success_time"},
	}

	for _, test := range tests {
		t.Run(test.metricID, func(t *testing.T) {
			got, ok := PerformanceEventTypeForMetric(test.metricID)
			if got != test.want || ok != test.derived {
				t.Fatalf("PerformanceEventTypeForMetric(%q) = %q, %t; want %q, %t", test.metricID, got, ok, test.want, test.derived)
			}
		})
	}
}

func TestPerformanceEventKnowledgeCoverage(t *testing.T) {
	if len(performanceEventKnowledgeTemplates) != len(performanceEventTypes) {
		t.Fatalf("knowledge templates = %d, event types = %d", len(performanceEventKnowledgeTemplates), len(performanceEventTypes))
	}
	for _, eventType := range performanceEventTypes {
		knowledge, ok := RenderPerformanceEventKnowledge(eventType, PerformanceEventKnowledgeContext{
			MetricID:     "pg.example.metric",
			Threshold:    10,
			TriggerValue: 12,
		})
		if !ok {
			t.Errorf("event type %q has no knowledge template", eventType)
			continue
		}
		for _, value := range []string{"pg.example.metric", "10", "12"} {
			if !strings.Contains(knowledge.CauseSummary, value) {
				t.Errorf("event type %q cause summary does not contain context %q", eventType, value)
			}
		}
		if strings.TrimSpace(knowledge.SuggestedAction) == "" {
			t.Errorf("event type %q has an empty suggested action", eventType)
		}
	}
}

func TestSnapshotMetricsDerivePerformanceEvents(t *testing.T) {
	for _, metricID := range []string{
		"pg.connection.active",
		"pg.connection.idle_in_transaction",
		"pg.transaction.long_count",
		"pg.transaction.max_duration_sec",
		"pg.lock.waiting_count",
		"pg.session.blocked_count",
	} {
		if _, snapshotApplicable := TriggerSnapshotScopeForMetric(metricID); !snapshotApplicable {
			t.Fatalf("test metric %q is not snapshot-applicable", metricID)
		}
		if _, derivesEvent := PerformanceEventTypeForMetric(metricID); !derivesEvent {
			t.Errorf("snapshot-applicable metric %q does not derive a performance event", metricID)
		}
	}
}
