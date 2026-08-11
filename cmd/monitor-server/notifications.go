package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

const (
	notificationPollInterval     = 500 * time.Millisecond
	notificationDeliveryTimeout  = 30 * time.Second
	notificationSnapshotFilename = "notification-channels.snapshot"
)

func notificationSnapshotPath(credentialDirectory string) string {
	return filepath.Join(credentialDirectory, notificationSnapshotFilename)
}

func sendPlatformUnavailableNotification(
	ctx context.Context,
	store *notify.ChannelSnapshotStore,
	keyring *instance.CredentialKeyring,
	failure platformhealth.FailureFact,
) error {
	snapshot, err := store.Load()
	if err != nil {
		return err
	}
	message := notify.Message{
		EventType: notify.EventPlatformUnavailable,
		Subject:   "[DBS Monitor] 平台自身不可用",
		Body: fmt.Sprintf("平台自身不可用\n\n事实源：%s\n故障码：%s\n发生时间：%s\n",
			failure.Source, failure.Code, failure.ObservedAt.UTC().Format(time.RFC3339)),
	}
	var deliveryErrors []error
	if channel := snapshot.SMTP; channel != nil && channel.Enabled {
		config := notify.SMTPConfig{
			Host: channel.Host, Port: int(channel.Port), From: channel.From,
			TLSMode: notify.TLSMode(channel.TLSMode), AuthType: notify.AuthType(channel.AuthType),
		}
		if channel.Username != nil {
			config.Username = *channel.Username
		}
		smtpReady := true
		if len(channel.AuthCiphertext) > 0 {
			if channel.AuthKeyVersion == nil {
				deliveryErrors = append(deliveryErrors, errors.New("SMTP snapshot authentication key version is missing"))
				smtpReady = false
			} else {
				password, decryptErr := keyring.DecryptSMTPPassword(channel.AuthCiphertext, *channel.AuthKeyVersion)
				if decryptErr != nil {
					deliveryErrors = append(deliveryErrors, fmt.Errorf("decrypt SMTP snapshot authentication value: %w", decryptErr))
					smtpReady = false
				} else {
					config.Password = password
				}
			}
		}
		if smtpReady {
			message.To = channel.Recipient
			if sendErr := notify.NewSMTPChannel(config).Send(ctx, message); sendErr != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("send platform failure through SMTP: %w", sendErr))
			}
		}
	}
	for _, target := range snapshot.Webhooks {
		if !target.Enabled {
			continue
		}
		targetID, parseErr := uuid.Parse(target.ID)
		if parseErr != nil {
			deliveryErrors = append(deliveryErrors, errors.New("Webhook snapshot target ID is invalid"))
			continue
		}
		signingValue, valueErr := keyring.DecryptWebhookSigningValue(targetID, target.SigningValueCiphertext, target.SigningKeyVersion)
		signatureHeader, headerErr := keyring.DecryptWebhookSignatureHeader(targetID, target.SignatureHeaderCiphertext, target.SigningKeyVersion)
		if valueErr != nil || headerErr != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("decrypt Webhook snapshot signing configuration: %w", errors.Join(valueErr, headerErr)))
			continue
		}
		channel := notify.NewWebhookChannel(notify.WebhookConfig{
			URL: target.URL, SigningValue: signingValue, SignatureHeader: signatureHeader,
			Timeout: notificationDeliveryTimeout,
		})
		if sendErr := channel.Send(ctx, message); sendErr != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("send platform failure through Webhook: %w", sendErr))
		}
	}
	return errors.Join(deliveryErrors...)
}

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
