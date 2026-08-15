package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func openStartupCredentialKeyring(
	ctx context.Context,
	platform *db.Pool,
	directory string,
	health *platformhealth.Store,
	logger *log.Logger,
	now time.Time,
) (*instance.CredentialKeyring, error) {
	hasEncryptedCredentials, err := databaseHasEncryptedCredentials(ctx, platform)
	if err != nil {
		return nil, err
	}

	keyring, keyringErr := instance.OpenCredentialKeyring(directory, hasEncryptedCredentials)
	if keyringErr != nil {
		var fault *instance.CredentialFault
		failureCode := "CREDENTIAL_KEYRING_FAILED"
		if errors.As(keyringErr, &fault) {
			failureCode = string(fault.Code)
		}
		health.Update(now, platformhealth.CredentialSource(platformhealth.CredentialFacts{
			Available:   true,
			FailureCode: failureCode,
		}))
		logger.Printf("monitor-server: credential keyring at %s is unavailable: %v; continuing with dependent operations disabled", directory, keyringErr)
		return keyring, nil
	}

	health.Update(now, platformhealth.CredentialSource(platformhealth.CredentialFacts{Available: true}))
	if keyring.GeneratedInitialKey() {
		version := keyring.CurrentVersion()
		path := filepath.Join(directory, fmt.Sprintf("master-key-v%d", version))
		logger.Printf("monitor-server: generated credential key version %d at %s", version, path)
		health.RecordCredentialKeyGenerated(now, version, path)
	}
	return keyring, nil
}

func databaseHasEncryptedCredentials(ctx context.Context, platform *db.Pool) (bool, error) {
	const query = `SELECT
		EXISTS (SELECT 1 FROM instance WHERE password_ciphertext IS NOT NULL)
		OR EXISTS (SELECT 1 FROM smtp_channel WHERE auth_ciphertext IS NOT NULL)
		OR EXISTS (SELECT 1 FROM webhook_target WHERE signing_value_ciphertext IS NOT NULL)`
	var hasEncryptedCredentials bool
	if err := platform.QueryRow(ctx, query).Scan(&hasEncryptedCredentials); err != nil {
		return false, fmt.Errorf("inspect encrypted credentials: %w", err)
	}
	return hasEncryptedCredentials, nil
}
