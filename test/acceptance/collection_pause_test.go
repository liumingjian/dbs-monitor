//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	pauseConditionTimeout = 90 * time.Second
	// 采集周期由 setIssue60TaskIntervals 统一压到 5 秒，冻结观察窗取四个周期。
	pauseObservationWindow = 20 * time.Second
	// 暂停刚生效时还有在途任务在回写，取冻结基线前先让它们落完。
	pauseSettleDelay = 8 * time.Second
)

func TestAcceptance_AC_07_F1(t *testing.T) {
	runCollectionEntry(t, "AC-07-F1", "暂停/恢复端点只认 PLATFORM_ADMIN，另外两档得规定拒绝且读端点不收窄", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18465)
		instanceID := createIssue60Instance(t, runtime.client, "AC-07-F1 target", "monitored", "monitored", issue60TargetPort(t))

		viewer := createSecurityUser(t, runtime.client, "ac-07-f1-viewer", api.READONLY)
		alertAdmin := createSecurityUser(t, runtime.client, "ac-07-f1-alert-admin", api.ALERTADMIN)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-07-f1-platform-admin", api.PLATFORMADMIN)

		for _, refused := range []struct {
			role api.Role
			user api.UserCreated
		}{
			{role: api.READONLY, user: viewer},
			{role: api.ALERTADMIN, user: alertAdmin},
		} {
			client := newSecurityClient(t, runtime.baseURL, runtime.caPath)
			loginSecurityUser(t, client, refused.user.User.Username, refused.user.InitialPassword)
			for _, paused := range []bool{true, false} {
				response, err := client.UpdateCollectionPauseWithResponse(context.Background(), instanceID,
					api.CollectionPauseInput{Paused: paused})
				if err != nil {
					t.Fatalf("%s updateCollectionPause(paused=%t): %v", refused.role, paused, err)
				}
				if response.StatusCode() != http.StatusForbidden {
					t.Fatalf("%s updateCollectionPause(paused=%t) = status %d body %s, want 403",
						refused.role, paused, response.StatusCode(), response.Body)
				}
			}
			// 写能力收窄不等于可见性收窄：读端点对这两档仍然是 200。
			status := readPauseStatus(t, client, instanceID)
			if status.Paused {
				t.Fatalf("%s saw the instance paused after a refused write", refused.role)
			}
		}

		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)
		if status := setPause(t, adminClient, instanceID, true, nil); !status.Paused {
			t.Fatal("PLATFORM_ADMIN pause did not take effect")
		}
		if status := setPause(t, adminClient, instanceID, false, nil); status.Paused {
			t.Fatal("PLATFORM_ADMIN resume did not take effect")
		}
	})
}

func TestAcceptance_AC_07_S1(t *testing.T) {
	runCollectionEntry(t, "AC-07-S1", "暂停冻结了调度与水位、查询一律 COLLECTION_PAUSED、Agent 照常在线而样本不写，恢复后从最新意图续采且不补跑", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18466)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-07-s1-platform-admin", api.PLATFORMADMIN)
		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)

		instanceID := createIssue60Instance(t, runtime.client, "AC-07-S1 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		startIssue60Agent(t, runtime, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricHostCPUUsagePercent)
		// 完整性水位要同源全部到期任务都满足了才推进，单条指标出点不代表它已经立起来。
		waitForPauseWatermark(t, runtime.client, instanceID)

		reason := "AC-07-S1 计划维护窗口"
		pausedAt := time.Now().UTC()
		status := setPause(t, adminClient, instanceID, true, &reason)
		// 操作人与时间系统自动必录，原因选填但一旦给出必须原样读回。
		assertPauseAttribution(t, status, platformAdmin.User.Id, reason)
		assertPauseAttribution(t, readPauseStatus(t, runtime.client, instanceID), platformAdmin.User.Id, reason)

		for _, metricID := range []metric.MetricID{
			metric.MetricConnectionTotal,
			metric.MetricAvailabilityReachable,
			metric.MetricCollectorLastSuccessTime,
			metric.MetricHostCPUUsagePercent,
		} {
			assertCollectionMetricUnavailable(t, runtime.client, instanceID, metricID, pausedAt.Add(-time.Minute), api.COLLECTIONPAUSED)
		}

		// 暂停生效的一刹那可能还有在途任务在回写，先让它们落完再取冻结基线。
		time.Sleep(pauseSettleDelay)
		before := readPauseInstance(t, runtime.client, instanceID)
		if before.LastCollectedAt == nil {
			t.Fatal("instance has no collection watermark to freeze")
		}
		beforeStates := readPauseTaskSuccessTimes(t, runtime.client, instanceID)

		time.Sleep(pauseObservationWindow)

		frozen := readPauseInstance(t, runtime.client, instanceID)
		if frozen.LastCollectedAt == nil || !frozen.LastCollectedAt.Equal(*before.LastCollectedAt) {
			t.Fatalf("collection watermark moved to %v during the pause, want it frozen at %v", frozen.LastCollectedAt, *before.LastCollectedAt)
		}
		// 水位不推进不等于判为采集失败：任务状态原地不动，没有新的失败被记下。
		if frozenStates := readPauseTaskSuccessTimes(t, runtime.client, instanceID); !equalPauseTaskTimes(beforeStates, frozenStates) {
			t.Fatalf("collection task states moved during the pause: before=%v after=%v", beforeStates, frozenStates)
		}
		// Agent 全程不知情：照常 push，在线判定不变。
		if frozen.AgentStatus != api.InstanceAgentOnline {
			t.Fatalf("Agent status during the pause = %s, want online", frozen.AgentStatus)
		}

		resumedAt := time.Now().UTC()
		if status := setPause(t, adminClient, instanceID, false, nil); status.Paused {
			t.Fatal("resume did not clear the pause")
		}

		eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
			current := readPauseInstance(t, runtime.client, instanceID)
			if current.LastCollectedAt == nil || !current.LastCollectedAt.After(*before.LastCollectedAt) {
				return false, fmt.Sprintf("collection watermark is still %v after the resume", current.LastCollectedAt)
			}
			return true, ""
		})

		// 恢复后从最新意图开始：暂停窗口里既没有服务端直采样本，也没有 Agent 样本被回灌。
		gapStart, gapEnd := pausedAt.Add(pauseSettleDelay), resumedAt.Add(-time.Second)
		for _, metricID := range []metric.MetricID{metric.MetricConnectionTotal, metric.MetricHostCPUUsagePercent} {
			values, _, detail := readCollectionMetric(runtime.client, instanceID, metricID, gapStart, gapEnd)
			if detail != "" {
				t.Fatal(detail)
			}
			if len(values) != 0 {
				t.Fatalf("metric %s replayed %d samples into the paused window", metricID, len(values))
			}
		}
	})
}

func TestAcceptance_AC_07_F3(t *testing.T) {
	runCollectionEntry(t, "AC-07-F3", "暂停叠加目标库不可达时已暂停优先，能力模块留在最后一份快照并在超有效期后投影 UNKNOWN，暂停不被自动解除", func(t *testing.T) {
		requireComposeProject(t)
		runtime := startIssue60Runtime(t, 18467)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-07-f3-platform-admin", api.PLATFORMADMIN)
		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)

		instanceID := createIssue60Instance(t, runtime.client, "AC-07-F3 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60Capabilities(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		reason := "AC-07-F3 暂停后目标库同时不可达"
		pausedStatus := setPause(t, adminClient, instanceID, true, &reason)
		lastSnapshot, detail := readCollectionCapabilities(runtime.client, instanceID)
		if lastSnapshot == nil {
			t.Fatal(detail)
		}
		observedAt := lastSnapshot[api.RolePgMonitor].ObservedAt
		if observedAt == nil {
			t.Fatal("capability snapshot taken before the pause carries no observed_at")
		}
		if lastSnapshot[api.RolePgMonitor].Status != api.PRESENT {
			t.Fatalf("role.pg_monitor before the pause = %s, want PRESENT", lastSnapshot[api.RolePgMonitor].Status)
		}

		stopCollectionTarget(t)

		// 已暂停优先：连着看几轮，指标绝不翻成 DB_UNREACHABLE。
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			_, code, detail := readCollectionMetric(runtime.client, instanceID, metric.MetricConnectionTotal,
				time.Now().UTC().Add(-time.Minute), time.Now().UTC().Add(time.Minute))
			if detail != "" {
				t.Fatal(detail)
			}
			if code == nil || *code != api.COLLECTIONPAUSED {
				t.Fatalf("metric reason during a paused outage = %v, want COLLECTION_PAUSED", code)
			}
			time.Sleep(time.Second)
		}
		// 快照仍是暂停前那一份：不可达没有把它改写成 UNKNOWN，也没有推进 observed_at。
		frozen, detail := readCollectionCapabilities(runtime.client, instanceID)
		if frozen == nil {
			t.Fatal(detail)
		}
		if frozen[api.RolePgMonitor].ObservedAt == nil || !frozen[api.RolePgMonitor].ObservedAt.Equal(*observedAt) {
			t.Fatalf("capability observed_at moved to %v during the pause, want it frozen at %v", frozen[api.RolePgMonitor].ObservedAt, *observedAt)
		}
		if frozen[api.RolePgMonitor].Status != api.PRESENT {
			t.Fatalf("frozen role.pg_monitor = %s, want the pre-pause PRESENT", frozen[api.RolePgMonitor].Status)
		}

		// 超有效期后按片①既有语义投影 UNKNOWN，本片无特例。
		waitUntilSecurity(t, observedAt.Add(metric.CapabilitySnapshotTTL+5*time.Second))
		expired, detail := readCollectionCapabilities(runtime.client, instanceID)
		if expired == nil {
			t.Fatal(detail)
		}
		if len(expired) != len(metric.Capabilities) {
			t.Fatalf("expired capability snapshot has %d entries, want the full list of %d", len(expired), len(metric.Capabilities))
		}
		for id, entry := range expired {
			if entry.Status != api.UNKNOWN {
				t.Fatalf("capability %s after the snapshot expired = %s, want UNKNOWN", id, entry.Status)
			}
		}

		// 目标库不可达不是解除暂停的理由：控制面留痕一字未动。
		current := readPauseStatus(t, runtime.client, instanceID)
		if !current.Paused {
			t.Fatal("the unreachable target silently resumed collection")
		}
		if current.UpdatedAt == nil || pausedStatus.UpdatedAt == nil || !current.UpdatedAt.Equal(*pausedStatus.UpdatedAt) {
			t.Fatalf("pause timestamp moved to %v, want it left at %v", current.UpdatedAt, pausedStatus.UpdatedAt)
		}
		if current.Reason == nil || *current.Reason != reason {
			t.Fatalf("pause reason = %v, want %q", current.Reason, reason)
		}
	})
}

func TestAcceptance_AC_07_S3(t *testing.T) {
	runCollectionEntry(t, "AC-07-S3", "冻结不转 RECOVERED 不发通知不新建实例，解冻按条件是否仍满足分别续 FIRING 或走正常恢复，历史留 FROZEN/UNFROZEN", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18468)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-07-s3-platform-admin", api.PLATFORMADMIN)
		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)

		instanceID := createIssue60Instance(t, runtime.client, "AC-07-S3 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		policyID := configurePauseNotificationPolicy(t, runtime.client, "AC-07-S3")
		// 持续满足：监控自身的连接就够让它一直越界，解冻后必须是同一个告警实例继续 FIRING。
		persistent := createPauseAlertRule(t, runtime.client, pauseRuleInput{
			name: "AC-07-S3 持续越界", metricID: metric.MetricConnectionTotal,
			threshold: 1, recoveryThreshold: 0, instanceID: instanceID, policyID: &policyID,
		})
		// 解冻时不再满足：靠一条真实的活跃会话把它顶起来，暂停期间把会话关掉。
		transient := createPauseAlertRule(t, runtime.client, pauseRuleInput{
			name: "AC-07-S3 暂停期消失", metricID: metric.MetricConnectionActive,
			threshold: 1, recoveryThreshold: 0.5, instanceID: instanceID, policyID: &policyID,
		})
		stopSession := startRetentionActiveSession(t)

		persistentAlert := waitForPauseFiringAlert(t, runtime.client, instanceID, persistent)
		transientAlert := waitForPauseFiringAlert(t, runtime.client, instanceID, transient)
		historyEnd := time.Now().UTC()

		reason := "AC-07-S3 冻结告警"
		pausedAt := time.Now().UTC()
		setPause(t, adminClient, instanceID, true, &reason)
		stopSession()

		// 冻结生效后才数通知：在途的那一次重复通知属于暂停之前的账。
		time.Sleep(pauseSettleDelay)
		beforePauseNotifications := len(readNotificationAttempts(t, runtime.client, transientAlert))
		frozen := readPauseAlerts(t, runtime.client, instanceID)
		time.Sleep(pauseObservationWindow)

		// 冻结的全部含义都在这一条比对里：不新建实例、不转 RECOVERED、不产生 NO_DATA。
		paused := readPauseAlerts(t, runtime.client, instanceID)
		if summary := describePauseAlerts(paused); summary != describePauseAlerts(frozen) {
			t.Fatalf("alert instances moved during the pause:\n frozen: %s\n during: %s", describePauseAlerts(frozen), summary)
		}
		for _, alertID := range []uuid.UUID{persistentAlert, transientAlert} {
			alert, exists := paused[alertID]
			if !exists {
				t.Fatalf("alert %s disappeared from the paused instance", alertID)
			}
			if alert.Status != api.FIRING {
				t.Fatalf("alert %s moved to %s during the pause, want it held at FIRING", alertID, alert.Status)
			}
			if !alert.Paused || alert.PausedAt == nil {
				t.Fatalf("alert %s carries no orthogonal PAUSED marker", alertID)
			}
			assertPauseAlertEvent(t, runtime.client, alertID, api.AlertEventFrozen)
		}
		if attempts := len(readNotificationAttempts(t, runtime.client, transientAlert)); attempts != beforePauseNotifications {
			t.Fatalf("notification attempts grew from %d to %d during the pause", beforePauseNotifications, attempts)
		}

		setPause(t, adminClient, instanceID, false, nil)

		// 条件仍满足：同一个 alert instance 继续 FIRING，不新建实例、不发假新告警。
		eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
			current := readPauseAlerts(t, runtime.client, instanceID)
			alert, exists := current[persistentAlert]
			if !exists {
				return false, fmt.Sprintf("alert %s disappeared after the unfreeze", persistentAlert)
			}
			if alert.Status != api.FIRING {
				return false, fmt.Sprintf("alert %s = %s after the unfreeze, want FIRING", persistentAlert, alert.Status)
			}
			for id, other := range current {
				if id != persistentAlert && other.RuleId == persistent {
					return false, fmt.Sprintf("rule %s opened a second alert instance %s", persistent, id)
				}
			}
			return true, ""
		})
		// 条件不再满足：走正常 RECOVERED 并通知。
		waitForRetentionRecovery(t, runtime.client, transientAlert)
		eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
			if attempts := len(readNotificationAttempts(t, runtime.client, transientAlert)); attempts <= beforePauseNotifications {
				return false, fmt.Sprintf("recovery produced no notification attempt (still %d)", attempts)
			}
			return true, ""
		})
		for _, alertID := range []uuid.UUID{persistentAlert, transientAlert} {
			assertPauseAlertEvent(t, runtime.client, alertID, api.AlertEventUnfrozen)
		}

		// 暂停前的历史照常可查；暂停期是缺桶而不是补出来的 0。
		values, _, detail := readCollectionMetric(runtime.client, instanceID, metric.MetricConnectionTotal, historyEnd.Add(-time.Minute), historyEnd)
		if detail != "" {
			t.Fatal(detail)
		}
		if len(values) == 0 {
			t.Fatal("history from before the pause is no longer queryable")
		}
		gap, _, detail := readCollectionMetric(runtime.client, instanceID, metric.MetricConnectionTotal, pausedAt.Add(pauseSettleDelay), pausedAt.Add(pauseSettleDelay+pauseObservationWindow))
		if detail != "" {
			t.Fatal(detail)
		}
		if len(gap) != 0 {
			t.Fatalf("the paused window came back with %d points, want an empty bucket rather than zeros", len(gap))
		}
	})
}

func setPause(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, paused bool, reason *string) api.CollectionPauseStatus {
	t.Helper()
	response, err := client.UpdateCollectionPauseWithResponse(context.Background(), instanceID,
		api.CollectionPauseInput{Paused: paused, Reason: reason})
	if err != nil {
		t.Fatalf("updateCollectionPause(paused=%t): %v", paused, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("updateCollectionPause(paused=%t) = status %d body %s", paused, response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func readPauseStatus(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) api.CollectionPauseStatus {
	t.Helper()
	response, err := client.GetCollectionPauseWithResponse(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("getCollectionPause: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("getCollectionPause = status %d body %s", response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func assertPauseAttribution(t *testing.T, status api.CollectionPauseStatus, actorID uuid.UUID, reason string) {
	t.Helper()
	if !status.Paused {
		t.Fatal("pause status does not report the instance as paused")
	}
	if status.UpdatedBy == nil || *status.UpdatedBy != actorID {
		t.Fatalf("pause actor = %v, want %s", status.UpdatedBy, actorID)
	}
	if status.UpdatedAt == nil {
		t.Fatal("pause carries no timestamp")
	}
	if status.Reason == nil || *status.Reason != reason {
		t.Fatalf("pause reason = %v, want %q", status.Reason, reason)
	}
}

func readPauseInstance(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) api.Instance {
	t.Helper()
	response, err := client.GetInstanceWithResponse(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("getInstance: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("getInstance = status %d body %s", response.StatusCode(), response.Body)
	}
	return *response.JSON200
}

func readPauseTaskSuccessTimes(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) map[string]string {
	t.Helper()
	response, err := client.ListCollectionTaskStatesWithResponse(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("listCollectionTaskStates: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("listCollectionTaskStates = status %d body %s", response.StatusCode(), response.Body)
	}
	times := make(map[string]string, len(*response.JSON200))
	for _, state := range *response.JSON200 {
		result := "none"
		if state.LastResult != nil {
			result = string(*state.LastResult)
		}
		times[string(state.TaskId)] = fmt.Sprintf("success=%s finished=%s result=%s",
			pauseTime(state.LastSuccessAt), pauseTime(state.LastFinishedAt), result)
	}
	return times
}

func pauseTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func equalPauseTaskTimes(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func waitForPauseWatermark(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
		if readPauseInstance(t, client, instanceID).LastCollectedAt == nil {
			return false, "collection-source integrity watermark has not been established yet"
		}
		return true, ""
	})
}

func describePauseAlerts(alerts map[uuid.UUID]api.AlertObservation) string {
	lines := make([]string, 0, len(alerts))
	for id, alert := range alerts {
		lines = append(lines, fmt.Sprintf("%s[%s]=%s", alert.RuleName, id, alert.Status))
	}
	slices.Sort(lines)
	return strings.Join(lines, " ")
}

func readPauseAlerts(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) map[uuid.UUID]api.AlertObservation {
	t.Helper()
	includePaused := true
	response, err := client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{
		InstanceId: &instanceID, IncludePaused: &includePaused,
	})
	if err != nil {
		t.Fatalf("listCurrentAlerts: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("listCurrentAlerts = status %d body %s", response.StatusCode(), response.Body)
	}
	alerts := make(map[uuid.UUID]api.AlertObservation, len(response.JSON200.Items))
	for _, alert := range response.JSON200.Items {
		alerts[alert.Id] = alert
	}
	return alerts
}

func assertPauseAlertEvent(t *testing.T, client *api.ClientWithResponses, alertID uuid.UUID, kind api.AlertEventKind) {
	t.Helper()
	eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
		response, err := client.ListAlertEventsWithResponse(context.Background(), alertID)
		if err != nil {
			return false, fmt.Sprintf("listAlertEvents: %v", err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return false, fmt.Sprintf("listAlertEvents = status %d body %s", response.StatusCode(), response.Body)
		}
		for _, event := range *response.JSON200 {
			if event.Kind == kind {
				return true, ""
			}
		}
		return false, fmt.Sprintf("alert %s history carries no %s event", alertID, kind)
	})
}

type pauseRuleInput struct {
	name              string
	metricID          metric.MetricID
	threshold         float64
	recoveryThreshold float64
	instanceID        uuid.UUID
	policyID          *uuid.UUID
}

func createPauseAlertRule(t *testing.T, client *api.ClientWithResponses, input pauseRuleInput) uuid.UUID {
	t.Helper()
	recoveryThreshold := input.recoveryThreshold
	recoveryCount := 1
	response, err := client.CreateAlertRuleWithResponse(context.Background(), api.AlertRuleInput{
		Name: input.name, MetricId: string(input.metricID), Aggregation: api.Latest,
		Operator: api.GreaterThanEqual, Threshold: input.threshold,
		RecoveryOperator: api.LessThan, RecoveryThreshold: &recoveryThreshold,
		WindowSeconds: 30, ConsecutiveCount: 1, RecoveryConsecutiveCount: &recoveryCount,
		Severity: api.Warning, NoDataPolicy: api.MarkNoData,
		Scope: api.INSTANCES, InstanceIds: []uuid.UUID{input.instanceID},
		EvaluationIntervalSeconds: 5, Enabled: true, NotificationPolicyId: input.policyID,
	})
	if err != nil {
		t.Fatalf("create alert rule %q: %v", input.name, err)
	}
	if response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		t.Fatalf("create alert rule %q = status %d body %s", input.name, response.StatusCode(), response.Body)
	}
	return response.JSON201.Id
}

// 只挂 Webhook：通知是否真的发出，不该取决于 mac 是否信任了验收用的 CA。
func configurePauseNotificationPolicy(t *testing.T, client *api.ClientWithResponses, suffix string) uuid.UUID {
	t.Helper()
	webhook, err := client.CreateWebhookTargetWithResponse(context.Background(), api.WebhookTargetInput{
		Name: suffix + " Webhook", Enabled: true, Url: webhookSinkAPI + "/notifications",
		SigningValue: stringPointer(webhookSigningValue), SignatureHeader: stringPointer(webhookSignatureHeader),
	})
	if err != nil || webhook.StatusCode() != http.StatusCreated || webhook.JSON201 == nil {
		t.Fatalf("create webhook target: status=%d body=%s error=%v", webhook.StatusCode(), webhook.Body, err)
	}
	contact, err := client.CreateNotificationContactWithResponse(context.Background(), api.NotificationContactInput{
		Name: suffix + " DBA", Email: "on-call@example.com",
	})
	if err != nil || contact.StatusCode() != http.StatusCreated || contact.JSON201 == nil {
		t.Fatalf("create notification contact: status=%d body=%s error=%v", contact.StatusCode(), contact.Body, err)
	}
	policy, err := client.CreateNotificationPolicyWithResponse(context.Background(), api.NotificationPolicyInput{
		Name: suffix + " policy", ContactIds: []uuid.UUID{contact.JSON201.Id}, ContactGroupIds: []uuid.UUID{},
		Channels:       []api.NotificationPolicyChannel{{Channel: api.PolicyWebhook, TargetId: &webhook.JSON201.Id}},
		SeverityFilter: []api.AlertSeverity{api.Warning, api.Critical},
		NotifyOnFire:   true, NotifyOnRecovery: true, RepeatInterval: 30,
	})
	if err != nil || policy.StatusCode() != http.StatusCreated || policy.JSON201 == nil {
		t.Fatalf("create notification policy: status=%d body=%s error=%v", policy.StatusCode(), policy.Body, err)
	}
	return policy.JSON201.Id
}

func waitForPauseFiringAlert(t *testing.T, client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) uuid.UUID {
	t.Helper()
	var alertID uuid.UUID
	eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
		alert, detail := findCollectionAlert(client, instanceID, ruleID)
		if alert == nil {
			return false, detail
		}
		if alert.Status != api.FIRING {
			return false, fmt.Sprintf("alert on rule %s = %s, want FIRING", ruleID, alert.Status)
		}
		alertID = alert.Id
		return true, ""
	})
	return alertID
}
