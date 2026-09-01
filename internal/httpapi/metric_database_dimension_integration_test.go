package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// 一条连接下的两个库：工作台能按库看，列表与总览看到的实例级值等于两库之和。
//
// 这是 #217 在 HTTP 边界上的全部行为。两条口径必须由同一个端点在同一份数据上给出，
// 否则「实例级值」和「按库明细」会各自算各自的，对不上时没人知道该信哪个。
func TestMetricSeriesDatabaseDimension(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_dbdim_%d", os.Getpid())
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	})

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
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	clock := newCurrentFixedClock()
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, clock, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	}, "")
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", login.StatusCode)
	}

	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", map[string]any{
		"name":     "two-databases",
		"host":     env("PGHOST", "localhost"),
		"port":     envInt("PGPORT", 55432),
		"username": env("PGUSER", "dbs_monitor"),
		"password": env("PGPASSWORD", "dbs_monitor"),
	}, "")
	createdBody := readResponseBody(t, created, "created instance")
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create instance status = %d, want 201: %s", created.StatusCode, createdBody)
	}
	var createResult api.InstanceCreated
	if err := json.Unmarshal(createdBody, &createResult); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	instanceID := createResult.Instance.Id

	observedAt := clock.Now().Add(-30 * time.Second).UTC().Truncate(time.Second)
	if err := metric.EnsurePartitions(ctx, platform, observedAt); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	// app 与 reporting 是同一条连接下的两个库；连接数是实例级的，没有库维度。
	writeSample(t, ctx, pool, instanceID, metric.MetricTPS, "app", observedAt, 12)
	writeSample(t, ctx, pool, instanceID, metric.MetricTPS, "reporting", observedAt, 30)
	writeSample(t, ctx, pool, instanceID, metric.MetricConnectionTotal, "", observedAt, 7)
	// 采集能力未知会让每个 pg.* 指标直接报 COLLECTION_FAILED，读取路径根本走不到序列上。
	seedPresentCapabilities(t, ctx, pool, instanceID, clock.Now())

	from := url.QueryEscape(observedAt.Add(-time.Minute).Format(time.RFC3339))
	to := url.QueryEscape(observedAt.Add(time.Minute).Format(time.RFC3339))
	seriesURL := func(byDatabase bool) string {
		return fmt.Sprintf(
			"%s/api/v1/instances/%s/metrics/series?metric=pg.tps&metric=pg.connection.total&from=%s&to=%s&step=raw&by_database=%t",
			server.URL, instanceID, from, to, byDatabase)
	}

	// 默认口径：列表与总览显示的实例级值，两库求和。
	aggregated := readMetricSeriesResponse(t, client, seriesURL(false))
	tps := metricByID(t, aggregated, "pg.tps")
	if len(tps.Series) != 1 {
		t.Fatalf("instance-level pg.tps returned %d series, want 1: %+v", len(tps.Series), tps.Series)
	}
	if len(tps.Series[0].Labels) != 0 {
		t.Fatalf("instance-level pg.tps carries dimensions %v, want none", tps.Series[0].Labels)
	}
	if value := singleValue(t, tps.Series[0].Points); value != 42 {
		t.Fatalf("instance-level pg.tps = %v, want 42 (12 + 30)", value)
	}

	// 工作台口径：一库一条序列，库名是那一个具名维度。
	perDatabase := readMetricSeriesResponse(t, client, seriesURL(true))
	tpsByDatabase := metricByID(t, perDatabase, "pg.tps")
	if len(tpsByDatabase.Series) != 2 {
		t.Fatalf("per-database pg.tps returned %d series, want 2: %+v", len(tpsByDatabase.Series), tpsByDatabase.Series)
	}
	values := map[string]float64{}
	for _, item := range tpsByDatabase.Series {
		name, exists := item.Labels["database"]
		if !exists {
			t.Fatalf("per-database series has no database dimension: %+v", item)
		}
		values[name] = singleValue(t, item.Points)
	}
	if values["app"] != 12 || values["reporting"] != 30 {
		t.Fatalf("per-database pg.tps = %v, want app=12 reporting=30", values)
	}
	if values["app"]+values["reporting"] != singleValue(t, tps.Series[0].Points) {
		t.Fatalf("per-database values %v do not sum to the instance-level value", values)
	}

	// 实例级指标不受影响：空维度存取，两种口径给出同一条序列。
	for _, response := range []metricSeriesResponse{aggregated, perDatabase} {
		connections := metricByID(t, response, "pg.connection.total")
		if len(connections.Series) != 1 || len(connections.Series[0].Labels) != 0 {
			t.Fatalf("instance-level metric changed shape: %+v", connections.Series)
		}
		if value := singleValue(t, connections.Series[0].Points); value != 7 {
			t.Fatalf("pg.connection.total = %v, want 7", value)
		}
	}

	// 空维度是存储上的事实，不只是接口上的：实例级指标不许长出库维度。
	var connectionDimensions []string
	rows, err := pool.Query(ctx, `SELECT database_name FROM metric_series
		WHERE instance_id = $1 AND metric_id = 'pg.connection.total'`, instanceID)
	if err != nil {
		t.Fatalf("read instance-level series: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan instance-level series: %v", err)
		}
		connectionDimensions = append(connectionDimensions, name)
	}
	rows.Close()
	if len(connectionDimensions) != 1 || connectionDimensions[0] != "" {
		t.Fatalf("pg.connection.total database dimensions = %q, want one empty string", connectionDimensions)
	}
}

func seedPresentCapabilities(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, observedAt time.Time) {
	t.Helper()
	states := make(map[string]string, len(metric.Capabilities))
	for _, capability := range metric.Capabilities {
		states[string(capability.ID)] = string(metric.CapabilityPresent)
	}
	encoded, err := json.Marshal(states)
	if err != nil {
		t.Fatalf("encode capability states: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
		VALUES ($1, $2, $3)`, instanceID, observedAt, encoded); err != nil {
		t.Fatalf("seed capability snapshot: %v", err)
	}
}

type metricSeriesResponse struct {
	Metrics []struct {
		Metric         string  `json:"metric"`
		Unit           string  `json:"unit"`
		Unavailability *string `json:"unavailability"`
		Series         []struct {
			Labels map[string]string `json:"labels"`
			Points [][]*float64      `json:"points"`
		} `json:"series"`
	} `json:"metrics"`
}

func readMetricSeriesResponse(t *testing.T, client *http.Client, requestURL string) metricSeriesResponse {
	t.Helper()
	response := getResponse(t, client, requestURL)
	body := readResponseBody(t, response, "metric series")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metric series status = %d, want 200: %s", response.StatusCode, body)
	}
	var decoded metricSeriesResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode metric series: %v", err)
	}
	return decoded
}

func metricByID(t *testing.T, response metricSeriesResponse, metricID string) struct {
	Metric         string  `json:"metric"`
	Unit           string  `json:"unit"`
	Unavailability *string `json:"unavailability"`
	Series         []struct {
		Labels map[string]string `json:"labels"`
		Points [][]*float64      `json:"points"`
	} `json:"series"`
} {
	t.Helper()
	for _, entry := range response.Metrics {
		if entry.Metric == metricID {
			if entry.Unavailability != nil {
				t.Fatalf("metric %q is unavailable: %s", metricID, *entry.Unavailability)
			}
			return entry
		}
	}
	t.Fatalf("metric %q is missing from the response", metricID)
	panic("unreachable")
}

func singleValue(t *testing.T, points [][]*float64) float64 {
	t.Helper()
	if len(points) != 1 || len(points[0]) != 2 || points[0][1] == nil {
		t.Fatalf("points = %v, want exactly one two-element point", points)
	}
	return *points[0][1]
}

func writeSample(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	metricID metric.MetricID,
	databaseName string,
	observedAt time.Time,
	value float64,
) {
	t.Helper()
	var seriesID int64
	if err := pool.QueryRow(ctx, `INSERT INTO metric_series (instance_id, metric_id, database_name, labels, labels_key, last_seen)
		VALUES ($1, $2, $3, '{}', '{}', $4) RETURNING series_id`,
		instanceID, string(metricID), databaseName, observedAt,
	).Scan(&seriesID); err != nil {
		t.Fatalf("insert series for %q on %q: %v", metricID, databaseName, err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)",
		seriesID, observedAt, value); err != nil {
		t.Fatalf("insert sample for %q on %q: %v", metricID, databaseName, err)
	}
}
