package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
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

// 一条「缓存命中率过低」的规则必须看到加权平均值。
//
// 场景是规范里那一句的原样：一个 200GB 主库崩到 60%，同实例下二十个空库各自 100%。
// 告警只在实例级值上判定，而实例级值来自库级序列的收敛——收敛方式选错了不会报错，
// 只会安静地给出一个既不触发也不恢复的数：算术平均给 98%，求和给 2060%。
// 这条用例断言判定用的 current_value 落在主库附近（≈60），而不是那两个数中的任何一个。
func TestCacheHitRatioAlertEvaluatesTheWeightedInstanceValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_alert_weight_%d", os.Getpid())
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

	targetID := createAlertTestInstance(t, ctx, pool, keyring, "weighted target")
	if err := metric.EnsurePartitions(ctx, pool, currentClock.now); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	// 主库：命中率 60%，每秒一万个块的访问量。空库：满分，但几乎没有访问量。
	seedWeightedSample(t, ctx, pool, targetID, "app", currentClock.now, 60, 10_000)
	for index := range 20 {
		seedWeightedSample(t, ctx, pool, targetID, fmt.Sprintf("empty-%d", index), currentClock.now, 100, 1)
	}

	ruleInput := map[string]any{
		"name": "缓存命中率过低", "metric_id": "pg.cache.hit_ratio",
		"aggregation": "latest", "operator": "<", "threshold": 90,
		"recovery_operator": ">=", "recovery_threshold": 95,
		"window_seconds": 60, "consecutive_count": 1, "recovery_consecutive_count": 1,
		"severity": "warning", "no_data_policy": "ignore",
		"scope": "INSTANCES", "instance_ids": []uuid.UUID{targetID},
		"evaluation_interval_seconds": 30, "enabled": true,
	}
	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/alert-rules", ruleInput, "")
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create cache hit ratio rule status = %d, want 201", created.StatusCode)
	}
	var createdRule struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}

	snapshotConnections := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	runAlertEvaluation(t, ctx, evaluator.New(platform, currentClock, snapshotConnections.WithTriggerSnapshotConnection))

	var status string
	var currentValue float64
	if err := pool.QueryRow(ctx, `SELECT status, current_value FROM alert_instance
		WHERE rule_id = $1 AND instance_id = $2`, createdRule.ID, targetID).Scan(&status, &currentValue); err != nil {
		t.Fatalf("read cache hit ratio alert: %v", err)
	}
	// (60 * 10000 + 100 * 1 * 20) / (10000 + 20) = 60.0798…
	if math.Abs(currentValue-60.08) > 0.01 {
		t.Fatalf("evaluated cache hit ratio = %v, want the weighted ≈60.08 (arithmetic mean would be 98.1, a sum 2060)", currentValue)
	}
	if status != "FIRING" {
		t.Fatalf("cache hit ratio alert status = %q, want FIRING", status)
	}
}

// seedWeightedSample 落下一个库在一个时刻上的命中率与它的权重。两条序列同库同时刻，
// 因为加权就是按这一对配对的——权重缺一个点，那一刻那个库就不参与。
func seedWeightedSample(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	database string,
	at time.Time,
	ratio, weight float64,
) {
	t.Helper()
	for _, sample := range []struct {
		metricID string
		value    float64
	}{
		{metricID: metric.MetricCacheHitRatio.String(), value: ratio},
		{metricID: metric.MetricCacheBlockAccessPerS.String(), value: weight},
	} {
		seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
			InstanceID:   pgtype.UUID{Bytes: instanceID, Valid: true},
			MetricID:     sample.metricID,
			DatabaseName: database,
			Labels:       []byte(`{}`),
			LabelsKey:    "{}",
			LastSeen:     pgtype.Timestamptz{Time: at, Valid: true},
		})
		if err != nil {
			t.Fatalf("create %s series for %q: %v", sample.metricID, database, err)
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, at, sample.value,
		); err != nil {
			t.Fatalf("insert %s sample for %q: %v", sample.metricID, database, err)
		}
	}
}
