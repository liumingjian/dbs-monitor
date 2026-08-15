//go:build acceptance

package acceptance_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestAcceptance_AC_09_F1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	platform := openDiagnosticAcceptanceDatabase(t, ctx)
	if err := httpapi.SeedAdmin(ctx, platform, "diagnostic-admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed bootstrap administrator: %v", err)
	}
	health := platformhealth.NewStore("3.0.0", time.Now().Add(-time.Hour), log.New(io.Discard, "", 0))
	server := httptest.NewTLSServer(httpapi.NewHandlerWithPlatformHealth(
		platform, clock.Real{}, nil, nil, "3.0.0", health,
	).Routes())
	defer server.Close()

	admin := diagnosticAcceptanceClient(t, server)
	loginDiagnosticAcceptanceUser(t, ctx, admin, "diagnostic-admin", "correct horse battery staple")
	users := []struct {
		username string
		role     api.Role
		client   *api.ClientWithResponses
	}{
		{username: "diagnostic-readonly", role: api.READONLY},
		{username: "diagnostic-alert-admin", role: api.ALERTADMIN},
	}
	for index := range users {
		user := &users[index]
		created, err := admin.CreateUserWithResponse(ctx, api.UserCreateInput{Username: user.username, Role: user.role})
		if err != nil {
			t.Fatalf("create %s through API: %v", user.role, err)
		}
		if created.StatusCode() != http.StatusCreated || created.JSON201 == nil {
			t.Fatalf("create %s status/body = %d/%s", user.role, created.StatusCode(), created.Body)
		}
		user.client = diagnosticAcceptanceClient(t, server)
		loginDiagnosticAcceptanceUser(t, ctx, user.client, user.username, created.JSON201.InitialPassword)
	}

	operations := diagnosticAcceptanceOperations()
	for _, operation := range operations {
		status, err := operation.call(ctx, admin)
		if err != nil {
			t.Fatalf("administrator %s: %v", operation.name, err)
		}
		if status != http.StatusOK {
			t.Errorf("administrator %s status = %d, want 200", operation.name, status)
		}
		for _, user := range users {
			status, err := operation.call(ctx, user.client)
			if err != nil {
				t.Fatalf("%s %s: %v", user.role, operation.name, err)
			}
			if status != http.StatusForbidden {
				t.Errorf("%s %s status = %d, want explicit 403", user.role, operation.name, status)
			}
		}
	}
}

type diagnosticAcceptanceOperation struct {
	name string
	call func(context.Context, *api.ClientWithResponses) (int, error)
}

func diagnosticAcceptanceOperations() []diagnosticAcceptanceOperation {
	return []diagnosticAcceptanceOperation{
		{name: "getPlatformHealth", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetPlatformHealthWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getDiskDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetDiskDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getSchedulerDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetSchedulerDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getPartitionDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetPartitionDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getCertificateDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetCertificateDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getKeyringDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetKeyringDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
		{name: "getPlatformDiagnostics", call: func(ctx context.Context, client *api.ClientWithResponses) (int, error) {
			response, err := client.GetPlatformDiagnosticsWithResponse(ctx)
			return diagnosticStatus(response, err)
		}},
	}
}

type diagnosticResponse interface {
	StatusCode() int
}

func diagnosticStatus[T diagnosticResponse](response T, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	return response.StatusCode(), nil
}

func diagnosticAcceptanceClient(t *testing.T, server *httptest.Server) *api.ClientWithResponses {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	httpClient := &http.Client{Transport: server.Client().Transport, Jar: jar}
	client, err := api.NewClientWithResponses(server.URL, api.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("create generated API client: %v", err)
	}
	return client
}

func loginDiagnosticAcceptanceUser(t *testing.T, ctx context.Context, client *api.ClientWithResponses, username, password string) {
	t.Helper()
	response, err := client.CreateSessionWithResponse(ctx, api.CreateSessionJSONRequestBody{
		Username: username, Password: password,
	})
	if err != nil {
		t.Fatalf("login %s through API: %v", username, err)
	}
	if response.StatusCode() != http.StatusNoContent {
		t.Fatalf("login %s status/body = %d/%s", username, response.StatusCode(), response.Body)
	}
}

func openDiagnosticAcceptanceDatabase(t *testing.T, ctx context.Context) *db.Pool {
	t.Helper()
	databaseName := fmt.Sprintf("dbs_monitor_acceptance_75_%d_%d", os.Getpid(), time.Now().UnixNano())
	admin, err := sql.Open("pgx", diagnosticAcceptanceConnectionString(diagnosticAcceptanceEnv("PGDATABASE", "dbs_monitor")))
	if err != nil {
		t.Fatalf("open acceptance database administrator: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create acceptance database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(credentialDirectory, 0o700); err != nil {
		t.Fatalf("create acceptance credential directory: %v", err)
	}
	migrationDatabase, err := sql.Open("pgx", diagnosticAcceptanceConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open acceptance migration database: %v", err)
	}
	if _, err := migrations.Up(ctx, migrationDatabase, credentialDirectory); err != nil {
		_ = migrationDatabase.Close()
		t.Fatalf("migrate acceptance database: %v", err)
	}
	if err := migrationDatabase.Close(); err != nil {
		t.Fatalf("close acceptance migration database: %v", err)
	}

	pool, err := pgxpool.New(ctx, diagnosticAcceptanceConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open acceptance platform pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return &db.Pool{Pool: pool}
}

func diagnosticAcceptanceConnectionString(database string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		diagnosticAcceptanceEnv("PGHOST", "localhost"), diagnosticAcceptanceEnv("PGPORT", "55432"),
		diagnosticAcceptanceEnv("PGUSER", "dbs_monitor"), diagnosticAcceptanceEnv("PGPASSWORD", "dbs_monitor"), database)
}

func diagnosticAcceptanceEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
