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
	"slices"
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
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformevent"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestHTTPSAPIAndAgentPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_http_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	t.Cleanup(func() { admin.Close() })
	if _, err := admin.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Fatalf("install pg_stat_statements in monitored target: %v", err)
	}
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := createTestCredentialDirectory(t)
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
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	health := platformhealth.NewStore("3.0.0", time.Now().Add(-time.Hour), log.New(io.Discard, "", 0))
	dialer := &countingTargetDialer{}
	agentBinaryDirectory := t.TempDir()
	agentBinary := []byte("dbs-monitor-agent-binary")
	for _, architecture := range []string{"amd64", "arm64"} {
		if err := os.WriteFile(filepath.Join(agentBinaryDirectory, "dbs-monitor-agent-linux-"+architecture), agentBinary, 0755); err != nil {
			t.Fatalf("write %s Agent binary: %v", architecture, err)
		}
	}
	distribution := httpapi.AgentDistribution{BinaryDirectory: agentBinaryDirectory, CAFingerprint: "test-ca-fingerprint"}
	server := httptest.NewTLSServer(httpapi.NewHandlerWithPlatformHealthAndAgentDistribution(
		platform, clock.Real{}, keyring, dialer, "3.0.0", health, distribution,
	).Routes())
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

	targetUsername := fmt.Sprintf("onboarding_target_%d", os.Getpid())
	targetPassword := "onboarding-target-secret"
	targetRoleIdentifier := pgx.Identifier{targetUsername}.Sanitize()
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		"CREATE ROLE %s LOGIN PASSWORD '%s'",
		targetRoleIdentifier,
		targetPassword,
	)); err != nil {
		t.Fatalf("create monitored target role: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+targetRoleIdentifier); err != nil {
			t.Errorf("drop monitored target role: %v", err)
		}
	})

	instanceInput := api.InstanceCreateInput{
		Name:     "target",
		Host:     env("PGHOST", "localhost"),
		Port:     envInt("PGPORT", 55432),
		Database: env("PGDATABASE", "dbs_monitor"),
		Username: targetUsername,
		Password: targetPassword,
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
	createResponseBody := readResponseBody(t, created, "created instance")
	if strings.Contains(strings.ToLower(string(createResponseBody)), "agent_token") {
		t.Fatalf("create instance response exposes Agent token: %s", createResponseBody)
	}
	if bytes.Contains(createResponseBody, []byte(instanceInput.Password)) {
		t.Fatalf("create instance response exposes submitted password: %s", createResponseBody)
	}
	var createBody api.InstanceCreated
	if err := json.Unmarshal(createResponseBody, &createBody); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	if createBody.Instance.Id == uuid.Nil {
		t.Fatalf("create response missing instance: %+v", createBody)
	}
	if !createBody.Instance.AgentMetricsEnabled {
		t.Fatal("create response should expose the enabled Agent metrics setting")
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
	downloadURL := server.URL + "/api/v1/agent/download?arch=linux%2Famd64"
	for name, token := range map[string]string{"missing": "", "wrong": "wrong-token"} {
		download := requestJSON(t, client, http.MethodGet, downloadURL, nil, token)
		download.Body.Close()
		if download.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s Agent token download status = %d, want 401", name, download.StatusCode)
		}
	}
	download := requestJSON(t, client, http.MethodGet, downloadURL, nil, agentToken)
	downloadBody, err := io.ReadAll(download.Body)
	download.Body.Close()
	if err != nil || download.StatusCode != http.StatusOK || !bytes.Equal(downloadBody, agentBinary) {
		t.Fatalf("authenticated Agent download = status %d body %q error %v", download.StatusCode, downloadBody, err)
	}
	invalidArchitecture := requestJSON(t, client, http.MethodGet, server.URL+"/api/v1/agent/download?arch=linux%2Fs390x", nil, agentToken)
	invalidArchitecture.Body.Close()
	if invalidArchitecture.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid Agent architecture status = %d, want 400", invalidArchitecture.StatusCode)
	}
	if err := os.Remove(filepath.Join(agentBinaryDirectory, "dbs-monitor-agent-linux-arm64")); err != nil {
		t.Fatalf("remove arm64 Agent fixture: %v", err)
	}
	missingBinary := requestJSON(t, client, http.MethodGet, server.URL+"/api/v1/agent/download?arch=linux%2Farm64", nil, agentToken)
	missingBinary.Body.Close()
	if missingBinary.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing Agent binary status = %d, want 503", missingBinary.StatusCode)
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
	oldTokenDownload := requestJSON(t, client, http.MethodGet, downloadURL, nil, oldAgentToken)
	oldTokenDownload.Body.Close()
	if oldTokenDownload.StatusCode != http.StatusUnauthorized {
		t.Fatalf("rotated Agent token download status = %d, want 401", oldTokenDownload.StatusCode)
	}
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
	var failedMetadataBody api.Error
	if err := json.NewDecoder(failedMetadata.Body).Decode(&failedMetadataBody); err != nil {
		t.Fatalf("decode failed metadata update: %v", err)
	}
	failedMetadata.Body.Close()
	if failedMetadataBody.Error.Code != api.NETWORKUNREACHABLE {
		t.Fatalf("failed metadata update code = %q, want %q", failedMetadataBody.Error.Code, api.NETWORKUNREACHABLE)
	}
	var storedName string
	var storedPort int
	if err := pool.QueryRow(ctx, "SELECT name, port FROM instance WHERE id = $1", instanceID).Scan(&storedName, &storedPort); err != nil {
		t.Fatalf("read metadata after failed update: %v", err)
	}
	if storedName != "target" || storedPort != instanceInput.Port {
		t.Fatalf("metadata after failed update = %q:%d, want target:%d", storedName, storedPort, instanceInput.Port)
	}

	dialCountBeforeDisplayUpdate := dialer.calls
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
	if dialer.calls != dialCountBeforeDisplayUpdate {
		t.Fatalf("display-only metadata update dialed target %d times, want 0", dialer.calls-dialCountBeforeDisplayUpdate)
	}
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
	credentialResponseBody := readResponseBody(t, rotated, "credential update")
	lowerCredentialResponse := strings.ToLower(string(credentialResponseBody))
	for _, forbidden := range []string{"password", "ciphertext", "key_version", "credential_version", "dsn"} {
		if strings.Contains(lowerCredentialResponse, forbidden) {
			t.Fatalf("credential update response exposes forbidden field %q: %s", forbidden, credentialResponseBody)
		}
	}
	if bytes.Contains(credentialResponseBody, []byte(instanceInput.Password)) {
		t.Fatalf("credential update response exposes submitted password: %s", credentialResponseBody)
	}
	var credentialResponse api.InstanceCredentialUpdated
	if err := json.Unmarshal(credentialResponseBody, &credentialResponse); err != nil {
		t.Fatalf("decode credential update response: %v", err)
	}
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
	var credentialActor, credentialSubjectID string
	if err := pool.QueryRow(ctx, `SELECT actor.username, event.subject_id::text
		FROM platform_event event JOIN app_user actor ON actor.id = event.actor_id
		WHERE event.kind = $1 ORDER BY event.occurred_at DESC, event.id DESC LIMIT 1`,
		platformevent.InstanceCredentialUpdated).Scan(&credentialActor, &credentialSubjectID); err != nil {
		t.Fatalf("read credential update attribution: %v", err)
	}
	if credentialActor != "admin" || credentialSubjectID != instanceID {
		t.Fatalf("credential update attribution = actor %q target %q, want admin and %s", credentialActor, credentialSubjectID, instanceID)
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

	assertMetricCatalog(t, client, server.URL+"/api/v1/metrics/catalog")

	tasksURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/tasks", server.URL, instanceID)
	tasks := getResponse(t, client, tasksURL)
	if tasks.StatusCode != http.StatusOK {
		t.Fatalf("collection task states status = %d, want 200", tasks.StatusCode)
	}
	var taskStates []api.CollectionTaskState
	if err := json.NewDecoder(tasks.Body).Decode(&taskStates); err != nil {
		t.Fatalf("decode collection task states: %v", err)
	}
	tasks.Body.Close()
	if len(taskStates) != 10 {
		t.Fatalf("collection task state count = %d, want 10", len(taskStates))
	}
	var activityTask *api.CollectionTaskState
	for index := range taskStates {
		if taskStates[index].TaskId == "pg.stat_activity" {
			activityTask = &taskStates[index]
			break
		}
	}
	if activityTask == nil || !slices.Contains(activityTask.MetricIds, "pg.connection.active") ||
		!slices.Equal(activityTask.RequiredCapabilities, []string{"role.pg_monitor"}) {
		t.Fatalf("pg.stat_activity dictionary projection = %+v", activityTask)
	}
	capabilitiesURL := fmt.Sprintf("%s/api/v1/instances/%s/collection/capabilities", server.URL, createBody.Instance.Id)
	capabilities := readCapabilities(t, client, capabilitiesURL)
	if len(capabilities) != 4 {
		t.Fatalf("capability count = %d, want 4", len(capabilities))
	}
	roleCapability := capabilityByID(t, capabilities, "role.pg_monitor")
	if roleCapability.Status != "UNKNOWN" || roleCapability.ObservedAt != nil || roleCapability.AffectedMetricCount != 24 || roleCapability.FixHint == nil {
		t.Fatalf("initial pg_monitor capability = %+v", roleCapability)
	}
	capabilityCases := []struct {
		name       string
		states     string
		capability string
		wantStatus string
	}{
		{
			name:       "all ready",
			states:     `{"role.pg_monitor":"PRESENT","ext.pg_stat_statements":"PRESENT","topo.has_replication":"PRESENT","topo.has_slot":"PRESENT"}`,
			capability: "role.pg_monitor",
			wantStatus: "PRESENT",
		},
		{
			name:       "fixable missing",
			states:     `{"role.pg_monitor":"MISSING","ext.pg_stat_statements":"PRESENT","topo.has_replication":"PRESENT","topo.has_slot":"PRESENT"}`,
			capability: "role.pg_monitor",
			wantStatus: "MISSING",
		},
		{
			name:       "structural not applicable",
			states:     `{"role.pg_monitor":"PRESENT","ext.pg_stat_statements":"PRESENT","topo.has_replication":"PRESENT","topo.has_slot":"NOT_APPLICABLE"}`,
			capability: "topo.has_slot",
			wantStatus: "NOT_APPLICABLE",
		},
	}
	for _, capabilityCase := range capabilityCases {
		t.Run("capability todo fact "+capabilityCase.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
				VALUES ($1, now(), $2::jsonb)
				ON CONFLICT (instance_id) DO UPDATE SET observed_at = EXCLUDED.observed_at, states = EXCLUDED.states`,
				createBody.Instance.Id, capabilityCase.states); err != nil {
				t.Fatalf("seed %s capability fact: %v", capabilityCase.name, err)
			}
			got := capabilityByID(t, readCapabilities(t, client, capabilitiesURL), capabilityCase.capability)
			if got.Status != capabilityCase.wantStatus || got.ObservedAt == nil {
				t.Fatalf("%s capability = %+v, want %s with observation time", capabilityCase.name, got, capabilityCase.wantStatus)
			}
			if capabilityCase.wantStatus == "NOT_APPLICABLE" && got.NAReason == nil {
				t.Fatalf("not-applicable capability lacks reason: %+v", got)
			}
		})
	}
	queryStatsURL := fmt.Sprintf("%s/api/v1/instances/%s/query-stats", server.URL, instanceID)
	if _, err := pool.Exec(ctx, `UPDATE instance_capability_snapshot
		SET states = jsonb_set(states, '{ext.pg_stat_statements}', '"MISSING"')
		WHERE instance_id = $1`, createBody.Instance.Id); err != nil {
		t.Fatalf("set missing pg_stat_statements capability: %v", err)
	}
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.EXTENSIONMISSING)
	if _, err := pool.Exec(ctx, "DELETE FROM instance_capability_snapshot WHERE instance_id = $1", createBody.Instance.Id); err != nil {
		t.Fatalf("restore absent capability snapshot: %v", err)
	}
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.FEATUREDISABLED)

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
	readOnlyJar.SetCookies(serverURL, []*http.Cookie{{Name: "__Host-dbs_monitor_session", Value: readOnlyToken, Path: "/"}})
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
	// updated_at 由 PG 的 now() 落值;下界必须取自同一时钟,取测试进程时钟
	// 会因主机与 PG(容器/VM)间亚毫秒漂移出现倒挂(#159 实测早 223µs)。
	var intervalUpdateStarted time.Time
	if err := pool.QueryRow(ctx, "SELECT now()").Scan(&intervalUpdateStarted); err != nil {
		t.Fatalf("read database clock for interval lower bound: %v", err)
	}
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
	var updatedBy, adminID uuid.UUID
	var updatedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT id FROM app_user WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("read admin id: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT interval_seconds, updated_by, updated_at FROM collection_task_config WHERE instance_id = $1 AND task_id = 'pg.stat_activity'`, instanceID).Scan(&persistedInterval, &updatedBy, &updatedAt); err != nil {
		t.Fatalf("read persisted collection interval: %v", err)
	}
	if persistedInterval != 7 {
		t.Fatalf("persisted interval = %d, want 7", persistedInterval)
	}
	if updatedBy != adminID || updatedAt.Before(intervalUpdateStarted) {
		t.Fatalf("interval attribution = actor %s at %s, want actor %s at or after %s", updatedBy, updatedAt, adminID, intervalUpdateStarted)
	}

	seriesURL := fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=pg.connection.total&from=%s&to=%s&step=raw",
		server.URL, instanceID,
		url.QueryEscape(time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)),
		url.QueryEscape(time.Now().Add(time.Minute).UTC().Format(time.RFC3339)))
	assertUnavailability(t, client, seriesURL, "COLLECTION_FAILED")
	assertUnavailability(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.tps", 1), "COLLECTION_FAILED")

	if _, err := admin.ExecContext(ctx, "GRANT pg_monitor TO "+targetRoleIdentifier); err != nil {
		t.Fatalf("grant pg_monitor to monitored target role: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, platform, time.Now().UTC()); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	collector := collect.New(platform, monitorpg.DirectDialer{}, clock.Real{}, keyring)
	collector.SetPlatformHealth(health)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect samples: %v", err)
	}
	roleCapability = capabilityByID(t, readCapabilities(t, client, capabilitiesURL), "role.pg_monitor")
	if roleCapability.Status != "PRESENT" || roleCapability.ObservedAt == nil {
		t.Fatalf("probed pg_monitor capability = %+v", roleCapability)
	}
	queryStatsResponse := getResponse(t, client, queryStatsURL)
	if queryStatsResponse.StatusCode != http.StatusOK {
		t.Fatalf("query statistics status = %d, want 200", queryStatsResponse.StatusCode)
	}
	var queryStats struct {
		SampledAt      *time.Time                   `json:"sampled_at"`
		Unavailability *api.Unavailability          `json:"unavailability"`
		Items          []map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(queryStatsResponse.Body).Decode(&queryStats); err != nil {
		t.Fatalf("decode query statistics snapshot: %v", err)
	}
	queryStatsResponse.Body.Close()
	if queryStats.SampledAt == nil {
		t.Fatal("query statistics snapshot is missing sampled_at")
	}
	if queryStats.Unavailability != nil {
		t.Fatalf("query statistics snapshot is unavailable: %s", *queryStats.Unavailability)
	}
	if len(queryStats.Items) == 0 {
		t.Fatal("query statistics snapshot has no entries")
	}
	// 带真实字面量的原始语句一个字段都不许有；归一化文本（`query_text`，与
	// `/api/v1/top-sql` 同一张 `query_statement_text` 表）则是刻意给出的 ——
	// 少了它，工作台的排行又只剩一个没人认得的 queryid。理由与守卫的另一半见
	// internal/api/query_statistics_test.go。
	for _, forbidden := range []string{"query", "sql", "sql_text"} {
		if _, exists := queryStats.Items[0][forbidden]; exists {
			t.Fatalf("query statistics entry exposes raw SQL text field %q", forbidden)
		}
	}
	// 归一化文本要真的接得上：它与排行分表存放（按 (实例, queryid) 去重），
	// 少了那次 LEFT JOIN，接口照样 200，页面上却又只剩 queryid。
	normalisedTexts := 0
	for _, item := range queryStats.Items {
		raw, exists := item["query_text"]
		if !exists {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			t.Fatalf("decode query_text: %v", err)
		}
		if text == "" {
			t.Fatal("query statistics entry carries an empty query_text; absent and empty must stay distinguishable")
		}
		normalisedTexts++
	}
	if normalisedTexts == 0 {
		t.Fatal("no query statistics entry carries query_text; the normalised text join is not wired up")
	}
	assertMetricSeriesHasPoints(t, client, seriesURL)
	assertMetricSeriesHasPoints(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.prepared_xacts.count", 1))
	assertMetricSeriesHasPoints(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.replication.role", 1))
	assertUnavailability(t, client, strings.Replace(seriesURL, "pg.connection.total", "pg.replication.wal_lag_bytes", 1), "NOT_APPLICABLE_ROLE")
	slotSeriesURL := strings.Replace(seriesURL, "pg.connection.total", "pg.replication_slot.retained_wal_bytes", 1)
	assertUnavailability(t, client, slotSeriesURL, "NOT_APPLICABLE_ROLE")
	tpsURL := strings.Replace(seriesURL, "pg.connection.total", "pg.tps", 1)
	assertUnavailability(t, client, tpsURL, "NO_SAMPLES_YET")
	if _, err := pool.Exec(ctx, `UPDATE instance_capability_snapshot
		SET states = jsonb_set(states, '{topo.has_slot}', '"PRESENT"')
		WHERE instance_id = $1`, createBody.Instance.Id); err != nil {
		t.Fatalf("set replication slot capability present: %v", err)
	}
	slotSampledAt := time.Now().UTC()
	var slotSeriesID int64
	if err := pool.QueryRow(ctx, `INSERT INTO metric_series
			(instance_id, metric_id, labels, labels_key, last_seen)
		VALUES ($1, 'pg.replication_slot.retained_wal_bytes', '{"slot":"caught-up"}', '{"slot":"caught-up"}', $2)
		RETURNING series_id`, createBody.Instance.Id, slotSampledAt).Scan(&slotSeriesID); err != nil {
		t.Fatalf("create replication slot metric series: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 0)", slotSeriesID, slotSampledAt); err != nil {
		t.Fatalf("insert zero replication slot sample: %v", err)
	}
	slotSeriesResponse := getResponse(t, client, slotSeriesURL)
	var slotSeriesBody api.MetricSeriesResponse
	if err := json.NewDecoder(slotSeriesResponse.Body).Decode(&slotSeriesBody); err != nil {
		slotSeriesResponse.Body.Close()
		t.Fatalf("decode replication slot metric series: %v", err)
	}
	slotSeriesResponse.Body.Close()
	if len(slotSeriesBody.Metrics) != 1 {
		t.Fatalf("replication slot metrics = %+v, want one metric", slotSeriesBody.Metrics)
	}
	slotMetric := slotSeriesBody.Metrics[0]
	if !slotMetric.Unavailability.IsNull() {
		t.Fatalf("replication slot unavailability = %+v, want null", slotMetric.Unavailability)
	}
	if len(slotMetric.Series) != 1 {
		t.Fatalf("replication slot series = %+v, want one series", slotMetric.Series)
	}
	caughtUpSeries := slotMetric.Series[0]
	if caughtUpSeries.Labels["slot"] != "caught-up" {
		t.Fatalf("replication slot labels = %+v, want slot=caught-up", caughtUpSeries.Labels)
	}
	if len(caughtUpSeries.Points) != 1 {
		t.Fatalf("replication slot points = %+v, want one point", caughtUpSeries.Points)
	}
	caughtUpPoint := caughtUpSeries.Points[0]
	if len(caughtUpPoint) != 2 {
		t.Fatalf("replication slot point = %+v, want timestamp and value", caughtUpPoint)
	}
	if caughtUpPoint[1] == nil {
		t.Fatalf("replication slot point = %+v, want zero backlog", caughtUpPoint)
	}
	if *caughtUpPoint[1] != 0 {
		t.Fatalf("replication slot backlog = %v, want 0", *caughtUpPoint[1])
	}
	if _, err := pool.Exec(ctx, `UPDATE instance_capability_snapshot
		SET states = jsonb_set(states, '{topo.has_slot}', '"NOT_APPLICABLE"')
		WHERE instance_id = $1`, createBody.Instance.Id); err != nil {
		t.Fatalf("restore absent replication slot capability: %v", err)
	}
	assertUnavailability(t, client, slotSeriesURL, "NOT_APPLICABLE_ROLE")
	// Wait until the query crosses the configured seven-second collection interval.
	longQueryPID, stopLongQuery := startLongRunningQuery(t, ctx, admin)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect long query sample: %v", err)
	}
	stopLongQuery()

	apiClient, err := api.NewClientWithResponses(server.URL, api.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("create generated API client: %v", err)
	}
	queryWindowStart := time.Now().UTC().Add(-time.Minute)
	queryWindowEnd := time.Now().UTC().Add(time.Minute)
	longQueriesResponse, err := apiClient.ListLongQuerySamplesWithResponse(ctx, createBody.Instance.Id, &api.ListLongQuerySamplesParams{
		From: queryWindowStart,
		To:   queryWindowEnd,
	})
	if err != nil {
		t.Fatalf("list long query samples through generated client: %v", err)
	}
	if longQueriesResponse.StatusCode() != http.StatusOK || longQueriesResponse.JSON200 == nil {
		t.Fatalf("long query samples status = %d body = %s", longQueriesResponse.StatusCode(), longQueriesResponse.Body)
	}
	longQuerySample, found := findLongQuerySample(longQueriesResponse.JSON200.Items, longQueryPID)
	if !found {
		t.Fatalf("long query samples = %+v, want PID %d from the real collection pipeline", longQueriesResponse.JSON200.Items, longQueryPID)
	}
	assertCollectedLongQuerySample(t, longQuerySample)

	rawStep := api.Raw
	longQueryMetricResponse, err := apiClient.GetMetricSeriesWithResponse(ctx, createBody.Instance.Id, &api.GetMetricSeriesParams{
		Metric: []api.MetricId{api.MetricIdPgQueryLongRunningCount},
		From:   queryWindowStart,
		To:     queryWindowEnd,
		Step:   &rawStep,
	})
	if err != nil {
		t.Fatalf("read long query metric through generated client: %v", err)
	}
	if longQueryMetricResponse.StatusCode() != http.StatusOK || longQueryMetricResponse.JSON200 == nil {
		t.Fatalf("long query metric status = %d body = %s", longQueryMetricResponse.StatusCode(), longQueryMetricResponse.Body)
	}
	if !hasLongQueryCountAt(longQueryMetricResponse.JSON200, longQuerySample.SampledAt) {
		t.Fatalf("long query sample at %s does not share a timestamp with its metric: %+v",
			longQuerySample.SampledAt, longQueryMetricResponse.JSON200.Metrics)
	}

	if err := collect.DropExpiredStatActivitySnapshots(ctx, platform, longQuerySample.SampledAt.Add(30*24*time.Hour+time.Microsecond)); err != nil {
		t.Fatalf("expire long query samples: %v", err)
	}
	expiredLongQueries, err := apiClient.ListLongQuerySamplesWithResponse(ctx, createBody.Instance.Id, &api.ListLongQuerySamplesParams{
		From: queryWindowStart,
		To:   queryWindowEnd,
	})
	if err != nil {
		t.Fatalf("list expired long query samples: %v", err)
	}
	if expiredLongQueries.StatusCode() != http.StatusOK || expiredLongQueries.JSON200 == nil {
		t.Fatalf("expired long query samples = status %d body %s", expiredLongQueries.StatusCode(), expiredLongQueries.Body)
	}
	if expiredLongQueries.JSON200.Total != 0 {
		t.Fatalf("expired long query samples total = %d, want 0", expiredLongQueries.JSON200.Total)
	}

	sampledAt := time.Now().UTC().Truncate(time.Second)
	oldSampledAt := sampledAt.Add(-31 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO query_statistics_snapshot
		(instance_id, sampled_at) VALUES ($1, $2)`, instanceID, oldSampledAt); err != nil {
		t.Fatalf("insert old query statistics snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO query_statistics_snapshot_entry
		(instance_id, sampled_at, queryid, database_oid, user_oid, calls, total_exec_time_ms)
		VALUES ($1, $2, 58, 1, 1, 1, 1)`, instanceID, oldSampledAt); err != nil {
		t.Fatalf("insert old query statistics entry: %v", err)
	}
	if err := collect.DropExpiredQueryStatisticsSnapshots(ctx, platform, sampledAt); err != nil {
		t.Fatalf("expire query statistics snapshots: %v", err)
	}
	var oldQueryStatistics int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM query_statistics_snapshot_entry
		WHERE sampled_at = $1`, oldSampledAt).Scan(&oldQueryStatistics); err != nil {
		t.Fatalf("count expired query statistics entries: %v", err)
	}
	if oldQueryStatistics != 0 {
		t.Fatalf("expired query statistics entries = %d, want 0", oldQueryStatistics)
	}
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
	alertUpdatedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(instance_id, metric_id, status, updated_at, rule_id, rule_version, severity, rule_snapshot)
		SELECT $1, rule.metric_id, 'OK', $2, rule.id, rule.version, rule.severity, version.snapshot
		FROM alert_rule rule
		JOIN alert_rule_version version ON version.rule_id = rule.id AND version.version = rule.version
		WHERE rule.id = '00000000-0000-0000-0000-000000000061'`, instanceID, alertUpdatedAt); err != nil {
		t.Fatalf("seed alert state: %v", err)
	}
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
	assertAlertEvaluationUnchanged(t, ctx, pool, instanceID, alertUpdatedAt)
	now := time.Now().UTC()
	backfill := []map[string]any{
		{"timestamp": now.Add(-90 * time.Second).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(9)},
		{"timestamp": now.Add(-4 * time.Minute).Format(time.RFC3339Nano), "metrics": metricsWithIOPS(20)},
		{"timestamp": now.Add(-5*time.Minute - time.Second).Format(time.RFC3339Nano), "metrics": hostMetrics},
	}
	accepted := report(now, "2.4.0", agentToken, backfill)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("valid report status = %d, want 200", accepted.StatusCode)
	}
	var acceptedBody api.AgentReportAccepted
	if err := json.NewDecoder(accepted.Body).Decode(&acceptedBody); err != nil {
		t.Fatalf("decode accepted Agent report: %v", err)
	}
	if acceptedBody.ServerVersion != "3.0.0" || acceptedBody.ServerTime.IsZero() {
		t.Fatalf("accepted Agent report response = %+v", acceptedBody)
	}
	accepted.Body.Close()
	reportedRegistration := getResponse(t, client, registrationURL)
	var reportedRegistrationBody api.AgentRegistration
	if err := json.NewDecoder(reportedRegistration.Body).Decode(&reportedRegistrationBody); err != nil {
		t.Fatalf("decode reported Agent registration: %v", err)
	}
	reportedRegistration.Body.Close()
	if reportedRegistrationBody.LastReportedAt == nil {
		t.Fatal("Agent registration should expose the latest accepted heartbeat")
	}

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
	assertAgentState(t, ctx, pool, instanceID, "2.4.0", "", "")
	health.Update(now, platformhealth.DiskSource(
		96, platformhealth.DiskNormal, platformhealth.DefaultDiskThresholds(),
	))
	health.Update(now, platformhealth.PlatformDatabaseCapacitySource(
		96, 100, health.PlatformDatabaseCapacityLevel(), platformhealth.DefaultDiskThresholds(),
	))
	diagnostics := getResponse(t, client, server.URL+"/api/v1/diagnostics/health")
	var diagnosticSnapshot api.PlatformHealthSnapshot
	if err := json.NewDecoder(diagnostics.Body).Decode(&diagnosticSnapshot); err != nil {
		t.Fatalf("decode disk emergency diagnostics: %v", err)
	}
	diagnostics.Body.Close()
	var diagnosticDisk *api.PlatformHealthSourceSnapshot
	for index := range diagnosticSnapshot.Sources {
		if diagnosticSnapshot.Sources[index].Source == api.HealthSourceDisk {
			diagnosticDisk = &diagnosticSnapshot.Sources[index]
			break
		}
	}
	if diagnostics.StatusCode != http.StatusOK || diagnosticDisk == nil || diagnosticDisk.DiskLevel == nil ||
		*diagnosticDisk.DiskLevel != "EMERGENCY" || diagnosticDisk.DiskUsagePercent == nil ||
		diagnosticDisk.DiskEmergencyPercent == nil || *diagnosticDisk.DiskEmergencyPercent != 95 {
		t.Fatalf("disk emergency diagnostics status/source = %d/%+v", diagnostics.StatusCode, diagnosticDisk)
	}
	rejectedAtEmergency := report(time.Now(), "2.4.0", agentToken, nil)
	rejectedAtEmergency.Body.Close()
	if rejectedAtEmergency.StatusCode != http.StatusBadRequest {
		t.Fatalf("disk emergency Agent report status = %d, want 400", rejectedAtEmergency.StatusCode)
	}
	var emergencyIOPSCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metric_sample sample
		JOIN metric_series series ON series.series_id = sample.series_id
		WHERE series.instance_id = $1 AND series.metric_id = 'host.disk.iops'`, instanceID).Scan(&emergencyIOPSCount); err != nil {
		t.Fatalf("count Agent samples after disk emergency: %v", err)
	}
	if emergencyIOPSCount != len(iops) {
		t.Fatalf("Agent samples after disk emergency = %d, want unchanged %d", emergencyIOPSCount, len(iops))
	}
	assertAgentState(t, ctx, pool, instanceID, "2.4.0", "DISK_EMERGENCY_WATERMARK", "磁盘紧急水位，样本写入已拒绝")
	controlWrite := requestJSON(t, client, http.MethodPut, tasksURL+"/pg.stat_activity", map[string]any{"interval_seconds": 7}, "")
	controlWrite.Body.Close()
	if controlWrite.StatusCode != http.StatusOK {
		t.Fatalf("control-plane write during disk emergency status = %d, want 200", controlWrite.StatusCode)
	}
	health.Update(now, platformhealth.DiskSource(
		77, health.DiskLevel(), platformhealth.DefaultDiskThresholds(),
	))
	health.Update(now, platformhealth.PlatformDatabaseCapacitySource(
		77, 100, health.PlatformDatabaseCapacityLevel(), platformhealth.DefaultDiskThresholds(),
	))
	recovered := requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/v1/report", map[string]any{
		"instance_id":   instanceID,
		"agent_version": "2.4.0",
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
		"metrics":       []map[string]any{},
	}, agentToken)
	recovered.Body.Close()
	if recovered.StatusCode != http.StatusOK {
		t.Fatalf("post-emergency Agent heartbeat status = %d, want 200", recovered.StatusCode)
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
	assertAlertEvaluationUnchanged(t, ctx, pool, instanceID, alertUpdatedAt)

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
	var instanceCount, identityCount, collectionConfigCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM instance),
		(SELECT count(*) FROM instance_identity),
		(SELECT count(*) FROM instance_collection_config)`).Scan(&instanceCount, &identityCount, &collectionConfigCount); err != nil {
		t.Fatalf("count onboarding rows after rejected create: %v", err)
	}
	if instanceCount != 0 || identityCount != 0 || collectionConfigCount != 0 {
		t.Fatalf("rows after rejected create: instances = %d, identities = %d, collection configs = %d; want all 0", instanceCount, identityCount, collectionConfigCount)
	}
}

type countingTargetDialer struct {
	calls int
}

func (dialer *countingTargetDialer) Dial(ctx context.Context, config *pgx.ConnConfig) (*monitorpg.TargetConn, error) {
	dialer.calls++
	return (monitorpg.DirectDialer{}).Dial(ctx, config)
}

func assertAlertEvaluationUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID string, updatedAt time.Time) {
	t.Helper()
	var status string
	var noDataCount int
	var gotUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status, no_data_count, updated_at FROM alert_instance WHERE instance_id = $1`, instanceID).
		Scan(&status, &noDataCount, &gotUpdatedAt); err != nil {
		t.Fatalf("read alert state after Agent report: %v", err)
	}
	if status != "OK" || noDataCount != 0 || !gotUpdatedAt.Equal(updatedAt) {
		t.Fatalf("alert state = (%q, %d, %s), want (OK, 0, %s)", status, noDataCount, gotUpdatedAt, updatedAt)
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

// 指标目录由服务端交付：展示名与单位从这里来，前端不再自己留一份。
func assertMetricCatalog(t *testing.T, client *http.Client, address string) {
	t.Helper()
	response := getResponse(t, client, address)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metric catalog status = %d, want 200", response.StatusCode)
	}
	var body api.MetricCatalog
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode metric catalog: %v", err)
	}
	if len(body.Metrics) != len(metric.Metrics) {
		t.Fatalf("catalog metric count = %d, want %d", len(body.Metrics), len(metric.Metrics))
	}
	if len(body.SemanticSlots) != len(metric.SemanticSlots) {
		t.Fatalf("catalog semantic slot count = %d, want %d", len(body.SemanticSlots), len(metric.SemanticSlots))
	}
	var throughput *api.MetricCatalogEntry
	for index := range body.Metrics {
		if body.Metrics[index].MetricId == metric.MetricTPS.String() {
			throughput = &body.Metrics[index]
			break
		}
	}
	if throughput == nil {
		t.Fatal("catalog is missing pg.tps")
	}
	slot, err := throughput.SemanticSlot.Get()
	if err != nil || slot != api.SlotThroughput {
		t.Fatalf("pg.tps semantic slot = %v (err %v), want %q", slot, err, api.SlotThroughput)
	}
	// TPS 是库级的：一条连接下每个库各有自己的 TPS，实例级值是它们的和（#217）。
	if throughput.DisplayName != "TPS" || throughput.Unit != "tx/s" ||
		throughput.Engine != api.MetricEnginePostgreSQL || throughput.Level != api.MetricLevelDatabase ||
		throughput.Aggregation != api.MetricAggregationSum {
		t.Fatalf("pg.tps catalog entry = %+v", *throughput)
	}
	// 连接数是实例级的，没有可聚合的东西——库维度不该蔓延到实例级指标上。
	for index := range body.Metrics {
		if body.Metrics[index].MetricId != metric.MetricConnectionTotal.String() {
			continue
		}
		if body.Metrics[index].Level != api.MetricLevelInstance || body.Metrics[index].Aggregation != api.MetricAggregationNone {
			t.Fatalf("pg.connection.total catalog entry = %+v", body.Metrics[index])
		}
	}
	for index := range body.Metrics {
		if body.Metrics[index].DisplayName == "" {
			t.Fatalf("catalog entry %q has no display name", body.Metrics[index].MetricId)
		}
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

func readResponseBody(t *testing.T, response *http.Response, description string) []byte {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", description, err)
	}
	return body
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

func assertQueryStatisticsUnavailability(t *testing.T, client *http.Client, address string, want api.Unavailability) {
	t.Helper()
	response := getResponse(t, client, address)
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("query statistics status = %d, want 200", response.StatusCode)
	}
	defer response.Body.Close()
	var body struct {
		Items          []api.QueryStatisticsEntry `json:"items"`
		Unavailability *api.Unavailability        `json:"unavailability"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode query statistics unavailability: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("query statistics response has %d items, want 0", len(body.Items))
	}
	if body.Unavailability == nil {
		t.Fatalf("query statistics response is missing unavailability, want %s", want)
	}
	if *body.Unavailability != want {
		t.Fatalf("query statistics unavailability = %s, want %s", *body.Unavailability, want)
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

func findLongQuerySample(samples []api.LongQuerySample, pid int32) (api.LongQuerySample, bool) {
	for _, sample := range samples {
		if sample.Pid == pid {
			return sample, true
		}
	}
	return api.LongQuerySample{}, false
}

func assertCollectedLongQuerySample(t *testing.T, sample api.LongQuerySample) {
	t.Helper()
	if sample.State == nil {
		t.Fatal("captured long query is missing state")
	}
	if *sample.State != "active" {
		t.Fatalf("captured long query state = %q, want active", *sample.State)
	}
	if sample.Username == nil {
		t.Fatal("captured long query is missing username")
	}
	if want := env("PGUSER", "dbs_monitor"); *sample.Username != want {
		t.Fatalf("captured long query username = %q, want %q", *sample.Username, want)
	}
	if sample.DatabaseName == nil {
		t.Fatal("captured long query is missing database name")
	}
	if want := env("PGDATABASE", "dbs_monitor"); *sample.DatabaseName != want {
		t.Fatalf("captured long query database name = %q, want %q", *sample.DatabaseName, want)
	}
	if sample.WaitEventType == nil {
		t.Fatal("captured long query is missing wait event type")
	}
	if sample.WaitEvent == nil {
		t.Fatal("captured long query is missing wait event")
	}
	if sample.QueryDurationMs < 5000 {
		t.Fatalf("captured long query duration = %dms, want at least 5000ms", sample.QueryDurationMs)
	}
	if sample.QueryStartedAt.After(sample.SampledAt) {
		t.Fatalf("captured long query started at %s after sample at %s", sample.QueryStartedAt, sample.SampledAt)
	}
	if sample.SnapshotOriginalCount < 1 {
		t.Fatalf("captured long query snapshot count = %d, want at least 1", sample.SnapshotOriginalCount)
	}
}

func hasLongQueryCountAt(response *api.MetricSeriesResponse, sampledAt time.Time) bool {
	for _, metric := range response.Metrics {
		for _, series := range metric.Series {
			for _, point := range series.Points {
				if len(point) == 2 && point[0] != nil && point[1] != nil &&
					int64(*point[0]) == sampledAt.Unix() && *point[1] >= 1 {
					return true
				}
			}
		}
	}
	return false
}

func startLongRunningQuery(t *testing.T, ctx context.Context, observer *sql.DB) (int32, func()) {
	t.Helper()
	connection, err := pgx.Connect(ctx, connectionString(env("PGDATABASE", "dbs_monitor")))
	if err != nil {
		t.Fatalf("open long query connection: %v", err)
	}
	if _, err := connection.Exec(ctx, "SET application_name = 'issue-56-long-query'"); err != nil {
		connection.Close(context.Background())
		t.Fatalf("identify long query connection: %v", err)
	}

	queryCtx, cancelQuery := context.WithCancel(ctx)
	queryDone := make(chan struct{})
	go func() {
		defer close(queryDone)
		_, _ = connection.Exec(queryCtx, "SELECT pg_sleep(20)")
	}()

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancelQuery()
		select {
		case <-queryDone:
		case <-time.After(2 * time.Second):
		}
		_ = connection.Close(context.Background())
	}
	t.Cleanup(stop)

	pid := int32(connection.PgConn().PID())
	deadline := time.Now().Add(10 * time.Second)
	for {
		var ready bool
		if err := observer.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE pid = $1 AND state = 'active'
			  AND statement_timestamp() - query_start > interval '7 seconds'
		)`, pid).Scan(&ready); err != nil {
			t.Fatalf("observe long query: %v", err)
		}
		if ready {
			return pid, stop
		}
		if time.Now().After(deadline) {
			t.Fatalf("query on PID %d did not cross the long-query threshold", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
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

func createTestCredentialDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "credentials")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create test credential directory: %v", err)
	}
	return directory
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
