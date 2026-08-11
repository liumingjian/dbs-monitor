package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestSendPlatformUnavailableNotificationFromSnapshot(t *testing.T) {
	t.Parallel()

	const signatureHeader = "X-DBS-Platform-Signature"
	received := make(chan []byte, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read direct platform notification: %v", err)
		}
		if request.Header.Get(signatureHeader) == "" {
			t.Error("direct platform notification has no signature")
		}
		received <- body
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("create direct notification keyring: %v", err)
	}
	targetID := uuid.MustParse("00000000-0000-0000-0000-000000000083")
	signingCiphertext, keyVersion, err := keyring.EncryptWebhookSigningValue(targetID, "local-signing-value")
	if err != nil {
		t.Fatalf("encrypt direct notification signing value: %v", err)
	}
	headerCiphertext, _, err := keyring.EncryptWebhookSignatureHeader(targetID, signatureHeader)
	if err != nil {
		t.Fatalf("encrypt direct notification signature header: %v", err)
	}
	store := notify.NewChannelSnapshotStore(filepath.Join(credentialDirectory, notificationSnapshotFilename))
	if err := store.Write(notify.ChannelSnapshot{
		FormatVersion: notify.ChannelSnapshotFormatVersion,
		Webhooks: []notify.SnapshotWebhookTarget{{
			ID: targetID.String(), Enabled: true, URL: receiver.URL,
			SigningValueCiphertext: signingCiphertext, SignatureHeaderCiphertext: headerCiphertext,
			SigningKeyVersion: keyVersion,
		}},
	}); err != nil {
		t.Fatalf("write direct notification snapshot: %v", err)
	}
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	if err := sendPlatformUnavailableNotification(context.Background(), store, keyring, platformhealth.FailureFact{
		Source: platformhealth.SourcePlatformDatabase, Code: "PLATFORM_DATABASE_UNREACHABLE", ObservedAt: now,
	}); err != nil {
		t.Fatalf("send direct platform notification: %v", err)
	}

	var payload notify.WebhookPayload
	if err := json.Unmarshal(<-received, &payload); err != nil {
		t.Fatalf("decode direct platform notification: %v", err)
	}
	if payload.EventType != notify.EventPlatformUnavailable ||
		!strings.Contains(payload.Subject, "平台自身不可用") ||
		!strings.Contains(payload.Body, "PLATFORM_DATABASE_UNREACHABLE") {
		t.Fatalf("direct platform notification payload = %+v", payload)
	}
}

func TestSendPlatformUnavailableNotificationReportsAllSnapshotErrors(t *testing.T) {
	t.Parallel()

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("create direct notification keyring: %v", err)
	}
	store := notify.NewChannelSnapshotStore(notificationSnapshotPath(credentialDirectory))
	if err := store.Write(notify.ChannelSnapshot{
		FormatVersion: notify.ChannelSnapshotFormatVersion,
		SMTP: &notify.SnapshotSMTPChannel{
			Enabled:        true,
			AuthCiphertext: []byte("encrypted-authentication-value"),
		},
		Webhooks: []notify.SnapshotWebhookTarget{{
			ID:      "invalid-target-id",
			Enabled: true,
		}},
	}); err != nil {
		t.Fatalf("write invalid direct notification snapshot: %v", err)
	}

	err = sendPlatformUnavailableNotification(context.Background(), store, keyring, platformhealth.FailureFact{})
	want := "SMTP snapshot authentication key version is missing\nWebhook snapshot target ID is invalid"
	if err == nil || err.Error() != want {
		t.Fatalf("direct notification error = %v, want %q", err, want)
	}
}
