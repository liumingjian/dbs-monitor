package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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

	"github.com/google/uuid"
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
	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, true)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	server := httptest.NewTLSServer(httpapi.NewHandlerWithVersion(platform, clock.Real{}, keyring, "3.0.0").Routes())
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

	tasksURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/tasks", server.URL, createBody.Instance.ID)
	tasks := getResponse(t, client, tasksURL)
	if tasks.StatusCode != http.StatusOK {
		t.Fatalf("collection task states status = %d, want 200", tasks.StatusCode)
	}
	var taskStates []struct {
		TaskID          string `json:"task_id"`
		IntervalSeconds int    `json:"interval_seconds"`
	}
	if err := json.NewDecoder(tasks.Body).Decode(&taskStates); err != nil {
		t.Fatalf("decode collection task states: %v", err)
	}
	tasks.Body.Close()
	if len(taskStates) != 8 {
		t.Fatalf("collection task state count = %d, want 8", len(taskStates))
	}

	readOnlyToken := "read-only-token"
	readOnlyHash := sha256.Sum256([]byte(readOnlyToken))
	readOnlyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role) VALUES ($1, 'reader', '\x00', 'READONLY')`, readOnlyID); err != nil {
		t.Fatalf("create read-only user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_session (token_hash, user_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, readOnlyHash[:], readOnlyID); err != nil {
		t.Fatalf("create read-only session: %v", err)
	}
	readOnlyJar, _ := cookiejar.New(nil)
	readOnlyClient := *client
	readOnlyClient.Jar = readOnlyJar
	serverURL, _ := url.Parse(server.URL)
	readOnlyJar.SetCookies(serverURL, []*http.Cookie{{Name: "dbs_monitor_session", Value: readOnlyToken, Path: "/"}})
	forbidden := requestJSON(t, &readOnlyClient, http.MethodPut, tasksURL+"/pg.stat_activity", map[string]any{"interval_seconds": 7}, "")
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only interval update status = %d, want 403", forbidden.StatusCode)
	}
	forbidden.Body.Close()

	belowFloor := requestJSON(t, client, http.MethodPut, tasksURL+"/pg.stat_activity", map[string]any{"interval_seconds": 4}, "")
	if belowFloor.StatusCode != http.StatusBadRequest {
		t.Fatalf("4-second interval update status = %d, want 400", belowFloor.StatusCode)
	}
	belowFloor.Body.Close()
	updatedInterval := requestJSON(t, client, http.MethodPut, tasksURL+"/pg.stat_activity", map[string]any{"interval_seconds": 7}, "")
	if updatedInterval.StatusCode != http.StatusOK {
		t.Fatalf("7-second interval update status = %d, want 200", updatedInterval.StatusCode)
	}
	var updatedTask struct {
		IntervalSeconds int `json:"interval_seconds"`
	}
	if err := json.NewDecoder(updatedInterval.Body).Decode(&updatedTask); err != nil {
		t.Fatalf("decode updated collection interval: %v", err)
	}
	updatedInterval.Body.Close()
	if updatedTask.IntervalSeconds != 7 {
		t.Fatalf("updated interval = %d, want 7", updatedTask.IntervalSeconds)
	}
	var persistedInterval int
	if err := pool.QueryRow(ctx, `SELECT interval_seconds FROM collection_task_config WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, createBody.Instance.ID).Scan(&persistedInterval); err != nil {
		t.Fatalf("read persisted collection interval: %v", err)
	}
	if persistedInterval != 7 {
		t.Fatalf("persisted interval = %d, want 7", persistedInterval)
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

	hostMetrics := []map[string]any{
		{"metric": "host.cpu.usage_percent", "value": 37.5},
		{"metric": "host.memory.usage_percent", "value": 61.25},
		{"metric": "host.disk.usage_percent", "value": 52.0},
		{"metric": "host.disk.free_bytes", "value": 1_000_000.0},
		{"metric": "host.disk.iops", "value": 15.0},
		{"metric": "host.disk.throughput_bytes_per_sec", "value": 2_000.0},
		{"metric": "host.network.bytes_per_sec", "value": 3_000.0},
	}
	metricsWithIOPS := func(value float64) []map[string]any {
		metrics := make([]map[string]any, 0, len(hostMetrics))
		for _, item := range hostMetrics {
			copy := map[string]any{"metric": item["metric"], "value": item["value"]}
			if copy["metric"] == "host.disk.iops" {
				copy["value"] = value
			}
			metrics = append(metrics, copy)
		}
		return metrics
	}
	report := func(timestamp time.Time, version, token string, backfill []map[string]any) *http.Response {
		return requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/v1/report", map[string]any{
			"instance_id":          createBody.Instance.ID,
			"agent_version":        version,
			"timestamp":            timestamp.UTC().Format(time.RFC3339Nano),
			"metrics":              hostMetrics,
			"backfill":             backfill,
			"unknown_future_field": true,
		}, token)
	}
	wrong := report(time.Now(), "2.4.0", "wrong-token", nil)
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", wrong.StatusCode)
	}
	wrong.Body.Close()
	skewed := report(time.Now().Add(-31*time.Second), "2.4.0", createBody.AgentToken, nil)
	if skewed.StatusCode != http.StatusBadRequest {
		t.Fatalf("skewed timestamp status = %d, want 400", skewed.StatusCode)
	}
	skewed.Body.Close()
	assertAgentState(t, ctx, pool, createBody.Instance.ID, "2.4.0", "CLOCK_SKEW", "时钟偏移")

	tooOld := report(time.Now(), "1.99.0", createBody.AgentToken, nil)
	if tooOld.StatusCode != http.StatusBadRequest {
		t.Fatalf("old Agent version status = %d, want 400", tooOld.StatusCode)
	}
	var oldVersionError struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(tooOld.Body).Decode(&oldVersionError); err != nil {
		t.Fatalf("decode old version response: %v", err)
	}
	tooOld.Body.Close()
	if oldVersionError.Error.Message != "版本过旧，需升级" {
		t.Fatalf("old version message = %q", oldVersionError.Error.Message)
	}
	assertAgentState(t, ctx, pool, createBody.Instance.ID, "1.99.0", "AGENT_VERSION_TOO_OLD", "版本过旧，需升级")

	alertUpdatedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(instance_id, metric_id, status, updated_at, rule_id, rule_version, severity, rule_snapshot)
		SELECT $1, rule.metric_id, 'OK', $2, rule.id, rule.version, rule.severity, version.snapshot
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = rule.version
		WHERE rule.id = '00000000-0000-0000-0000-000000000061'`, createBody.Instance.ID, alertUpdatedAt); err != nil {
		t.Fatalf("seed alert state: %v", err)
	}
	now := time.Now().UTC()
	backfill := []map[string]any{
		{"timestamp": now.Add(-90 * time.Second).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(9)},
		{"timestamp": now.Add(-4 * time.Minute).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(20)},
		{"timestamp": now.Add(-5*time.Minute - time.Second).Format(time.RFC3339Nano), "metrics": hostMetrics},
	}
	accepted := report(now, "2.4.0", createBody.AgentToken, backfill)
	if accepted.StatusCode != http.StatusNoContent {
		t.Fatalf("valid report status = %d, want 204", accepted.StatusCode)
	}
	accepted.Body.Close()

	for _, metricID := range []string{
		"host.cpu.usage_percent", "host.memory.usage_percent", "host.disk.usage_percent",
		"host.disk.free_bytes", "host.disk.iops", "host.disk.throughput_bytes_per_sec",
		"host.network.bytes_per_sec",
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
			JOIN metric_series series ON series.series_id = sample.series_id
			WHERE series.instance_id = $1 AND series.metric_id = $2`, createBody.Instance.ID, metricID).Scan(&count); err != nil {
			t.Fatalf("count %s points: %v", metricID, err)
		}
		if count != 3 {
			t.Fatalf("%s points = %d, want current plus two in-window backfill points", metricID, count)
		}
	}
	var iops []float64
	if err := pool.QueryRow(ctx, `SELECT array_agg(sample.value ORDER BY sample.ts)
		FROM metric_sample sample JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'host.disk.iops'`, createBody.Instance.ID).Scan(&iops); err != nil {
		t.Fatalf("read backfilled IOPS values: %v", err)
	}
	if len(iops) != 3 || iops[0] != 20 || iops[1] != 9 || iops[2] != 15 {
		t.Fatalf("backfilled IOPS values = %v, want [20 9 15] without reset reclassification", iops)
	}
	hostSeriesURL, err := url.Parse(fmt.Sprintf("%s/api/v1/instances/%s/metrics/series", server.URL, createBody.Instance.ID))
	if err != nil {
		t.Fatalf("parse host series URL: %v", err)
	}
	query := hostSeriesURL.Query()
	for _, metricID := range []string{
		"host.cpu.usage_percent", "host.memory.usage_percent", "host.disk.usage_percent",
		"host.disk.free_bytes", "host.disk.iops", "host.disk.throughput_bytes_per_sec",
		"host.network.bytes_per_sec",
	} {
		query.Add("metric", metricID)
	}
	query.Set("from", now.Add(-5*time.Minute).Format(time.RFC3339Nano))
	query.Set("to", now.Add(time.Minute).Format(time.RFC3339Nano))
	query.Set("step", "raw")
	hostSeriesURL.RawQuery = query.Encode()
	hostSeries := getResponse(t, client, hostSeriesURL.String())
	defer hostSeries.Body.Close()
	var hostSeriesBody struct {
		Metrics []struct {
			Metric string `json:"metric"`
			Series []struct {
				Points [][]*float64 `json:"points"`
			} `json:"series"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(hostSeries.Body).Decode(&hostSeriesBody); err != nil {
		t.Fatalf("decode host metric series: %v", err)
	}
	if len(hostSeriesBody.Metrics) != 7 {
		t.Fatalf("host metric series count = %d, want 7", len(hostSeriesBody.Metrics))
	}
	for _, item := range hostSeriesBody.Metrics {
		if len(item.Series) != 1 || len(item.Series[0].Points) != 3 {
			t.Fatalf("%s API points = %+v, want three raw points", item.Metric, item.Series)
		}
		if item.Metric == "host.cpu.usage_percent" {
			points := item.Series[0].Points
			if points[0][0] == nil || points[1][0] == nil || points[2][0] == nil ||
				*points[1][0]-*points[0][0] != 150 || *points[2][0]-*points[1][0] != 90 {
				t.Fatalf("CPU point timestamps = %+v, want Agent sample intervals 150s and 90s", points)
			}
		}
	}
	assertAgentState(t, ctx, pool, createBody.Instance.ID, "2.4.0", "", "")

	var unchanged time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM alert_instance WHERE instance_id = $1", createBody.Instance.ID).Scan(&unchanged); err != nil {
		t.Fatalf("read alert state after backfill: %v", err)
	}
	if !unchanged.Equal(alertUpdatedAt) {
		t.Fatalf("backfill changed alert evaluation state at %s, want %s", unchanged, alertUpdatedAt)
	}

	instanceResponse := getResponse(t, client, server.URL+"/api/v1/instances/"+createBody.Instance.ID)
	defer instanceResponse.Body.Close()
	var instanceBody struct {
		AgentVersion *string `json:"agent_version"`
	}
	if err := json.NewDecoder(instanceResponse.Body).Decode(&instanceBody); err != nil {
		t.Fatalf("decode instance Agent version: %v", err)
	}
	if instanceBody.AgentVersion == nil || *instanceBody.AgentVersion != "2.4.0" {
		t.Fatalf("instance agent_version = %v, want 2.4.0", instanceBody.AgentVersion)
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

func assertAgentState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID, version, code, message string) {
	t.Helper()
	var gotVersion string
	var gotCode, gotMessage sql.NullString
	if err := pool.QueryRow(ctx, `SELECT instance.agent_version, state.last_error_code, state.last_error_message
		FROM instance JOIN instance_collect_state state ON state.instance_id = instance.id AND state.source = 'AGENT'
		WHERE instance.id = $1`, instanceID).Scan(&gotVersion, &gotCode, &gotMessage); err != nil {
		t.Fatalf("read Agent state: %v", err)
	}
	if gotVersion != version || gotCode.String != code || gotMessage.String != message {
		t.Fatalf("Agent state = (%q, %q, %q), want (%q, %q, %q)", gotVersion, gotCode.String, gotMessage.String, version, code, message)
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
