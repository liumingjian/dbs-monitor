package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// 实例列表的服务端化与批量趋势端点，都在真实 HTTP + 真实库上断言。
//
// 放在这条缝上而不是仓储层：分页边界、筛选组合、排序稳定性都是接口的表现，
// 而不是某个查询的形状。测的是「同一个地址永远给出同一页」，不是表里有几行。

type instanceListPageResponse struct {
	Items []struct {
		ID     uuid.UUID `json:"id"`
		Name   string    `json:"name"`
		Host   string    `json:"host"`
		Engine string    `json:"engine"`
		Health struct {
			Status string `json:"status"`
			Flags  struct {
				NoData bool `json:"no_data"`
			} `json:"flags"`
		} `json:"health"`
	} `json:"items"`
	Total int `json:"total"`
}

func TestInstanceListPagingFilteringAndBatchedTrends(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_instance_list_%d", os.Getpid())
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

	// 七台实例，覆盖五个健康档位、一个正交标记与三种新鲜度。名称刻意按字典序排开，
	// 排序断言因此看得出「同档位内按名称」这一级。
	named := map[string]uuid.UUID{}
	for index, name := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"} {
		id := createInstanceListTestInstance(t, ctx, pool, keyring, name, fmt.Sprintf("10.0.0.%d", index+1))
		named[name] = id
	}
	// 采集能力快照全部齐备：缺了它，指标读取会以「采集失败」挡在时序前面，
	// 那不是这条用例要测的东西。
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
	for _, name := range []string{"alpha", "bravo", "charlie", "echo", "foxtrot"} {
		collected(name, time.Minute)
	}
	// golf 落后一整天，delta 从来没采到过：两者是「采集新鲜度」排序的两端。
	collected("golf", 24*time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE instance_collection_config SET collection_paused = true WHERE instance_id = $1`, named["echo"]); err != nil {
		t.Fatalf("pause echo: %v", err)
	}

	seedInstanceListAlert(t, ctx, pool, named["bravo"], "critical", "FIRING", now)
	seedInstanceListAlert(t, ctx, pool, named["charlie"], "warning", "FIRING", now)
	seedInstanceListAlert(t, ctx, pool, named["foxtrot"], "info", "NO_DATA", now)

	listInstances := func(query string) instanceListPageResponse {
		t.Helper()
		address := server.URL + "/api/v1/instances"
		if query != "" {
			address += "?" + query
		}
		response := getResponse(t, client, address)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("list instances %q status = %d", query, response.StatusCode)
		}
		var page instanceListPageResponse
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode instance list %q: %v", query, err)
		}
		return page
	}
	names := func(page instanceListPageResponse) []string {
		result := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			result = append(result, item.Name)
		}
		return result
	}
	assertNames := func(query string, wantTotal int, want ...string) instanceListPageResponse {
		t.Helper()
		page := listInstances(query)
		if page.Total != wantTotal {
			t.Fatalf("list %q total = %d, want %d", query, page.Total, wantTotal)
		}
		got := names(page)
		if len(got) != len(want) {
			t.Fatalf("list %q names = %v, want %v", query, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("list %q names = %v, want %v", query, got, want)
			}
		}
		return page
	}

	// 默认排序：严重 → 警告 → 未知 → 正常（同档按名称） → 已暂停。
	assertNames("", 7, "bravo", "charlie", "delta", "alpha", "foxtrot", "golf", "echo")

	// 分页边界：末页只有一条，越过末页得到空的一页与真实的总数（而不是被悄悄夹回最后一页）。
	firstPage := assertNames("page=1&page_size=3", 7, "bravo", "charlie", "delta")
	secondPage := assertNames("page=2&page_size=3", 7, "alpha", "foxtrot", "golf")
	lastPage := assertNames("page=3&page_size=3", 7, "echo")
	assertNames("page=4&page_size=3", 7)
	seen := map[uuid.UUID]bool{}
	for _, page := range []instanceListPageResponse{firstPage, secondPage, lastPage} {
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("instance %s appears on more than one page", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 7 {
		t.Fatalf("paging covered %d instances, want 7", len(seen))
	}

	// 搜索命中名称，也命中地址——地址整列去掉了，但它仍然在搜索索引里。
	assertNames("q=ALPHA", 1, "alpha")
	assertNames("q=10.0.0.4", 1, "delta")
	assertNames("q="+url.QueryEscape("10.0.0.4:5432"), 1, "delta")
	assertNames("q=nothing-matches", 0)

	// 同一组内多值是「或」，组与组之间是「与」。
	assertNames("status=CRITICAL&status=WARNING", 2, "bravo", "charlie")
	assertNames("severity=critical", 1, "bravo")
	assertNames("severity=critical&severity=warning", 2, "bravo", "charlie")
	assertNames("severity=info", 1, "foxtrot")
	assertNames("status=HEALTHY&q=golf", 1, "golf")
	assertNames("status=PAUSED&severity=critical", 0)
	assertNames("engine=POSTGRESQL&page_size=500", 7,
		"bravo", "charlie", "delta", "alpha", "foxtrot", "golf", "echo")

	// 标记是「与」：勾两个标记问的是「同时带这两个」。
	assertNames("flags=NO_DATA", 1, "foxtrot")
	assertNames("flags=NO_DATA&flags=MAINTENANCE", 0)

	// 排序：名称正反两向，以及「最不新鲜的在前」——从来没采到过的排最前面。
	assertNames("sort=name", 7, "alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf")
	assertNames("sort=-name", 7, "golf", "foxtrot", "echo", "delta", "charlie", "bravo", "alpha")
	stalest := listInstances("sort=stalest")
	if stalest.Items[0].Name != "delta" || stalest.Items[1].Name != "golf" {
		t.Fatalf("stalest order = %v, want delta then golf first", names(stalest))
	}

	// 排序稳定：同一个地址连着取两次，顺序逐字相同。
	firstRead := assertNames("sort=name&page=2&page_size=2", 7, "charlie", "delta")
	secondRead := assertNames("sort=name&page=2&page_size=2", 7, "charlie", "delta")
	for index := range firstRead.Items {
		if firstRead.Items[index].ID != secondRead.Items[index].ID {
			t.Fatalf("repeated page differs at %d", index)
		}
	}

	// ---- 批量趋势端点 ----
	from, to := now.Add(-time.Hour), now
	for _, name := range []string{"bravo", "charlie"} {
		seriesID := createAlertTestSeriesForMetric(t, ctx, pool, named[name], now, "pg.tps")
		insertAlertTestSample(t, ctx, pool, seriesID, now.Add(-30*time.Minute), 12)
		insertAlertTestSample(t, ctx, pool, seriesID, now.Add(-10*time.Minute), 34)
	}
	requested := []uuid.UUID{named["bravo"], named["charlie"], named["delta"]}
	address := fmt.Sprintf("%s/api/v1/instances/metrics/series?metric=pg.tps&from=%s&to=%s&step=5m",
		server.URL, url.QueryEscape(from.Format(time.RFC3339Nano)), url.QueryEscape(to.Format(time.RFC3339Nano)))
	for _, id := range requested {
		address += "&instance_id=" + id.String()
	}
	batch := getResponse(t, client, address)
	defer batch.Body.Close()
	if batch.StatusCode != http.StatusOK {
		t.Fatalf("batch metric series status = %d", batch.StatusCode)
	}
	var batchBody struct {
		Step      string `json:"step"`
		Instances []struct {
			InstanceID uuid.UUID `json:"instance_id"`
			Metrics    []struct {
				Metric         string  `json:"metric"`
				Unavailability *string `json:"unavailability"`
				Series         []struct {
					Points [][]*float64 `json:"points"`
				} `json:"series"`
			} `json:"metrics"`
		} `json:"instances"`
	}
	if err := json.NewDecoder(batch.Body).Decode(&batchBody); err != nil {
		t.Fatalf("decode batch metric series: %v", err)
	}
	// 请求里给的每一台都要回来，顺序照旧：调用方按 instance_id 对齐行。
	if len(batchBody.Instances) != len(requested) {
		t.Fatalf("batch covered %d instances, want %d", len(batchBody.Instances), len(requested))
	}
	for index, id := range requested {
		entry := batchBody.Instances[index]
		if entry.InstanceID != id {
			t.Fatalf("batch instance %d = %s, want %s", index, entry.InstanceID, id)
		}
		if len(entry.Metrics) != 1 || entry.Metrics[0].Metric != "pg.tps" {
			t.Fatalf("batch metrics for %s = %+v", id, entry.Metrics)
		}
	}
	for index := 0; index < 2; index++ {
		metrics := batchBody.Instances[index].Metrics[0]
		if metrics.Unavailability != nil || len(metrics.Series) != 1 || len(metrics.Series[0].Points) == 0 {
			t.Fatalf("batch trend for %s = %+v", batchBody.Instances[index].InstanceID, metrics)
		}
	}
	// 一个点都没有的那台不缺席，它带着缺数原因回来——否则调用方分不清「没采到」和「漏发了」。
	missing := batchBody.Instances[2].Metrics[0]
	if missing.Unavailability == nil || *missing.Unavailability != "NO_SAMPLES_YET" {
		t.Fatalf("batch trend for uncollected instance = %+v", missing)
	}

	// 只读用户看得见同样的东西：这两个端点都是 READONLY。
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
		VALUES ($1, 'list-reader', $2, 'READONLY')`, uuid.New(), passwordHashForInstanceListReader(t)); err != nil {
		t.Fatalf("seed readonly user: %v", err)
	}
	readerClient := loginAlertTestUser(t, server, "list-reader", "correct horse battery staple")
	readerResponse := getResponse(t, readerClient, server.URL+"/api/v1/instances?page_size=500")
	defer readerResponse.Body.Close()
	if readerResponse.StatusCode != http.StatusOK {
		t.Fatalf("readonly list status = %d", readerResponse.StatusCode)
	}
	readerBatch := getResponse(t, readerClient, address)
	defer readerBatch.Body.Close()
	if readerBatch.StatusCode != http.StatusOK {
		t.Fatalf("readonly batch status = %d", readerBatch.StatusCode)
	}
}

func createInstanceListTestInstance(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	keyring *instance.CredentialKeyring,
	name, host string,
) uuid.UUID {
	t.Helper()
	instanceID := uuid.New()
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused")
	if err != nil {
		t.Fatalf("encrypt %s credential: %v", name, err)
	}
	if _, err := pool.Exec(ctx, `WITH identity AS (
		INSERT INTO instance_identity (id, name) VALUES ($1, $2) RETURNING id
	), created AS (
		INSERT INTO instance (id, name, engine, host, port, database_name, username, password_ciphertext, password_key_version)
		SELECT id, $2, 'POSTGRESQL', $3, 5432, 'postgres', 'postgres', $4, $5 FROM identity RETURNING id
	)
	INSERT INTO instance_collection_config (instance_id) SELECT id FROM created`,
		instanceID, name, host, ciphertext, keyVersion); err != nil {
		t.Fatalf("seed instance %s: %v", name, err)
	}
	return instanceID
}

func seedInstanceListAlert(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	severity, status string,
	now time.Time,
) {
	t.Helper()
	ruleID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy, enabled, version)
		VALUES ($1, $2, 'pg.connection.total', 'latest', '>', 10, '<=', 8, 5, 1, 1, $3, 'mark_no_data', true, 1)`,
		ruleID, "rule-"+ruleID.String(), severity); err != nil {
		t.Fatalf("seed alert rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot) VALUES ($1, 1, '{}')`, ruleID); err != nil {
		t.Fatalf("seed alert rule version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_instance
		(instance_id, metric_id, status, updated_at, rule_id, rule_version, severity, current_value,
		 rule_snapshot, metric_dimension_key, first_triggered_at, first_rule_version, first_rule_snapshot,
		 recovered_at, disposition, disposition_by, disposition_at, ignore_reason_code)
		VALUES ($1, 'pg.connection.total', $2, $3, $4, 1, $5, 91, '{}', $5, $3, 1, '{}', NULL, 'NONE', NULL, NULL, NULL)`,
		instanceID, status, now.Add(-time.Hour), ruleID, severity); err != nil {
		t.Fatalf("seed alert instance: %v", err)
	}
}

// passwordHashForInstanceListReader 用与管理员相同的口令，只是换一个角色，
// 这样「只读用户看到的东西没变」这条断言不需要另起一套登录流程。
func passwordHashForInstanceListReader(t *testing.T) []byte {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash readonly password: %v", err)
	}
	return hash
}
