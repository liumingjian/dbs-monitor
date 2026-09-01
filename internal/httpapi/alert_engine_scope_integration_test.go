package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// 第二个引擎只存在于这个测试里。生产代码里的引擎全集仍然只有 PostgreSQL 一个
// （internal/dbengine.InstanceEngines），DDL 的 CHECK 也仍然只放 PostgreSQL 进来——
// 这里在测试库上把那两条 CHECK 摘掉、往目录里补一行绑定，只是为了让「跨引擎」这件
// 今天在界面上看不见的事，在它真正发生的那个接缝上看得见。
const (
	engineUnderTest       = "ENGINE_UNDER_TEST"
	engineUnderTestMetric = "other.connection.total"
)

// 一条建在语义位上的规则跨引擎：它存下的是 pg.connection.total，评估时解析到这台实例
// 的引擎绑在 connections 这个位上的那个指标，判定用的是后者的实例级值。
//
// 同一台实例上同时落着一条 pg.connection.total 的序列，取值远在阈值之下：解析要是没生效，
// 这条用例会看到那个数，而不是 700。
func TestSlotRuleEvaluatesTheMetricBoundOnTheInstanceEngine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_alert_engine_%d", os.Getpid())
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
	currentClock := newCurrentFixedClock()

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
	defer server.Close()
	client := loginAlertTestUser(t, server, "admin", "correct horse battery staple")

	declareTestEngine(t, ctx, pool)
	targetID := createAlertTestInstance(t, ctx, pool, keyring, "second engine target")
	if _, err := pool.Exec(ctx, "UPDATE instance SET engine = $2 WHERE id = $1", targetID, engineUnderTest); err != nil {
		t.Fatalf("move the instance onto the second engine: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, pool, currentClock.now); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	seedEngineScopeSample(t, ctx, pool, targetID, engineUnderTestMetric, currentClock.now, 700)
	seedEngineScopeSample(t, ctx, pool, targetID, metric.MetricConnectionTotal.String(), currentClock.now, 3)

	ruleInput := map[string]any{
		"name": "连接数过高", "metric_id": metric.MetricConnectionTotal.String(),
		"aggregation": "latest", "operator": ">=", "threshold": 500,
		"recovery_operator": "<=", "recovery_threshold": 400,
		"window_seconds": 60, "consecutive_count": 1, "recovery_consecutive_count": 1,
		"severity": "warning", "no_data_policy": "ignore",
		"scope": "INSTANCES", "instance_ids": []uuid.UUID{targetID},
		"evaluation_interval_seconds": 30, "enabled": true,
	}
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", ruleInput, "")
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create slot rule scoped to a second-engine instance status = %d, want 201", created.StatusCode)
	}
	var createdRule struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}

	snapshotConnections := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	runAlertEvaluation(t, ctx, evaluator.New(platform, currentClock, snapshotConnections.WithTriggerSnapshotConnection))

	var status, evaluatedMetricID string
	var currentValue float64
	if err := pool.QueryRow(ctx, `SELECT status, metric_id, current_value FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, targetID).Scan(&status, &evaluatedMetricID, &currentValue); err != nil {
		t.Fatalf("read slot rule alert: %v", err)
	}
	if evaluatedMetricID != engineUnderTestMetric {
		t.Errorf("evaluated metric = %q, want the second engine's binding %q", evaluatedMetricID, engineUnderTestMetric)
	}
	// 归因显示的仍是实例级值——本轮不做按库告警，一台实例一条告警一个数。
	if currentValue != 700 {
		t.Errorf("evaluated value = %v, want 700 (3 would mean the rule read the PostgreSQL metric)", currentValue)
	}
	if status != "FIRING" {
		t.Errorf("slot rule alert status = %q, want FIRING", status)
	}

	// 引擎私有指标（复制槽积压没有语义位）连作用域都进不去，而且拒绝要说明白为什么。
	privateRule := map[string]any{
		"name": "Slot 积压", "metric_id": metric.MetricReplicationSlotRetainedWAL.String(),
		"aggregation": "latest", "operator": ">=", "threshold": 1000,
		"recovery_operator": "<=", "recovery_threshold": 500,
		"window_seconds": 60, "consecutive_count": 1, "recovery_consecutive_count": 1,
		"severity": "warning", "no_data_policy": "ignore",
		"scope": "INSTANCES", "instance_ids": []uuid.UUID{targetID},
		"evaluation_interval_seconds": 30, "enabled": true,
	}
	rejected := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", privateRule, "")
	defer rejected.Body.Close()
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope an engine-private rule onto a second-engine instance status = %d, want 400", rejected.StatusCode)
	}
	rejection, err := io.ReadAll(rejected.Body)
	if err != nil {
		t.Fatalf("read rejection: %v", err)
	}
	body := string(rejection)
	for _, fragment := range []string{"second engine target", engineUnderTest, metric.MetricReplicationSlotRetainedWAL.String()} {
		if !strings.Contains(body, fragment) {
			t.Errorf("rejection %q does not say why: missing %q", body, fragment)
		}
	}
}

// declareTestEngine 在测试库里造出第二个引擎：摘掉两条只认 PostgreSQL 的 CHECK，
// 往目录里补一行把 connections 这个位绑到它自己的指标上，Go 侧的目录同样补一行——
// 两份目录必须说同一件事，评估读的是表，作用域校验读的是 internal/metric。
func declareTestEngine(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		"ALTER TABLE instance DROP CONSTRAINT instance_engine_check",
		"ALTER TABLE metric_catalog DROP CONSTRAINT metric_catalog_engine_check",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("relax engine constraint: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO metric_catalog
		(metric_id, engine, unit, display_name, semantic_slot, level, aggregation)
		VALUES ($1, $2, 'count', '总连接数', 'connections', 'INSTANCE', 'NONE')`,
		engineUnderTestMetric, engineUnderTest); err != nil {
		t.Fatalf("bind the connections slot on the second engine: %v", err)
	}

	original := metric.Metrics
	extended := make([]metric.Metric, len(original), len(original)+1)
	copy(extended, original)
	metric.Metrics = append(extended, metric.Metric{
		ID: engineUnderTestMetric, DisplayName: "总连接数", Engine: engineUnderTest,
		Level: metric.LevelInstance, Aggregation: metric.AggregationNone, Slot: metric.SlotConnections,
		Type: metric.MetricTypeGauge, Unit: "count", Dimensions: []string{"instance"},
		Calculation: metric.CalculationRaw, Alertability: metric.AlertabilityYes, Producer: metric.ProducerServerTask,
	})
	t.Cleanup(func() { metric.Metrics = original })
}

func seedEngineScopeSample(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	metricID string,
	at time.Time,
	value float64,
) {
	t.Helper()
	seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true}, MetricID: metricID,
		Labels: []byte(`{}`), LabelsKey: "{}", LastSeen: pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		t.Fatalf("create %s series: %v", metricID, err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, at, value,
	); err != nil {
		t.Fatalf("insert %s sample: %v", metricID, err)
	}
}
