package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

type credentialRotationResult struct {
	KeyVersion         int32
	CredentialsRotated int64
}

func rotateCredentialKeyring(ctx context.Context, platform *db.Pool, directory string) (credentialRotationResult, error) {
	keyring, reencrypt, err := instance.PrepareCredentialKeyRotation(ctx, instance.New(platform), directory)
	if err != nil {
		return credentialRotationResult{}, err
	}
	result := credentialRotationResult{}
	if reencrypt {
		if err := platform.InTx(ctx, func(tx pgx.Tx) error {
			result.CredentialsRotated, err = keyring.ReencryptCredentials(ctx, instance.New(tx))
			return err
		}); err != nil {
			return credentialRotationResult{}, fmt.Errorf("rotate instance credentials: %w", err)
		}
	}
	if err := keyring.RemoveUnreferencedKeys(ctx, instance.New(platform)); err != nil {
		return credentialRotationResult{}, err
	}
	result.KeyVersion = keyring.CurrentVersion()
	return result, nil
}
