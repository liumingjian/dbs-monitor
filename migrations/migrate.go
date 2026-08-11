package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

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
	lockConnection, err := database.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("reserve migration lock connection: %w", err)
	}
	if _, err := lockConnection.ExecContext(ctx, migrationAdvisoryLockSQL, migrationAdvisoryLockID); err != nil {
		lockConnection.Close()
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
	if err := migrateInstanceCredentials(ctx, database, credentialDirectory, current < plaintextPasswordRemovalVersion); err != nil {
		return applied, err
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

func migrateInstanceCredentials(ctx context.Context, database *sql.DB, credentialDirectory string, backfillPlaintext bool) error {
	var hasEncryptedCredentials bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM instance WHERE password_ciphertext IS NOT NULL)").Scan(&hasEncryptedCredentials); err != nil {
		return fmt.Errorf("inspect encrypted instance credentials: %w", err)
	}
	var hasSMTPTable bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass('public.smtp_channel') IS NOT NULL").Scan(&hasSMTPTable); err != nil {
		return fmt.Errorf("inspect SMTP credential schema: %w", err)
	}
	if hasSMTPTable {
		var hasEncryptedSMTP bool
		if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM smtp_channel WHERE auth_ciphertext IS NOT NULL)").Scan(&hasEncryptedSMTP); err != nil {
			return fmt.Errorf("inspect encrypted SMTP credential: %w", err)
		}
		hasEncryptedCredentials = hasEncryptedCredentials || hasEncryptedSMTP
	}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, hasEncryptedCredentials)
	if err != nil {
		return err
	}
	if !backfillPlaintext {
		return nil
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
