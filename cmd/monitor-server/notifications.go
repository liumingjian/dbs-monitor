package main

import (
	"context"
	"errors"
	"log"
	"time"

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
	config, err := notify.New(platform).GetSMTPChannel(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		log.Printf("read SMTP notification channel: %v", err)
		return
	}
	if !config.Enabled {
		return
	}

	var password string
	if config.AuthCiphertext != nil {
		password, err = keyring.DecryptSMTPPassword(config.AuthCiphertext, config.AuthKeyVersion.Int32)
		if err != nil {
			log.Printf("decrypt SMTP authentication value: %v", err)
			return
		}
	}
	channel := notify.NewSMTPChannel(notify.SMTPConfig{
		Host:     config.Host,
		Port:     int(config.Port),
		From:     config.FromAddress,
		Username: config.Username.String,
		Password: password,
		TLSMode:  notify.TLSMode(config.TlsMode),
		AuthType: notify.AuthType(config.AuthType),
	})
	dispatcher := notify.NewDispatcher(platform)
	for {
		attemptContext, cancel := context.WithTimeout(ctx, notificationDeliveryTimeout)
		processed, dispatchErr := dispatcher.DispatchOne(attemptContext, now, channel)
		cancel()
		if dispatchErr != nil {
			log.Printf("dispatch SMTP notification: %v", dispatchErr)
			return
		}
		if !processed {
			return
		}
		now = time.Now().UTC()
	}
}
