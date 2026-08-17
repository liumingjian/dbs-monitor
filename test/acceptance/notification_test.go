//go:build acceptance

package acceptance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	notificationAdminPassword = "acceptance-admin-password"
	notificationTimeout       = 45 * time.Second
	smtpSinkAPI               = "http://127.0.0.1:58025"
	webhookSinkAPI            = "http://127.0.0.1:58080"
	webhookSigningValue       = "AC-04 acceptance signing value"
	webhookSignatureHeader    = "X-DBS-Signature"
)

type notificationRuntime struct {
	client       *api.ClientWithResponses
	server       *managedProcess
	serverBinary string
	configPath   string
	certDir      string
	baseURL      string
	workDir      string
}

type notificationSinks struct {
	mailTotal int
	webhooks  []notificationWebhookRequest
}

type notificationWebhookRequest struct {
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func TestAcceptance_AC_04_S1(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-04-S1")
	}
	started := time.Now()
	defer recordNotificationResult(t, "AC-04-S1", "real evaluation delivered SMTP and signed Webhook firing, repeat, and recovery notifications after commit", started)

	resetNotificationSinks(t, http.StatusNoContent)
	runtime := startNotificationRuntime(t, 18454)
	assertNotificationSinks(t, notificationSinks{})
	policyID := configureNotificationDelivery(t, runtime.client, "AC-04-S1")
	instanceID := createIssue60Instance(t, runtime.client, "AC-04-S1 target", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, instanceID)
	waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

	inherited := createNotificationRule(t, runtime.client, "AC-04-S1 inherited", instanceID, nil, false)
	if inherited.NotificationPolicyId != nil || !strings.HasSuffix(inherited.EffectiveNotificationPolicyName, "（继承）") {
		t.Fatalf("inherited notification policy projection = %+v", inherited)
	}
	rule := createNotificationRule(t, runtime.client, "AC-04-S1 delivery", instanceID, &policyID, true)
	alertID := waitForNotificationAlertAndPostCommitDelivery(t, runtime.client, instanceID, rule.Id)
	waitForNotificationAttempts(t, runtime.client, alertID, 2, api.NotificationSent)
	assertWebhookSignatures(t, readNotificationSinks(t).webhooks)

	eventuallyNotification(t, 40*time.Second, func() (bool, string) {
		sinks := readNotificationSinks(t)
		attempts := readNotificationAttempts(t, runtime.client, alertID)
		return sinks.mailTotal >= 2 && len(sinks.webhooks) >= 2 && len(attempts) >= 4,
			fmt.Sprintf("repeat delivery: mail=%d webhook=%d attempts=%d", sinks.mailTotal, len(sinks.webhooks), len(attempts))
	})
	beforeAck := readNotificationSinks(t)
	acknowledged, err := runtime.client.UpdateAlertDispositionWithResponse(context.Background(), alertID, api.AlertDispositionInput{
		Disposition: api.AlertDispositionACKED,
	})
	if err != nil || acknowledged.StatusCode() != http.StatusOK {
		t.Fatalf("acknowledge alert: status=%d body=%s error=%v", acknowledged.StatusCode(), acknowledged.Body, err)
	}
	time.Sleep(32 * time.Second)
	assertNotificationSinks(t, beforeAck)

	recoveryThreshold := 1_000_000_000.0
	recoveryCount := 1
	updated, err := runtime.client.UpdateAlertRuleWithResponse(context.Background(), rule.Id, api.AlertRuleInput{
		Name: rule.Name, MetricId: rule.MetricId, Aggregation: api.Latest,
		Operator: api.GreaterThanEqual, Threshold: recoveryThreshold + 1, RecoveryOperator: api.LessThan,
		RecoveryThreshold: &recoveryThreshold, WindowSeconds: 60, ConsecutiveCount: 1,
		RecoveryConsecutiveCount: &recoveryCount, Severity: api.Critical, NoDataPolicy: api.Ignore,
		Scope: api.INSTANCES, InstanceIds: []uuid.UUID{instanceID}, EvaluationIntervalSeconds: 5,
		Enabled: true, NotificationPolicyId: &policyID,
	})
	if err != nil || updated.StatusCode() != http.StatusOK {
		t.Fatalf("update rule for recovery: status=%d body=%s error=%v", updated.StatusCode(), updated.Body, err)
	}
	eventuallyNotification(t, notificationTimeout, func() (bool, string) {
		history, listErr := runtime.client.ListAlertHistoryWithResponse(context.Background(), &api.ListAlertHistoryParams{InstanceId: &instanceID})
		if listErr != nil || history.StatusCode() != http.StatusOK || history.JSON200 == nil {
			return false, fmt.Sprintf("history status=%d error=%v", history.StatusCode(), listErr)
		}
		for _, item := range history.JSON200.Items {
			if item.Id == alertID {
				return true, ""
			}
		}
		return false, "recovered alert not visible"
	})
	eventuallyNotification(t, notificationTimeout, func() (bool, string) {
		sinks := readNotificationSinks(t)
		attempts := readNotificationAttempts(t, runtime.client, alertID)
		return sinks.mailTotal == beforeAck.mailTotal+1 && len(sinks.webhooks) == len(beforeAck.webhooks)+1 && len(attempts) == 6,
			fmt.Sprintf("recovery delivery: mail=%d webhook=%d attempts=%d", sinks.mailTotal, len(sinks.webhooks), len(attempts))
	})
	assertWebhookSignatures(t, readNotificationSinks(t).webhooks)
	assertNotificationHistory(t, runtime.client, alertID, "NOTIFICATION_SENT", 6)
}

func TestAcceptance_AC_04_S2(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-04-S2")
	}
	started := time.Now()
	defer recordNotificationResult(t, "AC-04-S2", "maintenance suppressed firing and repeat without stopping marked history; early end produced no catch-up and the next repeat delivered naturally", started)

	resetNotificationSinks(t, http.StatusNoContent)
	runtime := startNotificationRuntime(t, 18456)
	policyID := configureNotificationDelivery(t, runtime.client, "AC-04-S2")
	instanceID := createIssue60Instance(t, runtime.client, "AC-04-S2 target", "monitored", "monitored", issue60TargetPort(t))
	otherInstanceID := createIssue60Instance(t, runtime.client, "AC-04-S2 companion", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, instanceID)
	waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

	now := time.Now().UTC()
	created, err := runtime.client.CreateMaintenanceWindowWithResponse(context.Background(), api.MaintenanceWindowInput{
		InstanceIds: []uuid.UUID{instanceID, otherInstanceID},
		StartsAt:    now.Add(-time.Minute), EndsAt: now.Add(5 * time.Minute), Reason: "AC-04-S2 planned restart",
	})
	if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil || created.JSON201.Status != api.MaintenanceActive {
		t.Fatalf("create maintenance window: status=%d body=%s response=%+v error=%v", created.StatusCode(), created.Body, created.JSON201, err)
	}
	windowID := created.JSON201.Id
	windows, err := runtime.client.ListMaintenanceWindowsWithResponse(context.Background())
	if err != nil || windows.StatusCode() != http.StatusOK || windows.JSON200 == nil || len(*windows.JSON200) != 1 || len((*windows.JSON200)[0].InstanceIds) != 2 {
		t.Fatalf("list maintenance windows: status=%d body=%s response=%+v error=%v", windows.StatusCode(), windows.Body, windows.JSON200, err)
	}

	rule := createNotificationRule(t, runtime.client, "AC-04-S2 maintenance", instanceID, &policyID, true)
	alertID := waitForNotificationAlert(t, runtime.client, instanceID, rule.Id)
	eventuallyNotification(t, 40*time.Second, func() (bool, string) {
		events, listErr := runtime.client.ListAlertEventsWithResponse(context.Background(), alertID)
		if listErr != nil || events.StatusCode() != http.StatusOK || events.JSON200 == nil {
			return false, fmt.Sprintf("events status=%d error=%v", events.StatusCode(), listErr)
		}
		fired, suppressed := 0, 0
		for _, event := range *events.JSON200 {
			if event.Kind != api.AlertEventFired && event.Kind != api.AlertEventMaintenanceSuppressed {
				continue
			}
			if !event.InMaintenance || event.MaintenanceWindowId == nil || *event.MaintenanceWindowId != windowID {
				return false, fmt.Sprintf("unmarked maintenance event=%+v", event)
			}
			if event.Kind == api.AlertEventFired {
				fired++
			} else {
				suppressed++
			}
		}
		return fired == 1 && suppressed >= 2, fmt.Sprintf("fired=%d suppressed=%d events=%d", fired, suppressed, len(*events.JSON200))
	})
	detail, err := runtime.client.GetAlertDetailWithResponse(context.Background(), alertID)
	if err != nil || detail.StatusCode() != http.StatusOK || detail.JSON200 == nil || detail.JSON200.Status != api.FIRING ||
		!detail.JSON200.InMaintenance || detail.JSON200.MaintenanceWindowId == nil || *detail.JSON200.MaintenanceWindowId != windowID {
		t.Fatalf("maintenance alert detail: status=%d body=%s response=%+v error=%v", detail.StatusCode(), detail.Body, detail.JSON200, err)
	}
	assertNotificationSinks(t, notificationSinks{})
	if attempts := readNotificationAttempts(t, runtime.client, alertID); len(attempts) != 0 {
		t.Fatalf("maintenance notification attempts=%d, want 0", len(attempts))
	}

	ended, err := runtime.client.EndMaintenanceWindowWithResponse(context.Background(), windowID)
	if err != nil || ended.StatusCode() != http.StatusOK || ended.JSON200 == nil || ended.JSON200.Status != api.MaintenanceEnded {
		t.Fatalf("end maintenance window: status=%d body=%s response=%+v error=%v", ended.StatusCode(), ended.Body, ended.JSON200, err)
	}
	time.Sleep(2 * time.Second)
	assertNotificationSinks(t, notificationSinks{})
	eventuallyNotification(t, 40*time.Second, func() (bool, string) {
		sinks := readNotificationSinks(t)
		attempts := readNotificationAttempts(t, runtime.client, alertID)
		return sinks.mailTotal == 1 && len(sinks.webhooks) == 1 && len(attempts) == 2,
			fmt.Sprintf("natural repeat: mail=%d webhook=%d attempts=%d", sinks.mailTotal, len(sinks.webhooks), len(attempts))
	})
	assertWebhookSignatures(t, readNotificationSinks(t).webhooks)
}

func TestAcceptance_AC_04_F3(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-04-F3")
	}
	started := time.Now()
	defer recordNotificationResult(t, "AC-04-F3", "persisted deliveries resumed after restart and reached terminal failure after fixed exponential retries", started)

	resetNotificationSinks(t, http.StatusInternalServerError)
	stopNotificationComposeService(t, "smtp-sink")
	t.Cleanup(func() {
		startNotificationComposeService(t, "smtp-sink")
		resetNotificationSinks(t, http.StatusNoContent)
	})
	runtime := startNotificationRuntime(t, 18455)
	policyID := configureNotificationDelivery(t, runtime.client, "AC-04-F3")
	instanceID := createIssue60Instance(t, runtime.client, "AC-04-F3 target", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, instanceID)
	waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)
	rule := createNotificationRule(t, runtime.client, "AC-04-F3 delivery", instanceID, &policyID, true)
	alertID := waitForNotificationAlert(t, runtime.client, instanceID, rule.Id)

	eventuallyNotification(t, 10*time.Second, func() (bool, string) {
		attempts := readNotificationAttempts(t, runtime.client, alertID)
		return len(attempts) >= 2, fmt.Sprintf("first attempts=%d", len(attempts))
	})
	_ = runtime.server.Stop()
	runtime.restart(t, "restart")

	var terminal []api.NotificationAttempt
	eventuallyNotification(t, 15*time.Second, func() (bool, string) {
		terminal = readNotificationAttempts(t, runtime.client, alertID)
		if len(terminal) != 6 {
			return false, fmt.Sprintf("attempts=%d want=6", len(terminal))
		}
		for _, attempt := range terminal {
			if attempt.Status != api.NotificationFailed || attempt.AttemptCount != 3 || attempt.Result == nil || *attempt.Result != api.NotificationAttemptFailed {
				return false, fmt.Sprintf("non-terminal attempt=%+v", attempt)
			}
		}
		return true, ""
	})
	assertRetryTiming(t, terminal)
	assertNotificationHistory(t, runtime.client, alertID, "NOTIFICATION_FAILED", 2)
	failures, err := runtime.client.GetChannelFailuresWithResponse(context.Background())
	if err != nil || failures.StatusCode() != http.StatusOK || failures.JSON200 == nil || !failures.JSON200.HasFailures || len(failures.JSON200.Channels) != 2 {
		t.Fatalf("channel failure summary: status=%d body=%s response=%+v error=%v", failures.StatusCode(), failures.Body, failures.JSON200, err)
	}
	current, err := runtime.client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{InstanceId: &instanceID})
	if err != nil || current.StatusCode() != http.StatusOK || current.JSON200 == nil {
		t.Fatalf("list current alerts after delivery failure: status=%d body=%s error=%v", current.StatusCode(), current.Body, err)
	}
	// 独立库里内置规则也产出观测;只断本用例告警仍在 FIRING(通知失败不得改变告警状态)
	stillFiring := false
	for _, item := range current.JSON200.Items {
		if item.Id == alertID && string(item.Status) == "FIRING" {
			stillFiring = true
		}
	}
	if !stillFiring {
		t.Fatalf("notification failure changed alert state: response=%+v", current.JSON200)
	}
	time.Sleep(6 * time.Second)
	if attempts := readNotificationAttempts(t, runtime.client, alertID); len(attempts) != 6 {
		t.Fatalf("terminal notification retried again: attempts=%d", len(attempts))
	}
}

func startNotificationRuntime(t *testing.T, port int) *notificationRuntime {
	t.Helper()
	root := repositoryRoot(t)
	workDir := t.TempDir()
	serverBinary := filepath.Join(workDir, "dbs-monitor-server")
	buildBinary(t, root, serverBinary, "./cmd/monitor-server")
	certDir := filepath.Join(workDir, "certs")
	keyDir := filepath.Join(workDir, "credentials")
	binaryDir := filepath.Join(workDir, "agent-binaries")
	for _, directory := range []string{certDir, keyDir, binaryDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	runtime := &notificationRuntime{
		serverBinary: serverBinary,
		configPath:   writeServerConfig(t, workDir, recoveryDatabase(t, platformDatabaseURL(t)), keyDir, binaryDir),
		certDir:      certDir,
		baseURL:      fmt.Sprintf("https://127.0.0.1:%d", port),
		workDir:      workDir,
	}
	runtime.restart(t, "initial")
	return runtime
}

func (runtime *notificationRuntime) restart(t *testing.T, phase string) {
	t.Helper()
	logPath := filepath.Join(runtime.workDir, "server-"+phase+".log")
	port := strings.TrimPrefix(runtime.baseURL, "https://127.0.0.1:")
	runtime.server = startProcess(t, "notification server "+phase, runtime.serverBinary, logPath, []string{
		"DBS_MONITOR_CONFIG_FILE=" + runtime.configPath,
		"INITIAL_ADMIN_PASSWORD=" + notificationAdminPassword,
		"LISTEN_ADDR=127.0.0.1:" + port,
		"PUBLIC_HOST=127.0.0.1",
		"CERT_DIR=" + runtime.certDir,
		"SSL_CERT_FILE=" + os.Getenv("ACCEPTANCE_SMTP_CA_FILE"),
		"PGDATA=/",
	})
	runtime.client = waitForAPI(t, runtime.server, runtime.baseURL, filepath.Join(runtime.certDir, "ca.crt"))
	login, err := runtime.client.CreateSessionWithResponse(context.Background(), api.CreateSessionJSONRequestBody{
		Username: "admin", Password: notificationAdminPassword,
	})
	if err != nil || login.StatusCode() != http.StatusNoContent {
		t.Fatalf("login after %s start: status=%d body=%s error=%v", phase, login.StatusCode(), login.Body, err)
	}
}

func configureNotificationDelivery(t *testing.T, client *api.ClientWithResponses, suffix string) uuid.UUID {
	t.Helper()
	smtp, err := client.UpdateSMTPChannelWithResponse(context.Background(), api.SMTPChannelInput{
		Enabled: true, Host: "127.0.0.1", Port: 51025, FromAddress: "monitor@example.com",
		Recipient: "on-call@example.com", AuthType: api.SMTPAuthNone, TlsMode: api.SMTPStartTLS,
	})
	if err != nil || smtp.StatusCode() != http.StatusOK || smtp.JSON200 == nil {
		t.Fatalf("configure SMTP: status=%d body=%s error=%v", smtp.StatusCode(), smtp.Body, err)
	}
	assertNoNotificationSecrets(t, smtp.Body)
	webhook, err := client.CreateWebhookTargetWithResponse(context.Background(), api.WebhookTargetInput{
		Name: suffix + " Webhook", Enabled: true, Url: webhookSinkAPI + "/notifications",
		SigningValue: stringPointer(webhookSigningValue), SignatureHeader: stringPointer(webhookSignatureHeader),
	})
	if err != nil || webhook.StatusCode() != http.StatusCreated || webhook.JSON201 == nil {
		t.Fatalf("configure Webhook: status=%d body=%s error=%v", webhook.StatusCode(), webhook.Body, err)
	}
	assertNoNotificationSecrets(t, webhook.Body)
	contact, err := client.CreateNotificationContactWithResponse(context.Background(), api.NotificationContactInput{
		Name: suffix + " DBA", Email: "on-call@example.com",
	})
	if err != nil || contact.StatusCode() != http.StatusCreated || contact.JSON201 == nil {
		t.Fatalf("create contact: status=%d body=%s error=%v", contact.StatusCode(), contact.Body, err)
	}
	policy, err := client.CreateNotificationPolicyWithResponse(context.Background(), api.NotificationPolicyInput{
		Name: suffix + " policy", ContactIds: []uuid.UUID{contact.JSON201.Id}, ContactGroupIds: []uuid.UUID{},
		Channels:       []api.NotificationPolicyChannel{{Channel: api.PolicySMTP}, {Channel: api.PolicyWebhook, TargetId: &webhook.JSON201.Id}},
		SeverityFilter: []api.AlertSeverity{api.Critical}, NotifyOnFire: true, NotifyOnRecovery: true, RepeatInterval: 30,
	})
	if err != nil || policy.StatusCode() != http.StatusCreated || policy.JSON201 == nil {
		t.Fatalf("create notification policy: status=%d body=%s error=%v", policy.StatusCode(), policy.Body, err)
	}
	return policy.JSON201.Id
}

func createNotificationRule(t *testing.T, client *api.ClientWithResponses, name string, instanceID uuid.UUID, policyID *uuid.UUID, enabled bool) api.AlertRule {
	t.Helper()
	recoveryThreshold := -1.0
	recoveryCount := 1
	created, err := client.CreateAlertRuleWithResponse(context.Background(), api.AlertRuleInput{
		Name: name, MetricId: string(metric.MetricConnectionTotal), Aggregation: api.Latest,
		Operator: api.GreaterThanEqual, Threshold: 0, RecoveryOperator: api.LessThan,
		RecoveryThreshold: &recoveryThreshold, WindowSeconds: 60, ConsecutiveCount: 1,
		RecoveryConsecutiveCount: &recoveryCount, Severity: api.Critical, NoDataPolicy: api.Ignore,
		Scope: api.INSTANCES, InstanceIds: []uuid.UUID{instanceID}, EvaluationIntervalSeconds: 5,
		Enabled: enabled, NotificationPolicyId: policyID,
	})
	if err != nil || created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
		t.Fatalf("create alert rule: status=%d body=%s error=%v", created.StatusCode(), created.Body, err)
	}
	return *created.JSON201
}

func waitForNotificationAlertAndPostCommitDelivery(t *testing.T, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) uuid.UUID {
	t.Helper()
	var alertID uuid.UUID
	eventuallyNotification(t, notificationTimeout, func() (bool, string) {
		sinks := readNotificationSinks(t)
		alertID = currentNotificationAlert(t, client, instanceID, ruleID)
		if (sinks.mailTotal > 0 || len(sinks.webhooks) > 0) && alertID == uuid.Nil {
			t.Fatal("notification reached a network sink before the alert transaction was externally visible")
		}
		return alertID != uuid.Nil && sinks.mailTotal >= 1 && len(sinks.webhooks) >= 1,
			fmt.Sprintf("alert=%s mail=%d webhook=%d", alertID, sinks.mailTotal, len(sinks.webhooks))
	})
	return alertID
}

func waitForNotificationAlert(t *testing.T, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) uuid.UUID {
	t.Helper()
	var alertID uuid.UUID
	eventuallyNotification(t, notificationTimeout, func() (bool, string) {
		alertID = currentNotificationAlert(t, client, instanceID, ruleID)
		return alertID != uuid.Nil, fmt.Sprintf("alert=%s", alertID)
	})
	return alertID
}

func currentNotificationAlert(t *testing.T, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) uuid.UUID {
	t.Helper()
	response, err := client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{InstanceId: &instanceID})
	if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return uuid.Nil
	}
	for _, item := range response.JSON200.Items {
		if item.RuleId == ruleID {
			return item.Id
		}
	}
	return uuid.Nil
}

func readNotificationAttempts(t *testing.T, client *api.ClientWithResponses, alertID uuid.UUID) []api.NotificationAttempt {
	t.Helper()
	response, err := client.ListAlertNotificationsWithResponse(context.Background(), alertID)
	if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("list alert notifications: status=%d body=%s error=%v", response.StatusCode(), response.Body, err)
	}
	return *response.JSON200
}

func waitForNotificationAttempts(t *testing.T, client *api.ClientWithResponses, alertID uuid.UUID, count int, status api.NotificationAttemptStatus) {
	t.Helper()
	eventuallyNotification(t, notificationTimeout, func() (bool, string) {
		attempts := readNotificationAttempts(t, client, alertID)
		if len(attempts) != count {
			return false, fmt.Sprintf("attempts=%d want=%d", len(attempts), count)
		}
		for _, attempt := range attempts {
			if attempt.Status != status {
				return false, fmt.Sprintf("attempt status=%s want=%s", attempt.Status, status)
			}
		}
		return true, ""
	})
}

func assertRetryTiming(t *testing.T, attempts []api.NotificationAttempt) {
	t.Helper()
	byChannel := make(map[api.NotificationAttemptChannel][]api.NotificationAttempt)
	for _, attempt := range attempts {
		byChannel[attempt.Channel] = append(byChannel[attempt.Channel], attempt)
	}
	for channel, channelAttempts := range byChannel {
		if len(channelAttempts) != 3 {
			t.Fatalf("%s attempts=%d want=3", channel, len(channelAttempts))
		}
		for _, attempt := range channelAttempts {
			if attempt.RetryCount == nil {
				t.Fatalf("%s attempt has no retry count: %+v", channel, attempt)
			}
		}
		sort.Slice(channelAttempts, func(i, j int) bool { return *channelAttempts[i].RetryCount < *channelAttempts[j].RetryCount })
		for index, expectedRetry := range []int{0, 1, 2} {
			if channelAttempts[index].RetryCount == nil || *channelAttempts[index].RetryCount != expectedRetry || channelAttempts[index].AttemptedAt == nil {
				t.Fatalf("%s retry %d = %+v", channel, expectedRetry, channelAttempts[index])
			}
		}
		for index, minimum := range []time.Duration{time.Second, 2 * time.Second} {
			delay := channelAttempts[index+1].AttemptedAt.Sub(*channelAttempts[index].AttemptedAt)
			if delay < minimum || delay > 5*time.Second {
				t.Fatalf("%s retry delay %d = %s, want [%s, 5s]", channel, index+1, delay, minimum)
			}
		}
	}
}

func assertNotificationHistory(t *testing.T, client *api.ClientWithResponses, alertID uuid.UUID, kind string, count int) {
	t.Helper()
	detail, err := client.GetAlertDetailWithResponse(context.Background(), alertID)
	if err != nil || detail.StatusCode() != http.StatusOK || detail.JSON200 == nil {
		t.Fatalf("read alert detail: status=%d body=%s error=%v", detail.StatusCode(), detail.Body, err)
	}
	found := 0
	for _, result := range detail.JSON200.NotificationResults {
		if result["kind"] == kind {
			found++
		}
	}
	if found != count {
		t.Fatalf("notification history %s count=%d want=%d: %+v", kind, found, count, detail.JSON200.NotificationResults)
	}
}

func readNotificationSinks(t *testing.T) notificationSinks {
	t.Helper()
	mailResponse, err := http.Get(smtpSinkAPI + "/api/v1/messages")
	if err != nil {
		t.Fatalf("read SMTP sink: %v", err)
	}
	defer mailResponse.Body.Close()
	var mail struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(mailResponse.Body).Decode(&mail); err != nil || mailResponse.StatusCode != http.StatusOK {
		t.Fatalf("decode SMTP sink: status=%d error=%v", mailResponse.StatusCode, err)
	}
	webhookResponse, err := http.Get(webhookSinkAPI + "/requests")
	if err != nil {
		t.Fatalf("read Webhook sink: %v", err)
	}
	defer webhookResponse.Body.Close()
	var webhooks []notificationWebhookRequest
	if err := json.NewDecoder(webhookResponse.Body).Decode(&webhooks); err != nil || webhookResponse.StatusCode != http.StatusOK {
		t.Fatalf("decode Webhook sink: status=%d error=%v", webhookResponse.StatusCode, err)
	}
	return notificationSinks{mailTotal: mail.Total, webhooks: webhooks}
}

func assertNotificationSinks(t *testing.T, want notificationSinks) {
	t.Helper()
	got := readNotificationSinks(t)
	if got.mailTotal != want.mailTotal || len(got.webhooks) != len(want.webhooks) {
		t.Fatalf("notification sinks = mail %d webhook %d, want mail %d webhook %d", got.mailTotal, len(got.webhooks), want.mailTotal, len(want.webhooks))
	}
}

func resetNotificationSinks(t *testing.T, webhookStatus int) {
	t.Helper()
	postNotificationSink(t, fmt.Sprintf("%s/control/status/%d", webhookSinkAPI, webhookStatus))
	postNotificationSink(t, webhookSinkAPI+"/control/reset")
	// smtp-sink 可能刚被 compose 重启,HTTP 面就绪前的瞬态失败要重试
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := tryResetSMTPSink()
		if err == nil {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("reset SMTP sink: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func tryResetSMTPSink() error {
	request, err := http.NewRequest(http.MethodDelete, smtpSinkAPI+"/api/v1/messages", strings.NewReader(`{"IDs":[]}`))
	if err != nil {
		return fmt.Errorf("build SMTP reset request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("reset SMTP sink status=%d", response.StatusCode)
	}
	return nil
}

func postNotificationSink(t *testing.T, target string) {
	t.Helper()
	response, err := http.Post(target, "application/json", nil)
	if err != nil {
		t.Fatalf("control notification sink %s: %v", target, err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("control notification sink %s: status=%d", target, response.StatusCode)
	}
}

func assertWebhookSignatures(t *testing.T, requests []notificationWebhookRequest) {
	t.Helper()
	for index, request := range requests {
		mac := hmac.New(sha256.New, []byte(webhookSigningValue))
		_, _ = io.WriteString(mac, request.Body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := request.Headers[strings.ToLower(webhookSignatureHeader)]; got != want {
			t.Fatalf("Webhook signature %d = %q, want %q", index+1, got, want)
		}
	}
}

func assertNoNotificationSecrets(t *testing.T, body []byte) {
	t.Helper()
	lower := strings.ToLower(string(body))
	for _, forbidden := range []string{"password", "secret", "signing_value", "signature_header"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("notification response exposes forbidden field %q: %s", forbidden, body)
		}
	}
}

func stopNotificationComposeService(t *testing.T, service string) {
	t.Helper()
	runNotificationCompose(t, "stop", service)
}

func startNotificationComposeService(t *testing.T, service string) {
	t.Helper()
	runNotificationCompose(t, "start", service)
}

func runNotificationCompose(t *testing.T, action, service string) {
	t.Helper()
	project := os.Getenv("ACCEPTANCE_COMPOSE_PROJECT")
	if project == "" {
		t.Fatal("ACCEPTANCE_COMPOSE_PROJECT is required")
	}
	command := exec.Command("docker", "compose", "-p", project, action, service)
	command.Dir = repositoryRoot(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("docker compose %s %s: %v\n%s", action, service, err, output)
	}
}

func eventuallyNotification(t *testing.T, timeout time.Duration, condition func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var detail string
	for time.Now().Before(deadline) {
		ok, currentDetail := condition()
		if ok {
			return
		}
		detail = currentDetail
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("notification condition not reached after %s: %s", timeout, detail)
}

func recordNotificationResult(t *testing.T, entryID, passedMessage string, started time.Time) {
	status, message := resultPassed, passedMessage
	if t.Failed() {
		status, message = resultFailed, "notification acceptance failed; see go test output"
	}
	acceptanceReport.record(entryID, status, message, time.Since(started))
}

func stringPointer(value string) *string {
	return &value
}
