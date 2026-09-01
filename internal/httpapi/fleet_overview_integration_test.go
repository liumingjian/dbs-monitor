package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// 机群总览的聚合，在真实 HTTP + 真实库上断言。
//
// 最要紧的一条不是「数字是几」，而是**数字与它下钻到的那一页对得上**：总览说「不新鲜 2 台」，
// 点开的 `?flags=STALE_DATA` 就必须正好是那两台。两处各算各的时候，界面不会报错，
// 只会让读者点开一个数字看到行数不同的一页。所以每个计数都连着它的列表查询一起断言。

type fleetOverviewResponse struct {
	Total  int `json:"total"`
	Health struct {
		Critical int `json:"critical"`
		Warning  int `json:"warning"`
		Unknown  int `json:"unknown"`
		Healthy  int `json:"healthy"`
		Paused   int `json:"paused"`
	} `json:"health"`
	Collection struct {
		StaleData    int `json:"stale_data"`
		AgentOffline int `json:"agent_offline"`
		Paused       int `json:"paused"`
	} `json:"collection"`
	Attention []struct {
		Name   string `json:"name"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
	} `json:"attention"`
	Storage []struct {
		InstanceID   uuid.UUID `json:"instance_id"`
		InstanceName string    `json:"instance_name"`
		UsagePercent float64   `json:"usage_percent"`
		SampledAt    time.Time `json:"sampled_at"`
	} `json:"storage"`
}

func TestFleetOverviewAggregates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_fleet_overview_%d", os.Getpid())
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
	now := currentClock.Now()

	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
	defer server.Close()
	client := loginAlertTestUser(t, server, "admin", "correct horse battery staple")

	// 八台，覆盖五个健康档位与三种采集失效。名称按字典序排开，所以「需要关注」的顺序
	// 里看得出健康档位与严重度这两级排在名称之前。
	named := map[string]uuid.UUID{}
	for index, name := range []string{
		"aa-healthy", "bb-critical-two", "cc-critical-one", "dd-warning",
		"ee-unknown", "ff-paused", "gg-stale", "hh-agent-offline",
	} {
		named[name] = createInstanceListTestInstance(t, ctx, pool, keyring, name, fmt.Sprintf("10.1.0.%d", index+1))
	}
	for _, id := range named {
		if _, err := pool.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
			VALUES ($1, $2, '{"role.pg_monitor":"PRESENT","ext.pg_stat_statements":"PRESENT","topo.has_replication":"PRESENT","topo.has_slot":"PRESENT"}')`,
			id, now.Add(-time.Minute)); err != nil {
			t.Fatalf("seed capability snapshot: %v", err)
		}
	}
	collected := func(name string, age time.Duration) {
		if _, err := pool.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_success_at)
			VALUES ($1, 'SERVER_DIRECT', $2)`, named[name], now.Add(-age)); err != nil {
			t.Fatalf("seed collection state for %s: %v", name, err)
		}
	}
	for _, name := range []string{"aa-healthy", "bb-critical-two", "cc-critical-one", "dd-warning", "ff-paused", "hh-agent-offline"} {
		collected(name, time.Minute)
	}
	// gg 落后两小时（不新鲜），ee 从来没采到过（未知，也不新鲜）。
	collected("gg-stale", 2*time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE instance_collection_config SET collection_paused = true WHERE instance_id = $1`, named["ff-paused"]); err != nil {
		t.Fatalf("pause ff: %v", err)
	}
	// hh 装了 Agent 但一次都没上报：这就是 Agent 离线，而它的服务端直采还是好的 ——
	// 采集自监控这一层要抓的正是这种「看起来正常、其实有一路已经断了」。
	// 注册过的 Agent 才谈得上「离线」：表上的约束要求 agent_expected 的实例同时带着
	// 注册时刻与一枚有效令牌，所以这里照着注册后的形状造，而不是只翻一个布尔位。
	if _, err := pool.Exec(ctx, `UPDATE instance
		SET agent_expected = true,
		    agent_first_registered_at = $2,
		    agent_token_hash = decode(repeat('ab', 32), 'hex'),
		    agent_token_issued_at = $2
		WHERE id = $1`, named["hh-agent-offline"], now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("mark hh agent expected: %v", err)
	}

	seedInstanceListAlert(t, ctx, pool, named["bb-critical-two"], "critical", "FIRING", now)
	seedInstanceListAlert(t, ctx, pool, named["bb-critical-two"], "critical", "FIRING", now)
	seedInstanceListAlert(t, ctx, pool, named["cc-critical-one"], "critical", "FIRING", now)
	seedInstanceListAlert(t, ctx, pool, named["dd-warning"], "warning", "FIRING", now)

	// 容量水位：aa 挂两个盘，实例级取最高的那一个（91），不是两个盘的平均。
	// dd 的样本落在窗口之外，它因此**缺席**，而不是以 0 出现在榜尾。
	seedOverviewDiskSample(t, ctx, pool, named["aa-healthy"], "/", now.Add(-2*time.Minute), 40)
	seedOverviewDiskSample(t, ctx, pool, named["aa-healthy"], "/data", now.Add(-2*time.Minute), 91)
	seedOverviewDiskSample(t, ctx, pool, named["cc-critical-one"], "/", now.Add(-2*time.Minute), 77)
	seedOverviewDiskSample(t, ctx, pool, named["dd-warning"], "/", now.Add(-48*time.Hour), 99)

	overview := getFleetOverview(t, client, server.URL)

	if overview.Total != 8 {
		t.Fatalf("total = %d, want 8", overview.Total)
	}
	if overview.Health.Critical != 2 || overview.Health.Warning != 1 || overview.Health.Unknown != 1 ||
		overview.Health.Healthy != 3 || overview.Health.Paused != 1 {
		t.Fatalf("health counts = %+v", overview.Health)
	}
	if overview.Health.Critical+overview.Health.Warning+overview.Health.Unknown+
		overview.Health.Healthy+overview.Health.Paused != overview.Total {
		t.Fatalf("health tiers %+v do not add up to %d", overview.Health, overview.Total)
	}
	if overview.Collection.StaleData != 2 || overview.Collection.AgentOffline != 1 || overview.Collection.Paused != 1 {
		t.Fatalf("collection counts = %+v", overview.Collection)
	}

	// 需要关注的清单：严重（告警多的在前）→ 警告 → 未知；正常与已暂停一台都不在里面。
	attention := make([]string, 0, len(overview.Attention))
	for _, item := range overview.Attention {
		attention = append(attention, item.Name)
	}
	want := []string{"bb-critical-two", "cc-critical-one", "dd-warning", "ee-unknown"}
	if fmt.Sprint(attention) != fmt.Sprint(want) {
		t.Fatalf("attention = %v, want %v", attention, want)
	}

	if len(overview.Storage) != 2 {
		t.Fatalf("storage usage = %+v, want two instances", overview.Storage)
	}
	if overview.Storage[0].InstanceName != "aa-healthy" || overview.Storage[0].UsagePercent != 91 {
		t.Fatalf("storage usage head = %+v, want aa-healthy at 91", overview.Storage[0])
	}
	if overview.Storage[1].InstanceName != "cc-critical-one" || overview.Storage[1].UsagePercent != 77 {
		t.Fatalf("storage usage second = %+v, want cc-critical-one at 77", overview.Storage[1])
	}

	// 每个数字都下钻得到同样的一页：总览与列表共用同一份判定，这条断言是那句话的证据。
	assertDrillDown := func(query string, count int, names ...string) {
		t.Helper()
		page := getInstanceListPage(t, client, server.URL, query)
		if page.Total != count {
			t.Fatalf("drill-down %q total = %d, want %d", query, page.Total, count)
		}
		got := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			got = append(got, item.Name)
		}
		if fmt.Sprint(got) != fmt.Sprint(names) {
			t.Fatalf("drill-down %q names = %v, want %v", query, got, names)
		}
	}
	assertDrillDown("status=CRITICAL", overview.Health.Critical, "bb-critical-two", "cc-critical-one")
	assertDrillDown("status=WARNING", overview.Health.Warning, "dd-warning")
	assertDrillDown("status=UNKNOWN", overview.Health.Unknown, "ee-unknown")
	assertDrillDown("status=HEALTHY&sort=name", overview.Health.Healthy, "aa-healthy", "gg-stale", "hh-agent-offline")
	assertDrillDown("status=PAUSED", overview.Health.Paused, "ff-paused")
	assertDrillDown("flags=STALE_DATA&sort=name", overview.Collection.StaleData, "ee-unknown", "gg-stale")
	assertDrillDown("flags=AGENT_OFFLINE", overview.Collection.AgentOffline, "hh-agent-offline")
	assertDrillDown("status=PAUSED", overview.Collection.Paused, "ff-paused")

	// 总览是 READONLY：值班的人看得到它，才谈得上「登录就落在这一页」。
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
		VALUES ($1, 'overview-reader', $2, 'READONLY')`, uuid.New(), passwordHashForInstanceListReader(t)); err != nil {
		t.Fatalf("seed readonly user: %v", err)
	}
	readerClient := loginAlertTestUser(t, server, "overview-reader", "correct horse battery staple")
	readerOverview := getFleetOverview(t, readerClient, server.URL)
	if readerOverview.Total != overview.Total {
		t.Fatalf("readonly overview total = %d, want %d", readerOverview.Total, overview.Total)
	}
}

func getFleetOverview(t *testing.T, client *http.Client, serverURL string) fleetOverviewResponse {
	t.Helper()
	response := getResponse(t, client, serverURL+"/api/v1/overview")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fleet overview status = %d", response.StatusCode)
	}
	var overview fleetOverviewResponse
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatalf("decode fleet overview: %v", err)
	}
	return overview
}

func getInstanceListPage(t *testing.T, client *http.Client, serverURL, query string) instanceListPageResponse {
	t.Helper()
	address := serverURL + "/api/v1/instances"
	if query != "" {
		address += "?" + query
	}
	response := getResponse(t, client, address)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("instance list %q status = %d", query, response.StatusCode)
	}
	var page instanceListPageResponse
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode instance list %q: %v", query, err)
	}
	return page
}

// seedOverviewDiskSample 造一条按挂载点分开的磁盘序列并落一个样本。
// 挂载点进 labels：一台机器上两个盘是两条序列，实例级的水位由服务端在它们之间取最大值。
func seedOverviewDiskSample(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	mount string,
	at time.Time,
	value float64,
) {
	t.Helper()
	labels := fmt.Sprintf(`{"mount":%q}`, mount)
	seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true},
		MetricID:   "host.disk.usage_percent",
		Labels:     []byte(labels),
		LabelsKey:  "mount=" + mount,
		LastSeen:   pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		t.Fatalf("create disk series: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, pool, at); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	insertAlertTestSample(t, ctx, pool, seriesID, at, value)
}
