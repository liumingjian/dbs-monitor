package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestInstanceListHealthProjectionTracksAlertFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_health_%d", os.Getpid())
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

	userID := uuid.New()
	token := "instance-health-token"
	tokenHash := sha256.Sum256([]byte(token))
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
		VALUES ($1, 'health-reader', '\x00', 'READONLY')`, userID); err != nil {
		t.Fatalf("seed authenticated reader: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_session (token_hash, user_id, expires_at)
		VALUES ($1, $2, now() + interval '1 hour')`, tokenHash[:], userID); err != nil {
		t.Fatalf("seed reader session: %v", err)
	}

	server := httptest.NewTLSServer(httpapi.NewHandler(platform, clock.Real{}, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	serverURL, _ := url.Parse(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "__Host-dbs_monitor_session", Value: token, Path: "/"}})

	targetID := uuid.New()
	otherID := uuid.New()
	for id, name := range map[uuid.UUID]string{targetID: "target", otherID: "uncollected"} {
		ciphertext, version, encryptErr := keyring.EncryptPassword(id, "secret")
		if encryptErr != nil {
			t.Fatalf("encrypt %s password: %v", name, encryptErr)
		}
		if _, err := pool.Exec(ctx, `WITH identity AS (
			INSERT INTO instance_identity (id, name) VALUES ($1, $2) RETURNING id
		), created AS (
			INSERT INTO instance (id, name, host, port, database_name, username, password_ciphertext, password_key_version)
			SELECT id, $2, 'localhost', 5432, 'postgres', 'postgres', $3, $4 FROM identity RETURNING id
		)
		INSERT INTO instance_collection_config (instance_id) SELECT id FROM created`, id, name, ciphertext, version); err != nil {
			t.Fatalf("seed %s instance: %v", name, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_success_at)
		VALUES ($1, 'SERVER_DIRECT', $2)`, targetID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed collection facts: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
		VALUES ($1, $2, '{"role.pg_monitor":"MISSING","ext.pg_stat_statements":"PRESENT","topo.has_replication":"PRESENT","topo.has_slot":"NOT_APPLICABLE"}')`, targetID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("seed capability facts: %v", err)
	}

	ruleIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	for index, rule := range []struct{ name, severity string }{
		{"critical later", "critical"}, {"warning earlier", "warning"}, {"info earliest", "info"},
		{"ignored no data", "critical"}, {"recently recovered", "warning"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO alert_rule
			(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
			 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy, enabled, version)
			VALUES ($1, $2, 'pg.connection.total', 'latest', '>', 10, '<=', 8, 5, 1, 1, $3, 'mark_no_data', true, 1)`, ruleIDs[index], rule.name, rule.severity); err != nil {
			t.Fatalf("seed rule %s: %v", rule.name, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot)
			VALUES ($1, 1, '{}')`, ruleIDs[index]); err != nil {
			t.Fatalf("seed rule version %s: %v", rule.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(instance_id, metric_id, status, updated_at, rule_id, rule_version, severity, current_value,
		 rule_snapshot, metric_dimension_key, first_triggered_at, first_rule_version, first_rule_snapshot,
		 recovered_at, disposition, disposition_by, disposition_at, ignore_reason_code)
		VALUES
		($1, 'pg.connection.total', 'FIRING', $2, $3, 1, 'critical', 91, '{}', 'critical', $2, 1, '{}', NULL, 'NONE', NULL, NULL, NULL),
		($1, 'pg.connection.total', 'FIRING', $4, $5, 1, 'warning', 71, '{}', 'warning', $4, 1, '{}', NULL, 'NONE', NULL, NULL, NULL),
		($1, 'pg.connection.total', 'FIRING', $6, $7, 1, 'info', 51, '{}', 'info', $6, 1, '{}', NULL, 'NONE', NULL, NULL, NULL),
		($1, 'pg.connection.total', 'NO_DATA', $4, $8, 1, 'critical', NULL, '{}', 'ignored', $4, 1, '{}', NULL, 'IGNORED', $9, $4, 'FALSE_POSITIVE'),
		($1, 'pg.connection.total', 'RECOVERED', $10, $11, 1, 'warning', 1, '{}', 'recovered', $6, 1, '{}', $10, 'NONE', NULL, NULL, NULL)`,
		targetID,
		now.Add(-time.Hour), ruleIDs[0],
		now.Add(-2*time.Hour), ruleIDs[1],
		now.Add(-3*time.Hour), ruleIDs[2], ruleIDs[3], userID,
		now.Add(-time.Hour), ruleIDs[4],
	); err != nil {
		t.Fatalf("seed alert facts: %v", err)
	}

	listInstances := func() []instanceProjectionResponse {
		response := getResponse(t, client, server.URL+"/api/v1/instances")
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("list instances status = %d", response.StatusCode)
		}
		var instances []instanceProjectionResponse
		if err := json.NewDecoder(response.Body).Decode(&instances); err != nil {
			t.Fatalf("decode instances: %v", err)
		}
		return instances
	}
	findInstance := func(instances []instanceProjectionResponse, id uuid.UUID) instanceProjectionResponse {
		for _, found := range instances {
			if found.ID == id {
				return found
			}
		}
		t.Fatalf("instance %s missing from projection", id)
		return instanceProjectionResponse{}
	}
	getInstance := func(id uuid.UUID) instanceProjectionResponse {
		response := getResponse(t, client, server.URL+"/api/v1/instances/"+id.String())
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get instance status = %d", response.StatusCode)
		}
		var instance instanceProjectionResponse
		if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
			t.Fatalf("decode instance: %v", err)
		}
		return instance
	}

	initial := findInstance(listInstances(), targetID)
	if initial.Health.Status != "CRITICAL" || initial.Health.Attribution == nil || initial.Health.Attribution.RuleName != "critical later" ||
		initial.Health.Counts != (projectionCounts{Critical: 1, Warning: 1, Info: 1}) ||
		!initial.Health.Flags.NoData || !initial.Health.Flags.RecentlyRecovered || initial.Health.Flags.Ignored != 1 ||
		initial.Health.Flags.ConfigurationMissing != 1 || initial.AgentStatus != "not_installed" || initial.LastCollectedAt == nil ||
		initial.DataFreshnessSeconds == nil || *initial.DataFreshnessSeconds < 60 {
		t.Fatalf("initial target projection = %+v", initial)
	}
	if got := getInstance(targetID); !reflect.DeepEqual(got.Health, initial.Health) {
		t.Fatalf("detail health projection = %+v, want list projection %+v", got.Health, initial.Health)
	}
	if other := findInstance(listInstances(), otherID); other.Health.Status != "UNKNOWN" {
		t.Fatalf("uncollected instance projection = %+v, want UNKNOWN", other)
	}

	if _, err := pool.Exec(ctx, `UPDATE alert_instance SET status = 'RECOVERED', recovered_at = $2
		WHERE instance_id = $1 AND metric_dimension_key IN ('critical', 'warning')`, targetID, now); err != nil {
		t.Fatalf("recover alert facts: %v", err)
	}
	recovered := findInstance(listInstances(), targetID)
	if recovered.Health.Status != "HEALTHY" || recovered.Health.Attribution == nil || recovered.Health.Attribution.RuleName != "info earliest" ||
		recovered.Health.Counts != (projectionCounts{Info: 1}) || recovered.Health.Flags.Ignored != 1 {
		t.Fatalf("recovered target projection = %+v", recovered)
	}

	if _, err := pool.Exec(ctx, `UPDATE instance_collection_config
		SET collection_paused = true, collection_pause_updated_by = $2, collection_pause_updated_at = $3
		WHERE instance_id = $1`, targetID, userID, now); err != nil {
		t.Fatalf("pause instance: %v", err)
	}
	if paused := findInstance(listInstances(), targetID); paused.Health.Status != "PAUSED" || paused.Health.Counts.Info != 1 {
		t.Fatalf("paused target projection = %+v", paused)
	}
}

type instanceProjectionResponse struct {
	ID                   uuid.UUID  `json:"id"`
	AgentStatus          string     `json:"agent_status"`
	LastCollectedAt      *time.Time `json:"last_collected_at"`
	DataFreshnessSeconds *int       `json:"data_freshness_seconds"`
	Health               struct {
		Status      string `json:"status"`
		Attribution *struct {
			RuleName     string   `json:"rule_name"`
			CurrentValue *float64 `json:"current_value"`
		} `json:"attribution"`
		Counts projectionCounts `json:"counts"`
		Flags  struct {
			NoData               bool `json:"no_data"`
			InMaintenance        bool `json:"in_maintenance"`
			RecentlyRecovered    bool `json:"recently_recovered"`
			Ignored              int  `json:"ignored"`
			ConfigurationMissing int  `json:"configuration_missing"`
		} `json:"flags"`
	} `json:"health"`
}

type projectionCounts struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
}
