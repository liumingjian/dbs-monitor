package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestHTTPSAPIAndAgentPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_http_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(credentialDirectory, 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB); err != nil {
		t.Fatalf("migrate: %v", err)
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

	server := httptest.NewTLSServer(httpapi.NewHandler(platform, clock.Real{}, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar

	unauthenticated, err := client.Get(server.URL + "/api/v1/instances")
	if err != nil {
		t.Fatalf("send unauthenticated request: %v", err)
	}
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()

	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}
	login.Body.Close()

	targetPassword := env("PGPASSWORD", "dbs_monitor")
	instanceInput := map[string]any{
		"name": "target", "host": env("PGHOST", "localhost"), "port": envInt("PGPORT", 55432),
		"database": env("PGDATABASE", "dbs_monitor"), "username": env("PGUSER", "dbs_monitor"),
		"password": targetPassword,
	}
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", instanceInput, "")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create instance status = %d, want 201", created.StatusCode)
	}
	var createBody struct {
		AgentToken string `json:"agent_token"`
		Instance   struct {
			ID string `json:"id"`
		} `json:"instance"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	created.Body.Close()
	if createBody.AgentToken == "" || createBody.Instance.ID == "" {
		t.Fatalf("create response missing token or instance: %+v", createBody)
	}
	var agentMetricsEnabled bool
	if err := pool.QueryRow(ctx, "SELECT agent_metrics_enabled FROM instance_collection_config WHERE instance_id = $1", createBody.Instance.ID).Scan(&agentMetricsEnabled); err != nil {
		t.Fatalf("read default agent collection setting: %v", err)
	}
	if !agentMetricsEnabled {
		t.Fatal("new instance should enable agent metrics by default")
	}
	var originalCiphertext []byte
	var keyVersion int32
	var credentialVersion int64
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, password_key_version, credential_version
		FROM instance WHERE id = $1`, createBody.Instance.ID).Scan(&originalCiphertext, &keyVersion, &credentialVersion); err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	if bytes.Contains(originalCiphertext, []byte(targetPassword)) {
		t.Fatal("stored credential contains plaintext password")
	}
	if keyVersion != 1 || credentialVersion != 1 {
		t.Fatalf("initial key/credential versions = %d/%d, want 1/1", keyVersion, credentialVersion)
	}
	updated := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/instances/"+createBody.Instance.ID, instanceInput, "")
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update instance status = %d, want 200", updated.StatusCode)
	}
	updated.Body.Close()
	var updatedCiphertext []byte
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, credential_version FROM instance WHERE id = $1`, createBody.Instance.ID).
		Scan(&updatedCiphertext, &credentialVersion); err != nil {
		t.Fatalf("read updated credential: %v", err)
	}
	if bytes.Equal(originalCiphertext, updatedCiphertext) {
		t.Fatal("credential update reused the previous ciphertext")
	}
	if credentialVersion != 2 {
		t.Fatalf("updated credential version = %d, want 2", credentialVersion)
	}

	seriesURL := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.total&from=%s&to=%s&step=raw",
		server.URL, createBody.Instance.ID,
		url.QueryEscape(time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)),
		url.QueryEscape(time.Now().Add(time.Minute).UTC().Format(time.RFC3339)))
	assertUnavailability(t, client, seriesURL, "NO_SAMPLES_YET")
	assertUnavailability(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.tps", 1), "NO_SAMPLES_YET")

	collector := collect.New(platform, monitorpg.DirectDialer{}, clock.Real{}, keyring)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect samples: %v", err)
	}
	series, err := client.Get(seriesURL)
	if err != nil {
		t.Fatalf("get metric series: %v", err)
	}
	var seriesBody struct {
		Metrics []struct {
			Series json.RawMessage `json:"series"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(series.Body).Decode(&seriesBody); err != nil {
		t.Fatalf("decode metric series: %v", err)
	}
	series.Body.Close()
	if len(seriesBody.Metrics) != 1 || string(seriesBody.Metrics[0].Series) == "null" {
		t.Fatalf("metric API returned null series: %+v", seriesBody)
	}
	var returnedSeries []struct {
		Points [][]*float64 `json:"points"`
	}
	if err := json.Unmarshal(seriesBody.Metrics[0].Series, &returnedSeries); err != nil {
		t.Fatalf("decode returned series: %v", err)
	}
	if len(returnedSeries) == 0 || len(returnedSeries[0].Points) == 0 {
		t.Fatalf("metric API returned no points: %+v", returnedSeries)
	}
	assertStep(t, client, strings.Replace(seriesURL, "step=raw", "step=auto", 1), "15s")
	tooWideRaw := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.total&from=%s&to=%s&step=raw",
		server.URL, createBody.Instance.ID,
		url.QueryEscape(time.Now().Add(-7*time.Hour).UTC().Format(time.RFC3339)),
		url.QueryEscape(time.Now().UTC().Format(time.RFC3339)))
	tooWide := getResponse(t, client, tooWideRaw)
	if tooWide.StatusCode != http.StatusBadRequest {
		t.Fatalf("7h raw status = %d, want 400", tooWide.StatusCode)
	}
	tooWide.Body.Close()

	if _, err := pool.Exec(ctx, "UPDATE instance SET port = 1 WHERE id = $1", createBody.Instance.ID); err != nil {
		t.Fatalf("make instance unreachable: %v", err)
	}
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect unreachable instance: %v", err)
	}
	assertUnavailability(t, client, seriesURL, "DB_UNREACHABLE")
	if _, err := pool.Exec(ctx, "UPDATE instance SET port = $2 WHERE id = $1", createBody.Instance.ID, envInt("PGPORT", 55432)); err != nil {
		t.Fatalf("restore instance port: %v", err)
	}
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect restored instance: %v", err)
	}

	report := func(timestamp time.Time, token string) *http.Response {
		return requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/v1/report", map[string]any{
			"instance_id": createBody.Instance.ID,
			"timestamp":   timestamp.UTC().Format(time.RFC3339Nano),
			"metrics":     []map[string]any{{"metric": "host.cpu.usage_percent", "value": 37.5}},
		}, token)
	}
	wrong := report(time.Now(), "wrong-token")
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", wrong.StatusCode)
	}
	wrong.Body.Close()
	old := report(time.Now().Add(-31*time.Second), createBody.AgentToken)
	if old.StatusCode != http.StatusBadRequest {
		t.Fatalf("old timestamp status = %d, want 400", old.StatusCode)
	}
	old.Body.Close()
	accepted := report(time.Now(), createBody.AgentToken)
	if accepted.StatusCode != http.StatusNoContent {
		t.Fatalf("valid report status = %d, want 204", accepted.StatusCode)
	}
	accepted.Body.Close()

	var hostPoints int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.metric_id = 'host.cpu.usage_percent'`).Scan(&hostPoints); err != nil {
		t.Fatalf("count host metric points: %v", err)
	}
	if hostPoints != 1 {
		t.Fatalf("host.cpu.usage_percent points = %d, want 1", hostPoints)
	}

	if _, err := pool.Exec(ctx, "UPDATE instance SET password_key_version = 999 WHERE id = $1", createBody.Instance.ID); err != nil {
		t.Fatalf("set unknown credential key version: %v", err)
	}
	err = collector.RunOnce(ctx)
	var credentialFault *instance.CredentialFault
	if !errors.As(err, &credentialFault) || credentialFault.Code != instance.CredentialFaultUnknownKeyVersion {
		t.Fatalf("collection error = %v, want unknown credential key fault", err)
	}
	var lastErrorCode string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(last_error_code, '') FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, createBody.Instance.ID).Scan(&lastErrorCode); err != nil {
		t.Fatalf("read collection state after credential fault: %v", err)
	}
	if lastErrorCode != "" {
		t.Fatalf("credential fault was downgraded to target failure %q", lastErrorCode)
	}
}

func assertStep(t *testing.T, client *http.Client, address, want string) {
	t.Helper()
	response := getResponse(t, client, address)
	defer response.Body.Close()
	var body struct {
		Step string `json:"step"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode metric step: %v", err)
	}
	if body.Step != want {
		t.Fatalf("step = %q, want %q", body.Step, want)
	}
}

func getResponse(t *testing.T, client *http.Client, address string) *http.Response {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatalf("get %s: %v", address, err)
	}
	return response
}

func assertUnavailability(t *testing.T, client *http.Client, address, want string) {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatalf("get metric unavailability: %v", err)
	}
	defer response.Body.Close()
	var body struct {
		Metrics []struct {
			Unavailability *string `json:"unavailability"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode metric unavailability: %v", err)
	}
	if len(body.Metrics) != 1 || body.Metrics[0].Unavailability == nil || *body.Metrics[0].Unavailability != want {
		t.Fatalf("unavailability = %+v, want %s", body.Metrics, want)
	}
}

func requestJSON(t *testing.T, client *http.Client, method, address string, body any, bearer string) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request, err := http.NewRequest(method, address, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return response
}

func openSQL(t *testing.T, database string) *sql.DB {
	t.Helper()
	databaseHandle, err := sql.Open("pgx", connectionString(database))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := databaseHandle.Ping(); err != nil {
		databaseHandle.Close()
		t.Fatalf("ping database: %v", err)
	}
	return databaseHandle
}

func connectionString(database string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		env("PGHOST", "localhost"), envInt("PGPORT", 55432), env("PGUSER", "dbs_monitor"),
		env("PGPASSWORD", "dbs_monitor"), database)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	var value int
	if _, err := fmt.Sscanf(strings.TrimSpace(os.Getenv(name)), "%d", &value); err == nil && value > 0 {
		return value
	}
	return fallback
}
