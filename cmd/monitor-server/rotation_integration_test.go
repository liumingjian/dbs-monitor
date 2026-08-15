package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/migrations"
)

type rotationTestCredential struct {
	id       uuid.UUID
	password string
}

func TestMasterKeyRotationLockIsExclusiveAndReusable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connectionString := rotationConnectionString(env("PGDATABASE", "dbs_monitor"))
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()

	initialLock, err := acquireMasterKeyRotationLock(ctx, pool)
	if err != nil {
		t.Fatalf("acquire initial rotation lock: %v", err)
	}
	if _, err := acquireMasterKeyRotationLock(ctx, pool); !errors.Is(err, errMasterKeyRotationLockUnavailable) {
		t.Fatalf("contended rotation lock error = %v, want %v", err, errMasterKeyRotationLockUnavailable)
	}
	if err := initialLock.Release(); err != nil {
		t.Fatalf("release initial rotation lock: %v", err)
	}

	reacquiredLock, err := acquireMasterKeyRotationLock(ctx, pool)
	if err != nil {
		t.Fatalf("reacquire released rotation lock: %v", err)
	}
	if err := reacquiredLock.Release(); err != nil {
		t.Fatalf("release reacquired rotation lock: %v", err)
	}
}

func TestServerProcessLockIsExclusiveAndIndependentFromRotationLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connectionString := rotationConnectionString(env("PGDATABASE", "dbs_monitor"))
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()

	processLock, err := acquireServerProcessLock(ctx, pool)
	if err != nil {
		t.Fatalf("acquire initial server process lock: %v", err)
	}
	if _, err := acquireServerProcessLock(ctx, pool); !errors.Is(err, errServerProcessLockUnavailable) {
		t.Fatalf("contended server process lock error = %v, want %v", err, errServerProcessLockUnavailable)
	}
	rotationLock, err := acquireMasterKeyRotationLock(ctx, pool)
	if err != nil {
		t.Fatalf("rotation lock was coupled to server process lock: %v", err)
	}
	if err := rotationLock.Release(); err != nil {
		t.Fatalf("release independent rotation lock: %v", err)
	}
	if err := processLock.Release(); err != nil {
		t.Fatalf("release server process lock: %v", err)
	}

	reacquiredLock, err := acquireServerProcessLock(ctx, pool)
	if err != nil {
		t.Fatalf("reacquire released server process lock: %v", err)
	}
	if err := reacquiredLock.Release(); err != nil {
		t.Fatalf("release reacquired server process lock: %v", err)
	}
	if errServerProcessLockUnavailable.Error() == errMasterKeyRotationLockUnavailable.Error() {
		t.Fatal("server process and rotation lock errors must be distinct")
	}
}

func TestRotateCredentialKeyringIsAtomicAndRerunnable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_key_rotation_%d", os.Getpid())
	admin := openRotationSQL(t, env("PGDATABASE", "dbs_monitor"))
	t.Cleanup(func() { admin.Close() })
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop stale test database: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	credentialDirectory := createTestCredentialDirectory(t)
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
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	credentials := []rotationTestCredential{
		{id: uuid.MustParse("00000000-0000-0000-0000-000000000721"), password: "rotation-password-one"},
		{id: uuid.MustParse("00000000-0000-0000-0000-000000000722"), password: "rotation-password-two"},
	}
	for index, target := range credentials {
		ciphertext, version, err := keyring.EncryptPassword(target.id, target.password)
		if err != nil {
			t.Fatalf("encrypt credential %d: %v", index, err)
		}
		if _, err := pool.Exec(ctx, `WITH created_identity AS (
			INSERT INTO instance_identity (id, name) VALUES ($1, $2) RETURNING id
		)
			INSERT INTO instance
			(id, name, host, port, database_name, username, password_ciphertext, password_key_version)
			SELECT id, $2, 'localhost', 5432, 'postgres', 'monitor', $3, $4 FROM created_identity`,
			target.id, fmt.Sprintf("rotation-%d", index), ciphertext, version); err != nil {
			t.Fatalf("insert credential %d: %v", index, err)
		}
	}
	smtpCiphertext, smtpVersion, err := keyring.EncryptSMTPPassword("smtp-rotation-value")
	if err != nil {
		t.Fatalf("encrypt SMTP authentication value: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO smtp_channel
		(enabled, host, port, from_address, recipient, auth_type, username,
		 auth_ciphertext, auth_key_version, tls_mode, updated_at)
		VALUES (true, 'smtp.example.com', 465, 'monitor@example.com', 'dba@example.com',
		 'PLAIN', 'monitor', $1, $2, 'IMPLICIT', now())`, smtpCiphertext, smtpVersion); err != nil {
		t.Fatalf("insert SMTP authentication value: %v", err)
	}
	webhookID := uuid.MustParse("00000000-0000-0000-0000-000000000080")
	webhookValue, webhookVersion, err := keyring.EncryptWebhookSigningValue(webhookID, "webhook-rotation-value")
	if err != nil {
		t.Fatalf("encrypt Webhook signing value: %v", err)
	}
	webhookHeader, headerVersion, err := keyring.EncryptWebhookSignatureHeader(webhookID, "X-DBS-Signature")
	if err != nil || headerVersion != webhookVersion {
		t.Fatalf("encrypt Webhook signature header: version %d, error %v", headerVersion, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO webhook_target
		(id, name, enabled, url, signing_value_ciphertext, signature_header_ciphertext,
		 signing_key_version, created_at, updated_at)
		VALUES ($1, 'rotation-webhook', true, 'https://example.com/webhook', $2, $3, $4, now(), now())`,
		webhookID, webhookValue, webhookHeader, webhookVersion); err != nil {
		t.Fatalf("insert Webhook signing configuration: %v", err)
	}
	stagedKey := make([]byte, 32)
	if _, err := rand.Read(stagedKey); err != nil {
		t.Fatalf("generate staged key fixture: %v", err)
	}
	encodedStagedKey := []byte(base64.StdEncoding.EncodeToString(stagedKey) + "\n")
	if err := os.WriteFile(filepath.Join(credentialDirectory, "master-key-v2"), encodedStagedKey, 0o600); err != nil {
		t.Fatalf("write staged key fixture: %v", err)
	}

	result, err := rotateCredentialKeyring(ctx, platform, credentialDirectory)
	if err != nil {
		t.Fatalf("rotate to v2: %v", err)
	}
	if result.KeyVersion != 2 || result.CredentialsRotated != int64(len(credentials)+2) {
		t.Fatalf("rotation result = %+v, want version 2 and %d credentials", result, len(credentials)+2)
	}
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, credentials, 2)
	assertRotatedSMTPValue(t, ctx, pool, credentialDirectory, 2)
	assertRotatedWebhookValues(t, ctx, pool, credentialDirectory, webhookID, 2)
	assertRotatedNotificationSnapshot(t, ctx, pool, credentialDirectory, webhookID, 2)
	assertCredentialKeyringFiles(t, credentialDirectory, []string{"current", "master-key-v2"})

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
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, credentials, 2)
	assertRotatedSMTPValue(t, ctx, pool, credentialDirectory, 2)
	assertRotatedWebhookValues(t, ctx, pool, credentialDirectory, webhookID, 2)
	assertCredentialKeyringFiles(t, credentialDirectory, []string{"current", "master-key-v2", "master-key-v3"})
	interruptedKeyring, err := instance.OpenCredentialKeyring(credentialDirectory, true)
	if err != nil {
		t.Fatalf("open interrupted keyring: %v", err)
	}
	if err := interruptedKeyring.RemoveUnreferencedKeys(ctx, instance.New(platform)); err == nil || !strings.Contains(err.Error(), "still has 4 database references") {
		t.Fatalf("cleanup with old-key references error = %v, want reference diagnostic", err)
	}
	assertCredentialKeyringFiles(t, credentialDirectory, []string{"current", "master-key-v2", "master-key-v3"})

	if _, err := pool.Exec(ctx, "DROP TRIGGER fail_v3_rotation ON instance; DROP FUNCTION fail_v3_rotation()"); err != nil {
		t.Fatalf("remove interruption trigger: %v", err)
	}
	result, err = rotateCredentialKeyring(ctx, platform, credentialDirectory)
	if err != nil {
		t.Fatalf("resume rotation to v3: %v", err)
	}
	if result.KeyVersion != 3 || result.CredentialsRotated != int64(len(credentials)+2) {
		t.Fatalf("resumed rotation result = %+v, want version 3 and %d credentials", result, len(credentials)+2)
	}
	assertRotatedCredentials(t, ctx, pool, credentialDirectory, credentials, 3)
	assertRotatedSMTPValue(t, ctx, pool, credentialDirectory, 3)
	assertRotatedWebhookValues(t, ctx, pool, credentialDirectory, webhookID, 3)
	assertRotatedNotificationSnapshot(t, ctx, pool, credentialDirectory, webhookID, 3)
	assertCredentialKeyringFiles(t, credentialDirectory, []string{"current", "master-key-v3"})
}

func assertRotatedNotificationSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, directory string, targetID uuid.UUID, wantVersion int32) {
	t.Helper()
	snapshot, err := notify.NewChannelSnapshotStore(notificationSnapshotPath(directory)).Load()
	if err != nil {
		t.Fatalf("load rotated notification snapshot: %v", err)
	}
	if snapshot.SMTP == nil || snapshot.SMTP.AuthKeyVersion == nil || *snapshot.SMTP.AuthKeyVersion != wantVersion {
		t.Fatalf("snapshot SMTP key version = %+v, want %d", snapshot.SMTP, wantVersion)
	}
	if len(snapshot.Webhooks) != 1 || snapshot.Webhooks[0].ID != targetID.String() || snapshot.Webhooks[0].SigningKeyVersion != wantVersion {
		t.Fatalf("snapshot Webhook configuration = %+v, want target %s at key version %d", snapshot.Webhooks, targetID, wantVersion)
	}
	var smtpCiphertext, signingCiphertext, headerCiphertext []byte
	if err := pool.QueryRow(ctx, `SELECT auth_ciphertext FROM smtp_channel WHERE singleton`).Scan(&smtpCiphertext); err != nil {
		t.Fatalf("read rotated SMTP ciphertext: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT signing_value_ciphertext, signature_header_ciphertext FROM webhook_target WHERE id = $1`, targetID).
		Scan(&signingCiphertext, &headerCiphertext); err != nil {
		t.Fatalf("read rotated Webhook ciphertext: %v", err)
	}
	if !bytes.Equal(snapshot.SMTP.AuthCiphertext, smtpCiphertext) ||
		!bytes.Equal(snapshot.Webhooks[0].SigningValueCiphertext, signingCiphertext) ||
		!bytes.Equal(snapshot.Webhooks[0].SignatureHeaderCiphertext, headerCiphertext) {
		t.Fatal("rotated notification snapshot ciphertext differs from database")
	}
}

func assertRotatedWebhookValues(t *testing.T, ctx context.Context, pool *pgxpool.Pool, directory string, targetID uuid.UUID, wantVersion int32) {
	t.Helper()
	keyring, err := instance.OpenCredentialKeyring(directory, true)
	if err != nil {
		t.Fatalf("reopen keyring for Webhook: %v", err)
	}
	var valueCiphertext, headerCiphertext []byte
	var version int32
	if err := pool.QueryRow(ctx, `SELECT signing_value_ciphertext, signature_header_ciphertext, signing_key_version
		FROM webhook_target WHERE id = $1`, targetID).Scan(&valueCiphertext, &headerCiphertext, &version); err != nil {
		t.Fatalf("read Webhook signing configuration: %v", err)
	}
	if version != wantVersion {
		t.Fatalf("Webhook signing key version = %d, want %d", version, wantVersion)
	}
	if value, err := keyring.DecryptWebhookSigningValue(targetID, valueCiphertext, version); err != nil || value != "webhook-rotation-value" {
		t.Fatalf("decrypt Webhook signing value = %q, %v", value, err)
	}
	if header, err := keyring.DecryptWebhookSignatureHeader(targetID, headerCiphertext, version); err != nil || header != "X-DBS-Signature" {
		t.Fatalf("decrypt Webhook signature header = %q, %v", header, err)
	}
}

func assertRotatedSMTPValue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, directory string, wantVersion int32) {
	t.Helper()
	keyring, err := instance.OpenCredentialKeyring(directory, true)
	if err != nil {
		t.Fatalf("reopen keyring for SMTP: %v", err)
	}
	var ciphertext []byte
	var version int32
	if err := pool.QueryRow(ctx, `SELECT auth_ciphertext, auth_key_version
		FROM smtp_channel WHERE singleton`).Scan(&ciphertext, &version); err != nil {
		t.Fatalf("read SMTP authentication value: %v", err)
	}
	if version != wantVersion {
		t.Fatalf("SMTP authentication key version = %d, want %d", version, wantVersion)
	}
	value, err := keyring.DecryptSMTPPassword(ciphertext, version)
	if err != nil || value != "smtp-rotation-value" {
		t.Fatalf("decrypt SMTP authentication value = %q, %v", value, err)
	}
}

func assertRotatedCredentials(t *testing.T, ctx context.Context, pool *pgxpool.Pool, directory string, credentials []rotationTestCredential, wantVersion int32) {
	t.Helper()
	keyring, err := instance.OpenCredentialKeyring(directory, true)
	if err != nil {
		t.Fatalf("reopen keyring: %v", err)
	}
	for _, target := range credentials {
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

func assertCredentialKeyringFiles(t *testing.T, directory string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read keyring directory: %v", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "current" || strings.HasPrefix(name, "master-key-v") {
			names = append(names, name)
		}
	}
	if !slices.Equal(names, want) {
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
		env("PGHOST", "localhost"), env("PGPORT", "55432"), env("PGUSER", "dbs_monitor"),
		env("PGPASSWORD", "dbs_monitor"), database)
}
