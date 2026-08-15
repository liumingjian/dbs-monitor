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
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestCreatedAlertRuleFiresOnNextEvaluationCycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_alert_rule_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	migrationDB := openSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()

	pool, err := pgxpool.New(ctx, connectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	now := time.Now().UTC().Truncate(time.Second)
	currentClock := fixedClock{now: now}

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock).Routes())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	apiClient, err := api.NewClientWithResponses(server.URL, api.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	loginResponse, err := apiClient.CreateSessionWithResponse(ctx, api.CreateSessionJSONRequestBody{
		Username: "admin",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginResponse.StatusCode() != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", loginResponse.StatusCode())
	}
	if cookies := jar.Cookies(loginResponse.HTTPResponse.Request.URL); len(cookies) == 0 {
		t.Fatalf("login response did not populate the cookie jar: %v", loginResponse.HTTPResponse.Header)
	}

	createdInstanceResponse, err := apiClient.CreateInstanceWithResponse(ctx, api.InstanceInput{
		Name:     "target",
		Host:     env("PGHOST", "localhost"),
		Port:     envInt("PGPORT", 55432),
		Database: databaseName,
		Username: env("PGUSER", "dbs_monitor"),
		Password: env("PGPASSWORD", "dbs_monitor"),
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if createdInstanceResponse.StatusCode() != http.StatusCreated || createdInstanceResponse.JSON201 == nil {
		t.Fatalf("create instance status = %d, want 201", createdInstanceResponse.StatusCode())
	}
	instanceID := createdInstanceResponse.JSON201.Instance.Id

	createdRuleResponse, err := apiClient.CreateAlertRuleWithResponse(ctx, api.AlertRuleInput{
		Name:                     "Any PostgreSQL connection",
		MetricId:                 "pg.connection.total",
		Aggregation:              api.Latest,
		Operator:                 api.GreaterThanEqual,
		Threshold:                0,
		RecoveryOperator:         api.LessThan,
		RecoveryThreshold:        -1,
		WindowSeconds:            60,
		ConsecutiveCount:         2,
		RecoveryConsecutiveCount: 2,
		Severity:                 api.Warning,
		NoDataPolicy:             api.MarkNoData,
		Enabled:                  true,
	})
	if err != nil {
		t.Fatalf("create alert rule: %v", err)
	}
	if createdRuleResponse.StatusCode() != http.StatusCreated || createdRuleResponse.JSON201 == nil {
		t.Fatalf("create rule status = %d, want 201", createdRuleResponse.StatusCode())
	}
	createdRule := *createdRuleResponse.JSON201
	if createdRule.Version != 1 {
		t.Fatalf("created rule = %+v, want version 1", createdRule)
	}

	collector := collect.New(platform, monitorpg.DirectDialer{}, currentClock)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect rule sample: %v", err)
	}

	ruleEvaluator := evaluator.New(platform, currentClock)
	for range 2 {
		if err := ruleEvaluator.RunOnce(ctx); err != nil {
			t.Fatalf("evaluate rule: %v", err)
		}
	}

	var status, eventKind string
	var ruleVersion int
	var snapshot []byte
	err = pool.QueryRow(ctx, `SELECT instance.status, event.kind, event.rule_version, event.rule_snapshot
		FROM alert_instance instance
		JOIN alert_event event ON event.alert_instance_id = instance.id
		WHERE instance.rule_id = $1 AND instance.instance_id = $2 AND event.kind = 'FIRED'`,
		createdRule.Id, instanceID).Scan(&status, &eventKind, &ruleVersion, &snapshot)
	if err != nil {
		t.Fatalf("read firing state and event: %v", err)
	}
	if status != "FIRING" || eventKind != "FIRED" || ruleVersion != 1 || !json.Valid(snapshot) {
		t.Fatalf("state/event = %s/%s version=%d snapshot=%s", status, eventKind, ruleVersion, snapshot)
	}
	var ruleSnapshot struct {
		MetricID          string  `json:"metric_id"`
		Threshold         float64 `json:"threshold"`
		RecoveryThreshold float64 `json:"recovery_threshold"`
		Severity          string  `json:"severity"`
		Version           int     `json:"version"`
	}
	if err := json.Unmarshal(snapshot, &ruleSnapshot); err != nil {
		t.Fatalf("decode rule snapshot: %v", err)
	}
	if ruleSnapshot.MetricID != "pg.connection.total" || ruleSnapshot.Threshold != 0 ||
		ruleSnapshot.RecoveryThreshold != -1 || ruleSnapshot.Severity != "warning" || ruleSnapshot.Version != 1 {
		t.Fatalf("rule snapshot = %+v", ruleSnapshot)
	}
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

func (clock fixedClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

var _ clock.Clock = fixedClock{}
