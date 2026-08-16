package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestWebhookDeliveryFailuresAndSecretBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationHTTPTestDatabase(t, ctx)
	defer platform.Close()

	const (
		signingValue    = "issue-80-signing-value"
		signatureHeader = "X-DBS-Signature"
	)
	var requestsMu sync.Mutex
	var requests [][]byte
	var signatures []string
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Webhook request: %v", err)
		}
		requestsMu.Lock()
		requests = append(requests, body)
		signatures = append(signatures, request.Header.Get(signatureHeader))
		requestCount := len(requests)
		requestsMu.Unlock()
		if requestCount < 3 {
			http.Error(writer, "retry", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "notification-channels.snapshot")
	snapshotStore := notify.NewChannelSnapshotStore(snapshotPath)
	handler := httpapi.NewHandler(platform, clock.Real{}, keyring)
	handler.SetNotificationSnapshotStore(snapshotStore)
	server := httptest.NewTLSServer(handler.Routes())
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

	const smtpPassword = "issue-83-smtp-password"
	smtpResponse := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/notification-channels/smtp", map[string]any{
		"enabled": true, "host": "smtp.example.com", "port": 465,
		"from_address": "monitor@example.com", "recipient": "dba@example.com",
		"auth_type": "PLAIN", "username": "monitor", "password": smtpPassword, "tls_mode": "IMPLICIT",
	}, "")
	smtpBody, err := io.ReadAll(smtpResponse.Body)
	smtpResponse.Body.Close()
	if err != nil || smtpResponse.StatusCode != http.StatusOK {
		t.Fatalf("update SMTP channel = status %d, read error %v, body %s", smtpResponse.StatusCode, err, smtpBody)
	}
	var smtpActor string
	var smtpUpdatedAt time.Time
	if err := platform.QueryRow(ctx, `SELECT actor.username, smtp.updated_at
		FROM smtp_channel smtp JOIN app_user actor ON actor.id = smtp.updated_by
		WHERE smtp.singleton`).Scan(&smtpActor, &smtpUpdatedAt); err != nil {
		t.Fatalf("read SMTP channel update attribution: %v", err)
	}
	if smtpActor != "admin" || smtpUpdatedAt.IsZero() {
		t.Fatalf("SMTP channel attribution = actor %q at %s, want admin and a timestamp", smtpActor, smtpUpdatedAt)
	}
	var smtpCiphertext []byte
	var smtpKeyVersion int32
	if err := platform.QueryRow(ctx, `SELECT auth_ciphertext, auth_key_version FROM smtp_channel WHERE singleton`).
		Scan(&smtpCiphertext, &smtpKeyVersion); err != nil {
		t.Fatalf("read stored SMTP authentication configuration: %v", err)
	}
	snapshot, err := snapshotStore.Load()
	if err != nil || snapshot.SMTP == nil || !bytes.Equal(snapshot.SMTP.AuthCiphertext, smtpCiphertext) ||
		snapshot.SMTP.AuthKeyVersion == nil || *snapshot.SMTP.AuthKeyVersion != smtpKeyVersion {
		t.Fatalf("SMTP notification snapshot = %+v, %v", snapshot.SMTP, err)
	}
	snapshotContents, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read SMTP notification snapshot: %v", err)
	}
	if bytes.Contains(snapshotContents, []byte(smtpPassword)) {
		t.Fatal("SMTP notification snapshot contains plaintext authentication value")
	}

	createdResponse := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/notification-channels/webhooks", map[string]any{
		"name": "On-call gateway", "enabled": true, "url": receiver.URL,
		"signing_value": signingValue, "signature_header": signatureHeader,
	}, "")
	createdBody, err := io.ReadAll(createdResponse.Body)
	createdResponse.Body.Close()
	if err != nil || createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create Webhook target = status %d, read error %v, body %s", createdResponse.StatusCode, err, createdBody)
	}
	assertNoWebhookSigningMaterial(t, createdBody, signingValue, signatureHeader)
	var target api.WebhookTarget
	if err := json.Unmarshal(createdBody, &target); err != nil {
		t.Fatalf("decode created Webhook target: %v", err)
	}
	if !target.SigningConfigured || target.Url != receiver.URL {
		t.Fatalf("created Webhook target = %+v", target)
	}
	var webhookActor string
	var webhookUpdatedAt time.Time
	if err := platform.QueryRow(ctx, `SELECT actor.username, target.updated_at
		FROM webhook_target target JOIN app_user actor ON actor.id = target.updated_by
		WHERE target.id = $1`, target.Id).Scan(&webhookActor, &webhookUpdatedAt); err != nil {
		t.Fatalf("read Webhook target update attribution: %v", err)
	}
	if webhookActor != "admin" || webhookUpdatedAt.IsZero() {
		t.Fatalf("Webhook target attribution = actor %q at %s, want admin and a timestamp", webhookActor, webhookUpdatedAt)
	}

	var valueCiphertext, headerCiphertext []byte
	var keyVersion int32
	if err := platform.QueryRow(ctx, `SELECT signing_value_ciphertext, signature_header_ciphertext, signing_key_version
		FROM webhook_target WHERE id = $1`, target.Id).Scan(&valueCiphertext, &headerCiphertext, &keyVersion); err != nil {
		t.Fatalf("read stored Webhook signing configuration: %v", err)
	}
	if strings.Contains(string(valueCiphertext), signingValue) || strings.Contains(string(headerCiphertext), signatureHeader) {
		t.Fatal("Webhook signing configuration was stored in plaintext")
	}
	if got, err := keyring.DecryptWebhookSigningValue(target.Id, valueCiphertext, keyVersion); err != nil || got != signingValue {
		t.Fatalf("decrypt stored Webhook signing value = %q, %v", got, err)
	}
	if got, err := keyring.DecryptWebhookSignatureHeader(target.Id, headerCiphertext, keyVersion); err != nil || got != signatureHeader {
		t.Fatalf("decrypt stored Webhook signature header = %q, %v", got, err)
	}
	snapshot, err = snapshotStore.Load()
	if err != nil || len(snapshot.Webhooks) != 1 ||
		!bytes.Equal(snapshot.Webhooks[0].SigningValueCiphertext, valueCiphertext) ||
		!bytes.Equal(snapshot.Webhooks[0].SignatureHeaderCiphertext, headerCiphertext) ||
		snapshot.Webhooks[0].SigningKeyVersion != keyVersion {
		t.Fatalf("Webhook notification snapshot = %+v, %v", snapshot.Webhooks, err)
	}
	snapshotContents, err = os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read Webhook notification snapshot: %v", err)
	}
	for _, plaintext := range []string{smtpPassword, signingValue, signatureHeader} {
		if bytes.Contains(snapshotContents, []byte(plaintext)) {
			t.Fatalf("notification snapshot contains plaintext secret %q", plaintext)
		}
	}

	listed := getResponse(t, client, server.URL+"/api/v1/notification-channels/webhooks")
	listedBody, err := io.ReadAll(listed.Body)
	listed.Body.Close()
	if err != nil || listed.StatusCode != http.StatusOK {
		t.Fatalf("list Webhook targets = status %d, read error %v", listed.StatusCode, err)
	}
	assertNoWebhookSigningMaterial(t, listedBody, signingValue, signatureHeader)
	updatedResponse := requestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/notification-channels/webhooks/"+target.Id.String(), map[string]any{
			"name": "Primary on-call gateway", "enabled": true, "url": receiver.URL,
		}, "")
	updatedBody, err := io.ReadAll(updatedResponse.Body)
	updatedResponse.Body.Close()
	if err != nil || updatedResponse.StatusCode != http.StatusOK {
		t.Fatalf("update Webhook target = status %d, read error %v, body %s", updatedResponse.StatusCode, err, updatedBody)
	}
	assertNoWebhookSigningMaterial(t, updatedBody, signingValue, signatureHeader)
	var updated api.WebhookTarget
	if err := json.Unmarshal(updatedBody, &updated); err != nil || updated.Name != "Primary on-call gateway" || !updated.SigningConfigured {
		t.Fatalf("updated Webhook target = %+v, %v", updated, err)
	}
	var retainedValue, retainedHeader []byte
	if err := platform.QueryRow(ctx, `SELECT signing_value_ciphertext, signature_header_ciphertext
		FROM webhook_target WHERE id = $1`, target.Id).Scan(&retainedValue, &retainedHeader); err != nil {
		t.Fatalf("read retained Webhook signing configuration: %v", err)
	}
	if !bytes.Equal(retainedValue, valueCiphertext) || !bytes.Equal(retainedHeader, headerCiphertext) {
		t.Fatal("metadata-only Webhook update replaced signing configuration")
	}
	snapshot, err = snapshotStore.Load()
	if err != nil || len(snapshot.Webhooks) != 1 || snapshot.Webhooks[0].Name != "Primary on-call gateway" {
		t.Fatalf("updated Webhook notification snapshot = %+v, %v", snapshot.Webhooks, err)
	}

	testResponse := requestJSON(t, client, http.MethodPost,
		server.URL+"/api/v1/notification-channels/webhooks/"+target.Id.String()+"/test", nil, "")
	var queued api.NotificationQueued
	if err := json.NewDecoder(testResponse.Body).Decode(&queued); err != nil {
		t.Fatalf("decode queued Webhook test: %v", err)
	}
	testResponse.Body.Close()
	if testResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("queue Webhook test status = %d, want 202", testResponse.StatusCode)
	}

	channel := notify.NewWebhookChannel(notify.WebhookConfig{
		URL: receiver.URL, SigningValue: signingValue, SignatureHeader: signatureHeader, Timeout: time.Second,
	})
	channels := map[string]notify.Channel{
		notify.WebhookChannelKey(pgtype.UUID{Bytes: target.Id, Valid: true}): channel,
	}
	dispatcher := notify.NewDispatcher(platform)
	startedAt := time.Now().UTC().Add(time.Second)
	for attempt, attemptedAt := range []time.Time{startedAt, startedAt.Add(time.Second), startedAt.Add(3 * time.Second)} {
		if processed, err := dispatcher.DispatchOne(ctx, attemptedAt, channels); err != nil || !processed {
			t.Fatalf("Webhook attempt %d = processed %t, error %v", attempt+1, processed, err)
		}
		if attempt < 2 {
			if processed, err := dispatcher.DispatchOne(ctx, attemptedAt.Add(500*time.Millisecond), channels); err != nil || processed {
				t.Fatalf("early Webhook retry %d = processed %t, error %v", attempt+1, processed, err)
			}
		}
	}
	requestsMu.Lock()
	if len(requests) != 3 || len(signatures) != 3 {
		t.Fatalf("Webhook requests = %d bodies and %d signatures, want 3", len(requests), len(signatures))
	}
	for index, body := range requests {
		var payload notify.WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil || payload.EventType != notify.EventTest {
			t.Fatalf("Webhook payload %d = %+v, %v", index+1, payload, err)
		}
		mac := hmac.New(sha256.New, []byte(signingValue))
		_, _ = mac.Write(body)
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if signatures[index] != wantSignature {
			t.Fatalf("Webhook signature %d = %q, want %q", index+1, signatures[index], wantSignature)
		}
	}
	requestsMu.Unlock()
	var status string
	var attemptCount int
	if err := platform.QueryRow(ctx, `SELECT status, attempt_count FROM notification_delivery WHERE id = $1`, queued.Id).
		Scan(&status, &attemptCount); err != nil || status != "SENT" || attemptCount != 3 {
		t.Fatalf("Webhook test delivery = %s with %d attempts, query error %v", status, attemptCount, err)
	}

	failureStart := startedAt.Add(10 * time.Minute)
	for index := 0; index < 21; index++ {
		failedAt := failureStart.Add(time.Duration(index) * time.Second)
		if _, err := platform.Exec(ctx, `WITH delivery AS (
			INSERT INTO notification_delivery (
				event_type, channel, channel_target_id, target, template_id, payload,
				status, attempt_count, next_attempt_at, created_at, completed_at
			) VALUES ('TEST', 'WEBHOOK', $1, $2, 'builtin.webhook.test.v1', '{}'::jsonb,
				'FAILED', 3, $3, $3, $3)
			RETURNING id
		)
		INSERT INTO notification_attempt (notification_id, attempted_at, result, failure_reason, retry_count)
		SELECT id, $3, 'FAILED', $4, 2 FROM delivery`, target.Id, receiver.URL, failedAt, fmt.Sprintf("failure-%02d", index)); err != nil {
			t.Fatalf("insert terminal Webhook failure %d: %v", index, err)
		}
	}
	assertFailureOverview(t, client, server.URL, 21, 20, "failure-20")

	clearedAt := failureStart.Add(30 * time.Second)
	if _, err := platform.Exec(ctx, `WITH delivery AS (
		INSERT INTO notification_delivery (
			event_type, channel, channel_target_id, target, template_id, payload,
			status, attempt_count, next_attempt_at, created_at, completed_at
		) VALUES ('TEST', 'WEBHOOK', $1, $2, 'builtin.webhook.test.v1', '{}'::jsonb,
			'SENT', 1, $3, $3, $3)
		RETURNING id
	)
	INSERT INTO notification_attempt (notification_id, attempted_at, result, retry_count)
	SELECT id, $3, 'SENT', 0 FROM delivery`, target.Id, receiver.URL, clearedAt); err != nil {
		t.Fatalf("insert clearing Webhook success: %v", err)
	}
	assertFailureOverview(t, client, server.URL, 0, 0, "")
	var retainedFailures int
	if err := platform.QueryRow(ctx, `SELECT count(*) FROM notification_delivery
		WHERE channel_target_id = $1 AND status = 'FAILED'`, target.Id).Scan(&retainedFailures); err != nil || retainedFailures != 21 {
		t.Fatalf("retained Webhook audit failures = %d, %v; want 21", retainedFailures, err)
	}
	deleted := requestJSON(t, client, http.MethodDelete,
		server.URL+"/api/v1/notification-channels/webhooks/"+target.Id.String(), nil, "")
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete Webhook target status = %d, want 204", deleted.StatusCode)
	}
	listed = getResponse(t, client, server.URL+"/api/v1/notification-channels/webhooks")
	defer listed.Body.Close()
	var remaining []api.WebhookTarget
	if err := json.NewDecoder(listed.Body).Decode(&remaining); err != nil || len(remaining) != 0 {
		t.Fatalf("Webhook targets after delete = %+v, %v", remaining, err)
	}
	snapshot, err = snapshotStore.Load()
	if err != nil || len(snapshot.Webhooks) != 0 || snapshot.SMTP == nil {
		t.Fatalf("notification snapshot after Webhook delete = %+v, %v", snapshot, err)
	}
}

func assertNoWebhookSigningMaterial(t *testing.T, body []byte, signingValue, signatureHeader string) {
	t.Helper()
	text := string(body)
	for _, forbidden := range []string{signingValue, signatureHeader, "signing_value", "signature_header"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Webhook response exposed %q: %s", forbidden, body)
		}
	}
}

func assertFailureOverview(t *testing.T, client *http.Client, serverURL string, wantCount, wantRecords int, wantReason string) {
	t.Helper()
	response := getResponse(t, client, serverURL+"/api/v1/notification-channels/failures")
	defer response.Body.Close()
	var overview api.ChannelFailureOverview
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("read channel failure overview = status %d, error %v", response.StatusCode, err)
	}
	if wantCount == 0 {
		if overview.HasFailures || len(overview.Channels) != 0 {
			t.Fatalf("cleared channel failure overview = %+v", overview)
		}
		return
	}
	if !overview.HasFailures || len(overview.Channels) != 1 {
		t.Fatalf("active channel failure overview = %+v", overview)
	}
	summary := overview.Channels[0]
	if summary.Channel != api.FailureWebhook || summary.RecentFailureCount != wantCount ||
		len(summary.RecentFailures) != wantRecords || summary.LastFailureReason != wantReason {
		t.Fatalf("channel failure summary = %+v", summary)
	}
}

func notificationHTTPTestDatabase(t *testing.T, ctx context.Context) (*db.Pool, *instance.CredentialKeyring) {
	t.Helper()
	databaseName := fmt.Sprintf("dbs_monitor_webhook_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		admin.Close()
		t.Fatalf("create Webhook test database: %v", err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
		admin.Close()
	})
	directory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, directory); err != nil {
		migrationDB.Close()
		t.Fatalf("migrate Webhook test database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open Webhook test database: %v", err)
	}
	keyring, err := instance.OpenCredentialKeyring(directory, false)
	if err != nil {
		pool.Close()
		t.Fatalf("open Webhook test keyring: %v", err)
	}
	return &db.Pool{Pool: pool}, keyring
}
