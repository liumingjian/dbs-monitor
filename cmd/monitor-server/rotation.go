package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

const (
	masterKeyRotationLockID  int64 = 0x524f5441 // "ROTA"; the database OID provides the remaining scope.
	masterKeyRotationLockSQL       = `SELECT pg_try_advisory_lock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
	masterKeyRotationUnlockSQL = `SELECT pg_advisory_unlock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
)

var errMasterKeyRotationLockUnavailable = errors.New("master key rotation lock is held by a running server or another rotation")

type masterKeyRotationLock struct {
	connection *pgxpool.Conn
}

type credentialRotationResult struct {
	KeyVersion         int32
	CredentialsRotated int64
}

func runMasterKeyRotationCommand(ctx context.Context) (returnedErr error) {
	connectionString := env("DATABASE_URL", defaultDatabaseURL)
	credentialDirectory := env("CREDENTIALS_DIR", defaultCredentialDirectory)
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
	log.Printf("rotated %d instance credentials to master key v%d", result.CredentialsRotated, result.KeyVersion)
	return nil
}

func acquireMasterKeyRotationLock(ctx context.Context, pool *pgxpool.Pool) (*masterKeyRotationLock, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve master key rotation lock connection: %w", err)
	}
	var acquired bool
	if err := connection.QueryRow(ctx, masterKeyRotationLockSQL, masterKeyRotationLockID).Scan(&acquired); err != nil {
		connection.Release()
		return nil, fmt.Errorf("acquire master key rotation advisory lock: %w", err)
	}
	if !acquired {
		connection.Release()
		return nil, errMasterKeyRotationLockUnavailable
	}
	return &masterKeyRotationLock{connection: connection}, nil
}

func (lock *masterKeyRotationLock) Release() error {
	if lock == nil || lock.connection == nil {
		return nil
	}
	connection := lock.connection
	lock.connection = nil
	defer connection.Release()

	var unlocked bool
	if err := connection.QueryRow(context.Background(), masterKeyRotationUnlockSQL, masterKeyRotationLockID).Scan(&unlocked); err != nil {
		_ = connection.Conn().Close(context.Background())
		return fmt.Errorf("release master key rotation advisory lock: %w", err)
	}
	if !unlocked {
		return fmt.Errorf("release master key rotation advisory lock: lock was not held")
	}
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
