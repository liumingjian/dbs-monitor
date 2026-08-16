package notify

import (
	"strings"
	"testing"
	"time"
)

func TestAlertMessageTemplates(t *testing.T) {
	payload := AlertPayload{
		AlertInstanceID: "2d5d723e-8ca1-46bb-b5a7-2d13d2891ff9",
		RuleName:        "连接数过高",
		InstanceName:    "orders-primary",
		MetricID:        "pg.connection.total",
		Severity:        "critical",
		CurrentValue:    "92",
	}
	tests := []struct {
		event EventType
		want  string
	}{
		{event: EventFiring, want: "告警触发"},
		{event: EventRecovery, want: "告警恢复"},
		{event: EventRepeat, want: "告警仍在持续"},
	}
	for _, test := range tests {
		t.Run(string(test.event), func(t *testing.T) {
			message, ok := FormatAlertMessage(test.event, payload)
			if !ok {
				t.Fatal("FormatAlertMessage() did not return a template")
			}
			for _, required := range []string{test.want, payload.RuleName, payload.InstanceName, payload.MetricID, payload.Severity, payload.CurrentValue, payload.AlertInstanceID} {
				if !strings.Contains(message.Subject+message.Body, required) {
					t.Errorf("message does not contain %q", required)
				}
			}
		})
	}
	if _, ok := FormatAlertMessage(EventType("NO_DATA"), payload); ok {
		t.Fatal("NO_DATA must not have a notification template")
	}
}

func TestRetryScheduleIsFixed(t *testing.T) {
	if MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", MaxAttempts)
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	for failureCount, expected := range want {
		cappedExpected := min(expected, 1500*time.Millisecond)
		if got := RetryDelay(failureCount+1, 1500*time.Millisecond); got != cappedExpected {
			t.Errorf("RetryDelay(%d) = %s, want %s", failureCount+1, got, cappedExpected)
		}
	}
}

func TestNotificationSuppressionDecision(t *testing.T) {
	tests := []struct {
		name  string
		event EventType
		facts SuppressionFacts
		want  bool
	}{
		{name: "ordinary firing", event: EventFiring, want: true},
		{name: "acknowledged firing still sends", event: EventFiring, facts: SuppressionFacts{Acknowledged: true}, want: true},
		{name: "acknowledged recovery still sends", event: EventRecovery, facts: SuppressionFacts{Acknowledged: true}, want: true},
		{name: "acknowledged repeat stops", event: EventRepeat, facts: SuppressionFacts{Acknowledged: true}},
		{name: "maintenance suppresses", event: EventFiring, facts: SuppressionFacts{Maintenance: true}},
		{name: "pause suppresses", event: EventRecovery, facts: SuppressionFacts{Paused: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldDeliver(test.event, test.facts); got != test.want {
				t.Fatalf("ShouldDeliver() = %t, want %t", got, test.want)
			}
		})
	}
}
