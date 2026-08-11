package notify

import (
	"testing"
	"time"
)

func TestNotificationDue(t *testing.T) {
	last := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	interval := time.Hour

	tests := []struct {
		name        string
		eventType   EventType
		last        *time.Time
		disposition string
		now         time.Time
		want        bool
	}{
		{name: "repeat before interval", eventType: EventRepeat, last: &last, disposition: "NONE", now: last.Add(interval - time.Second), want: false},
		{name: "repeat at interval", eventType: EventRepeat, last: &last, disposition: "NONE", now: last.Add(interval), want: true},
		{name: "repeat after interval", eventType: EventRepeat, last: &last, disposition: "IGNORED", now: last.Add(interval + time.Second), want: true},
		{name: "acknowledgement stops repeat", eventType: EventRepeat, last: &last, disposition: "ACKED", now: last.Add(2 * interval), want: false},
		{name: "recovery notification is unaffected by acknowledgement", eventType: EventRecovery, last: &last, disposition: "ACKED", now: last, want: true},
		{name: "firing notification is unaffected by acknowledgement", eventType: EventFiring, last: &last, disposition: "ACKED", now: last, want: true},
		{name: "new lifecycle waits for its own firing notification", eventType: EventRepeat, last: nil, disposition: "NONE", now: last.Add(2 * interval), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NotificationDue(test.eventType, test.last, interval, test.disposition, test.now); got != test.want {
				t.Fatalf("NotificationDue() = %t, want %t", got, test.want)
			}
		})
	}
}
