package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
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
	log.Printf("rotated %d instance credentials to master key v%d", result.CredentialsRotated, result.KeyVersion)
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
			rotated, err := keyring.ReencryptCredentials(ctx, instance.New(tx))
			credentialsRotated = rotated
			return err
		}); err != nil {
			return credentialRotationResult{}, fmt.Errorf("rotate instance credentials: %w", err)
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
