package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

const (
	notificationPollInterval    = 500 * time.Millisecond
	notificationDeliveryTimeout = 30 * time.Second
)

func runNotificationDelivery(ctx context.Context, platform *db.Pool, keyring *instance.CredentialKeyring) {
	ticker := time.NewTicker(notificationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			drainNotifications(ctx, platform, keyring, now.UTC())
		}
	}
}

func drainNotifications(ctx context.Context, platform *db.Pool, keyring *instance.CredentialKeyring, now time.Time) {
	queries := notify.New(platform)
	channels := make(map[string]notify.Channel)
	config, err := queries.GetSMTPChannel(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("read SMTP notification channel: %v", err)
		return
	}
	if err == nil && config.Enabled {
		smtpConfig := notify.SMTPConfig{
			Host:     config.Host,
			Port:     int(config.Port),
			From:     config.FromAddress,
			TLSMode:  notify.TLSMode(config.TlsMode),
			AuthType: notify.AuthType(config.AuthType),
		}
		var decryptErr error
		if config.AuthCiphertext != nil {
			smtpConfig.Username = config.Username.String
			smtpConfig.Password, decryptErr = keyring.DecryptSMTPPassword(config.AuthCiphertext, config.AuthKeyVersion.Int32)
		}
		if decryptErr != nil {
			log.Printf("decrypt SMTP authentication value: %v", decryptErr)
		} else {
			channels[notify.SMTPChannelKey] = notify.NewSMTPChannel(smtpConfig)
		}
	}
	webhooks, err := queries.ListWebhookTargets(ctx)
	if err != nil {
		log.Printf("read Webhook notification targets: %v", err)
		return
	}
	for _, target := range webhooks {
		targetID := uuid.UUID(target.ID.Bytes)
		signingValue, valueErr := keyring.DecryptWebhookSigningValue(targetID, target.SigningValueCiphertext, target.SigningKeyVersion)
		signatureHeader, headerErr := keyring.DecryptWebhookSignatureHeader(targetID, target.SignatureHeaderCiphertext, target.SigningKeyVersion)
		if valueErr != nil || headerErr != nil {
			log.Printf("decrypt Webhook signing configuration for %s: %v %v", targetID, valueErr, headerErr)
			continue
		}
		channels[notify.WebhookChannelKey(target.ID)] = notify.NewWebhookChannel(notify.WebhookConfig{
			URL:             target.Url,
			SigningValue:    signingValue,
			SignatureHeader: signatureHeader,
			Timeout:         notificationDeliveryTimeout,
		})
	}
	dispatcher := notify.NewDispatcher(platform)
	if _, err := dispatcher.EnqueueDueRepeats(ctx, now); err != nil {
		log.Printf("schedule repeat notifications: %v", err)
		return
	}
	for {
		attemptContext, cancel := context.WithTimeout(ctx, notificationDeliveryTimeout)
		processed, dispatchErr := dispatcher.DispatchOne(attemptContext, now, channels)
		cancel()
		if dispatchErr != nil {
			log.Printf("dispatch notification: %v", dispatchErr)
			return
		}
		if !processed {
			return
		}
		now = time.Now().UTC()
	}
}
