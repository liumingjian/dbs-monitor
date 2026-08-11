package notify

import "time"

func NotificationDue(event EventType, lastNotification *time.Time, repeatInterval time.Duration, disposition string, now time.Time) bool {
	switch event {
	case EventFiring, EventRecovery:
		return true
	case EventRepeat:
		if disposition == "ACKED" || lastNotification == nil {
			return false
		}
		return !now.Before(lastNotification.Add(repeatInterval))
	default:
		return false
	}
}
