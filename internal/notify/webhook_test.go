package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookChannelSendsSignedJSON(t *testing.T) {
	t.Parallel()

	const signingValue = "webhook-signing-value"
	received := make(chan struct {
		body      []byte
		signature string
	}, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read webhook body: %v", err)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q, want application/json", request.Header.Get("Content-Type"))
		}
		received <- struct {
			body      []byte
			signature string
		}{body: body, signature: request.Header.Get("X-DBS-Signature")}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	channel := NewWebhookChannel(WebhookConfig{
		URL:             receiver.URL,
		SigningValue:    signingValue,
		SignatureHeader: "X-DBS-Signature",
		Timeout:         time.Second,
	})
	message := Message{EventType: EventFiring, Subject: "Database alert", Body: "Connections are high"}
	if err := channel.Send(context.Background(), message); err != nil {
		t.Fatalf("send webhook: %v", err)
	}

	request := <-received
	var payload WebhookPayload
	if err := json.Unmarshal(request.body, &payload); err != nil {
		t.Fatalf("decode webhook payload: %v", err)
	}
	if payload.EventType != EventFiring || payload.Subject != message.Subject || payload.Body != message.Body {
		t.Fatalf("webhook payload = %+v", payload)
	}
	mac := hmac.New(sha256.New, []byte(signingValue))
	_, _ = mac.Write(request.body)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if request.signature != wantSignature {
		t.Fatalf("signature = %q, want %q", request.signature, wantSignature)
	}
}
