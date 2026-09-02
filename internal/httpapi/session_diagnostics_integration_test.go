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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestSessionSnapshotAndQueryStatisticsStates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_session_diagnostics_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
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

	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "not-used")
	if err != nil {
		t.Fatalf("encrypt instance password: %v", err)
	}
	if _, err := instance.New(platform).CreateInstance(ctx, instance.CreateInstanceParams{
		ID:                 pgtype.UUID{Bytes: instanceID, Valid: true},
		Name:               "session-diagnostics-target",
		Host:               "127.0.0.1",
		Port:               5432,
		Engine:             string(instance.EnginePostgreSQL),
		DatabaseName:       instance.BootstrapDatabaseColumn("postgres"),
		Username:           "monitor",
		PasswordCiphertext: ciphertext,
		PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}

	server := httptest.NewTLSServer(httpapi.NewHandler(platform, clock.Real{}, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}
	login.Body.Close()

	now := time.Now().UTC().Truncate(time.Second)
	seedSessionSnapshot(t, ctx, pool, instanceID, now)
	sessionsURL := fmt.Sprintf("%s/api/v1/instances/%s/sessions", server.URL, instanceID)
	sessionsResponse := getResponse(t, client, sessionsURL)
	var sessions struct {
		SampledAt      *time.Time                   `json:"sampled_at"`
		OriginalCount  *int                         `json:"original_count"`
		Truncated      bool                         `json:"truncated"`
		Unavailability *api.Unavailability          `json:"unavailability"`
		Items          []map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(sessionsResponse.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions snapshot: %v", err)
	}
	sessionsResponse.Body.Close()
	if sessions.SampledAt == nil || sessions.OriginalCount == nil || *sessions.OriginalCount != 750 ||
		!sessions.Truncated || sessions.Unavailability != nil || len(sessions.Items) != 500 {
		t.Fatalf("sessions snapshot = %+v, want capped current snapshot", sessions)
	}
	assertNoSQLFields(t, sessions.Items[0])

	if _, err := pool.Exec(ctx, "UPDATE instance_session_snapshot SET sampled_at = $2 WHERE instance_id = $1", instanceID, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("age session snapshot: %v", err)
	}
	staleResponse := getResponse(t, client, sessionsURL)
	var stale struct {
		Unavailability *api.Unavailability        `json:"unavailability"`
		Items          []api.SessionSnapshotEntry `json:"items"`
	}
	if err := json.NewDecoder(staleResponse.Body).Decode(&stale); err != nil {
		t.Fatalf("decode stale sessions snapshot: %v", err)
	}
	staleResponse.Body.Close()
	if stale.Unavailability == nil || *stale.Unavailability != api.STALE || len(stale.Items) != 0 {
		t.Fatalf("stale sessions snapshot = %+v, want STALE without rows", stale)
	}

	queryStatsURL := fmt.Sprintf("%s/api/v1/instances/%s/query-stats", server.URL, instanceID)
	setQueryStatisticsCapabilities(t, ctx, pool, instanceID, now, "PRESENT", "MISSING")
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.EXTENSIONMISSING)
	setQueryStatisticsCapabilities(t, ctx, pool, instanceID, now, "MISSING", "PRESENT")
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.PERMISSIONDENIED)
	setQueryStatisticsCapabilities(t, ctx, pool, instanceID, now, "PRESENT", "PRESENT")
	setQueryStatisticsTaskState(t, ctx, pool, instanceID, "FAILED", "QUERY_FAILED")
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.COLLECTIONFAILED)
	setQueryStatisticsTaskState(t, ctx, pool, instanceID, "SUCCESS", "COUNTER_RESET")
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.COUNTERRESET)
	setQueryStatisticsTaskState(t, ctx, pool, instanceID, "SUCCESS", "")
	if _, err := pool.Exec(ctx, "INSERT INTO query_statistics_snapshot (instance_id, sampled_at) VALUES ($1, $2)", instanceID, now); err != nil {
		t.Fatalf("insert empty query statistics snapshot: %v", err)
	}
	assertQueryStatisticsUnavailability(t, client, queryStatsURL, api.NODATAINRANGE)
	if _, err := pool.Exec(ctx, `INSERT INTO query_statistics_snapshot_entry
		(instance_id, sampled_at, queryid, database_oid, user_oid, calls, total_exec_time_ms)
		VALUES ($1, $2, 86, 1, 2, 3, 4.5)`, instanceID, now); err != nil {
		t.Fatalf("insert query statistics entry: %v", err)
	}
	available := getResponse(t, client, queryStatsURL)
	var statistics struct {
		Unavailability *api.Unavailability          `json:"unavailability"`
		Items          []map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(available.Body).Decode(&statistics); err != nil {
		t.Fatalf("decode available query statistics: %v", err)
	}
	available.Body.Close()
	if statistics.Unavailability != nil || len(statistics.Items) != 1 {
		t.Fatalf("query statistics = %+v, want one available row", statistics)
	}
	assertNoSQLFields(t, statistics.Items[0])
}

func seedSessionSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, sampledAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO instance_session_snapshot
		(instance_id, sampled_at, original_count, truncated) VALUES ($1, $2, 750, false)`, instanceID, sampledAt); err != nil {
		t.Fatalf("insert session snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instance_session_snapshot_entry
		(instance_id, pid, username, database_name, state, query_started_at,
		 query_duration_ms, wait_event_type, wait_event, blocking_pids)
		SELECT $1, value, 'operator', 'postgres', 'active', $2::timestamptz - interval '6 seconds',
		       6000, 'Lock', 'transactionid', ARRAY[value - 1]
		FROM generate_series(1, 501) value`, instanceID, sampledAt); err != nil {
		t.Fatalf("insert session snapshot entries: %v", err)
	}
}

func setQueryStatisticsCapabilities(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, observedAt time.Time, role, extension string) {
	t.Helper()
	states := fmt.Sprintf(`{"role.pg_monitor":%q,"ext.pg_stat_statements":%q,"topo.has_replication":"NOT_APPLICABLE","topo.has_slot":"NOT_APPLICABLE"}`, role, extension)
	if _, err := pool.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
		VALUES ($1, $2, $3::jsonb)
		ON CONFLICT (instance_id) DO UPDATE SET observed_at = EXCLUDED.observed_at, states = EXCLUDED.states`, instanceID, observedAt, states); err != nil {
		t.Fatalf("set capability snapshot: %v", err)
	}
}

func setQueryStatisticsTaskState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, result, code string) {
	t.Helper()
	var errorCode any
	if code != "" {
		errorCode = code
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instance_collection_task_state
		(instance_id, task_id, last_result, last_error_code) VALUES ($1, 'pg.stat_statements', $2, $3)
		ON CONFLICT (instance_id, task_id) DO UPDATE SET last_result = EXCLUDED.last_result,
		last_error_code = EXCLUDED.last_error_code`, instanceID, result, errorCode); err != nil {
		t.Fatalf("set query statistics task state: %v", err)
	}
}

func assertNoSQLFields(t *testing.T, item map[string]json.RawMessage) {
	t.Helper()
	for _, forbidden := range []string{"query", "sql", "query_text", "sql_text"} {
		if _, exists := item[forbidden]; exists {
			t.Fatalf("response exposes SQL text field %q", forbidden)
		}
	}
}
