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
	credentialSchemaVersion         = 6
	plaintextPasswordRemovalVersion = 7
)

func Open(connectionString string) (*sql.DB, error) {
	database, err := sql.Open("pgx", connectionString)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	return database, nil
}

func Up(ctx context.Context, database *sql.DB, credentialDirectory string) (int, error) {
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
	applied := 0
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
	return applied + len(results), nil
}

func migrateInstanceCredentials(ctx context.Context, database *sql.DB, credentialDirectory string, backfillPlaintext bool) error {
	var hasEncryptedCredentials bool
	if err := database.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM instance WHERE password_ciphertext IS NOT NULL)").Scan(&hasEncryptedCredentials); err != nil {
		return fmt.Errorf("inspect encrypted instance credentials: %w", err)
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
