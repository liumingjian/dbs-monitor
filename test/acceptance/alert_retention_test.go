//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

const acceptanceAlertHistoryRetention = 2 * time.Minute

func TestAcceptance_AC_03_F6(t *testing.T) {
	runRecoveryEntry(t, "AC-03-F6", func(t *testing.T) {
		databaseURL := recoveryDatabase(t, platformDatabaseURL(t))
		stack := newRecoveryStackWithAlertRetention(t, databaseURL, acceptanceAlertHistoryRetention)
		client := stack.waitForApplication(t)
		instanceID, _ := createRecoveryAgent(t, client, "AC-03-F6")
		ruleID := createActiveSessionRetentionRule(t, client, instanceID)

		stopSession := startRetentionActiveSession(t)
		expiredAlert := waitForRetentionFiringAlert(t, client, instanceID, ruleID, uuid.Nil)
		stopSession()
		expiredRecoveredAt := waitForRetentionRecovery(t, client, expiredAlert)

		waitUntil := expiredRecoveredAt.Add(acceptanceAlertHistoryRetention)
		if delay := time.Until(waitUntil); delay > 0 {
			time.Sleep(delay)
		}

		stopSession = startRetentionActiveSession(t)
		freshAlert := waitForRetentionFiringAlert(t, client, instanceID, ruleID, expiredAlert)
		stopSession()
		waitForRetentionRecovery(t, client, freshAlert)

		stopSession = startRetentionActiveSession(t)
		defer stopSession()
		firingAlert := waitForRetentionFiringAlert(t, client, instanceID, ruleID, freshAlert)

		connection := connectRecoveryDatabase(t, databaseURL)
		defer connection.Close(context.Background())
		expiredBaseline := readAlertHistoryCounts(t, connection, expiredAlert)
		freshBaseline := readAlertHistoryCounts(t, connection, freshAlert)
		firingBaseline := readAlertHistoryCounts(t, connection, firingAlert)
		assertCompleteAlertHistory(t, "expired recovered", expiredBaseline)
		assertCompleteAlertHistory(t, "fresh recovered", freshBaseline)
		assertCompleteAlertHistory(t, "unrecovered", firingBaseline)

		if err := stack.process.Stop(); err != nil {
			t.Fatalf("stop server before retention pass: %v", err)
		}
		stack.start(t)
		stack.waitForApplication(t)

		waitForAlertHistoryDeletion(t, connection, expiredAlert)
		if counts := readAlertHistoryCounts(t, connection, freshAlert); counts != freshBaseline {
			t.Fatalf("fresh recovered alert history after cleanup = %+v, want %+v", counts, freshBaseline)
		}
		if counts := readAlertHistoryCounts(t, connection, firingAlert); counts.alerts != 1 || counts.events < firingBaseline.events ||
			counts.triggerSnapshots != 1 || counts.performanceEvents != 1 {
			t.Fatalf("unrecovered alert history after cleanup = %+v, want retained baseline %+v", counts, firingBaseline)
		}
	})
}

func createActiveSessionRetentionRule(t *testing.T, client *api.ClientWithResponses, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	recoveryThreshold := 1.0
	recoveryCount := 1
	response, err := client.CreateAlertRuleWithResponse(context.Background(), api.AlertRuleInput{
		Name:                      "AC-03-F6 active sessions",
		MetricId:                  "pg.connection.active",
		Aggregation:               api.Latest,
		Operator:                  api.GreaterThanEqual,
		Threshold:                 1,
		RecoveryOperator:          api.LessThan,
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             30,
		ConsecutiveCount:          1,
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  api.Warning,
		NoDataPolicy:              api.MarkNoData,
		Scope:                     api.INSTANCES,
		InstanceIds:               []uuid.UUID{instanceID},
		EvaluationIntervalSeconds: 5,
		Enabled:                   true,
	})
	if err != nil {
		t.Fatalf("create retention alert rule through API: %v", err)
	}
	if response.StatusCode() != http.StatusCreated || response.JSON201 == nil {
		t.Fatalf("create retention alert rule = status %d body %s", response.StatusCode(), response.Body)
	}
	return response.JSON201.Id
}

func startRetentionActiveSession(t *testing.T) func() {
	t.Helper()
	targetURL := fmt.Sprintf("postgres://monitored:monitored@127.0.0.1:%d/monitored?sslmode=disable",
		envInt(t, "ACCEPTANCE_TARGET_PORT", 55447))
	connection, err := pgx.Connect(context.Background(), targetURL)
	if err != nil {
		t.Fatalf("connect active-session retention fixture: %v", err)
	}
	queryContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = connection.Exec(queryContext, "SELECT pg_sleep(300)")
		close(done)
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("active-session retention fixture did not stop")
			}
			_ = connection.Close(context.Background())
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitForRetentionFiringAlert(t *testing.T, client *api.ClientWithResponses, instanceID, ruleID, previousAlertID uuid.UUID) uuid.UUID {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.ListCurrentAlertsWithResponse(context.Background(), &api.ListCurrentAlertsParams{InstanceId: &instanceID})
		if err == nil && response.JSON200 != nil {
			for _, alert := range response.JSON200.Items {
				if alert.RuleId == ruleID && alert.Id != previousAlertID && alert.Status == api.FIRING {
					return alert.Id
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("retention rule %s did not create a new FIRING alert", ruleID)
	return uuid.Nil
}

func waitForRetentionRecovery(t *testing.T, client *api.ClientWithResponses, alertID uuid.UUID) time.Time {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.GetAlertDetailWithResponse(context.Background(), alertID)
		if err == nil && response.JSON200 != nil && response.JSON200.Status == api.RECOVERED && response.JSON200.RecoveredAt != nil {
			return *response.JSON200.RecoveredAt
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("alert %s did not recover", alertID)
	return time.Time{}
}

type alertHistoryCounts struct {
	alerts            int
	events            int
	triggerSnapshots  int
	performanceEvents int
}

func readAlertHistoryCounts(t *testing.T, connection *pgx.Conn, alertID uuid.UUID) alertHistoryCounts {
	t.Helper()
	var counts alertHistoryCounts
	err := connection.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM alert_instance WHERE id = $1),
		(SELECT count(*) FROM alert_event WHERE alert_instance_id = $1),
		(SELECT count(*) FROM alert_trigger_snapshot WHERE alert_instance_id = $1),
		(SELECT count(*) FROM performance_event WHERE alert_instance_id = $1)`, alertID).Scan(
		&counts.alerts, &counts.events, &counts.triggerSnapshots, &counts.performanceEvents,
	)
	if err != nil {
		t.Fatalf("read alert history counts for %s: %v", alertID, err)
	}
	return counts
}

func waitForAlertHistoryDeletion(t *testing.T, connection *pgx.Conn, alertID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if readAlertHistoryCounts(t, connection, alertID) == (alertHistoryCounts{}) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	counts := readAlertHistoryCounts(t, connection, alertID)
	if counts != (alertHistoryCounts{}) {
		t.Fatalf("expired recovered alert history after cleanup = %+v, want all rows deleted", counts)
	}
}

func assertCompleteAlertHistory(t *testing.T, name string, counts alertHistoryCounts) {
	t.Helper()
	if counts.alerts != 1 || counts.events == 0 || counts.triggerSnapshots != 1 || counts.performanceEvents != 1 {
		t.Fatalf("%s alert history baseline = %+v, want one alert, snapshot, performance event, and at least one AlertEvent", name, counts)
	}
}
