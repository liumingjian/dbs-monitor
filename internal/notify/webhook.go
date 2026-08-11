package notify

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
	"time"
)

const defaultWebhookTimeout = 10 * time.Second

type WebhookConfig struct {
	URL             string
	SigningValue    string
	SignatureHeader string
	Timeout         time.Duration
}

type WebhookPayload struct {
	EventType EventType `json:"event_type"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
}

type WebhookChannel struct {
	config WebhookConfig
	client *http.Client
}

func NewWebhookChannel(config WebhookConfig) *WebhookChannel {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	return &WebhookChannel{config: config, client: &http.Client{Timeout: timeout}}
}

func (channel *WebhookChannel) Send(ctx context.Context, message Message) error {
	payload, err := json.Marshal(WebhookPayload{
		EventType: message.EventType,
		Subject:   message.Subject,
		Body:      message.Body,
	})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.config.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	mac := hmac.New(sha256.New, []byte(channel.config.SigningValue))
	_, _ = mac.Write(payload)
	request.Header.Set(channel.config.SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))

	response, err := channel.client.Do(request)
	if err != nil {
		return fmt.Errorf("send webhook request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}
