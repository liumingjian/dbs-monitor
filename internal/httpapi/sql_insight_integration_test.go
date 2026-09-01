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

	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/migrations"
)

// SQL 洞察，在真实 HTTP + 真实库上断言。
//
// 最要紧的一条是**文本去重的可观察行为**：同一条 SQL 采两次，接口只返回一条，
// 且文本按最新的更新。它刻意放在 HTTP 层而不是仓储层——读者关心的是「页面上会不会
// 出现两行一样的 SQL」，不是 query_statement_text 表里有几行。写入走的是采集端
// 那个同一个入口（collect.PersistNormalizedStatementTexts），所以这条断言测的是
// 生产代码的去重，不是测试自己抄的一段 SQL。

type topSqlEntryResponse struct {
	InstanceID      uuid.UUID `json:"instance_id"`
	InstanceName    string    `json:"instance_name"`
	Queryid         string    `json:"queryid"`
	QueryText       *string   `json:"query_text"`
	Calls           int64     `json:"calls"`
	TotalExecTimeMs float64   `json:"total_exec_time_ms"`
}

type topSqlListResponse struct {
	Items []topSqlEntryResponse `json:"items"`
}

func TestSqlInsightTopSqlDeduplicatesStatementText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_sql_insight_%d", os.Getpid())
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

	orders := createInstanceListTestInstance(t, ctx, pool, keyring, "aa-orders", "10.2.0.1")
	ledger := createInstanceListTestInstance(t, ctx, pool, keyring, "bb-ledger", "10.2.0.2")

	// 第一次采集：订单库最费时的那条语句，外加账务库一条更便宜的。
	collectStatements(t, ctx, pool, orders, now.Add(-10*time.Minute), []collectedStatement{
		{queryID: 4242, databaseOID: 16384, userOID: 10, calls: 100, totalExecTimeMS: 5_000,
			text: "SELECT * FROM orders WHERE id = $1"},
	})
	collectStatements(t, ctx, pool, ledger, now.Add(-9*time.Minute), []collectedStatement{
		{queryID: 77, databaseOID: 16384, userOID: 10, calls: 3, totalExecTimeMS: 900,
			text: "SELECT sum(amount) FROM entries WHERE day = $1"},
	})

	// 第二次采集：同一条 SQL 又采了一遍，指标涨了，文本被 PostgreSQL 换了个归一化写法。
	// 同一个 queryid 这一轮还在两个库下各出现一次——合并成一行是这个端点的定义。
	collectStatements(t, ctx, pool, orders, now.Add(-2*time.Minute), []collectedStatement{
		{queryID: 4242, databaseOID: 16384, userOID: 10, calls: 140, totalExecTimeMS: 7_000,
			text: "SELECT * FROM orders WHERE id = $1 /* 第二次采集的文本 */"},
		{queryID: 4242, databaseOID: 16385, userOID: 10, calls: 10, totalExecTimeMS: 500,
			text: "SELECT * FROM orders WHERE id = $1 /* 第二次采集的文本 */"},
	})

	list := getTopSql(t, client, server.URL, "")
	if len(list.Items) != 2 {
		t.Fatalf("top SQL items = %d, want 2 (one row per instance and queryid)", len(list.Items))
	}

	// 排序按总耗时降序，跨实例。
	first, second := list.Items[0], list.Items[1]
	if first.InstanceName != "aa-orders" || second.InstanceName != "bb-ledger" {
		t.Fatalf("top SQL order = %q, %q; want the costliest statement first", first.InstanceName, second.InstanceName)
	}
	if first.Queryid != "4242" {
		t.Fatalf("top SQL queryid = %q, want 4242", first.Queryid)
	}
	// 采两次只留一行，且文本是最新那次的。
	if first.QueryText == nil || *first.QueryText != "SELECT * FROM orders WHERE id = $1 /* 第二次采集的文本 */" {
		t.Fatalf("top SQL text = %v, want the text from the latest collection", first.QueryText)
	}
	// 指标取最近一次快照，两个库的行合并：140 + 10 次调用、7500ms。
	if first.Calls != 150 || first.TotalExecTimeMs != 7_500 {
		t.Fatalf("top SQL metrics = %d calls / %.1f ms, want 150 / 7500.0", first.Calls, first.TotalExecTimeMs)
	}
	if first.InstanceID != orders {
		t.Fatalf("top SQL instance = %s, want %s", first.InstanceID, orders)
	}

	// limit 是「前几条」，不是分页。
	limited := getTopSql(t, client, server.URL, "?limit=1")
	if len(limited.Items) != 1 || limited.Items[0].Queryid != "4242" {
		t.Fatalf("limited top SQL = %+v, want just the costliest row", limited.Items)
	}

	// 总览的第五块与这一页读的是同一份事实。
	overview := getFleetOverviewTopSql(t, client, server.URL)
	if len(overview) != 2 || overview[0].Queryid != first.Queryid || overview[0].QueryText == nil ||
		*overview[0].QueryText != *first.QueryText {
		t.Fatalf("overview top SQL = %+v, want the same ranking as the SQL insight page", overview)
	}

	// 只读用户看得到这一页：SQL 洞察是排查用的读物，不是管理动作。
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role)
		VALUES ($1, 'sql-reader', $2, 'READONLY')`, uuid.New(), passwordHashForInstanceListReader(t)); err != nil {
		t.Fatalf("seed readonly user: %v", err)
	}
	readerClient := loginAlertTestUser(t, server, "sql-reader", "correct horse battery staple")
	readerList := getTopSql(t, readerClient, server.URL, "")
	if len(readerList.Items) != len(list.Items) {
		t.Fatalf("readonly top SQL items = %d, want %d", len(readerList.Items), len(list.Items))
	}

	assertLongQuerySamplesStoreNoStatementText(t, ctx, pool)
}

// assertLongQuerySamplesStoreNoStatementText 守住这张票的安全要求：
// pg_stat_activity 的原始语句在任何情况下都不落库。
//
// 长查询采样是最容易被「顺手加一列语句」的地方——页面上一行只有耗时与等待事件，
// 看起来就差那一列。这里按列名清单断言，所以有人给这张表加一列文本时，
// 变红的是一条写着理由的用例，而不是某次安全评审。
func assertLongQuerySamplesStoreNoStatementText(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_name = 'long_query_sample' ORDER BY column_name`)
	if err != nil {
		t.Fatalf("read long_query_sample columns: %v", err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	want := []string{
		"blocking_pids", "client_address", "database_name", "instance_id", "pid",
		"query_duration_ms", "query_started_at", "sampled_at", "state",
		"transaction_duration_ms", "transaction_started_at", "username", "wait_event", "wait_event_type",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("long_query_sample columns = %v, want %v (raw pg_stat_activity text must never be stored)", got, want)
	}
}

type collectedStatement struct {
	queryID         int64
	databaseOID     uint32
	userOID         uint32
	calls           int64
	totalExecTimeMS float64
	text            string
}

// collectStatements 造一次查询统计采集：一张快照 + 若干条目 + 归一化文本。
// 文本走采集端的那个入口，指标按快照表的形状直接写——两者分表正是这张票的设计。
func collectStatements(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	instanceID uuid.UUID,
	sampledAt time.Time,
	statements []collectedStatement,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO query_statistics_snapshot (instance_id, sampled_at)
		VALUES ($1, $2)`, instanceID, sampledAt); err != nil {
		t.Fatalf("seed query statistics snapshot: %v", err)
	}
	texts := map[int64]string{}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, `INSERT INTO query_statistics_snapshot_entry
			(instance_id, sampled_at, queryid, database_oid, user_oid, calls, total_exec_time_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			instanceID, sampledAt, statement.queryID, statement.databaseOID, statement.userOID,
			statement.calls, statement.totalExecTimeMS); err != nil {
			t.Fatalf("seed query statistics entry: %v", err)
		}
		texts[statement.queryID] = statement.text
	}
	if err := collect.PersistNormalizedStatementTexts(ctx, pool,
		pgtype.UUID{Bytes: instanceID, Valid: true}, sampledAt, texts); err != nil {
		t.Fatalf("persist normalised statement text: %v", err)
	}
}

func getTopSql(t *testing.T, client *http.Client, serverURL, query string) topSqlListResponse {
	t.Helper()
	response := getResponse(t, client, serverURL+"/api/v1/top-sql"+query)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("top SQL status = %d", response.StatusCode)
	}
	var list topSqlListResponse
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatalf("decode top SQL: %v", err)
	}
	return list
}

func getFleetOverviewTopSql(t *testing.T, client *http.Client, serverURL string) []topSqlEntryResponse {
	t.Helper()
	response := getResponse(t, client, serverURL+"/api/v1/overview")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fleet overview status = %d", response.StatusCode)
	}
	var overview struct {
		TopSql []topSqlEntryResponse `json:"top_sql"`
	}
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatalf("decode fleet overview: %v", err)
	}
	return overview.TopSql
}
