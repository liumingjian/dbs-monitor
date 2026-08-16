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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/platformevent"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestUserLifecycleAndPasswordFlows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	platform := openUserTestDatabase(t, ctx)
	if err := SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed administrator: %v", err)
	}
	server := httptest.NewTLSServer(NewHandler(platform, clock.Real{}, nil).Routes())
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
	for _, path := range []string{
		"/api/v1/diagnostics/health",
		"/api/v1/diagnostics/disk",
		"/api/v1/diagnostics/scheduler",
		"/api/v1/diagnostics/partitions",
		"/api/v1/diagnostics/certificate",
		"/api/v1/diagnostics/keyring",
		"/api/v1/diagnostics/platform",
	} {
		assertUserStatus(t, userJSONRequest(t, admin, http.MethodGet, server.URL+path, nil), http.StatusOK)
		assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+path, nil), http.StatusForbidden)
	}
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
	assertUserPlatformEvent(t, ctx, platform, platformevent.LoginFailed, "", "operator", "")

	reset := userJSONRequest(t, admin, http.MethodPost, server.URL+"/api/v1/users/"+createdUser.User.ID+"/password", nil)
	assertUserStatus(t, reset, http.StatusOK)
	var resetBody struct {
		Password string `json:"password"`
	}
	decodeUserJSON(t, reset, &resetBody)
	if len(resetBody.Password) < 12 {
		t.Fatalf("reset password length = %d, want at least 12", len(resetBody.Password))
	}
	assertUserPlatformEvent(t, ctx, platform, platformevent.UserPasswordReset, "admin", "", createdUser.User.ID)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusUnauthorized)

	operator = userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": resetBody.Password,
	}), http.StatusNoContent)
	secondOperatorSession := userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, secondOperatorSession, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": resetBody.Password,
	}), http.StatusNoContent)
	disabled := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+createdUser.User.ID+"/status", map[string]any{"enabled": false})
	assertUserStatus(t, disabled, http.StatusOK)
	assertUserPlatformEvent(t, ctx, platform, platformevent.UserStatusChanged, "admin", "", createdUser.User.ID)
	assertUserStatus(t, userJSONRequest(t, operator, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusUnauthorized)
	assertUserStatus(t, userJSONRequest(t, secondOperatorSession, http.MethodGet, server.URL+"/api/v1/users", nil), http.StatusUnauthorized)
	var disabledSessionCount int
	if err := platform.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE user_id = $1`, createdUser.User.ID).Scan(&disabledSessionCount); err != nil {
		t.Fatalf("count disabled user sessions: %v", err)
	}
	if disabledSessionCount != 0 {
		t.Fatalf("disabled user sessions = %d, want 0", disabledSessionCount)
	}
	assertUserStatus(t, userJSONRequest(t, userTestClient(t, server), http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "operator", "password": resetBody.Password,
	}), http.StatusUnauthorized)

	selfDisable := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+adminUser.ID+"/status", map[string]any{"enabled": false})
	assertUserError(t, selfDisable, http.StatusBadRequest, errSelfDisable.Error())
	assertUserPlatformEvent(t, ctx, platform, platformevent.UserStatusChangeRejected, "admin", "", adminUser.ID)
	events := userJSONRequest(t, admin, http.MethodGet, server.URL+"/api/v1/platform-events", nil)
	assertUserStatus(t, events, http.StatusOK)
	var eventFeed []struct {
		Kind    string `json:"kind"`
		Actor   string `json:"actor"`
		Subject string `json:"subject_id"`
	}
	decodeUserJSON(t, events, &eventFeed)
	if len(eventFeed) == 0 || eventFeed[0].Kind != platformevent.UserStatusChangeRejected ||
		eventFeed[0].Actor != "admin" || eventFeed[0].Subject != adminUser.ID {
		t.Fatalf("latest platform event = %+v, want attributed rejected self-disable", eventFeed)
	}
	selfDowngrade := userJSONRequest(t, admin, http.MethodPut, server.URL+"/api/v1/users/"+adminUser.ID+"/role", map[string]any{"role": "READONLY"})
	assertUserError(t, selfDowngrade, http.StatusBadRequest, errSelfDowngrade.Error())
}

func assertUserPlatformEvent(t *testing.T, ctx context.Context, platform *db.Pool, kind, actorUsername, actorSubject, subjectID string) {
	t.Helper()
	var gotActorUsername, gotActorSubject, gotSubjectID string
	err := platform.QueryRow(ctx, `SELECT coalesce(actor.username, ''), coalesce(event.actor_subject, ''),
		coalesce(event.subject_id::text, '')
		FROM platform_event event
		LEFT JOIN app_user actor ON actor.id = event.actor_id
		WHERE event.kind = $1
		ORDER BY event.occurred_at DESC, event.id DESC
		LIMIT 1`, kind).Scan(&gotActorUsername, &gotActorSubject, &gotSubjectID)
	if err != nil {
		t.Fatalf("read %s platform event: %v", kind, err)
	}
	if gotActorUsername != actorUsername || gotActorSubject != actorSubject || gotSubjectID != subjectID {
		t.Fatalf("%s attribution = actor %q, subject %q, target %q; want %q, %q, %q",
			kind, gotActorUsername, gotActorSubject, gotSubjectID, actorUsername, actorSubject, subjectID)
	}
}

func TestPlatformAdminGuardSerializesConcurrentMutations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const (
		firstAdminUsername = "first-admin"
		firstAdminPassword = "correct horse battery staple"
	)

	platform := openUserTestDatabase(t, ctx)
	if err := SeedAdmin(ctx, platform, firstAdminUsername, firstAdminPassword); err != nil {
		t.Fatalf("seed first administrator: %v", err)
	}

	handler := NewHandler(platform, clock.Real{}, nil)
	server := httptest.NewTLSServer(handler.Routes())
	defer server.Close()

	firstAdminClient := userTestClient(t, server)
	assertUserStatus(t, userJSONRequest(t, firstAdminClient, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": firstAdminUsername, "password": firstAdminPassword,
	}), http.StatusNoContent)
	currentUserResponse := userJSONRequest(t, firstAdminClient, http.MethodGet, server.URL+"/api/v1/me", nil)
	assertUserStatus(t, currentUserResponse, http.StatusOK)
	var firstAdmin struct {
		ID uuid.UUID `json:"id"`
	}
	decodeUserJSON(t, currentUserResponse, &firstAdmin)

	createUserResponse := userJSONRequest(t, firstAdminClient, http.MethodPost, server.URL+"/api/v1/users", map[string]any{
		"username": "second-admin", "role": "PLATFORM_ADMIN",
	})
	assertUserStatus(t, createUserResponse, http.StatusCreated)
	var secondAdmin struct {
		User struct {
			ID uuid.UUID `json:"id"`
		} `json:"user"`
	}
	decodeUserJSON(t, createUserResponse, &secondAdmin)
	firstAdminID := firstAdmin.ID
	secondAdminID := secondAdmin.User.ID

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		results <- handler.setUserEnabled(ctx, secondAdminID, firstAdminID, false)
	}()
	go func() {
		<-start
		results <- handler.setUserRole(ctx, firstAdminID, secondAdminID, "READONLY")
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
	if _, err := migrations.Up(ctx, migrationDB, filepath.Join(t.TempDir(), "credentials")); err != nil {
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
