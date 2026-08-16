package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestGetNotificationPolicySettingsUsesConfiguredRepeatMinimum(t *testing.T) {
	handler := &Handler{}
	handler.SetNotificationRepeatIntervalMinimum(30 * time.Second)

	response, err := handler.GetNotificationPolicySettings(context.Background(), api.GetNotificationPolicySettingsRequestObject{})
	if err != nil {
		t.Fatalf("get notification policy settings: %v", err)
	}
	settings, ok := response.(api.GetNotificationPolicySettings200JSONResponse)
	if !ok {
		t.Fatalf("response = %T, want 200 JSON response", response)
	}
	if settings.RepeatIntervalMinimum != 30 {
		t.Fatalf("repeat interval minimum = %d, want 30", settings.RepeatIntervalMinimum)
	}
}

func TestNotificationPolicyValuesUsesConfiguredRepeatMinimum(t *testing.T) {
	handler := &Handler{}
	handler.SetNotificationRepeatIntervalMinimum(30 * time.Second)
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
