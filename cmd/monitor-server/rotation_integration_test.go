package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestRotateCredentialKeyringIsAtomicAndRerunnable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_key_rotation_%d", os.Getpid())
	admin := openRotationSQL(t, rotationEnv("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	migrationDB := openRotationSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, rotationConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, true)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	instances := []struct {
		id       uuid.UUID
		password string
	}{
		{id: uuid.MustParse("00000000-0000-0000-0000-000000000721"), password: "rotation-password-one"},
		{id: uuid.MustParse("00000000-0000-0000-0000-000000000722"), password: "rotation-password-two"},
	}
	for index, target := range instances {
		ciphertext, version, err := keyring.EncryptPassword(target.id, target.password)
		if err != nil {
			t.Fatalf("encrypt credential %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO instance
			(id, name, host, port, database_name, username, password_ciphertext, password_key_version)
			VALUES ($1, $2, 'localhost', 5432, 'postgres', 'monitor', $3, $4)`,
			target.id, fmt.Sprintf("rotation-%d", index), ciphertext, version); err != nil {
			t.Fatalf("insert credential %d: %v", index, err)
		}
	}
	interruptedKey := make([]byte, 32)
	if _, err := rand.Read(interruptedKey); err != nil {
		t.Fatalf("generate interrupted key fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(credentialDirectory, "master-key-v2"), interruptedKey, 0o600); err != nil {
		t.Fatalf("write interrupted key fixture: %v", err)
	}

	result, err := rotateCredentialKeyring(ctx, platform, credentialDirectory)
	if err != nil {
		t.Fatalf("rotate to v2: %v", err)
	}
	if result.KeyVersion != 2 || result.CredentialsRotated != int64(len(instances)) {
		t.Fatalf("rotation result = %+v, want version 2 and %d credentials", result, len(instances))
	}
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, instances, 2)
	assertKeyVersions(t, credentialDirectory, []string{"current", "master-key-v2"})

	if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_v3_rotation() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.id = '00000000-0000-0000-0000-000000000722' AND NEW.password_key_version = 3 THEN
				RAISE EXCEPTION 'injected rotation interruption';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER fail_v3_rotation BEFORE UPDATE ON instance
		FOR EACH ROW EXECUTE FUNCTION fail_v3_rotation()`); err != nil {
		t.Fatalf("install interruption trigger: %v", err)
	}
	if _, err := rotateCredentialKeyring(ctx, platform, credentialDirectory); err == nil {
		t.Fatal("interrupted rotation succeeded")
	}
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, instances, 2)
	assertKeyVersions(t, credentialDirectory, []string{"current", "master-key-v2", "master-key-v3"})

	if _, err := pool.Exec(ctx, "DROP TRIGGER fail_v3_rotation ON instance; DROP FUNCTION fail_v3_rotation()"); err != nil {
		t.Fatalf("remove interruption trigger: %v", err)
	}
	result, err = rotateCredentialKeyring(ctx, platform, credentialDirectory)
	if err != nil {
		t.Fatalf("resume rotation to v3: %v", err)
	}
	if result.KeyVersion != 3 || result.CredentialsRotated != int64(len(instances)) {
		t.Fatalf("resumed rotation result = %+v, want version 3 and %d credentials", result, len(instances))
	}
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, instances, 3)
	assertKeyVersions(t, credentialDirectory, []string{"current", "master-key-v3"})
}

func assertRotatedCredentials(t *testing.T, ctx context.Context, pool *pgxpool.Pool, directory string, instances []struct {
	id       uuid.UUID
	password string
}, wantVersion int32) {
	t.Helper()
	keyring, err := instance.OpenCredentialKeyring(directory, true)
	if err != nil {
		t.Fatalf("reopen keyring: %v", err)
	}
	for _, target := range instances {
		var ciphertext []byte
		var version int32
		if err := pool.QueryRow(ctx, `SELECT password_ciphertext, password_key_version FROM instance WHERE id = $1`, target.id).
			Scan(&ciphertext, &version); err != nil {
			t.Fatalf("read credential %s: %v", target.id, err)
		}
		if version != wantVersion {
			t.Fatalf("credential %s key version = %d, want %d", target.id, version, wantVersion)
		}
		plaintext, err := keyring.DecryptPassword(target.id, ciphertext, version)
		if err != nil {
			t.Fatalf("decrypt credential %s: %v", target.id, err)
		}
		if plaintext != target.password {
			t.Fatalf("credential %s plaintext changed", target.id)
		}
	}
}

func assertKeyVersions(t *testing.T, directory string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read keyring directory: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() == "current" || len(entry.Name()) >= len("master-key-v") && entry.Name()[:len("master-key-v")] == "master-key-v" {
			names = append(names, entry.Name())
		}
	}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("keyring files = %v, want %v", names, want)
	}
}

func openRotationSQL(t *testing.T, database string) *sql.DB {
	t.Helper()
	databaseHandle, err := sql.Open("pgx", rotationConnectionString(database))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := databaseHandle.Ping(); err != nil {
		databaseHandle.Close()
		t.Fatalf("ping database: %v", err)
	}
	return databaseHandle
}

func rotationConnectionString(database string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		rotationEnv("PGHOST", "localhost"), rotationEnv("PGPORT", "55432"), rotationEnv("PGUSER", "dbs_monitor"),
		rotationEnv("PGPASSWORD", "dbs_monitor"), database)
}

func rotationEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
