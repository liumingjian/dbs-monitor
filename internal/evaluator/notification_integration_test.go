package evaluator

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestNotificationCommitOrderingRecoveryAndDurableRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationTestDatabase(t, ctx)
	defer platform.Close()

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	currentClock := &notificationClock{now: now}
	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused-test-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.New(platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "orders-primary",
		Host: "127.0.0.1", Port: 5432, Engine: string(instance.EnginePostgreSQL),
		DatabaseName: instance.BootstrapDatabaseColumn("postgres"), Username: "monitor",
		PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create notification test instance: %v", err)
	}
	if _, err := platform.Exec(ctx, `UPDATE instance
		SET agent_expected = true, agent_first_registered_at = $2, agent_token_revoked_at = $2
		WHERE id = $1`, instanceID, now); err != nil {
		t.Fatal(err)
	}
	ruleID := uuid.New()
	if _, err := platform.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy,
		 enabled, version, scope, evaluation_interval_seconds)
		VALUES ($1, 'Agent tracer', 'agent.status', 'latest', '=', 0, '=', 1,
		 30, 1, 1, 'critical', 'mark_no_data', true, 1, 'ALL', 5)`, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot)
		VALUES ($1, 1, '{"name":"Agent tracer","metric_id":"agent.status"}')`, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO smtp_channel
		(enabled, host, port, from_address, recipient, auth_type, tls_mode, updated_at)
		VALUES (true, '127.0.0.1', 465, 'monitor@example.com', 'dba@example.com', 'NONE', 'IMPLICIT', $1)`, now); err != nil {
		t.Fatal(err)
	}

	address, messages, closeSMTP := startNotificationSMTPReceiver(t, 2)
	defer closeSMTP()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("parse SMTP receiver address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse SMTP receiver port: %v", err)
	}
	channel := notify.NewSMTPChannel(notify.SMTPConfig{
		Host: host, Port: port, From: "monitor@example.com", TLSMode: notify.TLSImplicit, AuthType: notify.AuthNone,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true},
	})
	dispatcher := notify.NewDispatcher(platform)

	blocker, err := platform.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_lock(79)"); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `CREATE FUNCTION block_notification_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(79); RETURN NEW; END $$;
		CREATE TRIGGER notification_commit_gate AFTER INSERT ON notification_delivery
		FOR EACH ROW EXECUTE FUNCTION block_notification_commit()`); err != nil {
		t.Fatal(err)
	}
	evaluation := New(platform, currentClock, nil)
	evaluationDone := make(chan error, 1)
	go func() { evaluationDone <- evaluation.RunOnce(ctx) }()
	waitForNotificationLock(t, ctx, platform, evaluationDone)
	channels := map[string]notify.Channel{notify.SMTPChannelKey: channel}
	if processed, err := dispatcher.DispatchOne(ctx, now, channels); err != nil || processed {
		t.Fatalf("delivery before evaluator commit = %t, %v; want false, nil", processed, err)
	}
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_unlock(79)"); err != nil {
		t.Fatal(err)
	}
	if err := <-evaluationDone; err != nil {
		t.Fatalf("evaluate firing transition: %v", err)
	}
	if _, err := platform.Exec(ctx, "DROP TRIGGER notification_commit_gate ON notification_delivery; DROP FUNCTION block_notification_commit()"); err != nil {
		t.Fatal(err)
	}
	if processed, err := dispatcher.DispatchOne(ctx, now, channels); err != nil || !processed {
		t.Fatalf("delivery after evaluator commit = %t, %v; want true, nil", processed, err)
	}
	if message := <-messages; !strings.Contains(message, "Agent tracer") || !strings.Contains(message, "dba@example.com") {
		t.Fatalf("firing message missing alert facts:\n%s", message)
	}

	currentClock.now = now.Add(6 * time.Second)
	if _, err := platform.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_report_at)
		VALUES ($1, 'AGENT', $2)`, instanceID, currentClock.now); err != nil {
		t.Fatal(err)
	}
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate recovery transition: %v", err)
	}
	if processed, err := dispatcher.DispatchOne(ctx, currentClock.now, channels); err != nil || !processed {
		t.Fatalf("recovery delivery = %t, %v; want true, nil", processed, err)
	}
	if message := <-messages; !strings.Contains(message, "告警恢复") {
		t.Fatalf("recovery message missing recovery template:\n%s", message)
	}
	var sentDeliveries, sentAttempts, sentEvents int
	if err := platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM notification_delivery WHERE alert_instance_id IS NOT NULL AND status = 'SENT'),
		(SELECT count(*) FROM notification_attempt attempt JOIN notification_delivery delivery ON delivery.id = attempt.notification_id WHERE delivery.alert_instance_id IS NOT NULL AND attempt.result = 'SENT'),
		(SELECT count(*) FROM alert_event WHERE kind = 'NOTIFICATION_SENT')`).Scan(&sentDeliveries, &sentAttempts, &sentEvents); err != nil {
		t.Fatal(err)
	}
	if sentDeliveries != 2 || sentAttempts != 2 || sentEvents != 2 {
		t.Fatalf("sent audit facts = %d deliveries, %d attempts, %d events; want 2 each", sentDeliveries, sentAttempts, sentEvents)
	}

	queued, err := notify.New(platform).EnqueueTestNotification(ctx, notify.EnqueueTestNotificationParams{
		Target: "failure@example.com", NextAttemptAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	failing := notify.NewSMTPChannel(notify.SMTPConfig{
		Host: "127.0.0.1", Port: 1, From: "monitor@example.com", TLSMode: notify.TLSImplicit, AuthType: notify.AuthNone,
	})
	failingChannels := map[string]notify.Channel{notify.SMTPChannelKey: failing}
	for attempt, attemptedAt := range []time.Time{now, now.Add(time.Second), now.Add(3 * time.Second)} {
		if processed, err := notify.NewDispatcher(platform).DispatchOne(ctx, attemptedAt, failingChannels); err != nil || !processed {
			t.Fatalf("failed attempt %d = %t, %v", attempt+1, processed, err)
		}
		if attempt < 2 {
			early := attemptedAt.Add(500 * time.Millisecond)
			if processed, err := notify.NewDispatcher(platform).DispatchOne(ctx, early, failingChannels); err != nil || processed {
				t.Fatalf("early retry after attempt %d = %t, %v; want false, nil", attempt+1, processed, err)
			}
		}
	}
	var status string
	var attemptCount, historyCount int
	var attemptedTimes []time.Time
	if err := platform.QueryRow(ctx, `SELECT status, attempt_count,
		(SELECT count(*) FROM notification_attempt WHERE notification_id = delivery.id),
		(SELECT array_agg(attempted_at ORDER BY retry_count) FROM notification_attempt WHERE notification_id = delivery.id)
		FROM notification_delivery delivery WHERE id = $1`, uuid.UUID(queued.Bytes)).Scan(&status, &attemptCount, &historyCount, &attemptedTimes); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" || attemptCount != 3 || historyCount != 3 {
		t.Fatalf("terminal delivery = %s, %d attempts, %d history rows; want FAILED, 3, 3", status, attemptCount, historyCount)
	}
	wantTimes := []time.Time{now, now.Add(time.Second), now.Add(3 * time.Second)}
	for index := range wantTimes {
		if !attemptedTimes[index].Equal(wantTimes[index]) {
			t.Errorf("attempt %d at %s, want %s", index+1, attemptedTimes[index], wantTimes[index])
		}
	}
}

func TestNotificationRepeatAcknowledgementAndNewLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationTestDatabase(t, ctx)
	defer platform.Close()

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused-test-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.New(platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "repeat-primary",
		Host: "127.0.0.1", Port: 5432, Engine: string(instance.EnginePostgreSQL),
		DatabaseName: instance.BootstrapDatabaseColumn("postgres"), Username: "monitor",
		PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create repeat test instance: %v", err)
	}
	ruleID := uuid.New()
	actorID := uuid.New()
	if _, err := platform.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role, enabled, created_at)
		VALUES ($1, 'repeat-admin', decode('00', 'hex'), 'ALERT_ADMIN', true, $2)`, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy,
		 enabled, version, scope, evaluation_interval_seconds)
		VALUES ($1, 'Repeat tracer', 'pg.connection.total', 'latest', '>=', 20, '<', 15,
		 30, 1, 1, 'critical', 'mark_no_data', true, 1, 'ALL', 30)`, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `UPDATE notification_policy SET repeat_interval = 900 WHERE is_default`); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO smtp_channel
		(enabled, host, port, from_address, recipient, auth_type, tls_mode, updated_at)
		VALUES (true, '127.0.0.1', 465, 'monitor@example.com', 'dba@example.com', 'NONE', 'IMPLICIT', $1)`, now); err != nil {
		t.Fatal(err)
	}

	createAlert := func(at time.Time) uuid.UUID {
		t.Helper()
		var alertID uuid.UUID
		if err := platform.QueryRow(ctx, `INSERT INTO alert_instance
			(rule_id, instance_id, metric_id, metric_dimension_key, status, rule_version,
			 severity, current_value, rule_snapshot, first_triggered_at, first_rule_version, first_rule_snapshot, updated_at)
			VALUES ($1, $2, 'pg.connection.total', '{}', 'FIRING', 1,
			 'critical', 25, '{"name":"Repeat tracer"}', $3, 1, '{"name":"Repeat tracer"}', $3)
			RETURNING id`, ruleID, instanceID, at).Scan(&alertID); err != nil {
			t.Fatalf("create alert lifecycle: %v", err)
		}
		return alertID
	}
	markSent := func(alertID uuid.UUID, eventType string, at time.Time) {
		t.Helper()
		if _, err := platform.Exec(ctx, `UPDATE notification_delivery
			SET status = 'SENT', attempt_count = 1, completed_at = $3
			WHERE alert_instance_id = $1 AND event_type = $2 AND status = 'PENDING'`, alertID, eventType, at); err != nil {
			t.Fatalf("mark %s sent: %v", eventType, err)
		}
	}
	queue := func(alertID uuid.UUID, eventType notify.EventType, at time.Time) int {
		t.Helper()
		ids, err := notify.New(platform).EnqueueAlertNotifications(ctx, notify.EnqueueAlertNotificationsParams{
			AlertInstanceID: pgtype.UUID{Bytes: alertID, Valid: true}, EventType: string(eventType),
			TemplateID: pgtype.Text{String: "builtin.test.v1", Valid: true}, Payload: []byte(`{"test":true}`),
			NextAttemptAt: pgtype.Timestamptz{Time: at, Valid: true},
		})
		if err != nil {
			t.Fatalf("queue %s: %v", eventType, err)
		}
		return len(ids)
	}

	firstAlertID := createAlert(now)
	if count := queue(firstAlertID, notify.EventFiring, now); count != 1 {
		t.Fatalf("firing deliveries = %d, want 1", count)
	}
	markSent(firstAlertID, string(notify.EventFiring), now)
	dispatcher := notify.NewDispatcher(platform)
	if count, err := dispatcher.EnqueueDueRepeats(ctx, now.Add(899*time.Second)); err != nil || count != 0 {
		t.Fatalf("repeat before interval = %d, %v; want 0, nil", count, err)
	}
	if count, err := dispatcher.EnqueueDueRepeats(ctx, now.Add(900*time.Second)); err != nil || count != 1 {
		t.Fatalf("repeat at interval = %d, %v; want 1, nil", count, err)
	}
	markSent(firstAlertID, string(notify.EventRepeat), now.Add(900*time.Second))
	if _, err := platform.Exec(ctx, `UPDATE alert_instance SET disposition = 'ACKED',
		disposition_by = $2, disposition_at = $3
		WHERE id = $1`, firstAlertID, actorID, now.Add(901*time.Second)); err != nil {
		t.Fatal(err)
	}
	if count, err := dispatcher.EnqueueDueRepeats(ctx, now.Add(1800*time.Second)); err != nil || count != 0 {
		t.Fatalf("acknowledged repeat = %d, %v; want 0, nil", count, err)
	}
	if count := queue(firstAlertID, notify.EventRecovery, now.Add(1800*time.Second)); count != 1 {
		t.Fatalf("recovery deliveries after acknowledgement = %d, want 1", count)
	}
	if _, err := platform.Exec(ctx, `UPDATE alert_instance SET status = 'RECOVERED', recovered_at = $2 WHERE id = $1`, firstAlertID, now.Add(1800*time.Second)); err != nil {
		t.Fatal(err)
	}

	secondFiringAt := now.Add(2 * time.Hour)
	secondAlertID := createAlert(secondFiringAt)
	if count := queue(secondAlertID, notify.EventFiring, secondFiringAt); count != 1 {
		t.Fatalf("new lifecycle firing deliveries = %d, want 1", count)
	}
	markSent(secondAlertID, string(notify.EventFiring), secondFiringAt)
	if count, err := dispatcher.EnqueueDueRepeats(ctx, secondFiringAt.Add(900*time.Second)); err != nil || count != 1 {
		t.Fatalf("new lifecycle repeat = %d, %v; want 1, nil", count, err)
	}
	var disposition string
	if err := platform.QueryRow(ctx, `SELECT disposition FROM alert_instance WHERE id = $1`, secondAlertID).Scan(&disposition); err != nil || disposition != "NONE" {
		t.Fatalf("new lifecycle disposition = %q, %v; want NONE", disposition, err)
	}
}

func TestMaintenanceWindowSuppressesWithoutStoppingEvaluationOrReplaying(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationTestDatabase(t, ctx)
	defer platform.Close()

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	currentClock := &notificationClock{now: now}
	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused-test-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.New(platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "maintenance-primary",
		Host: "127.0.0.1", Port: 5432, Engine: string(instance.EnginePostgreSQL),
		DatabaseName: instance.BootstrapDatabaseColumn("postgres"), Username: "monitor",
		PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatal(err)
	}
	actorID := uuid.New()
	windowID := uuid.New()
	ruleID := uuid.New()
	if _, err := platform.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role, enabled, created_at)
		VALUES ($1, 'maintenance-admin', decode('00', 'hex'), 'ALERT_ADMIN', true, $2)`, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `UPDATE instance SET agent_expected = true, agent_first_registered_at = $1, agent_token_revoked_at = $1 WHERE id = $2`, now, instanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy,
		 enabled, version, scope, evaluation_interval_seconds)
		VALUES ($1, 'Maintenance tracer', 'agent.status', 'latest', '=', 0, '=', 1,
			 30, 1, 1, 'critical', 'mark_no_data', true, 1, 'ALL', 5)`, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot)
		VALUES ($1, 1, '{"name":"Maintenance tracer","metric_id":"agent.status"}')`, ruleID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO maintenance_window (id, starts_at, ends_at, reason, created_by, created_at, updated_at, updated_by)
		VALUES ($1, $2::timestamptz - interval '1 minute', $2::timestamptz + interval '10 minutes', 'planned restart', $3, $2, $2, $3)`, windowID, now, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO maintenance_window_instance (maintenance_window_id, instance_id) VALUES ($1, $2)`, windowID, instanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `UPDATE notification_policy SET repeat_interval = 900 WHERE is_default`); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.Exec(ctx, `INSERT INTO smtp_channel
		(enabled, host, port, from_address, recipient, auth_type, tls_mode, updated_at)
		VALUES (true, '127.0.0.1', 465, 'monitor@example.com', 'dba@example.com', 'NONE', 'IMPLICIT', $1)`, now); err != nil {
		t.Fatal(err)
	}

	evaluation := New(platform, currentClock, nil)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate during maintenance: %v", err)
	}
	var alertID uuid.UUID
	var status string
	var inMaintenance bool
	var referencedWindow uuid.UUID
	var deliveryCount, firingEvents, suppressionEvents int
	if err := platform.QueryRow(ctx, `SELECT alert.id, alert.status, alert.in_maintenance, alert.maintenance_window_id,
		(SELECT count(*) FROM notification_delivery WHERE alert_instance_id = alert.id),
		(SELECT count(*) FROM alert_event WHERE alert_instance_id = alert.id AND kind = 'FIRED'
		 AND in_maintenance AND maintenance_window_id = $3),
		(SELECT count(*) FROM alert_event WHERE alert_instance_id = alert.id AND kind = 'MAINTENANCE_SUPPRESSED'
		 AND in_maintenance AND maintenance_window_id = $3)
		FROM alert_instance alert WHERE alert.rule_id = $1 AND alert.instance_id = $2`, ruleID, instanceID, windowID).
		Scan(&alertID, &status, &inMaintenance, &referencedWindow, &deliveryCount, &firingEvents, &suppressionEvents); err != nil {
		t.Fatal(err)
	}
	if status != "FIRING" || !inMaintenance || referencedWindow != windowID || deliveryCount != 0 || firingEvents != 1 || suppressionEvents != 1 {
		t.Fatalf("maintenance evaluation = status %s, marked %t/%s, deliveries %d, fired events %d, suppressions %d",
			status, inMaintenance, referencedWindow, deliveryCount, firingEvents, suppressionEvents)
	}

	dispatcher := notify.NewDispatcher(platform)
	if _, err := platform.Exec(ctx, `INSERT INTO notification_delivery
		(alert_instance_id, event_type, channel, target, template_id, payload, next_attempt_at, created_at)
		VALUES ($1, 'FIRING', 'SMTP', 'dba@example.com', 'builtin.notification.firing.v1', '{}'::jsonb, $2, $2)`, alertID, now); err != nil {
		t.Fatal(err)
	}
	if processed, err := dispatcher.DispatchOne(ctx, now, map[string]notify.Channel{}); err != nil || !processed {
		t.Fatalf("delivery queued before maintenance = %t, %v; want suppressed processing", processed, err)
	}
	if err := platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM notification_delivery WHERE alert_instance_id = $1),
		(SELECT count(*) FROM alert_event WHERE alert_instance_id = $1 AND kind = 'MAINTENANCE_SUPPRESSED'
		 AND maintenance_window_id = $2)`, alertID, windowID).Scan(&deliveryCount, &suppressionEvents); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 0 || suppressionEvents != 2 {
		t.Fatalf("dispatch-time maintenance suppression = %d deliveries, %d audit events; want 0, 2", deliveryCount, suppressionEvents)
	}
	if count, err := dispatcher.EnqueueDueRepeats(ctx, now.Add(10*time.Minute+time.Second)); err != nil || count != 0 {
		t.Fatalf("repeat immediately after maintenance = %d, %v; want 0, nil", count, err)
	}
	currentClock.now = now.Add(15 * time.Minute)
	if err := evaluation.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate after maintenance: %v", err)
	}
	if count, err := dispatcher.EnqueueDueRepeats(ctx, currentClock.now); err != nil || count != 1 {
		t.Fatalf("repeat at next interval = %d, %v; want 1, nil", count, err)
	}

	address, messages, closeSMTP := startNotificationSMTPReceiver(t, 1)
	defer closeSMTP()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	channel := notify.NewSMTPChannel(notify.SMTPConfig{
		Host: host, Port: port, From: "monitor@example.com", TLSMode: notify.TLSImplicit, AuthType: notify.AuthNone,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true},
	})
	if processed, err := dispatcher.DispatchOne(ctx, currentClock.now, map[string]notify.Channel{notify.SMTPChannelKey: channel}); err != nil || !processed {
		t.Fatalf("post-maintenance repeat delivery = %t, %v", processed, err)
	}
	if message := <-messages; !strings.Contains(message, "Maintenance tracer") {
		t.Fatalf("post-maintenance repeat missing alert facts:\n%s", message)
	}
	if err := platform.QueryRow(ctx, `SELECT in_maintenance FROM alert_instance WHERE id = $1`, alertID).Scan(&inMaintenance); err != nil || inMaintenance {
		t.Fatalf("alert maintenance marker after evaluation = %t, %v; want false", inMaintenance, err)
	}
}

type notificationClock struct{ now time.Time }

func (clock *notificationClock) Now() time.Time { return clock.now }
func (*notificationClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

func waitForNotificationLock(t *testing.T, ctx context.Context, platform *db.Pool, evaluationDone <-chan error) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		var waiting bool
		if err := platform.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND objid = 79 AND NOT granted
		)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case err := <-evaluationDone:
			t.Fatalf("evaluation exited before notification commit gate: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("evaluation did not reach the pre-commit notification gate")
}

func notificationTestDatabase(t *testing.T, ctx context.Context) (*db.Pool, *instance.CredentialKeyring) {
	t.Helper()
	baseURL := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", notificationEnv("PGUSER", "dbs_monitor"), notificationEnv("PGHOST", "127.0.0.1"), notificationEnv("PGPORT", "55432"), notificationEnv("PGDATABASE", "dbs_monitor"))
	admin, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	databaseName := fmt.Sprintf("dbs_monitor_notification_%d", os.Getpid())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })
	connectionURL := strings.Replace(baseURL, "/"+notificationEnv("PGDATABASE", "dbs_monitor")+"?", "/"+databaseName+"?", 1)
	migrationDB, err := migrations.Open(connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	directory := createTestCredentialDirectory(t)
	if _, err := migrations.Up(ctx, migrationDB, directory); err != nil {
		t.Fatalf("migrate notification test database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionURL)
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := instance.OpenCredentialKeyring(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	return &db.Pool{Pool: pool}, keyring
}

func notificationEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func startNotificationSMTPReceiver(t *testing.T, messageCount int) (string, <-chan string, func()) {
	t.Helper()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", notificationTLSConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, messageCount)
	go func() {
		for count := 0; count < messageCount; count++ {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			handleNotificationSMTP(connection, messages)
		}
	}()
	return listener.Addr().String(), messages, func() { listener.Close() }
}

func handleNotificationSMTP(connection net.Conn, messages chan<- string) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	reply := func(value string) {
		_, _ = writer.WriteString(value + "\r\n")
		_ = writer.Flush()
	}
	reply("220 localhost ESMTP")
	for _, response := range []string{"250 localhost", "250 OK", "250 OK", "354 send data"} {
		if _, err := reader.ReadString('\n'); err != nil {
			return
		}
		reply(response)
	}
	var message strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if line == ".\r\n" {
			break
		}
		message.WriteString(line)
	}
	messages <- message.String()
	reply("250 queued")
	if _, err := reader.ReadString('\n'); err == nil {
		reply("221 bye")
	}
}

func notificationTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test SMTP key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
}

func createTestCredentialDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create test credential directory: %v", err)
	}
	return directory
}
