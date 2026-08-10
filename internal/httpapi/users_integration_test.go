package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestUserLifecycleAndPasswordFlows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	platform := openUserTestDatabase(t, ctx)
	if err := SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed administrator: %v", err)
	}
	server := httptest.NewTLSServer(NewHandler(platform, clock.Real{}).Routes())
	defer server.Close()

	admin := userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, admin, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}), http.StatusNoContent)

	me := userJSONRequest(t, admin, http.MethodGet, server.URL+"/api/v1/me", nil)
	assertUserStatus(t, me, http.StatusOK)
	var adminUser struct {
		ID string `json:"id"`
	}
	decodeUserJSON(t, me, &adminUser)

	created := userJSONRequest(t, admin, http.MethodPost, server.URL+"/api/v1/users", map[string]any{
		"username": "operator", "role": "READONLY",
	})
	assertUserStatus(t, created, http.StatusCreated)
	var createdUser struct {
		InitialPassword string `json:"initial_password"`
		User            struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decodeUserJSON(t, created, &createdUser)
	if len(createdUser.InitialPassword) < 12 || createdUser.User.ID == "" {
		t.Fatalf("created user response = %+v", createdUser)
	}

	operator := userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": createdUser.InitialPassword,
	}), http.StatusNoContent)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusOK)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodPost, server.URL+"/api/v1/users", map[string]any{
		"username": "forbidden", "role": "READONLY",
	}), http.StatusForbidden)

	short := userJSONRequest(t, operator, http.MethodPut, server.URL+"/api/v1/password", map[string]any{
		"old_password": createdUser.InitialPassword, "new_password": "短口令短",
	})
	assertUserStatus(t, short, http.StatusBadRequest)
	wrongOld := userJSONRequest(t, operator, http.MethodPut, server.URL+"/api/v1/password", map[string]any{
		"old_password": "wrong old password", "new_password": "new password long enough",
	})
	assertUserStatus(t, wrongOld, http.StatusBadRequest)
	changed := userJSONRequest(t, operator, http.MethodPut, server.URL+"/api/v1/password", map[string]any{
		"old_password": createdUser.InitialPassword, "new_password": "new password long enough",
	})
	assertUserStatus(t, changed, http.StatusNoContent)
	failedOldLogin := userJSONRequest(t, userTestClient(t, server), http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": createdUser.InitialPassword,
	})
	assertUserStatus(t, failedOldLogin, http.StatusUnauthorized)

	reset := userJSONRequest(t, admin, http.MethodPost, server.URL+"/api/v1/users/"+createdUser.User.ID+"/password", nil)
	assertUserStatus(t, reset, http.StatusOK)
	var resetBody struct {
		Password string `json:"password"`
	}
	decodeUserJSON(t, reset, &resetBody)
	if len(resetBody.Password) < 12 {
		t.Fatalf("reset password length = %d, want at least 12", len(resetBody.Password))
	}
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusUnauthorized)

	operator = userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": resetBody.Password,
	}), http.StatusNoContent)
	disabled := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+createdUser.User.ID+"/status", map[string]any{"enabled": false})
	assertUserStatus(t, disabled, http.StatusOK)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusUnauthorized)
	assertUserStatus(t, userJSONRequest(t, userTestClient(t, server), http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": resetBody.Password,
	}), http.StatusUnauthorized)

	selfDisable := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+adminUser.ID+"/status", map[string]any{"enabled": false})
	assertUserError(t, selfDisable, http.StatusBadRequest, errSelfDisable.Error())
	selfDowngrade := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+adminUser.ID+"/role", map[string]any{"role": "READONLY"})
	assertUserError(t, selfDowngrade, http.StatusBadRequest, errSelfDowngrade.Error())
}

func TestPlatformAdminGuardSerializesConcurrentMutations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	platform := openUserTestDatabase(t, ctx)
	first, second := uuid.New(), uuid.New()
	for _, user := range []struct {
		id       uuid.UUID
		username string
	}{{first, "first-admin"}, {second, "second-admin"}} {
		if _, err := platform.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
			VALUES ($1, $2, 'hash', 'PLATFORM_ADMIN')`, user.id, user.username); err != nil {
			t.Fatalf("insert %s: %v", user.username, err)
		}
	}

	handler := NewHandler(platform, nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- handler.setUserEnabled(ctx, second, first, false)
	}()
	go func() {
		<-start
		results <- handler.setUserRole(ctx, first, second, "READONLY")
	}()
	close(start)

	succeeded, rejected := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, errLastPlatformAdmin):
			rejected++
		default:
			t.Fatalf("concurrent mutation error = %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent results = %d succeeded, %d rejected; want 1 and 1", succeeded, rejected)
	}

	var enabledAdmins int
	if err := platform.QueryRow(ctx, `SELECT count(*) FROM app_user
		WHERE enabled AND role = 'PLATFORM_ADMIN'`).Scan(&enabledAdmins); err != nil {
		t.Fatalf("count enabled administrators: %v", err)
	}
	if enabledAdmins != 1 {
		t.Fatalf("enabled platform administrators = %d, want 1", enabledAdmins)
	}

	var survivor uuid.UUID
	if err := platform.QueryRow(ctx, `SELECT id FROM app_user
		WHERE enabled AND role = 'PLATFORM_ADMIN'`).Scan(&survivor); err != nil {
		t.Fatalf("find surviving administrator: %v", err)
	}
	if err := handler.setUserEnabled(ctx, survivor, survivor, false); !errors.Is(err, errSelfDisable) {
		t.Fatalf("self-disable error = %v, want %v", err, errSelfDisable)
	}
	if err := handler.setUserRole(ctx, survivor, survivor, "ALERT_ADMIN"); !errors.Is(err, errSelfDowngrade) {
		t.Fatalf("self-downgrade error = %v, want %v", err, errSelfDowngrade)
	}
}

func openUserTestDatabase(t *testing.T, ctx context.Context) *db.Pool {
	t.Helper()
	admin, err := sql.Open("pgx", userTestConnectionString(userTestEnv("PGDATABASE", "dbs_monitor")))
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	databaseName := fmt.Sprintf("dbs_monitor_users_test_%d", os.Getpid())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	migrationDB, err := sql.Open("pgx", userTestConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	if _, err := migrations.Up(ctx, migrationDB); err != nil {
		migrationDB.Close()
		t.Fatalf("migrate: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, userTestConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return &db.Pool{Pool: pool}
}

func userTestConnectionString(database string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		userTestEnv("PGHOST", "localhost"), userTestEnv("PGPORT", "55432"),
		userTestEnv("PGUSER", "dbs_monitor"), userTestEnv("PGPASSWORD", "dbs_monitor"), database)
}

func userTestEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func userTestClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	client := &http.Client{Transport: server.Client().Transport}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client.Jar = jar
	return client
}

func userJSONRequest(t *testing.T, client *http.Client, method, address string, body any) *http.Response {
	t.Helper()
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	request, err := http.NewRequest(method, address, &encoded)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return response
}

func assertUserStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		response.Body.Close()
		t.Fatalf("response status = %d, want %d", response.StatusCode, want)
	}
	contents, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(contents))
}

func decodeUserJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func assertUserError(t *testing.T, response *http.Response, status int, message string) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("response status = %d, want %d", response.StatusCode, status)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if !strings.Contains(body.Error.Message, message) {
		t.Fatalf("error message = %q, want %q", body.Error.Message, message)
	}
}
