package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestMissingCredentialKeyringKeepsControlPlaneAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	databaseName := fmt.Sprintf("dbs_monitor_keyring_startup_%d", os.Getpid())
	admin := openRotationSQL(t, env("PGDATABASE", "dbs_monitor"))
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
		admin.Close()
		t.Fatalf("drop stale test database: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
		admin.Close()
	})

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	migrationDB := openRotationSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		migrationDB.Close()
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()
	if err := os.Mkdir(credentialDirectory, 0o700); err != nil {
		t.Fatalf("precreate credential directory: %v", err)
	}
	generatedAt := time.Date(2026, time.August, 15, 11, 0, 0, 0, time.UTC)
	var generationJournal bytes.Buffer
	generationHealth := platformhealth.NewStore("3.0.0", generatedAt.Add(-time.Minute), log.New(&generationJournal, "", 0))

	pool, err := pgxpool.New(ctx, rotationConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	if _, err := pool.Exec(ctx, `INSERT INTO smtp_channel (
		enabled, host, port, from_address, recipient, auth_type, tls_mode, updated_at
	) VALUES (false, 'smtp.example.com', 587, 'monitor@example.com', 'ops@example.com', 'NONE', 'STARTTLS', $1)`, generatedAt); err != nil {
		t.Fatalf("insert non-secret SMTP configuration: %v", err)
	}
	originalKeyring, err := openStartupCredentialKeyring(
		ctx, platform, credentialDirectory, generationHealth, log.New(&generationJournal, "", 0), generatedAt,
	)
	if err != nil {
		t.Fatalf("initialize original keyring: %v", err)
	}
	if !strings.Contains(generationJournal.String(), `"event":"credential_key_generated"`) {
		t.Fatalf("first-start journal = %q, want explicit generation event", generationJournal.String())
	}
	if source := generationHealth.Source(platformhealth.SourceCredentialKeyring); source.Status != platformhealth.StatusOK {
		t.Fatalf("generated keyring health = %+v, want OK", source)
	}
	instanceID := uuid.MustParse("00000000-0000-0000-0000-000000000131")
	ciphertext, keyVersion, err := originalKeyring.EncryptPassword(instanceID, "issue-131-secret")
	if err != nil {
		t.Fatalf("encrypt test credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `WITH identity AS (
		INSERT INTO instance_identity (id, name) VALUES ($1, 'issue-131') RETURNING id
	)
		INSERT INTO instance (id, name, host, port, database_name, username, password_ciphertext, password_key_version)
		SELECT id, 'issue-131', 'localhost', 5432, 'postgres', 'monitor', $2, $3 FROM identity`,
		instanceID, ciphertext, keyVersion); err != nil {
		t.Fatalf("insert encrypted test credential: %v", err)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "issue-131-admin-password"); err != nil {
		t.Fatalf("seed test administrator: %v", err)
	}
	if err := os.RemoveAll(credentialDirectory); err != nil {
		t.Fatalf("remove credential keyring: %v", err)
	}

	var journal bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), log.New(&journal, "", 0))
	keyring, err := openStartupCredentialKeyring(ctx, platform, credentialDirectory, health, log.New(&journal, "", 0), now)
	if err != nil {
		t.Fatalf("open missing startup keyring: %v", err)
	}
	if _, err := os.Stat(credentialDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup recreated missing credential directory: %v", err)
	}
	keyringHealth := health.Source(platformhealth.SourceCredentialKeyring)
	if keyringHealth.Status != platformhealth.StatusFailed || keyringHealth.Code != string(instance.CredentialFaultMissingKey) {
		t.Fatalf("keyring health = %+v, want FAILED/%s", keyringHealth, instance.CredentialFaultMissingKey)
	}

	handler := httpapi.NewHandlerWithPlatformHealth(
		platform, clock.Real{}, keyring, monitorpg.DirectDialer{}, "3.0.0", health,
	)
	server := httptest.NewTLSServer(handler.Routes())
	defer server.Close()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "issue-131-admin-password"})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create login request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("login with missing keyring: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	_, err = keyring.DecryptPassword(instanceID, ciphertext, keyVersion)
	var fault *instance.CredentialFault
	if !errors.As(err, &fault) || fault.Code != instance.CredentialFaultMissingKey {
		t.Fatalf("dependent decryption error = %v, want missing-key fault", err)
	}
}
