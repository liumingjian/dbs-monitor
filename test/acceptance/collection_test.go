//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	collectionConditionTimeout = 90 * time.Second
	// 能力快照按 metric.CapabilitySnapshotTTL 重探，能力面的每一次翻转都得等它一轮。
	capabilityRefreshTimeout = metric.CapabilitySnapshotTTL + 90*time.Second
	// 内置采集类规则要连续 3 个 30 秒评估周期才转 FIRING，恢复同样滞回一次。
	collectionAlertTimeout  = 5 * time.Minute
	collectionTargetService = "acceptance-target"

	unreachableEntryMessage = "停目标库容器后指标返回 DB_UNREACHABLE、能力快照整份投影 UNKNOWN，且探针失败与背压跳过在任务状态上可区分"
	unreachableRuleMessage  = "内置规则 pg.availability.reachable 被真实停库打到 FIRING，恢复后不残留未恢复告警"
)

func TestAcceptance_AC_01_F1(t *testing.T) {
	runCollectionEntry(t, "AC-01-F1", "三档角色对任务采样周期写端点各得规定结局，周期读回恒等于配置值", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18460)
		instanceID := createIssue60Instance(t, runtime.client, "AC-01-F1 target", "monitored", "monitored", issue60TargetPort(t))

		// D9：三档角色账号一律经用户管理 API 创建，不直插 user 表。
		viewer := createSecurityUser(t, runtime.client, "ac-01-f1-viewer", api.READONLY)
		alertAdmin := createSecurityUser(t, runtime.client, "ac-01-f1-alert-admin", api.ALERTADMIN)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-01-f1-platform-admin", api.PLATFORMADMIN)

		configured := collectionTaskInterval(t, runtime.client, instanceID, metric.TaskStatActivity)
		const denied, granted = 41, 43

		for _, refused := range []struct {
			role api.Role
			user api.UserCreated
		}{
			{role: api.READONLY, user: viewer},
			{role: api.ALERTADMIN, user: alertAdmin},
		} {
			client := newSecurityClient(t, runtime.baseURL, runtime.caPath)
			loginSecurityUser(t, client, refused.user.User.Username, refused.user.InitialPassword)
			response, err := client.UpdateCollectionTaskIntervalWithResponse(
				context.Background(), instanceID, string(metric.TaskStatActivity),
				api.CollectionTaskIntervalInput{IntervalSeconds: denied},
			)
			if err != nil {
				t.Fatalf("%s updateCollectionTaskInterval: %v", refused.role, err)
			}
			if response.StatusCode() != http.StatusForbidden {
				t.Fatalf("%s updateCollectionTaskInterval = status %d body %s, want 403", refused.role, response.StatusCode(), response.Body)
			}
			if current := collectionTaskInterval(t, runtime.client, instanceID, metric.TaskStatActivity); current != configured {
				t.Fatalf("%s silently changed the interval to %d, want it left at %d", refused.role, current, configured)
			}
		}

		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)
		accepted, err := adminClient.UpdateCollectionTaskIntervalWithResponse(
			context.Background(), instanceID, string(metric.TaskStatActivity),
			api.CollectionTaskIntervalInput{IntervalSeconds: granted},
		)
		if err != nil {
			t.Fatalf("PLATFORM_ADMIN updateCollectionTaskInterval: %v", err)
		}
		if accepted.StatusCode() != http.StatusOK || accepted.JSON200 == nil {
			t.Fatalf("PLATFORM_ADMIN updateCollectionTaskInterval = status %d body %s, want 200", accepted.StatusCode(), accepted.Body)
		}
		if accepted.JSON200.IntervalSeconds != granted {
			t.Fatalf("write response interval = %d, want %d", accepted.JSON200.IntervalSeconds, granted)
		}
		if current := collectionTaskInterval(t, runtime.client, instanceID, metric.TaskStatActivity); current != granted {
			t.Fatalf("interval read back as %d, want %d", current, granted)
		}
	})
}

func TestAcceptance_AC_01_F3(t *testing.T) {
	runCollectionEntry(t, "AC-01-F3", "真实回收 pg_monitor 后能力转 MISSING/fixable，影响指标计数与字典一致，受影响指标返回 PERMISSION_DENIED", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18461)
		admin := openIssue60Target(t, "monitored", "monitored", "monitored")
		defer admin.Close(context.Background())

		const role = "ac-01-f3-monitor"
		execCollectionSQL(t, admin, fmt.Sprintf("DROP ROLE IF EXISTS %q", role))
		execCollectionSQL(t, admin, fmt.Sprintf("CREATE ROLE %q LOGIN PASSWORD 'monitored'", role))
		execCollectionSQL(t, admin, fmt.Sprintf("GRANT pg_monitor TO %q", role))
		t.Cleanup(func() {
			cleanup := openIssue60Target(t, "monitored", "monitored", "monitored")
			defer cleanup.Close(context.Background())
			execCollectionSQL(t, cleanup, fmt.Sprintf("DROP ROLE IF EXISTS %q", role))
		})

		instanceID := createIssue60Instance(t, runtime.client, "AC-01-F3 target", "monitored", role, issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		revokedAt := time.Now().UTC()
		execCollectionSQL(t, admin, fmt.Sprintf("REVOKE pg_monitor FROM %q", role))

		// 「影响 N 项指标」必须沿 Task.Requires 反查得出，所以断言值由字典现算，不引第二张表。
		wantAffected := collectionAffectedMetricCount(metric.CapabilityRolePGMonitor)
		if wantAffected == 0 {
			t.Fatal("collection dictionary declares no metric behind role.pg_monitor")
		}
		eventuallyIssue60(t, capabilityRefreshTimeout, func() (bool, string) {
			entries, detail := readCollectionCapabilities(runtime.client, instanceID)
			if entries == nil {
				return false, detail
			}
			entry, exists := entries[api.RolePgMonitor]
			if !exists {
				return false, "capability snapshot omitted role.pg_monitor"
			}
			if entry.Status != api.MISSING {
				return false, fmt.Sprintf("role.pg_monitor status = %s, want MISSING", entry.Status)
			}
			if entry.Class != api.Fixable {
				return false, fmt.Sprintf("role.pg_monitor class = %s, want fixable", entry.Class)
			}
			if entry.FixHint == nil || *entry.FixHint == "" {
				return false, "role.pg_monitor carries no FixHint"
			}
			if entry.AffectedMetricCount != wantAffected {
				return false, fmt.Sprintf("role.pg_monitor affected metric count = %d, want %d", entry.AffectedMetricCount, wantAffected)
			}
			return true, ""
		})

		assertCollectionMetricUnavailableWithin(t, runtime.client, instanceID, metric.MetricConnectionTotal, revokedAt, api.PERMISSIONDENIED, capabilityRefreshTimeout)
	})
}

// AC-01-F5 与 BUILTIN-1 共用同一次「停目标库容器」现场（矩阵 rides_on），
// 断言集各自独立：先跑完片条目自己的断言并定分，再跑内置规则那一组。
func TestAcceptance_AC_01_F5(t *testing.T) {
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for AC-01-F5")
	}
	started := time.Now()
	hostRecorded := false
	defer func() {
		if !hostRecorded {
			recordCollectionResult(t, "AC-01-F5", unreachableEntryMessage, true, started)
		}
	}()

	requireComposeProject(t)
	runtime := startIssue60Runtime(t, 18462)
	instanceID := createIssue60Instance(t, runtime.client, "AC-01-F5 target", "monitored", "monitored", issue60TargetPort(t))
	setIssue60TaskIntervals(t, runtime.client, instanceID)
	// 先取得一份完整的、非 UNKNOWN 的快照，后面的整份 UNKNOWN 才排除得掉「从未采过」。
	waitForIssue60Capabilities(t, runtime.client, instanceID)
	waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

	outageStart := time.Now().UTC()
	stopCollectionTarget(t)

	assertCollectionMetricUnavailable(t, runtime.client, instanceID, metric.MetricConnectionTotal, outageStart, api.DBUNREACHABLE)
	eventuallyIssue60(t, capabilityRefreshTimeout, func() (bool, string) {
		entries, detail := readCollectionCapabilities(runtime.client, instanceID)
		if entries == nil {
			return false, detail
		}
		if len(entries) != len(metric.Capabilities) {
			return false, fmt.Sprintf("capability snapshot has %d entries, want the full list of %d", len(entries), len(metric.Capabilities))
		}
		for id, entry := range entries {
			if entry.Status != api.UNKNOWN {
				return false, fmt.Sprintf("capability %s status = %s, want UNKNOWN", id, entry.Status)
			}
		}
		return true, ""
	})

	// 探针失败与背压跳过在任务状态上是两条不同的记录路径：失败记 FAILED/TIMED_OUT、带原因码、
	// 累加连续失败数，并写下 reachable=false；跳过只换一个 last_result 取值，连续失败数原地不动，
	// 也不写那条样本。这条断言盯的就是这三处差别。
	// 那条 reachable=false 样本本身在停库期间被读侧的 DB_UNREACHABLE 罩住，看不见；
	// 它真的落了库这件事由搭车的 BUILTIN-1 证明 —— 内置规则正是靠它才打得起来。
	eventuallyIssue60(t, collectionConditionTimeout, func() (bool, string) {
		state, detail := readCollectionTaskState(runtime.client, instanceID, metric.TaskProbe)
		if state == nil {
			return false, detail
		}
		if state.LastResult == nil {
			return false, "probe task has no recorded result"
		}
		switch *state.LastResult {
		case api.FAILED, api.TIMEDOUT:
		default:
			return false, fmt.Sprintf("probe task result = %s, want FAILED or TIMED_OUT", *state.LastResult)
		}
		if state.LastErrorCode == nil || *state.LastErrorCode == "" {
			return false, "probe task failure carries no error code"
		}
		if state.ConsecutiveFailures == 0 {
			return false, "probe task recorded no consecutive failure, which is how a backpressure skip looks"
		}
		return true, ""
	})

	// 片条目的断言到此为止，先定分：搭车的 BUILTIN-1 独立计分，成败互不牵连。
	recordCollectionResult(t, "AC-01-F5", unreachableEntryMessage, t.Failed(), started)
	hostRecorded = true

	builtinStarted := time.Now()
	defer func() {
		recordCollectionResult(t, "BUILTIN-1", unreachableRuleMessage, t.Failed(), builtinStarted)
	}()
	assertBuiltinUnreachableRuleFires(t, runtime.client, instanceID)
}

func TestAcceptance_AC_01_F6(t *testing.T) {
	runCollectionEntry(t, "AC-01-F6", "主库上的复制与 slot 能力为结构性不适用，复制类指标返回 NOT_APPLICABLE_ROLE，与真实 slot 的字节数严格可区分", func(t *testing.T) {
		runtime := startIssue60Runtime(t, 18463)
		admin := openIssue60Target(t, "monitored", "monitored", "monitored")
		defer admin.Close(context.Background())

		const slot = "ac_01_f6"
		dropCollectionSlot(t, admin, slot)
		t.Cleanup(func() {
			cleanup := openIssue60Target(t, "monitored", "monitored", "monitored")
			defer cleanup.Close(context.Background())
			dropCollectionSlot(t, cleanup, slot)
		})

		instanceID := createIssue60Instance(t, runtime.client, "AC-01-F6 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60Capabilities(t, runtime.client, instanceID)

		structuralStart := time.Now().UTC()
		for _, capabilityID := range []api.CapabilitySnapshotEntryCapabilityId{api.TopoHasReplication, api.TopoHasSlot} {
			eventuallyIssue60(t, collectionConditionTimeout, func() (bool, string) {
				entries, detail := readCollectionCapabilities(runtime.client, instanceID)
				if entries == nil {
					return false, detail
				}
				entry, exists := entries[capabilityID]
				if !exists {
					return false, fmt.Sprintf("capability snapshot omitted %s", capabilityID)
				}
				if entry.Status != api.NOTAPPLICABLE {
					return false, fmt.Sprintf("capability %s status = %s, want NOT_APPLICABLE", capabilityID, entry.Status)
				}
				// 结构性不适用是值不是缺失：走 NAReason，不进配置缺失待办，也就没有修复指引。
				if entry.Class != api.Structural {
					return false, fmt.Sprintf("capability %s class = %s, want structural", capabilityID, entry.Class)
				}
				if entry.NaReason == nil || *entry.NaReason == "" {
					return false, fmt.Sprintf("capability %s carries no NAReason", capabilityID)
				}
				if entry.FixHint != nil && *entry.FixHint != "" {
					return false, fmt.Sprintf("structural capability %s offers a fix hint %q", capabilityID, *entry.FixHint)
				}
				return true, ""
			})
		}

		for _, metricID := range []metric.MetricID{
			metric.MetricReplicationReplayLagMS,
			metric.MetricReplicationWALLagBytes,
			metric.MetricReplicationSlotRetainedWAL,
		} {
			assertCollectionMetricUnavailable(t, runtime.client, instanceID, metricID, structuralStart, api.NOTAPPLICABLEROLE)
		}

		// 造一个真实 slot：同一条指标此时必须给出真实字节数（可以是 0），
		// 与上面「不适用」的无点 + NA 码严格可区分。
		backlogStart := time.Now().UTC()
		execCollectionSQL(t, admin, fmt.Sprintf("SELECT pg_create_physical_replication_slot('%s', true)", slot))
		eventuallyIssue60(t, capabilityRefreshTimeout, func() (bool, string) {
			entries, detail := readCollectionCapabilities(runtime.client, instanceID)
			if entries == nil {
				return false, detail
			}
			if entry, exists := entries[api.TopoHasSlot]; !exists || entry.Status != api.PRESENT {
				return false, fmt.Sprintf("capability topo.has_slot = %v, want PRESENT", entry.Status)
			}
			return true, ""
		})
		eventuallyIssue60(t, capabilityRefreshTimeout, func() (bool, string) {
			points, reason, detail := readCollectionMetric(runtime.client, instanceID, metric.MetricReplicationSlotRetainedWAL, backlogStart, time.Now().UTC().Add(time.Minute))
			if detail != "" {
				return false, detail
			}
			if reason != nil {
				return false, fmt.Sprintf("real replication slot still reports %s", *reason)
			}
			if len(points) == 0 {
				return false, "real replication slot produced no retained-WAL sample"
			}
			for _, value := range points {
				if value < 0 {
					return false, fmt.Sprintf("retained WAL bytes = %v, want a non-negative measurement", value)
				}
			}
			return true, ""
		})
	})
}

func assertBuiltinUnreachableRuleFires(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) {
	t.Helper()
	rule := collectionBuiltinRule(t, client, "database_unreachable")
	if rule.Severity != api.Critical {
		t.Fatalf("built-in unreachable rule severity = %s, want critical", rule.Severity)
	}
	if !rule.Enabled {
		t.Fatal("built-in unreachable rule is disabled")
	}

	var alertID uuid.UUID
	eventuallyIssue60(t, collectionAlertTimeout, func() (bool, string) {
		alert, detail := findCollectionAlert(client, instanceID, rule.Id)
		if alert == nil {
			return false, detail
		}
		if alert.Status != api.FIRING {
			return false, fmt.Sprintf("built-in unreachable alert status = %s, want FIRING", alert.Status)
		}
		if alert.Severity != api.Critical {
			return false, fmt.Sprintf("built-in unreachable alert severity = %s, want critical", alert.Severity)
		}
		alertID = alert.Id
		return true, ""
	})

	// INV-2：实例健康是未恢复告警的最坏归并，critical 必须把实例染成 CRITICAL。
	eventuallyIssue60(t, collectionConditionTimeout, func() (bool, string) {
		response, err := client.GetInstanceWithResponse(context.Background(), instanceID)
		if err != nil || response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return false, fmt.Sprintf("read instance health: status=%d error=%v", response.StatusCode(), err)
		}
		if response.JSON200.Health.Status != api.HealthCritical {
			return false, fmt.Sprintf("instance health = %s, want CRITICAL", response.JSON200.Health.Status)
		}
		return true, ""
	})

	startCollectionTarget(t)
	eventuallyIssue60(t, collectionAlertTimeout, func() (bool, string) {
		if alert, _ := findCollectionAlert(client, instanceID, rule.Id); alert != nil {
			return false, fmt.Sprintf("alert %s is still unresolved with status %s", alert.Id, alert.Status)
		}
		recovered, detail := findCollectionHistoricalAlert(client, instanceID, alertID)
		if recovered == nil {
			return false, detail
		}
		if recovered.Status != api.RECOVERED {
			return false, fmt.Sprintf("alert %s history status = %s, want RECOVERED", alertID, recovered.Status)
		}
		return true, ""
	})
}

func runCollectionEntry(t *testing.T, id, passedMessage string, exercise func(*testing.T)) {
	t.Helper()
	if os.Getenv("ACCEPTANCE_PLATFORM_DATABASE_URL") == "" {
		t.Skip("ACCEPTANCE_PLATFORM_DATABASE_URL is required for " + id)
	}
	started := time.Now()
	defer func() {
		recordCollectionResult(t, id, passedMessage, t.Failed(), started)
	}()
	exercise(t)
}

func recordCollectionResult(t *testing.T, id, passedMessage string, failed bool, started time.Time) {
	t.Helper()
	status, message := resultPassed, passedMessage
	if failed {
		status, message = resultFailed, id+" failed; see go test output"
	}
	acceptanceReport.record(id, status, message, time.Since(started))
}

func collectionAffectedMetricCount(capabilityID metric.CapabilityID) int {
	count := 0
	for _, task := range metric.Tasks {
		for _, required := range task.Requires {
			if required != capabilityID {
				continue
			}
			count += len(task.Yields)
			break
		}
	}
	return count
}

func collectionTaskInterval(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID, taskID metric.TaskID) int {
	t.Helper()
	state, detail := readCollectionTaskState(client, instanceID, taskID)
	if state == nil {
		t.Fatal(detail)
	}
	return state.IntervalSeconds
}

func readCollectionTaskState(client *api.ClientWithResponses, instanceID uuid.UUID, taskID metric.TaskID) (*api.CollectionTaskState, string) {
	response, err := client.ListCollectionTaskStatesWithResponse(context.Background(), instanceID)
	if err != nil {
		return nil, fmt.Sprintf("collection task states unavailable: error=%v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, fmt.Sprintf("collection task states unavailable: status=%d body=%s", response.StatusCode(), response.Body)
	}
	for _, state := range *response.JSON200 {
		if string(state.TaskId) == string(taskID) {
			found := state
			return &found, ""
		}
	}
	return nil, fmt.Sprintf("collection task %s is absent from the task state list", taskID)
}

func readCollectionCapabilities(client *api.ClientWithResponses, instanceID uuid.UUID) (map[api.CapabilitySnapshotEntryCapabilityId]api.CapabilitySnapshotEntry, string) {
	response, err := client.ListCapabilitySnapshotWithResponse(context.Background(), instanceID)
	if err != nil {
		return nil, fmt.Sprintf("capability snapshot unavailable: error=%v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, fmt.Sprintf("capability snapshot unavailable: status=%d body=%s", response.StatusCode(), response.Body)
	}
	entries := make(map[api.CapabilitySnapshotEntryCapabilityId]api.CapabilitySnapshotEntry, len(*response.JSON200))
	for _, entry := range *response.JSON200 {
		entries[entry.CapabilityId] = entry
	}
	return entries, ""
}

func readCollectionMetric(
	client *api.ClientWithResponses,
	instanceID uuid.UUID,
	metricID metric.MetricID,
	from, to time.Time,
) ([]float64, *api.Unavailability, string) {
	step := api.Raw
	response, err := client.GetMetricSeriesWithResponse(context.Background(), instanceID, &api.GetMetricSeriesParams{
		Metric: []api.GetMetricSeriesParamsMetric{api.GetMetricSeriesParamsMetric(metricID)},
		From:   from.UTC(), To: to.UTC(), Step: &step,
	})
	if err != nil {
		return nil, nil, fmt.Sprintf("metric %s unavailable: error=%v", metricID, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, nil, fmt.Sprintf("metric %s unavailable: status=%d body=%s", metricID, response.StatusCode(), response.Body)
	}
	if len(response.JSON200.Metrics) != 1 {
		return nil, nil, fmt.Sprintf("metric %s returned %d entries, want 1", metricID, len(response.JSON200.Metrics))
	}
	item := response.JSON200.Metrics[0]
	var values []float64
	for _, series := range item.Series {
		for _, point := range series.Points {
			if len(point) == 2 && point[1] != nil {
				values = append(values, *point[1])
			}
		}
	}
	if !item.Unavailability.IsSpecified() {
		return nil, nil, fmt.Sprintf("metric %s omitted the required unavailability field", metricID)
	}
	if item.Unavailability.IsNull() {
		return values, nil, ""
	}
	code, getErr := item.Unavailability.Get()
	if getErr != nil {
		return nil, nil, fmt.Sprintf("metric %s unavailability: %v", metricID, getErr)
	}
	return values, &code, ""
}

// 「有原因码」和「有数据但全零」是两种结局，这里两者一起断死。
func assertCollectionMetricUnavailable(
	t *testing.T,
	client *api.ClientWithResponses,
	instanceID uuid.UUID,
	metricID metric.MetricID,
	from time.Time,
	want api.Unavailability,
) {
	t.Helper()
	assertCollectionMetricUnavailableWithin(t, client, instanceID, metricID, from, want, collectionConditionTimeout)
}

func assertCollectionMetricUnavailableWithin(
	t *testing.T,
	client *api.ClientWithResponses,
	instanceID uuid.UUID,
	metricID metric.MetricID,
	from time.Time,
	want api.Unavailability,
	timeout time.Duration,
) {
	t.Helper()
	eventuallyIssue60(t, timeout, func() (bool, string) {
		values, reason, detail := readCollectionMetric(client, instanceID, metricID, from, time.Now().UTC().Add(time.Minute))
		if detail != "" {
			return false, detail
		}
		if reason == nil {
			return false, fmt.Sprintf("metric %s reported %d points and no reason, want %s", metricID, len(values), want)
		}
		if *reason != want {
			return false, fmt.Sprintf("metric %s reason = %s, want %s", metricID, *reason, want)
		}
		if len(values) != 0 {
			return false, fmt.Sprintf("metric %s carries %d points alongside reason %s", metricID, len(values), want)
		}
		return true, ""
	})
}

func collectionBuiltinRule(t *testing.T, client *api.ClientWithResponses, identifier string) api.AlertRule {
	t.Helper()
	response, err := client.ListAlertRulesWithResponse(context.Background())
	if err != nil {
		t.Fatalf("list alert rules: %v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("list alert rules = status %d body %s", response.StatusCode(), response.Body)
	}
	for _, rule := range *response.JSON200 {
		if rule.BuiltinIdentifier != nil && *rule.BuiltinIdentifier == identifier {
			if !rule.IsBuiltin {
				t.Fatalf("rule %s carries built-in identifier %s but is not marked built-in", rule.Id, identifier)
			}
			return rule
		}
	}
	t.Fatalf("built-in rule %s is absent from the rule list", identifier)
	return api.AlertRule{}
}

func findCollectionAlert(client *api.ClientWithResponses, instanceID, ruleID uuid.UUID) (*api.AlertObservation, string) {
	response, err := client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{InstanceId: &instanceID})
	if err != nil {
		return nil, fmt.Sprintf("current alerts unavailable: error=%v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, fmt.Sprintf("current alerts unavailable: status=%d body=%s", response.StatusCode(), response.Body)
	}
	for _, alert := range response.JSON200.Items {
		if alert.RuleId == ruleID {
			found := alert
			return &found, ""
		}
	}
	return nil, fmt.Sprintf("no unresolved alert for rule %s on instance %s", ruleID, instanceID)
}

func findCollectionHistoricalAlert(client *api.ClientWithResponses, instanceID, alertID uuid.UUID) (*api.AlertObservation, string) {
	response, err := client.ListAlertHistoryWithResponse(context.Background(), &api.ListAlertHistoryParams{InstanceId: &instanceID})
	if err != nil {
		return nil, fmt.Sprintf("alert history unavailable: error=%v", err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		return nil, fmt.Sprintf("alert history unavailable: status=%d body=%s", response.StatusCode(), response.Body)
	}
	for _, alert := range response.JSON200.Items {
		if alert.Id == alertID {
			found := alert
			return &found, ""
		}
	}
	return nil, fmt.Sprintf("alert %s is absent from the instance history", alertID)
}

func execCollectionSQL(t *testing.T, connection *pgx.Conn, statement string) {
	t.Helper()
	if _, err := connection.Exec(context.Background(), statement); err != nil {
		t.Fatalf("run %q on the target database: %v", statement, err)
	}
}

func dropCollectionSlot(t *testing.T, connection *pgx.Conn, slot string) {
	t.Helper()
	execCollectionSQL(t, connection,
		fmt.Sprintf("SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = '%s'", slot))
}

func requireComposeProject(t *testing.T) string {
	t.Helper()
	project := os.Getenv("ACCEPTANCE_COMPOSE_PROJECT")
	if project == "" {
		t.Fatal("ACCEPTANCE_COMPOSE_PROJECT is required to stop the real target container")
	}
	return project
}

func stopCollectionTarget(t *testing.T) {
	t.Helper()
	runCollectionCompose(t, "stop", collectionTargetService)
	t.Cleanup(func() { startCollectionTarget(t) })
}

func startCollectionTarget(t *testing.T) {
	t.Helper()
	runCollectionCompose(t, "start", collectionTargetService)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := pgx.Connect(context.Background(),
			fmt.Sprintf("postgres://monitored:monitored@127.0.0.1:%d/monitored?sslmode=disable", issue60TargetPort(t)))
		if err == nil {
			connection.Close(context.Background())
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("target database did not accept connections again after restart")
}

func runCollectionCompose(t *testing.T, action, service string) {
	t.Helper()
	command := exec.Command("docker", "compose", "-p", requireComposeProject(t), action, service)
	command.Dir = repositoryRoot(t)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("docker compose %s %s: %v\n%s", action, service, err, output)
	}
}

// BUILTIN-3 独立执行一次（矩阵 rides_on 为空）。24 号 D15 外溢到实现的硬要求是
// 「采集新鲜度阈值须可参数化到秒级」：内置规则不可删、不可停用、级别不得降到 info，
// 但阈值本来就是可改的，所以这里经 updateAlertRule 把它压到秒级再打，
// 既不直写历史样本，也就不需要往 D4 白名单里加例外。
func TestAcceptance_BUILTIN_3(t *testing.T) {
	runCollectionEntry(t, "BUILTIN-3", "内置规则 collector.last_success_time 被真实停采集打到 FIRING，采集恢复后按滞回语义恢复", func(t *testing.T) {
		requireComposeProject(t)
		runtime := startIssue60Runtime(t, 18471)
		instanceID := createIssue60Instance(t, runtime.client, "BUILTIN-3 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricCollectorLastSuccessTime)

		shipped := collectionBuiltinRule(t, runtime.client, "data_stale")
		// 下限即默认：改 info 被拒由 INV-4 覆盖，这里只断默认值本身。
		if shipped.Severity != api.Warning {
			t.Fatalf("built-in data-stale rule severity = %s, want warning", shipped.Severity)
		}
		if shipped.MetricId != string(metric.MetricCollectorLastSuccessTime) {
			t.Fatalf("built-in data-stale rule metric = %s, want collector.last_success_time", shipped.MetricId)
		}
		tuned := updateCollectionAlertRule(t, runtime.client, shipped, func(input *api.AlertRuleInput) {
			threshold, recovery := 20.0, 10.0
			consecutive := 1
			input.Threshold = threshold
			input.RecoveryThreshold = &recovery
			input.WindowSeconds = 30
			input.ConsecutiveCount = consecutive
			input.RecoveryConsecutiveCount = &consecutive
			input.EvaluationIntervalSeconds = 5
		})
		if tuned.Threshold != 20 {
			t.Fatalf("freshness threshold = %v, want the requested 20 seconds", tuned.Threshold)
		}
		// 调低的阈值随本条目自己的平台库一起消失，不会漏给别的条目。

		stopCollectionTarget(t)
		alertID := waitForPauseFiringAlert(t, runtime.client, instanceID, shipped.Id)
		alert, detail := findCollectionAlert(runtime.client, instanceID, shipped.Id)
		if alert == nil {
			t.Fatal(detail)
		}
		if alert.Severity != api.Warning {
			t.Fatalf("data-stale alert severity = %s, want warning", alert.Severity)
		}

		startCollectionTarget(t)
		waitForRetentionRecovery(t, runtime.client, alertID)
	})
}

func updateCollectionAlertRule(
	t *testing.T,
	client *api.ClientWithResponses,
	rule api.AlertRule,
	mutate func(*api.AlertRuleInput),
) api.AlertRule {
	t.Helper()
	recoveryThreshold := rule.RecoveryThreshold
	recoveryCount := rule.RecoveryConsecutiveCount
	input := api.AlertRuleInput{
		Name: rule.Name, MetricId: rule.MetricId, Aggregation: rule.Aggregation,
		Operator: rule.Operator, Threshold: rule.Threshold,
		RecoveryOperator: rule.RecoveryOperator, RecoveryThreshold: &recoveryThreshold,
		WindowSeconds: rule.WindowSeconds, ConsecutiveCount: rule.ConsecutiveCount,
		RecoveryConsecutiveCount: &recoveryCount, Severity: rule.Severity,
		NoDataPolicy: rule.NoDataPolicy, Scope: rule.Scope, InstanceIds: rule.InstanceIds,
		EvaluationIntervalSeconds: rule.EvaluationIntervalSeconds, Enabled: rule.Enabled,
		NotificationPolicyId: rule.NotificationPolicyId,
	}
	mutate(&input)
	response, err := client.UpdateAlertRuleWithResponse(context.Background(), rule.Id, input)
	if err != nil {
		t.Fatalf("update alert rule %s: %v", rule.Name, err)
	}
	if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
		t.Fatalf("update alert rule %s = status %d body %s", rule.Name, response.StatusCode(), response.Body)
	}
	return *response.JSON200
}
