package notify

import "time"

func NotificationDue(event EventType, lastNotification *time.Time, repeatInterval time.Duration, disposition string, now time.Time) bool {
	if event != EventRepeat {
		return event == EventFiring || event == EventRecovery
	}
	if disposition == "ACKED" || lastNotification == nil {
		return false
	}
	return !now.Before(lastNotification.Add(repeatInterval))
}
