package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var files embed.FS

const (
	credentialSchemaVersion               = 6
	plaintextPasswordRemovalVersion       = 7
	migrationAdvisoryLockID         int64 = 0x4442534d // "DBSM"; the database OID provides the remaining scope.
	migrationAdvisoryLockSQL              = `SELECT pg_advisory_lock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
	migrationAdvisoryUnlockSQL = `SELECT pg_advisory_unlock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
)

func Open(connectionString string) (*sql.DB, error) {
	database, err := sql.Open("pgx", connectionString)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	return database, nil
}

func Up(ctx context.Context, database *sql.DB, credentialDirectory string) (applied int, returnedErr error) {
	return up(ctx, database, credentialDirectory, 0)
}

func UpWithLockTimeout(ctx context.Context, database *sql.DB, credentialDirectory string, lockWaitTimeout time.Duration) (applied int, returnedErr error) {
	return up(ctx, database, credentialDirectory, lockWaitTimeout)
}

func up(ctx context.Context, database *sql.DB, credentialDirectory string, lockWaitTimeout time.Duration) (applied int, returnedErr error) {
	lockConnection, err := database.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("reserve migration lock connection: %w", err)
	}
	lockContext := ctx
	cancelLockWait := func() {}
	if lockWaitTimeout > 0 {
		lockContext, cancelLockWait = context.WithTimeout(ctx, lockWaitTimeout)
	}
	_, err = lockConnection.ExecContext(lockContext, migrationAdvisoryLockSQL, migrationAdvisoryLockID)
	cancelLockWait()
	if err != nil {
		lockConnection.Close()
		if errors.Is(lockContext.Err(), context.DeadlineExceeded) {
			return 0, fmt.Errorf("migration advisory lock wait timed out after %s: %w", lockWaitTimeout, context.DeadlineExceeded)
		}
		return 0, fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		var unlocked bool
		unlockErr := lockConnection.QueryRowContext(context.Background(), migrationAdvisoryUnlockSQL, migrationAdvisoryLockID).Scan(&unlocked)
		closeErr := lockConnection.Close()
		var releaseErr error
		switch {
		case unlockErr != nil:
			releaseErr = fmt.Errorf("release migration advisory lock: %w", unlockErr)
		case !unlocked:
			releaseErr = fmt.Errorf("release migration advisory lock: lock was not held")
		case closeErr != nil:
			releaseErr = fmt.Errorf("close migration lock connection: %w", closeErr)
		}
		if returnedErr == nil {
			returnedErr = releaseErr
		}
	}()

	root, err := fs.Sub(files, ".")
	if err != nil {
		return 0, fmt.Errorf("migration filesystem: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, database, root)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	if current < credentialSchemaVersion {
		results, err := provider.UpTo(ctx, credentialSchemaVersion)
		if err != nil {
			return 0, fmt.Errorf("apply credential schema migration: %w", err)
		}
		applied += len(results)
	}
	if current < plaintextPasswordRemovalVersion {
		if err := migrateInstanceCredentials(ctx, database, credentialDirectory); err != nil {
			return applied, err
		}
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return applied, fmt.Errorf("apply migrations: %w", err)
	}
	applied += len(results)
	if err := reconcileAlertingSeeds(ctx, database); err != nil {
		return applied, err
	}
	return applied, nil
}

func migrateInstanceCredentials(ctx context.Context, database *sql.DB, credentialDirectory string) error {
	var hasPlaintextCredentials bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM instance WHERE password_ciphertext IS NULL)").Scan(&hasPlaintextCredentials); err != nil {
		return fmt.Errorf("inspect plaintext instance credentials: %w", err)
	}
	if !hasPlaintextCredentials {
		return nil
	}

	var hasEncryptedCredentials bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM instance WHERE password_ciphertext IS NOT NULL)").Scan(&hasEncryptedCredentials); err != nil {
		return fmt.Errorf("inspect encrypted instance credentials: %w", err)
	}
	var hasSMTPTable bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass('smtp_channel') IS NOT NULL").Scan(&hasSMTPTable); err != nil {
		return fmt.Errorf("inspect SMTP credential schema: %w", err)
	}
	if hasSMTPTable {
		var hasEncryptedSMTP bool
		if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM smtp_channel WHERE auth_ciphertext IS NOT NULL)").Scan(&hasEncryptedSMTP); err != nil {
			return fmt.Errorf("inspect encrypted SMTP credential: %w", err)
		}
		hasEncryptedCredentials = hasEncryptedCredentials || hasEncryptedSMTP
	}
	var hasWebhookTable bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass('webhook_target') IS NOT NULL").Scan(&hasWebhookTable); err != nil {
		return fmt.Errorf("inspect Webhook credential schema: %w", err)
	}
	if hasWebhookTable {
		var hasEncryptedWebhook bool
		if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM webhook_target)").Scan(&hasEncryptedWebhook); err != nil {
			return fmt.Errorf("inspect encrypted Webhook credentials: %w", err)
		}
		hasEncryptedCredentials = hasEncryptedCredentials || hasEncryptedWebhook
	}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, hasEncryptedCredentials)
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin instance credential migration: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id::text, password FROM instance WHERE password_ciphertext IS NULL ORDER BY id")
	if err != nil {
		return fmt.Errorf("read plaintext instance credentials: %w", err)
	}
	defer rows.Close()
	type plaintextCredential struct {
		id       uuid.UUID
		password string
	}
	credentials := make([]plaintextCredential, 0)
	for rows.Next() {
		var idText, password string
		if err := rows.Scan(&idText, &password); err != nil {
			return fmt.Errorf("scan plaintext instance credential: %w", err)
		}
		id, err := uuid.Parse(idText)
		if err != nil {
			return fmt.Errorf("parse instance identifier during credential migration: %w", err)
		}
		credentials = append(credentials, plaintextCredential{id: id, password: password})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close plaintext instance credentials: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read plaintext instance credentials: %w", err)
	}
	for _, credential := range credentials {
		ciphertext, keyVersion, err := keyring.EncryptPassword(credential.id, credential.password)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE instance
			SET password_ciphertext = $2, password_key_version = $3
			WHERE id = $1 AND password_ciphertext IS NULL`, credential.id, ciphertext, keyVersion); err != nil {
			return fmt.Errorf("write encrypted instance credential: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit instance credential migration: %w", err)
	}
	return nil
}
