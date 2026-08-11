package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

type credentialRotationResult struct {
	KeyVersion         int32
	CredentialsRotated int64
}

func runMasterKeyRotationCommand(ctx context.Context) error {
	connectionString := env("DATABASE_URL", defaultDatabaseURL)
	credentialDirectory := env("CREDENTIALS_DIR", defaultCredentialDirectory)
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer pool.Close()

	result, err := rotateCredentialKeyring(ctx, &db.Pool{Pool: pool}, credentialDirectory)
	if err != nil {
		return err
	}
	log.Printf("rotated %d credentials to master key v%d", result.CredentialsRotated, result.KeyVersion)
	return nil
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
			credentialsRotated = instanceCredentialsRotated + smtpCredentialsRotated
			return nil
		}); err != nil {
			return credentialRotationResult{}, fmt.Errorf("rotate credentials: %w", err)
		}
	}
	if err := keyring.RemoveUnreferencedKeys(ctx, platformQueries); err != nil {
		return credentialRotationResult{}, err
	}
	return credentialRotationResult{
		KeyVersion:         keyring.CurrentVersion(),
		CredentialsRotated: credentialsRotated,
	}, nil
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
