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
	snapshotStore *notify.ChannelSnapshotStore,
	keyring *instance.CredentialKeyring,
	failure platformhealth.FailureFact,
) error {
	snapshot, err := snapshotStore.Load()
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
		if err := sendPlatformUnavailableSMTP(ctx, channel, keyring, message); err != nil {
			deliveryErrors = append(deliveryErrors, err)
		}
	}
	for _, target := range snapshot.Webhooks {
		if !target.Enabled {
			continue
		}
		if err := sendPlatformUnavailableWebhook(ctx, target, keyring, message); err != nil {
			deliveryErrors = append(deliveryErrors, err)
		}
	}
	return errors.Join(deliveryErrors...)
}

func sendPlatformUnavailableSMTP(
	ctx context.Context,
	channel *notify.SnapshotSMTPChannel,
	keyring *instance.CredentialKeyring,
	message notify.Message,
) error {
	config := notify.SMTPConfig{
		Host:     channel.Host,
		Port:     int(channel.Port),
		From:     channel.From,
		TLSMode:  notify.TLSMode(channel.TLSMode),
		AuthType: notify.AuthType(channel.AuthType),
	}
	if channel.Username != nil {
		config.Username = *channel.Username
	}
	if len(channel.AuthCiphertext) > 0 {
		if channel.AuthKeyVersion == nil {
			return errors.New("SMTP snapshot authentication key version is missing")
		}
		password, err := keyring.DecryptSMTPPassword(channel.AuthCiphertext, *channel.AuthKeyVersion)
		if err != nil {
			return fmt.Errorf("decrypt SMTP snapshot authentication value: %w", err)
		}
		config.Password = password
	}
	message.To = channel.Recipient
	if err := notify.NewSMTPChannel(config).Send(ctx, message); err != nil {
		return fmt.Errorf("send platform failure through SMTP: %w", err)
	}
	return nil
}

func sendPlatformUnavailableWebhook(
	ctx context.Context,
	target notify.SnapshotWebhookTarget,
	keyring *instance.CredentialKeyring,
	message notify.Message,
) error {
	targetID, err := uuid.Parse(target.ID)
	if err != nil {
		return errors.New("Webhook snapshot target ID is invalid")
	}
	signingValue, valueErr := keyring.DecryptWebhookSigningValue(targetID, target.SigningValueCiphertext, target.SigningKeyVersion)
	signatureHeader, headerErr := keyring.DecryptWebhookSignatureHeader(targetID, target.SignatureHeaderCiphertext, target.SigningKeyVersion)
	if valueErr != nil || headerErr != nil {
		return fmt.Errorf("decrypt Webhook snapshot signing configuration: %w", errors.Join(valueErr, headerErr))
	}
	channel := notify.NewWebhookChannel(notify.WebhookConfig{
		URL:             target.URL,
		SigningValue:    signingValue,
		SignatureHeader: signatureHeader,
		Timeout:         notificationDeliveryTimeout,
	})
	if err := channel.Send(ctx, message); err != nil {
		return fmt.Errorf("send platform failure through Webhook: %w", err)
	}
	return nil
}

func runNotificationDelivery(ctx context.Context, platform *db.Pool, keyring *instance.CredentialKeyring, retryBackoffCap time.Duration) {
	ticker := time.NewTicker(notificationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			drainNotifications(ctx, platform, keyring, retryBackoffCap, now.UTC())
		}
	}
}

func drainNotifications(ctx context.Context, platform *db.Pool, keyring *instance.CredentialKeyring, retryBackoffCap time.Duration, now time.Time) {
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
	dispatcher := notify.NewDispatcherWithRetryBackoffCap(platform, retryBackoffCap)
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
