package notify

import (
	"fmt"
	"time"
)

type EventType string

const (
	EventFiring   EventType = "FIRING"
	EventRecovery EventType = "RECOVERY"
	EventRepeat   EventType = "REPEAT"
	EventTest     EventType = "TEST"
)

const MaxAttempts = 3

type Message struct {
	EventType EventType
	To        string
	Subject   string
	Body      string
}

type AlertPayload struct {
	AlertInstanceID string `json:"alert_instance_id"`
	RuleName        string `json:"rule_name"`
	InstanceName    string `json:"instance_name"`
	MetricID        string `json:"metric_id"`
	Severity        string `json:"severity"`
	CurrentValue    string `json:"current_value"`
}

type SuppressionFacts struct {
	Maintenance  bool
	Acknowledged bool
	Paused       bool
}

func ShouldDeliver(event EventType, facts SuppressionFacts) bool {
	if facts.Maintenance || facts.Paused {
		return false
	}
	return event != EventRepeat || !facts.Acknowledged
}

func FormatAlertMessage(event EventType, payload AlertPayload) (Message, bool) {
	var label string
	switch event {
	case EventFiring:
		label = "告警触发"
	case EventRecovery:
		label = "告警恢复"
	case EventRepeat:
		label = "告警仍在持续"
	default:
		return Message{}, false
	}
	body := fmt.Sprintf(`%s

规则：%s
实例：%s
指标：%s
级别：%s
当前值：%s
告警实例：%s
`, label, payload.RuleName, payload.InstanceName, payload.MetricID, payload.Severity, payload.CurrentValue, payload.AlertInstanceID)
	return Message{EventType: event, Subject: fmt.Sprintf("[DBS Monitor] %s：%s", label, payload.RuleName), Body: body}, true
}

func FormatTestMessage() Message {
	return Message{
		EventType: EventTest,
		Subject:   "[DBS Monitor] 渠道测试通知",
		Body:      "这是一条 DBS Monitor 渠道测试通知。\n",
	}
}

func RetryDelay(failureCount int) time.Duration {
	if failureCount <= 0 {
		return 0
	}
	return time.Second << (failureCount - 1)
}
