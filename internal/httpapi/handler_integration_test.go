package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
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

	instanceInput := api.InstanceCreateInput{
		Name:     "target",
		Host:     env("PGHOST", "localhost"),
		Port:     envInt("PGPORT", 55432),
		Database: env("PGDATABASE", "dbs_monitor"),
		Username: env("PGUSER", "dbs_monitor"),
		Password: env("PGPASSWORD", "dbs_monitor"),
	}
	assertCreateRejected(t, ctx, client, server.URL, pool, api.InstanceCreateInput{
		Name:     "unreachable",
		Host:     instanceInput.Host,
		Port:     1,
		Database: instanceInput.Database,
		Username: instanceInput.Username,
		Password: instanceInput.Password,
	}, api.NETWORKUNREACHABLE)
	assertCreateRejected(t, ctx, client, server.URL, pool, api.InstanceCreateInput{
		Name:     "bad-auth",
		Host:     instanceInput.Host,
		Port:     instanceInput.Port,
		Database: instanceInput.Database,
		Username: instanceInput.Username,
		Password: "definitely-wrong-password",
	}, api.AUTHFAILED)
	if os.Getenv("PG12PORT") != "" {
		assertCreateRejected(t, ctx, client, server.URL, pool, api.InstanceCreateInput{
			Name:     "unsupported",
			Host:     env("PG12HOST", "localhost"),
			Port:     envInt("PG12PORT", 55431),
			Database: env("PG12DATABASE", "monitored"),
			Username: env("PG12USER", "monitored"),
			Password: env("PG12PASSWORD", "monitored"),
		}, api.ONBOARDINGVERSIONUNSUPPORTED)
	}
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", instanceInput, "")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create instance status = %d, want 201", created.StatusCode)
	}
	var createBody api.InstanceCreated
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	created.Body.Close()
	if createBody.Instance.Id == uuid.Nil {
		t.Fatalf("create response missing instance: %+v", createBody)
	}
	instanceID := createBody.Instance.Id.String()
	registrationURL := server.URL + "/api/v1/instances/" + instanceID + "/agent/registration"
	initialRegistration := getResponse(t, client, registrationURL)
	var initialRegistrationBody api.AgentRegistration
	if err := json.NewDecoder(initialRegistration.Body).Decode(&initialRegistrationBody); err != nil {
		t.Fatalf("decode initial Agent registration: %v", err)
	}
	initialRegistration.Body.Close()
	if initialRegistrationBody.State != api.NEVERREGISTERED || initialRegistrationBody.AgentExpected {
		t.Fatalf("initial Agent registration = %+v, want never registered and not expected", initialRegistrationBody)
	}
	registered := requestJSON(t, client, http.MethodPost, registrationURL, nil, "")
	if registered.StatusCode != http.StatusOK {
		t.Fatalf("register Agent status = %d, want 200", registered.StatusCode)
	}
	var registeredBody api.AgentTokenIssued
	if err := json.NewDecoder(registered.Body).Decode(&registeredBody); err != nil {
		t.Fatalf("decode Agent registration response: %v", err)
	}
	registered.Body.Close()
	if registeredBody.AgentToken == nil || registeredBody.Registration.State != api.EXPECTEDONLINE {
		t.Fatalf("registered Agent response = %+v", registeredBody)
	}
	agentToken := *registeredBody.AgentToken
	decodedToken, err := base64.RawURLEncoding.DecodeString(agentToken)
	if err != nil || len(decodedToken) != 32 {
		t.Fatalf("issued Agent token decodes to %d bytes with error %v, want 32 bytes", len(decodedToken), err)
	}
	var storedAgentTokenHash []byte
	if err := pool.QueryRow(ctx, "SELECT agent_token_hash FROM instance WHERE id = $1", instanceID).Scan(&storedAgentTokenHash); err != nil {
		t.Fatalf("read stored Agent token hash: %v", err)
	}
	wantAgentTokenHash := sha256.Sum256([]byte(agentToken))
	if !bytes.Equal(storedAgentTokenHash, wantAgentTokenHash[:]) || bytes.Contains(storedAgentTokenHash, []byte(agentToken)) {
		t.Fatal("Agent token was not stored exclusively as its SHA-256 hash")
	}
	registrationRead := getResponse(t, client, registrationURL)
	var registrationReadBody map[string]any
	if err := json.NewDecoder(registrationRead.Body).Decode(&registrationReadBody); err != nil {
		t.Fatalf("decode persisted Agent registration: %v", err)
	}
	registrationRead.Body.Close()
	if _, exposed := registrationReadBody["agent_token"]; exposed {
		t.Fatal("Agent registration read exposed the one-time token")
	}
	oldAgentToken := agentToken
	rotatedAgent := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances/"+instanceID+"/agent/token/rotation", nil, "")
	if rotatedAgent.StatusCode != http.StatusOK {
		t.Fatalf("rotate Agent token status = %d, want 200", rotatedAgent.StatusCode)
	}
	var rotatedAgentBody api.AgentTokenIssued
	if err := json.NewDecoder(rotatedAgent.Body).Decode(&rotatedAgentBody); err != nil {
		t.Fatalf("decode rotated Agent token: %v", err)
	}
	rotatedAgent.Body.Close()
	if rotatedAgentBody.AgentToken == nil || *rotatedAgentBody.AgentToken == oldAgentToken {
		t.Fatal("Agent token rotation did not issue a distinct token")
	}
	agentToken = *rotatedAgentBody.AgentToken
	var agentMetricsEnabled bool
	if err := pool.QueryRow(ctx, "SELECT agent_metrics_enabled FROM instance_collection_config WHERE instance_id = $1", instanceID).Scan(&agentMetricsEnabled); err != nil {
		t.Fatalf("read default agent collection setting: %v", err)
	}
	if !agentMetricsEnabled {
		t.Fatal("new instance should enable agent metrics by default")
	}
	var originalCiphertext []byte
	var keyVersion int32
	var credentialVersion int64
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, password_key_version, credential_version
		FROM instance WHERE id = $1`, instanceID).Scan(&originalCiphertext, &keyVersion, &credentialVersion); err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	if bytes.Contains(originalCiphertext, []byte(instanceInput.Password)) {
		t.Fatal("stored credential contains plaintext password")
	}
	if keyVersion != 1 || credentialVersion != 1 {
		t.Fatalf("initial key/credential versions = %d/%d, want 1/1", keyVersion, credentialVersion)
	}
	failedMetadata := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/instances/"+instanceID, api.InstanceMetadataInput{
		Name:     "must-not-commit",
		Host:     instanceInput.Host,
		Port:     1,
		Database: instanceInput.Database,
	}, "")
	if failedMetadata.StatusCode != http.StatusBadRequest {
		t.Fatalf("failed metadata update status = %d, want 400", failedMetadata.StatusCode)
	}
	failedMetadata.Body.Close()
	var storedName string
	var storedPort int
	if err := pool.QueryRow(ctx, "SELECT name, port FROM instance WHERE id = $1", instanceID).Scan(&storedName, &storedPort); err != nil {
		t.Fatalf("read metadata after failed update: %v", err)
	}
	if storedName != "target" || storedPort != instanceInput.Port {
		t.Fatalf("metadata after failed update = %q:%d, want target:%d", storedName, storedPort, instanceInput.Port)
	}

	updated := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/instances/"+instanceID, api.InstanceMetadataInput{
		Name:     "renamed target",
		Host:     instanceInput.Host,
		Port:     instanceInput.Port,
		Database: instanceInput.Database,
	}, "")
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("update instance status = %d, want 200", updated.StatusCode)
	}
	updated.Body.Close()
	var updatedCiphertext []byte
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, credential_version FROM instance WHERE id = $1`, instanceID).
		Scan(&updatedCiphertext, &credentialVersion); err != nil {
		t.Fatalf("read credential after metadata update: %v", err)
	}
	if !bytes.Equal(originalCiphertext, updatedCiphertext) || credentialVersion != 1 {
		t.Fatal("metadata update changed the stored credential")
	}

	credentialsURL := server.URL + "/api/v1/instances/" + instanceID + "/credentials"
	failedCredential := requestJSON(t, client, http.MethodPut, credentialsURL, api.InstanceCredentialInput{
		Username: instanceInput.Username,
		Password: "definitely-wrong-password",
	}, "")
	if failedCredential.StatusCode != http.StatusBadRequest {
		t.Fatalf("failed credential update status = %d, want 400", failedCredential.StatusCode)
	}
	failedCredential.Body.Close()
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, credential_version FROM instance WHERE id = $1`, instanceID).
		Scan(&updatedCiphertext, &credentialVersion); err != nil {
		t.Fatalf("read credential after failed rotation: %v", err)
	}
	if !bytes.Equal(originalCiphertext, updatedCiphertext) || credentialVersion != 1 {
		t.Fatal("failed credential update changed the stored credential")
	}

	rotated := requestJSON(t, client, http.MethodPut, credentialsURL, api.InstanceCredentialInput{
		Username: instanceInput.Username,
		Password: instanceInput.Password,
	}, "")
	if rotated.StatusCode != http.StatusOK {
		t.Fatalf("credential update status = %d, want 200", rotated.StatusCode)
	}
	var credentialResponse api.InstanceCredentialUpdated
	if err := json.NewDecoder(rotated.Body).Decode(&credentialResponse); err != nil {
		t.Fatalf("decode credential update response: %v", err)
	}
	rotated.Body.Close()
	if credentialResponse.Username != instanceInput.Username {
		t.Fatalf("credential response username = %q", credentialResponse.Username)
	}
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, credential_version FROM instance WHERE id = $1`, instanceID).
		Scan(&updatedCiphertext, &credentialVersion); err != nil {
		t.Fatalf("read updated credential: %v", err)
	}
	if bytes.Equal(originalCiphertext, updatedCiphertext) || credentialVersion != 2 {
		t.Fatalf("successful credential update did not rotate ciphertext/version: version %d", credentialVersion)
	}
	rotatedCiphertext := bytes.Clone(updatedCiphertext)

	updatedHost := "127.0.0.1"
	if instanceInput.Host == updatedHost {
		updatedHost = "localhost"
	}
	updatedConnection := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/instances/"+instanceID, api.InstanceMetadataInput{
		Name:     "renamed target",
		Host:     updatedHost,
		Port:     instanceInput.Port,
		Database: instanceInput.Database,
	}, "")
	if updatedConnection.StatusCode != http.StatusOK {
		t.Fatalf("connection metadata update status = %d, want 200", updatedConnection.StatusCode)
	}
	updatedConnection.Body.Close()
	if err := pool.QueryRow(ctx, `SELECT password_ciphertext, credential_version FROM instance WHERE id = $1`, instanceID).
		Scan(&updatedCiphertext, &credentialVersion); err != nil {
		t.Fatalf("read credential after connection metadata update: %v", err)
	}
	if !bytes.Equal(rotatedCiphertext, updatedCiphertext) || credentialVersion != 3 {
		t.Fatalf("connection metadata update did not preserve ciphertext and advance version: version %d", credentialVersion)
	}

	tasksURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/tasks", server.URL, instanceID)
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
	capabilitiesURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/capabilities", server.URL, createBody.Instance.Id)
	capabilities := readCapabilities(t, client, capabilitiesURL)
	if len(capabilities) != 4 {
		t.Fatalf("capability count = %d, want 4", len(capabilities))
	}
	roleCapability := capabilityByID(t, capabilities, "role.pg_monitor")
	if roleCapability.Status != "UNKNOWN" || roleCapability.ObservedAt != nil || roleCapability.AffectedMetricCount != 19 || roleCapability.FixHint == nil {
		t.Fatalf("initial pg_monitor capability = %+v", roleCapability)
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
	if err := pool.QueryRow(ctx, `SELECT interval_seconds FROM collection_task_config WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, instanceID).Scan(&persistedInterval); err != nil {
		t.Fatalf("read persisted collection interval: %v", err)
	}
	if persistedInterval != 7 {
		t.Fatalf("persisted interval = %d, want 7", persistedInterval)
	}

	seriesURL := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.total&from=%s&to=%s&step=raw",
		server.URL, instanceID,
		url.QueryEscape(time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)),
		url.QueryEscape(time.Now().Add(time.Minute).UTC().Format(time.RFC3339)))
	assertUnavailability(t, client, seriesURL, "COLLECTION_FAILED")
	assertUnavailability(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.tps", 1), "COLLECTION_FAILED")

	collector := collect.New(platform, monitorpg.DirectDialer{}, clock.Real{}, keyring)
	health := platformhealth.NewStore("3.0.0", time.Now().Add(-time.Hour), log.New(io.Discard, "", 0))
	collector.SetPlatformHealth(health)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect samples: %v", err)
	}
	roleCapability = capabilityByID(t, readCapabilities(t, client, capabilitiesURL), "role.pg_monitor")
	if roleCapability.Status != "PRESENT" || roleCapability.ObservedAt == nil {
		t.Fatalf("probed pg_monitor capability = %+v", roleCapability)
	}
	assertMetricSeriesHasPoints(t, client, seriesURL)
	tpsURL := strings.Replace(seriesURL, "pg.connection.total", "pg.tps", 1)
	if _, err := pool.Exec(ctx, `UPDATE instance_collection_task_state
		SET last_error_code = 'COUNTER_RESET', last_error_message = 'database statistics counters reset'
		WHERE instance_id = $1 AND task_id = 'pg.stat_database'`, createBody.Instance.Id); err != nil {
		t.Fatalf("mark counter reset: %v", err)
	}
	assertUnavailability(t, client, tpsURL, "COUNTER_RESET")
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect pg_stat_database rate samples: %v", err)
	}
	assertMetricSeriesHasPoints(t, client, tpsURL)
	assertStep(t, client, strings.Replace(seriesURL, "step=raw", "step=auto", 1), "15s")
	if _, err := pool.Exec(ctx, `UPDATE instance_capability_snapshot
		SET states = jsonb_set(states, '{role.pg_monitor}', '"MISSING"')
		WHERE instance_id = $1`, createBody.Instance.Id); err != nil {
		t.Fatalf("set missing pg_monitor capability: %v", err)
	}
	assertUnavailability(t, client, seriesURL, "PERMISSION_DENIED")
	if _, err := pool.Exec(ctx, `UPDATE instance_capability_snapshot
		SET states = jsonb_set(states, '{role.pg_monitor}', '"PRESENT"')
		WHERE instance_id = $1`, createBody.Instance.Id); err != nil {
		t.Fatalf("restore pg_monitor capability: %v", err)
	}
	tooWideRaw := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.total&from=%s&to=%s&step=raw",
		server.URL, instanceID,
		url.QueryEscape(time.Now().Add(-7*time.Hour).UTC().Format(time.RFC3339)),
		url.QueryEscape(time.Now().UTC().Format(time.RFC3339)))
	tooWide := getResponse(t, client, tooWideRaw)
	if tooWide.StatusCode != http.StatusBadRequest {
		t.Fatalf("7h raw status = %d, want 400", tooWide.StatusCode)
	}
	tooWide.Body.Close()

	if _, err := pool.Exec(ctx, "UPDATE instance SET port = 1 WHERE id = $1", instanceID); err != nil {
		t.Fatalf("make instance unreachable: %v", err)
	}
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect unreachable instance: %v", err)
	}
	assertUnavailability(t, client, seriesURL, "DB_UNREACHABLE")
	if _, err := pool.Exec(ctx, "UPDATE instance SET port = $2 WHERE id = $1", instanceID, instanceInput.Port); err != nil {
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
			"instance_id":          instanceID,
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
	oldToken := report(time.Now(), "2.4.0", oldAgentToken, nil)
	if oldToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("rotated Agent token status = %d, want old token rejected with 401", oldToken.StatusCode)
	}
	oldToken.Body.Close()
	skewed := report(time.Now().Add(-31*time.Second), "2.4.0", agentToken, nil)
	if skewed.StatusCode != http.StatusBadRequest {
		t.Fatalf("skewed timestamp status = %d, want 400", skewed.StatusCode)
	}
	skewed.Body.Close()
	assertAgentState(t, ctx, pool, instanceID, "2.4.0", "CLOCK_SKEW", "时钟偏移")

	tooOld := report(time.Now(), "1.99.0", agentToken, nil)
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
	assertAgentState(t, ctx, pool, instanceID, "1.99.0", "AGENT_VERSION_TOO_OLD", "版本过旧，需升级")

	alertUpdatedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(instance_id, metric_id, status, updated_at, rule_id, rule_version, severity, rule_snapshot)
		SELECT $1, rule.metric_id, 'OK', $2, rule.id, rule.version, rule.severity, version.snapshot
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = rule.version
		WHERE rule.id = '00000000-0000-0000-0000-000000000061'`, instanceID, alertUpdatedAt); err != nil {
		t.Fatalf("seed alert state: %v", err)
	}
	now := time.Now().UTC()
	backfill := []map[string]any{
		{"timestamp": now.Add(-90 * time.Second).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(9)},
		{"timestamp": now.Add(-4 * time.Minute).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(20)},
		{"timestamp": now.Add(-5*time.Minute - time.Second).Format(time.RFC3339Nano), "metrics": hostMetrics},
	}
	accepted := report(now, "2.4.0", agentToken, backfill)
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
			WHERE series.instance_id = $1 AND series.metric_id = $2`, instanceID, metricID).Scan(&count); err != nil {
			t.Fatalf("count %s points: %v", metricID, err)
		}
		if count != 3 {
			t.Fatalf("%s points = %d, want current plus two in-window backfill points", metricID, count)
		}
	}
	var iops []float64
	if err := pool.QueryRow(ctx, `SELECT array_agg(sample.value ORDER BY sample.ts)
		FROM metric_sample sample JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'host.disk.iops'`, instanceID).Scan(&iops); err != nil {
		t.Fatalf("read backfilled IOPS values: %v", err)
	}
	if len(iops) != 3 || iops[0] != 20 || iops[1] != 9 || iops[2] != 15 {
		t.Fatalf("backfilled IOPS values = %v, want [20 9 15] without reset reclassification", iops)
	}
	hostSeriesURL, err := url.Parse(fmt.Sprintf("%s/api/v1/instances/%s/metrics/series", server.URL, instanceID))
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
	assertAgentState(t, ctx, pool, instanceID, "2.4.0", "", "")

	var unchanged time.Time
	if err := pool.QueryRow(ctx, "SELECT updated_at FROM alert_instance WHERE instance_id = $1", instanceID).Scan(&unchanged); err != nil {
		t.Fatalf("read alert state after backfill: %v", err)
	}
	if !unchanged.Equal(alertUpdatedAt) {
		t.Fatalf("backfill changed alert evaluation state at %s, want %s", unchanged, alertUpdatedAt)
	}

	instanceResponse := getResponse(t, client, server.URL+"/api/v1/instances/"+instanceID)
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

	revoked := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances/"+instanceID+"/agent/token/revocation", nil, "")
	var revokedBody api.AgentRegistration
	if err := json.NewDecoder(revoked.Body).Decode(&revokedBody); err != nil {
		t.Fatalf("decode revoked Agent registration: %v", err)
	}
	revoked.Body.Close()
	if revoked.StatusCode != http.StatusOK || revokedBody.State != api.REVOKED || !revokedBody.AgentExpected || revokedBody.RevokedAt == nil {
		t.Fatalf("revoked Agent registration = status %d body %+v", revoked.StatusCode, revokedBody)
	}
	revokedReport := report(time.Now(), "2.4.0", agentToken, nil)
	if revokedReport.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked Agent token report status = %d, want 401", revokedReport.StatusCode)
	}
	revokedReport.Body.Close()

	var historicalSampleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1`, instanceID).Scan(&historicalSampleCount); err != nil {
		t.Fatalf("count historical Agent samples: %v", err)
	}
	disabled := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances/"+instanceID+"/agent/disable", nil, "")
	var disabledBody api.AgentRegistration
	if err := json.NewDecoder(disabled.Body).Decode(&disabledBody); err != nil {
		t.Fatalf("decode disabled Agent registration: %v", err)
	}
	disabled.Body.Close()
	if disabled.StatusCode != http.StatusOK || disabledBody.State != api.DISABLED || disabledBody.AgentExpected {
		t.Fatalf("disabled Agent registration = status %d body %+v", disabled.StatusCode, disabledBody)
	}
	var samplesAfterDisable int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1`, instanceID).Scan(&samplesAfterDisable); err != nil {
		t.Fatalf("count Agent samples after disable: %v", err)
	}
	if samplesAfterDisable != historicalSampleCount {
		t.Fatalf("Agent samples after disable = %d, want historical %d preserved", samplesAfterDisable, historicalSampleCount)
	}

	reenabled := requestJSON(t, client, http.MethodPost, registrationURL, nil, "")
	var reenabledBody api.AgentTokenIssued
	if err := json.NewDecoder(reenabled.Body).Decode(&reenabledBody); err != nil {
		t.Fatalf("decode re-enabled Agent registration: %v", err)
	}
	reenabled.Body.Close()
	if reenabled.StatusCode != http.StatusOK || reenabledBody.AgentToken == nil ||
		reenabledBody.Registration.State != api.EXPECTEDONLINE || *reenabledBody.AgentToken == agentToken {
		t.Fatalf("re-enabled Agent response = status %d body %+v", reenabled.StatusCode, reenabledBody)
	}
	if registeredBody.Registration.FirstRegisteredAt == nil || reenabledBody.Registration.FirstRegisteredAt == nil ||
		!registeredBody.Registration.FirstRegisteredAt.Equal(*reenabledBody.Registration.FirstRegisteredAt) {
		t.Fatal("re-enabling Agent did not preserve its first registration time")
	}

	if _, err := pool.Exec(ctx, "UPDATE instance SET password_key_version = 999 WHERE id = $1", instanceID); err != nil {
		t.Fatalf("set unknown credential key version: %v", err)
	}
	err = collector.RunOnce(ctx)
	var credentialFault *instance.CredentialFault
	if !errors.As(err, &credentialFault) || credentialFault.Code != instance.CredentialFaultUnknownKeyVersion {
		t.Fatalf("collection error = %v, want unknown credential key fault", err)
	}
	credentialHealth := health.Source(platformhealth.SourceCredentialKeyring)
	if credentialHealth.Status != platformhealth.StatusFailed || credentialHealth.Code != string(instance.CredentialFaultUnknownKeyVersion) {
		t.Fatalf("credential platform health = %+v, want FAILED/%s", credentialHealth, instance.CredentialFaultUnknownKeyVersion)
	}
	var lastErrorCode string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(last_error_code, '') FROM instance_collect_state
		WHERE instance_id = $1 AND source = 'SERVER_DIRECT'`, instanceID).Scan(&lastErrorCode); err != nil {
		t.Fatalf("read collection state after credential fault: %v", err)
	}
	if lastErrorCode != "" {
		t.Fatalf("credential fault was downgraded to target failure %q", lastErrorCode)
	}
}

func assertCreateRejected(t *testing.T, ctx context.Context, client *http.Client, serverURL string, pool *pgxpool.Pool, input api.InstanceCreateInput, wantCode api.ErrorErrorCode) {
	t.Helper()
	response := requestJSON(t, client, http.MethodPost, serverURL+"/api/v1/instances", input, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("create rejection status = %d, want 400", response.StatusCode)
	}
	var body api.Error
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode create rejection: %v", err)
	}
	if body.Error.Code != wantCode {
		t.Fatalf("create rejection code = %q, want %q", body.Error.Code, wantCode)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM instance").Scan(&count); err != nil {
		t.Fatalf("count rejected instances: %v", err)
	}
	if count != 0 {
		t.Fatalf("instance count after rejected create = %d, want 0", count)
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

func assertMetricSeriesHasPoints(t *testing.T, client *http.Client, address string) {
	t.Helper()
	response := getResponse(t, client, address)
	defer response.Body.Close()
	var body struct {
		Metrics []struct {
			Series []struct {
				Points [][]*float64 `json:"points"`
			} `json:"series"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode metric series: %v", err)
	}
	if len(body.Metrics) != 1 || len(body.Metrics[0].Series) == 0 || len(body.Metrics[0].Series[0].Points) == 0 {
		t.Fatalf("metric API returned no points: %+v", body)
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

type capabilityResponse struct {
	CapabilityID        string     `json:"capability_id"`
	Status              string     `json:"status"`
	ObservedAt          *time.Time `json:"observed_at"`
	FixHint             *string    `json:"fix_hint"`
	NAReason            *string    `json:"na_reason"`
	AffectedMetricCount int        `json:"affected_metric_count"`
}

func readCapabilities(t *testing.T, client *http.Client, address string) []capabilityResponse {
	t.Helper()
	response := getResponse(t, client, address)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("capability snapshot status = %d, want 200", response.StatusCode)
	}
	defer response.Body.Close()
	var capabilities []capabilityResponse
	if err := json.NewDecoder(response.Body).Decode(&capabilities); err != nil {
		t.Fatalf("decode capability snapshot: %v", err)
	}
	return capabilities
}

func capabilityByID(t *testing.T, capabilities []capabilityResponse, id string) capabilityResponse {
	t.Helper()
	for _, capability := range capabilities {
		if capability.CapabilityID == id {
			return capability
		}
	}
	t.Fatalf("capability %q is missing from %+v", id, capabilities)
	return capabilityResponse{}
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
