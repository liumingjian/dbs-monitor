package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformevent"
)

const masterKeyRotationLockID int64 = 0x524f5441 // "ROTA"

var errMasterKeyRotationLockUnavailable = errors.New("master key rotation lock is held by a running server or another rotation")

type credentialRotationResult struct {
	KeyVersion         int32
	CredentialsRotated int64
}

func runMasterKeyRotationCommand(ctx context.Context) (returnedErr error) {
	configPath := env("DBS_MONITOR_CONFIG_FILE", defaultServerConfigPath)
	config, permissionsSecure, err := loadServerConfig(configPath)
	if err != nil {
		return err
	}
	if !permissionsSecure {
		log.Printf("monitor-server: config file %s permissions are not 0600; continuing", configPath)
	}
	connectionString := config.PlatformDatabaseURL
	credentialDirectory := config.MasterKeyPath
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer pool.Close()

	rotationLock, err := acquireMasterKeyRotationLock(ctx, pool)
	if err != nil {
		return fmt.Errorf("refuse offline master key rotation: %w", err)
	}
	defer func() {
		if err := rotationLock.Release(); returnedErr == nil && err != nil {
			returnedErr = err
		}
	}()

	result, err := rotateCredentialKeyring(ctx, &db.Pool{Pool: pool}, credentialDirectory)
	if err != nil {
		return err
	}
	if err := platformevent.Record(ctx, pool, platformevent.Event{
		Kind: platformevent.MasterKeyRotated, OccurredAt: time.Now().UTC(),
		ActorSubject: fmt.Sprintf("uid:%d", os.Geteuid()),
	}); err != nil {
		return fmt.Errorf("record master key rotation actor: %w", err)
	}
	log.Printf("rotated %d credentials to master key v%d", result.CredentialsRotated, result.KeyVersion)
	return nil
}

func acquireMasterKeyRotationLock(ctx context.Context, pool *pgxpool.Pool) (*databaseAdvisoryLock, error) {
	return acquireDatabaseAdvisoryLock(
		ctx, pool, masterKeyRotationLockID, "master key rotation", errMasterKeyRotationLockUnavailable,
	)
}

func rotateCredentialKeyring(ctx context.Context, platform *db.Pool, directory string) (credentialRotationResult, error) {
	platformQueries := instance.New(platform)
	keyring, needsReencryption, err := instance.PrepareCredentialKeyRotation(ctx, platformQueries, directory)
	if err != nil {
		return credentialRotationResult{}, err
	}

	var credentialsRotated int64
	if needsReencryption {
		if err := platform.InTx(ctx, func(tx pgx.Tx) error {
			instanceCredentialsRotated, err := keyring.ReencryptCredentials(ctx, instance.New(tx))
			if err != nil {
				return err
			}
			smtpCredentialsRotated, err := reencryptSMTPChannel(ctx, tx, keyring)
			if err != nil {
				return err
			}
			webhookCredentialsRotated, err := reencryptWebhookTargets(ctx, tx, keyring)
			if err != nil {
				return err
			}
			credentialsRotated = instanceCredentialsRotated + smtpCredentialsRotated + webhookCredentialsRotated
			return nil
		}); err != nil {
			return credentialRotationResult{}, fmt.Errorf("rotate credentials: %w", err)
		}
	}
	if err := notify.NewChannelSnapshotStore(notificationSnapshotPath(directory)).Sync(ctx, platform); err != nil {
		return credentialRotationResult{}, fmt.Errorf("refresh notification snapshot after key rotation: %w", err)
	}
	if err := keyring.RemoveUnreferencedKeys(ctx, platformQueries); err != nil {
		return credentialRotationResult{}, err
	}
	return credentialRotationResult{
		KeyVersion:         keyring.CurrentVersion(),
		CredentialsRotated: credentialsRotated,
	}, nil
}

func reencryptWebhookTargets(ctx context.Context, tx pgx.Tx, keyring *instance.CredentialKeyring) (int64, error) {
	queries := notify.New(tx)
	targets, err := queries.ListWebhookTargetsForKeyRotation(ctx)
	if err != nil {
		return 0, fmt.Errorf("read Webhook signing configuration for key rotation: %w", err)
	}
	var rotated int64
	for _, target := range targets {
		if target.SigningKeyVersion == keyring.CurrentVersion() {
			continue
		}
		targetID := uuid.UUID(target.ID.Bytes)
		signingValue, err := keyring.DecryptWebhookSigningValue(targetID, target.SigningValueCiphertext, target.SigningKeyVersion)
		if err != nil {
			return 0, err
		}
		signatureHeader, err := keyring.DecryptWebhookSignatureHeader(targetID, target.SignatureHeaderCiphertext, target.SigningKeyVersion)
		if err != nil {
			return 0, err
		}
		signingValueCiphertext, signingKeyVersion, err := keyring.EncryptWebhookSigningValue(targetID, signingValue)
		if err != nil {
			return 0, err
		}
		signatureHeaderCiphertext, _, err := keyring.EncryptWebhookSignatureHeader(targetID, signatureHeader)
		if err != nil {
			return 0, err
		}
		if err := queries.UpdateWebhookTargetSigningKey(ctx, notify.UpdateWebhookTargetSigningKeyParams{
			ID:                        target.ID,
			SigningValueCiphertext:    signingValueCiphertext,
			SignatureHeaderCiphertext: signatureHeaderCiphertext,
			SigningKeyVersion:         signingKeyVersion,
		}); err != nil {
			return 0, fmt.Errorf("update Webhook signing key version: %w", err)
		}
		rotated++
	}
	return rotated, nil
}

func reencryptSMTPChannel(ctx context.Context, tx pgx.Tx, keyring *instance.CredentialKeyring) (int64, error) {
	queries := notify.New(tx)
	channel, err := queries.GetSMTPChannelForKeyRotation(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read SMTP authentication for key rotation: %w", err)
	}
	if channel.AuthKeyVersion.Int32 == keyring.CurrentVersion() {
		return 0, nil
	}
	password, err := keyring.DecryptSMTPPassword(channel.AuthCiphertext, channel.AuthKeyVersion.Int32)
	if err != nil {
		return 0, err
	}
	ciphertext, version, err := keyring.EncryptSMTPPassword(password)
	if err != nil {
		return 0, err
	}
	if err := queries.UpdateSMTPChannelAuthKey(ctx, notify.UpdateSMTPChannelAuthKeyParams{
		AuthCiphertext: ciphertext,
		AuthKeyVersion: pgtype.Int4{Int32: version, Valid: true},
	}); err != nil {
		return 0, fmt.Errorf("update SMTP authentication key version: %w", err)
	}
	return 1, nil
}
