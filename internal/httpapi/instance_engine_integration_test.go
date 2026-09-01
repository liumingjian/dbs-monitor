package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// 接入时不填 bootstrap database 也要能建成：库名只是建连接用的，不限定监控范围，
// PostgreSQL 缺省连 postgres。同时确认引擎被记下来并回到接口上。
func TestOnboardingWithoutBootstrapDatabaseDefaultsToPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_engine_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	server := httptest.NewTLSServer(httpapi.NewHandler(platform, newCurrentFixedClock(), keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}

	// 请求体里既没有 engine 也没有 database —— 两者都该由服务端补齐。
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", map[string]any{
		"name":     "no-bootstrap-database",
		"host":     env("PGHOST", "localhost"),
		"port":     envInt("PGPORT", 55432),
		"username": env("PGUSER", "dbs_monitor"),
		"password": env("PGPASSWORD", "dbs_monitor"),
	}, "")
	body := readResponseBody(t, created, "created instance")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create instance status = %d, want 201: %s", created.StatusCode, body)
	}
	var createBody api.InstanceCreated
	if err := json.Unmarshal(body, &createBody); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	if createBody.Instance.Engine != api.InstanceEnginePostgreSQL {
		t.Fatalf("created engine = %q, want POSTGRESQL", createBody.Instance.Engine)
	}
	if createBody.Instance.Database != "postgres" {
		t.Fatalf("created bootstrap database = %q, want postgres", createBody.Instance.Database)
	}

	var storedEngine, storedDatabase string
	if err := pool.QueryRow(ctx,
		"SELECT engine, database_name FROM instance WHERE id = $1", createBody.Instance.Id,
	).Scan(&storedEngine, &storedDatabase); err != nil {
		t.Fatalf("read stored instance: %v", err)
	}
	if storedEngine != "POSTGRESQL" || storedDatabase != "postgres" {
		t.Fatalf("stored engine/database = %q/%q, want POSTGRESQL/postgres", storedEngine, storedDatabase)
	}

	fetched := requestJSON(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/v1/instances/%s", server.URL, createBody.Instance.Id), nil, "")
	fetchedBody := readResponseBody(t, fetched, "fetched instance")
	if fetched.StatusCode != http.StatusOK {
		t.Fatalf("get instance status = %d, want 200: %s", fetched.StatusCode, fetchedBody)
	}
	var fetchedInstance api.Instance
	if err := json.Unmarshal(fetchedBody, &fetchedInstance); err != nil {
		t.Fatalf("decode fetched instance: %v", err)
	}
	if fetchedInstance.Engine != api.InstanceEnginePostgreSQL {
		t.Fatalf("fetched engine = %q, want POSTGRESQL", fetchedInstance.Engine)
	}

	// 未知引擎必须在拨号之前就被挡掉，而不是拿着它去连一台 PostgreSQL。
	rejected := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", map[string]any{
		"name":     "unknown-engine",
		"engine":   "MYSQL",
		"host":     env("PGHOST", "localhost"),
		"port":     envInt("PGPORT", 55432),
		"username": env("PGUSER", "dbs_monitor"),
		"password": env("PGPASSWORD", "dbs_monitor"),
	}, "")
	rejectedBody := readResponseBody(t, rejected, "rejected instance")
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("create with unknown engine status = %d, want 400: %s", rejected.StatusCode, rejectedBody)
	}
	var errorBody api.Error
	if err := json.Unmarshal(rejectedBody, &errorBody); err != nil {
		t.Fatalf("decode rejection: %v", err)
	}
	if errorBody.Error.Code != api.VALIDATIONFAILED {
		t.Fatalf("rejection code = %q, want VALIDATION_FAILED", errorBody.Error.Code)
	}
}
