//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const (
	platformDatabaseService = "acceptance-platform"
	// 平台库缺席得够久，采集与评估两条循环都会在这段窗口里空转好几轮。
	platformOutageWindow = 30 * time.Second
)

// 与 AC-07-F4 同源注入（22 号 D4）：手段都是真停平台 PG 容器，断言点不同 ——
// 本条断的是「不污染目标侧」，AC-07-F4 断的是「暂停状态不丢不误恢复」。
func TestAcceptance_AC_01_F4(t *testing.T) {
	runCollectionEntry(t, "AC-01-F4", "平台库不可达期间目标侧零新增告警实例、零 NO_DATA、零样本，恢复后采集继续且不补跑", func(t *testing.T) {
		requireComposeProject(t)
		runtime := startIssue60Runtime(t, 18469)
		instanceID := createIssue60Instance(t, runtime.client, "AC-01-F4 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		ruleID := createPauseAlertRule(t, runtime.client, pauseRuleInput{
			name: "AC-01-F4 常态越界", metricID: metric.MetricConnectionTotal,
			threshold: 1, recoveryThreshold: 0, instanceID: instanceID,
		})
		alertID := waitForPauseFiringAlert(t, runtime.client, instanceID, ruleID)
		before := readPauseAlerts(t, runtime.client, instanceID)

		outageStart := time.Now().UTC()
		stopPlatformDatabase(t)
		time.Sleep(platformOutageWindow)
		outageEnd := time.Now().UTC()
		startPlatformDatabase(t)

		// 平台库恢复后采集继续。
		eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
			current := readPauseInstance(t, runtime.client, instanceID)
			if current.LastCollectedAt == nil || !current.LastCollectedAt.After(outageEnd) {
				return false, fmt.Sprintf("collection watermark is still %v after the platform database came back", current.LastCollectedAt)
			}
			return true, ""
		})

		assertNoTargetContamination(t, runtime.client, instanceID, before, alertID)

		// metric_sample 该实例零新增行：DB 层只读计数，前后对比。
		gapStart, gapEnd := outageStart.Add(2*time.Second), outageEnd.Add(-2*time.Second)
		if samples := countInstanceSamples(t, runtime.databaseURL, instanceID, gapStart, gapEnd); samples != 0 {
			t.Fatalf("platform outage window carries %d samples for the target instance, want none", samples)
		}
		// 不补跑、不造占位样本：采集恢复之后再数一次，窗口里依旧是空的。
		time.Sleep(15 * time.Second)
		if samples := countInstanceSamples(t, runtime.databaseURL, instanceID, gapStart, gapEnd); samples != 0 {
			t.Fatalf("collection replayed %d samples into the platform outage window", samples)
		}
	})
}

func TestAcceptance_AC_07_F4(t *testing.T) {
	runCollectionEntry(t, "AC-07-F4", "平台库不可达前后暂停状态不丢不误恢复、留痕完好，期间不产生 NO_DATA 也不产生新告警实例", func(t *testing.T) {
		requireComposeProject(t)
		runtime := startIssue60Runtime(t, 18470)
		platformAdmin := createSecurityUser(t, runtime.client, "ac-07-f4-platform-admin", api.PLATFORMADMIN)
		adminClient := newSecurityClient(t, runtime.baseURL, runtime.caPath)
		loginSecurityUser(t, adminClient, platformAdmin.User.Username, platformAdmin.InitialPassword)

		instanceID := createIssue60Instance(t, runtime.client, "AC-07-F4 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		ruleID := createPauseAlertRule(t, runtime.client, pauseRuleInput{
			name: "AC-07-F4 常态越界", metricID: metric.MetricConnectionTotal,
			threshold: 1, recoveryThreshold: 0, instanceID: instanceID,
		})
		alertID := waitForPauseFiringAlert(t, runtime.client, instanceID, ruleID)
		before := readPauseAlerts(t, runtime.client, instanceID)

		reason := "AC-07-F4 暂停期间遇上平台自身故障"
		paused := setPause(t, adminClient, instanceID, true, &reason)

		stopPlatformDatabase(t)
		time.Sleep(platformOutageWindow)
		startPlatformDatabase(t)

		// 控制面单一真相源未被改写：暂停仍生效，操作人/时间/原因一字未动。
		var restored api.CollectionPauseStatus
		eventuallyIssue60(t, pauseConditionTimeout, func() (bool, string) {
			response, err := runtime.client.GetCollectionPauseWithResponse(context.Background(), instanceID)
			if err != nil || response.StatusCode() != 200 || response.JSON200 == nil {
				return false, fmt.Sprintf("getCollectionPause after the outage: status=%d error=%v", response.StatusCode(), err)
			}
			restored = *response.JSON200
			return true, ""
		})
		assertPauseAttribution(t, restored, platformAdmin.User.Id, reason)
		if paused.UpdatedAt == nil || restored.UpdatedAt == nil || !restored.UpdatedAt.Equal(*paused.UpdatedAt) {
			t.Fatalf("pause timestamp after the outage = %v, want it left at %v", restored.UpdatedAt, paused.UpdatedAt)
		}
		// 平台自身故障不是恢复采集的理由，指标仍然只说「已暂停」。
		assertCollectionMetricUnavailable(t, runtime.client, instanceID, metric.MetricConnectionTotal,
			time.Now().UTC().Add(-time.Minute), api.COLLECTIONPAUSED)

		assertNoTargetContamination(t, runtime.client, instanceID, before, alertID)
	})
}

func assertNoTargetContamination(
	t *testing.T,
	client *api.ClientWithResponses,
	instanceID uuid.UUID,
	before map[uuid.UUID]api.AlertObservation,
	alertID uuid.UUID,
) {
	t.Helper()
	after := readPauseAlerts(t, client, instanceID)
	for id, alert := range after {
		previous, existed := before[id]
		if !existed {
			t.Fatalf("platform outage opened a new alert instance %s on rule %s", id, alert.RuleName)
		}
		if alert.Status == api.NODATA && previous.Status != api.NODATA {
			t.Fatalf("platform outage pushed alert %s on rule %s into NO_DATA", id, alert.RuleName)
		}
	}
	response, err := client.ListAlertEventsWithResponse(context.Background(), alertID)
	if err != nil || response.StatusCode() != 200 || response.JSON200 == nil {
		t.Fatalf("listAlertEvents: status=%d body=%s error=%v", response.StatusCode(), response.Body, err)
	}
	for _, event := range *response.JSON200 {
		if event.Kind == api.AlertEventNoDataEntered {
			t.Fatalf("alert %s recorded a NO_DATA_ENTERED event at %s during the platform outage", alertID, event.EvaluatedAt)
		}
	}
}

func countInstanceSamples(t *testing.T, databaseURL string, instanceID uuid.UUID, from, to time.Time) int64 {
	t.Helper()
	connection, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect to the platform database for a read-only sample count: %v", err)
	}
	defer connection.Close(context.Background())
	var samples int64
	err = connection.QueryRow(context.Background(), `SELECT count(*)
		FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND sample.ts >= $2 AND sample.ts < $3`,
		instanceID, from, to).Scan(&samples)
	if err != nil {
		t.Fatalf("count instance samples: %v", err)
	}
	return samples
}

func stopPlatformDatabase(t *testing.T) {
	t.Helper()
	runCollectionCompose(t, "stop", platformDatabaseService)
	t.Cleanup(func() { startPlatformDatabase(t) })
}

func startPlatformDatabase(t *testing.T) {
	t.Helper()
	runCollectionCompose(t, "start", platformDatabaseService)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := pgx.Connect(context.Background(), platformDatabaseURL(t))
		if err == nil {
			connection.Close(context.Background())
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("platform database did not accept connections again after restart")
}
