package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestIssue60DerivedMetricsAndRealUnavailabilityProducers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d", os.Getpid())
	platformDatabase := "dbs_monitor_issue60_platform_" + suffix
	targetDatabase := "dbs_monitor_issue60_target_" + suffix
	missingExtensionDatabase := "dbs_monitor_issue60_noext_" + suffix
	targetRole := "dbs_monitor_issue60_role_" + suffix
	admin := openSQL(t, env("PGDATABASE", "dbs_monitor"))
	defer admin.Close()

	platformIdentifier := pgx.Identifier{platformDatabase}.Sanitize()
	targetIdentifier := pgx.Identifier{targetDatabase}.Sanitize()
	missingExtensionIdentifier := pgx.Identifier{missingExtensionDatabase}.Sanitize()
	roleIdentifier := pgx.Identifier{targetRole}.Sanitize()
	for _, identifier := range []string{platformIdentifier, targetIdentifier, missingExtensionIdentifier} {
		_, _ = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	}
	_, _ = admin.ExecContext(ctx, "DROP ROLE IF EXISTS "+roleIdentifier)
	if _, err := admin.ExecContext(ctx, "CREATE ROLE "+roleIdentifier+" LOGIN PASSWORD 'issue60-password'"); err != nil {
		t.Fatalf("create issue 60 target role: %v", err)
	}
	for _, identifier := range []string{platformIdentifier, targetIdentifier, missingExtensionIdentifier} {
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
			t.Fatalf("create issue 60 database %s: %v", identifier, err)
		}
	}
	t.Cleanup(func() {
		for _, identifier := range []string{platformIdentifier, targetIdentifier, missingExtensionIdentifier} {
			_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
		}
		_, _ = admin.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+roleIdentifier)
	})

	targetAdmin := openSQL(t, targetDatabase)
	defer targetAdmin.Close()
	if _, err := targetAdmin.ExecContext(ctx, "CREATE EXTENSION pg_stat_statements"); err != nil {
		t.Fatalf("install pg_stat_statements in issue 60 target: %v", err)
	}

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSQL(t, platformDatabase)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate issue 60 platform database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, connectionString(platformDatabase))
	if err != nil {
		t.Fatalf("open issue 60 platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open issue 60 keyring: %v", err)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "issue60-password"); err != nil {
		t.Fatalf("seed issue 60 admin: %v", err)
	}

	currentClock := newCurrentFixedClock()
	if err := metric.EnsurePartitions(ctx, platform, currentClock.now); err != nil {
		t.Fatalf("create metric partitions: %v", err)
	}
	health := platformhealth.NewStore("3.0.0", currentClock.now.Add(-time.Hour), log.New(io.Discard, "", 0))
	server := httptest.NewTLSServer(httpapi.NewHandlerWithPlatformHealth(
		platform, currentClock, keyring, monitorpg.DirectDialer{}, "3.0.0", health,
	).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	login := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
		"username": "admin", "password": "issue60-password",
	}, "")
	login.Body.Close()
	if login.StatusCode != http.StatusNoContent {
		t.Fatalf("issue 60 login status = %d, want 204", login.StatusCode)
	}

	created := requestJSON(t, client, http.MethodPost, server.URL+"/api/v1/instances", api.InstanceCreateInput{
		Name: "issue 60 target", Host: env("PGHOST", "localhost"), Port: envInt("PGPORT", 55432),
		Database: targetDatabase, Username: targetRole, Password: "issue60-password",
	}, "")
	var createdBody api.InstanceCreated
	if err := decodeJSONResponse(created, &createdBody); err != nil {
		t.Fatalf("create issue 60 instance: %v", err)
	}
	instanceID := createdBody.Instance.Id.String()
	seriesURL := func(metricID string, from, to time.Time) string {
		return fmt.Sprintf("%s/api/v1/instances/%s/metrics/series?metric=%s&from=%s&to=%s&step=raw",
			server.URL, instanceID, url.QueryEscape(metricID),
			url.QueryEscape(from.Format(time.RFC3339Nano)), url.QueryEscape(to.Format(time.RFC3339Nano)))
	}
	currentSeriesURL := func(metricID string) string {
		return seriesURL(metricID, currentClock.now.Add(-time.Minute), currentClock.now.Add(time.Minute))
	}
	queryStatsURL := fmt.Sprintf("%s/api/v1/instances/%s/query-stats", server.URL, instanceID)
	producedUnavailability := make(map[api.Unavailability]struct{})
	assertProducedMetric := func(address string, want api.Unavailability) {
		t.Helper()
		assertUnavailability(t, client, address, string(want))
		producedUnavailability[want] = struct{}{}
	}
	assertProducedQueryStatistics := func(want api.Unavailability) {
		t.Helper()
		assertQueryStatisticsUnavailability(t, client, queryStatsURL, want)
		producedUnavailability[want] = struct{}{}
	}

	assertProducedMetric(currentSeriesURL("pg.probe.latency_ms"), api.NOSAMPLESYET)
	assertProducedMetric(currentSeriesURL("host.cpu.usage_percent"), api.NOTAPPLICABLEROLE)
	assertProducedQueryStatistics(api.FEATUREDISABLED)

	collector := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	collector.SetPlatformHealth(health)
	if _, err := admin.ExecContext(ctx, "GRANT pg_monitor TO "+roleIdentifier); err != nil {
		t.Fatalf("grant pg_monitor for issue 60 failure: %v", err)
	}
	if _, err := targetAdmin.ExecContext(ctx, "REVOKE ALL ON pg_stat_statements FROM PUBLIC"); err != nil {
		t.Fatalf("revoke issue 60 query statistics access: %v", err)
	}
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect failed query statistics task: %v", err)
	}
	assertProducedQueryStatistics(api.COLLECTIONFAILED)
	if _, err := targetAdmin.ExecContext(ctx, "GRANT SELECT ON pg_stat_statements TO PUBLIC"); err != nil {
		t.Fatalf("restore issue 60 query statistics access: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "REVOKE pg_monitor FROM "+roleIdentifier); err != nil {
		t.Fatalf("revoke pg_monitor for issue 60 permission state: %v", err)
	}

	currentClock.Advance(6 * time.Minute)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect missing-role target: %v", err)
	}
	assertProducedMetric(currentSeriesURL("pg.connection.total"), api.PERMISSIONDENIED)

	if _, err := admin.ExecContext(ctx, "GRANT pg_monitor TO "+roleIdentifier); err != nil {
		t.Fatalf("grant pg_monitor for issue 60 target: %v", err)
	}
	currentClock.Advance(6 * time.Minute)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect healthy issue 60 target: %v", err)
	}
	assertMetricSeriesHasPoints(t, client, currentSeriesURL("pg.availability.reachable"))
	assertMetricSeriesHasPoints(t, client, currentSeriesURL("collector.last_success_time"))
	assertUnavailability(t, client, currentSeriesURL("pg.replication.wal_lag_bytes"), "NOT_APPLICABLE_ROLE")
	assertProducedMetric(seriesURL(
		"pg.connection.total", currentClock.now.Add(-time.Hour), currentClock.now.Add(-30*time.Minute),
	), api.NODATAINRANGE)

	registration := requestJSON(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/v1/instances/%s/agent/registration", server.URL, instanceID), nil, "")
	var registrationBody api.AgentTokenIssued
	if err := decodeJSONResponse(registration, &registrationBody); err != nil || registrationBody.AgentToken == nil {
		t.Fatalf("register issue 60 Agent: body=%+v error=%v", registrationBody, err)
	}
	report := requestJSON(t, client, http.MethodPost, server.URL+"/api/agent/v1/report", map[string]any{
		"instance_id": instanceID, "agent_version": "2.0.0", "timestamp": currentClock.now,
		"metrics": []map[string]any{{"metric": "host.cpu.usage_percent", "value": 25.0}},
	}, *registrationBody.AgentToken)
	var accepted api.AgentReportAccepted
	if err := decodeJSONResponse(report, &accepted); err != nil {
		t.Fatalf("decode issue 60 Agent report response: %v", err)
	}
	if report.StatusCode != http.StatusOK || accepted.ServerVersion == "" || accepted.ServerTime.IsZero() {
		t.Fatalf("issue 60 Agent report = status %d body %+v, want 200 with server identity and time", report.StatusCode, accepted)
	}
	assertMetricPointValue(t, client, currentSeriesURL("agent.status"), metric.AgentStatusEncodings[metric.AgentStatusOnline])
	for _, item := range metric.Metrics {
		assertMetricCurveOrClosedUnavailability(t, client, currentSeriesURL(item.ID.String()), item.ID.String())
	}

	currentClock.Advance(5 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect issue 60 rate baseline: %v", err)
	}
	if _, err := targetAdmin.ExecContext(ctx, "SELECT pg_stat_reset()"); err != nil {
		t.Fatalf("reset target database counters: %v", err)
	}
	currentClock.Advance(5 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect issue 60 counter reset: %v", err)
	}
	assertProducedMetric(seriesURL(
		"pg.tps", currentClock.now.Add(-time.Second), currentClock.now.Add(time.Second),
	), api.COUNTERRESET)

	currentClock.Advance(20 * time.Second)
	assertProducedMetric(seriesURL(
		"pg.connection.active", currentClock.now.Add(-time.Second), currentClock.now.Add(time.Second),
	), api.STALE)
	currentClock.Advance(metric.AgentOfflineAfter)
	assertProducedMetric(currentSeriesURL("host.cpu.usage_percent"), api.AGENTOFFLINE)
	assertMetricPointValue(t, client, currentSeriesURL("agent.status"), metric.AgentStatusEncodings[metric.AgentStatusOffline])

	unreachable := requestJSON(t, client, http.MethodPut, fmt.Sprintf("%s/api/v1/instances/%s", server.URL, instanceID), api.InstanceMetadataInput{
		Name: "issue 60 target", Host: env("PGHOST", "localhost"), Port: 1, Database: targetDatabase,
	}, "")
	unreachable.Body.Close()
	if unreachable.StatusCode != http.StatusOK {
		t.Fatalf("make issue 60 target unreachable status = %d, want 200", unreachable.StatusCode)
	}
	currentClock.Advance(5 * time.Second)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect issue 60 unreachable target: %v", err)
	}
	assertProducedMetric(currentSeriesURL("pg.connection.total"), api.DBUNREACHABLE)

	updated := requestJSON(t, client, http.MethodPut, fmt.Sprintf("%s/api/v1/instances/%s", server.URL, instanceID), api.InstanceMetadataInput{
		Name: "issue 60 target", Host: env("PGHOST", "localhost"), Port: envInt("PGPORT", 55432),
		Database: missingExtensionDatabase,
	}, "")
	updated.Body.Close()
	if updated.StatusCode != http.StatusOK {
		t.Fatalf("switch issue 60 target database status = %d, want 200", updated.StatusCode)
	}
	currentClock.Advance(6 * time.Minute)
	if err := collector.RunOnce(ctx); err != nil {
		t.Fatalf("collect issue 60 missing-extension target: %v", err)
	}
	assertProducedQueryStatistics(api.EXTENSIONMISSING)

	wantProduced := []api.Unavailability{
		api.NOSAMPLESYET,
		api.NODATAINRANGE,
		api.STALE,
		api.COLLECTIONFAILED,
		api.DBUNREACHABLE,
		api.AGENTOFFLINE,
		api.PERMISSIONDENIED,
		api.EXTENSIONMISSING,
		api.FEATUREDISABLED,
		api.NOTAPPLICABLEROLE,
		api.COUNTERRESET,
	}
	if len(producedUnavailability) != len(wantProduced) {
		t.Fatalf("real unavailability producer count = %d, want %d: %v", len(producedUnavailability), len(wantProduced), producedUnavailability)
	}
	for _, want := range wantProduced {
		if _, ok := producedUnavailability[want]; !ok {
			t.Errorf("real unavailability producer %s was not observed", want)
		}
	}
}

func assertMetricPointValue(t *testing.T, client *http.Client, address string, want float64) {
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
		t.Fatalf("decode projected metric: %v", err)
	}
	if len(body.Metrics) != 1 || len(body.Metrics[0].Series) != 1 || len(body.Metrics[0].Series[0].Points) != 1 ||
		len(body.Metrics[0].Series[0].Points[0]) != 2 || body.Metrics[0].Series[0].Points[0][1] == nil ||
		*body.Metrics[0].Series[0].Points[0][1] != want {
		t.Fatalf("projected metric points = %+v, want value %v", body.Metrics, want)
	}
}

func assertMetricCurveOrClosedUnavailability(t *testing.T, client *http.Client, address, metricID string) {
	t.Helper()
	response := getResponse(t, client, address)
	defer response.Body.Close()
	var body struct {
		Metrics []struct {
			Series []struct {
				Points [][]*float64 `json:"points"`
			} `json:"series"`
			Unavailability *api.Unavailability `json:"unavailability"`
		} `json:"metrics"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode dictionary metric %s: %v", metricID, err)
	}
	if len(body.Metrics) != 1 {
		t.Fatalf("dictionary metric %s response count = %d, want 1", metricID, len(body.Metrics))
	}
	hasPoints := false
	for _, series := range body.Metrics[0].Series {
		if len(series.Points) > 0 {
			hasPoints = true
			break
		}
	}
	if hasPoints {
		if body.Metrics[0].Unavailability != nil {
			t.Fatalf("dictionary metric %s has both points and an unavailability: %+v", metricID, body.Metrics[0])
		}
		return
	}
	if body.Metrics[0].Unavailability == nil {
		t.Fatalf("dictionary metric %s has neither points nor an unavailability: %+v", metricID, body.Metrics[0])
	}
	switch *body.Metrics[0].Unavailability {
	case api.NOSAMPLESYET, api.NODATAINRANGE, api.STALE, api.COLLECTIONFAILED, api.DBUNREACHABLE,
		api.AGENTOFFLINE, api.PERMISSIONDENIED, api.EXTENSIONMISSING, api.FEATUREDISABLED,
		api.NOTAPPLICABLEROLE, api.COUNTERRESET:
		return
	default:
		t.Fatalf("dictionary metric %s has neither points nor a closed unavailability: %+v", metricID, body.Metrics[0])
	}
}

func decodeJSONResponse(response *http.Response, target any) error {
	defer response.Body.Close()
	return json.NewDecoder(response.Body).Decode(target)
}
