package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
)

func TestNotificationContactAndPolicyCRUD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationHTTPTestDatabase(t, ctx)
	defer platform.Close()

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, clock.Real{}, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}

	contactResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/notification-contacts", map[string]any{
		"name": "Primary DBA", "email": "primary@example.com", "external_id": "on-call-1",
	}, "")
	contact := decodeNotificationResponse[api.NotificationContact](t, contactResponse, http.StatusCreated)
	if contact.Name != "Primary DBA" || string(contact.Email) != "primary@example.com" || contact.ExternalId == nil || *contact.ExternalId != "on-call-1" {
		t.Fatalf("created contact = %+v", contact)
	}

	updatedContactResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/notification-contacts/"+contact.Id.String(), map[string]any{
			"name": "Primary on-call", "email": "primary@example.com",
		}, "")
	updatedContact := decodeNotificationResponse[api.NotificationContact](t, updatedContactResponse, http.StatusOK)
	if updatedContact.Name != "Primary on-call" || updatedContact.ExternalId != nil {
		t.Fatalf("updated contact = %+v", updatedContact)
	}

	groupResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/notification-contact-groups", map[string]any{
		"name": "Database team", "contact_ids": []string{contact.Id.String()},
	}, "")
	group := decodeNotificationResponse[api.NotificationContactGroup](t, groupResponse, http.StatusCreated)
	if group.Name != "Database team" || len(group.ContactIds) != 1 || group.ContactIds[0] != contact.Id {
		t.Fatalf("created contact group = %+v", group)
	}

	groupUpdateResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/notification-contact-groups/"+group.Id.String(), map[string]any{
			"name": "Escalation team", "contact_ids": []string{},
		}, "")
	group = decodeNotificationResponse[api.NotificationContactGroup](t, groupUpdateResponse, http.StatusOK)
	if group.Name != "Escalation team" || len(group.ContactIds) != 0 {
		t.Fatalf("updated contact group = %+v", group)
	}

	policiesResponse := getResponse(t, client, server.URL+"/api/v1/notification-policies")
	policies := decodeNotificationResponse[[]api.NotificationPolicy](t, policiesResponse, http.StatusOK)
	if len(policies) != 1 || !policies[0].IsDefault || policies[0].RepeatInterval != 3600 {
		t.Fatalf("seeded policies = %+v", policies)
	}
	defaultPolicy := policies[0]

	defaultUpdateResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/notification-policies/"+defaultPolicy.Id.String(), map[string]any{
			"name": "Global on-call", "contact_ids": []string{contact.Id.String()},
			"contact_group_ids": []string{group.Id.String()}, "channels": []map[string]any{{"channel": "SMTP"}},
			"severity_filter": []string{"critical", "warning", "info"}, "notify_on_fire": true,
			"notify_on_recovery": true, "repeat_interval": 1800,
		}, "")
	defaultPolicy = decodeNotificationResponse[api.NotificationPolicy](t, defaultUpdateResponse, http.StatusOK)
	if !defaultPolicy.IsDefault || defaultPolicy.Name != "Global on-call" || defaultPolicy.RepeatInterval != 1800 ||
		len(defaultPolicy.ContactIds) != 1 || len(defaultPolicy.ContactGroupIds) != 1 || len(defaultPolicy.Channels) != 1 {
		t.Fatalf("updated default policy = %+v", defaultPolicy)
	}

	deleteDefaultResponse := requestJSON(t, client, http.MethodDelete,
		server.URL+"/api/v1/notification-policies/"+defaultPolicy.Id.String(), nil, "")
	deleteDefaultBody, _ := io.ReadAll(deleteDefaultResponse.Body)
	deleteDefaultResponse.Body.Close()
	if deleteDefaultResponse.StatusCode != http.StatusConflict {
		t.Fatalf("delete default policy = status %d, body %s; want 409", deleteDefaultResponse.StatusCode, deleteDefaultBody)
	}

	invalidPolicyResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/notification-policies", map[string]any{
		"name": "Invalid", "contact_ids": []string{}, "contact_group_ids": []string{},
		"channels": []map[string]any{{"channel": "SMTP"}}, "severity_filter": []string{"critical"},
		"notify_on_fire": true, "notify_on_recovery": true, "repeat_interval": 899,
	}, "")
	invalidPolicyResponse.Body.Close()
	if invalidPolicyResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid repeat interval status = %d, want 400", invalidPolicyResponse.StatusCode)
	}

	overrideResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/notification-policies", map[string]any{
		"name": "Critical only", "contact_ids": []string{contact.Id.String()}, "contact_group_ids": []string{},
		"channels": []map[string]any{{"channel": "SMTP"}}, "severity_filter": []string{"critical"},
		"notify_on_fire": true, "notify_on_recovery": false, "repeat_interval": 900,
	}, "")
	override := decodeNotificationResponse[api.NotificationPolicy](t, overrideResponse, http.StatusCreated)
	if override.IsDefault || override.Name != "Critical only" || override.NotifyOnRecovery || override.RepeatInterval != 900 {
		t.Fatalf("created override policy = %+v", override)
	}

	rulesResponse := getResponse(t, client, server.URL+"/api/v1/alert-rules")
	rules := decodeNotificationResponse[[]api.AlertRule](t, rulesResponse, http.StatusOK)
	if len(rules) == 0 {
		t.Fatal("seeded alert rules are empty")
	}
	rule := rules[0]
	recoveryCount := rule.RecoveryConsecutiveCount
	recoveryThreshold := rule.RecoveryThreshold
	ruleInput := api.AlertRuleInput{
		Name: rule.Name, MetricId: rule.MetricId, Aggregation: rule.Aggregation,
		Operator: rule.Operator, Threshold: rule.Threshold, RecoveryOperator: rule.RecoveryOperator,
		RecoveryThreshold: &recoveryThreshold, WindowSeconds: rule.WindowSeconds,
		ConsecutiveCount: rule.ConsecutiveCount, RecoveryConsecutiveCount: &recoveryCount,
		Severity: rule.Severity, NoDataPolicy: rule.NoDataPolicy, Scope: rule.Scope,
		InstanceIds: rule.InstanceIds, EvaluationIntervalSeconds: rule.EvaluationIntervalSeconds,
		Enabled: rule.Enabled, NotificationPolicyId: &override.Id,
	}
	overriddenRuleResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/alert-rules/"+rule.Id.String(), ruleInput, "")
	overriddenRule := decodeNotificationResponse[api.AlertRule](t, overriddenRuleResponse, http.StatusOK)
	if overriddenRule.NotificationPolicyId == nil || *overriddenRule.NotificationPolicyId != override.Id ||
		overriddenRule.EffectiveNotificationPolicyName != override.Name {
		t.Fatalf("overridden alert rule = %+v", overriddenRule)
	}

	ruleInput.NotificationPolicyId = nil
	inheritedRuleResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/alert-rules/"+rule.Id.String(), ruleInput, "")
	inheritedRule := decodeNotificationResponse[api.AlertRule](t, inheritedRuleResponse, http.StatusOK)
	if inheritedRule.NotificationPolicyId != nil || inheritedRule.EffectiveNotificationPolicyName != "Global on-call（继承）" {
		t.Fatalf("inherited alert rule = %+v", inheritedRule)
	}

	deleteOverrideResponse := requestJSON(t, client, http.MethodDelete,
		server.URL+"/api/v1/notification-policies/"+override.Id.String(), nil, "")
	deleteOverrideResponse.Body.Close()
	if deleteOverrideResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("delete override policy status = %d, want 204", deleteOverrideResponse.StatusCode)
	}
}

func decodeNotificationResponse[T any](t *testing.T, response *http.Response, wantStatus int) T {
	t.Helper()
	defer response.Body.Close()
	var value T
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil || response.StatusCode != wantStatus {
		t.Fatalf("decode notification response = status %d, error %v", response.StatusCode, err)
	}
	return value
}
