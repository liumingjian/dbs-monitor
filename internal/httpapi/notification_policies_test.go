package httpapi

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestNotificationPolicyValuesUsesConfiguredRepeatMinimum(t *testing.T) {
	handler := &Handler{notificationRepeatMinimum: 30 * time.Second}
	input := &api.NotificationPolicyInput{
		Name:             "policy",
		Channels:         []api.NotificationPolicyChannel{{Channel: api.PolicySMTP}},
		NotifyOnFire:     true,
		NotifyOnRecovery: true,
		RepeatInterval:   29,
		SeverityFilter:   []api.AlertSeverity{api.Critical},
	}

	if _, ok := handler.notificationPolicyValues(input); ok {
		t.Fatal("29 second repeat interval should be rejected")
	}
	input.RepeatInterval = 30
	if _, ok := handler.notificationPolicyValues(input); !ok {
		t.Fatal("30 second repeat interval should be accepted")
	}
}
