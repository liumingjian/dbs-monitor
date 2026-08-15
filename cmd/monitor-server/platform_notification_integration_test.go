package main

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
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestPlatformDatabaseFailureSendsSnapshotDirectlyWithoutProductWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	databaseName := fmt.Sprintf("dbs_monitor_issue83_%d", os.Getpid())
	admin := openRotationSQL(t, env("PGDATABASE", "dbs_monitor"))
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
		admin.Close()
		t.Fatalf("drop stale issue 83 database: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		admin.Close()
		t.Fatalf("create issue 83 database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
		admin.Close()
	})

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openRotationSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		migrationDB.Close()
		t.Fatalf("migrate issue 83 database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, rotationConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open issue 83 platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open issue 83 keyring: %v", err)
	}

	const (
		signingValue    = "issue-83-webhook-signing-value"
		signatureHeader = "X-DBS-Platform-Signature"
	)
	received := make(chan struct {
		body      []byte
		signature string
	}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("read platform failure Webhook: %v", readErr)
		}
		received <- struct {
			body      []byte
			signature string
		}{body: body, signature: request.Header.Get(signatureHeader)}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	targetID := uuid.MustParse("00000000-0000-0000-0000-000000000083")
	signingCiphertext, keyVersion, err := keyring.EncryptWebhookSigningValue(targetID, signingValue)
	if err != nil {
		t.Fatalf("encrypt issue 83 signing value: %v", err)
	}
	headerCiphertext, headerVersion, err := keyring.EncryptWebhookSignatureHeader(targetID, signatureHeader)
	if err != nil || headerVersion != keyVersion {
		t.Fatalf("encrypt issue 83 signature header: version %d, error %v", headerVersion, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO webhook_target
		(id, name, enabled, url, signing_value_ciphertext, signature_header_ciphertext,
		 signing_key_version, created_at, updated_at)
		VALUES ($1, 'platform-unavailable', true, $2, $3, $4, $5, now(), now())`,
		targetID, receiver.URL, signingCiphertext, headerCiphertext, keyVersion); err != nil {
		t.Fatalf("insert issue 83 Webhook configuration: %v", err)
	}
	snapshotStore := notify.NewChannelSnapshotStore(notificationSnapshotPath(credentialDirectory))
	if err := snapshotStore.Sync(ctx, platform); err != nil {
		t.Fatalf("write issue 83 notification snapshot: %v", err)
	}
	before := productNotificationWriteCounts(t, ctx, pool)

	unavailablePool, err := pgxpool.New(ctx, "postgres://dbs_monitor@127.0.0.1:1/dbs_monitor?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unavailable platform pool: %v", err)
	}
	defer unavailablePool.Close()
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), nil)
	var deliveryErr error
	health.SetFailureObserver(func(failure platformhealth.FailureFact) {
		deliveryErr = sendPlatformUnavailableNotification(ctx, snapshotStore, keyring, failure)
	})
	refreshPlatformDatabaseHealth(ctx, &db.Pool{Pool: unavailablePool}, health, now)
	if deliveryErr != nil {
		t.Fatalf("send platform unavailable notification: %v", deliveryErr)
	}

	request := <-received
	var payload notify.WebhookPayload
	if err := json.Unmarshal(request.body, &payload); err != nil {
		t.Fatalf("decode platform failure Webhook: %v", err)
	}
	if payload.EventType != notify.EventPlatformUnavailable ||
		!strings.Contains(payload.Subject, "平台自身不可用") ||
		!strings.Contains(payload.Body, "PLATFORM_DATABASE_UNREACHABLE") {
		t.Fatalf("platform failure Webhook payload = %+v", payload)
	}
	mac := hmac.New(sha256.New, []byte(signingValue))
	_, _ = mac.Write(request.body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); request.signature != want {
		t.Fatalf("platform failure Webhook signature = %q, want %q", request.signature, want)
	}
	for _, forbidden := range [][]byte{[]byte(signingValue), signingCiphertext, headerCiphertext} {
		if bytes.Contains(request.body, forbidden) {
			t.Fatalf("platform failure Webhook exposes notification secret material %q", forbidden)
		}
	}
	after := productNotificationWriteCounts(t, ctx, pool)
	if before != after {
		t.Fatalf("product alert/notification writes changed from %v to %v", before, after)
	}
}

type notificationWriteCounts struct {
	Alerts     int64
	Deliveries int64
	Attempts   int64
	Events     int64
}

func productNotificationWriteCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) notificationWriteCounts {
	t.Helper()
	var counts notificationWriteCounts
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM alert_instance),
		(SELECT count(*) FROM notification_delivery),
		(SELECT count(*) FROM notification_attempt),
		(SELECT count(*) FROM alert_event)`).Scan(
		&counts.Alerts, &counts.Deliveries, &counts.Attempts, &counts.Events,
	); err != nil {
		t.Fatalf("read product notification write counts: %v", err)
	}
	return counts
}
