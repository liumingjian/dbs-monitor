package evaluator

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
)

func TestAcceptance_AC_03_S2_TriggerSnapshotCapturesRealBlockingChainOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseName := fmt.Sprintf("dbs_monitor_trigger_snapshot_%d", os.Getpid())
	admin := openSnapshotSQL(t, snapshotEnv("PGDATABASE", "dbs_monitor"))
	defer admin.Close()
	identifier := pgx.Identifier{databaseName}.Sanitize()
	admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+identifier+" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)") })

	credentialDirectory := createTestCredentialDirectory(t)
	migrationDB := openSnapshotSQL(t, databaseName)
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	migrationDB.Close()
	pool, err := pgxpool.New(ctx, snapshotConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open platform pool: %v", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, false)
	if err != nil {
		t.Fatalf("open credential keyring: %v", err)
	}

	instanceID := uuid.New()
	password := snapshotEnv("PGPASSWORD", "dbs_monitor")
	ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, password)
	if err != nil {
		t.Fatalf("encrypt target password: %v", err)
	}
	if _, err := instance.New(pool).CreateInstance(ctx, instance.CreateInstanceParams{
		ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "snapshot-target",
		Host: snapshotEnv("PGHOST", "localhost"), Port: int32(snapshotEnvInt("PGPORT", 55432)),
		DatabaseName: databaseName, Username: snapshotEnv("PGUSER", "dbs_monitor"),
		PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create target instance: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	currentClock := &snapshotClock{now: now}
	maintenanceOwnerID := uuid.New()
	maintenanceWindowID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO app_user (id, username, password_hash, role, enabled, created_at)
		VALUES ($1, 'snapshot-maintenance-owner', decode('00', 'hex'), 'ALERT_ADMIN', true, $2)`, maintenanceOwnerID, now); err != nil {
		t.Fatalf("create maintenance owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO maintenance_window
		(id, starts_at, ends_at, reason, created_by, created_at, updated_at)
		VALUES ($1, $2, $2::timestamptz + interval '1 minute', 'snapshot maintenance', $3, $2, $2)`, maintenanceWindowID, now, maintenanceOwnerID); err != nil {
		t.Fatalf("create snapshot maintenance window: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO maintenance_window_instance (maintenance_window_id, instance_id)
		VALUES ($1, $2)`, maintenanceWindowID, instanceID); err != nil {
		t.Fatalf("scope snapshot maintenance window: %v", err)
	}
	ruleID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy,
		 enabled, version, scope, evaluation_interval_seconds)
		VALUES ($1, 'blocked sessions', 'pg.session.blocked_count', 'latest', '>=', 1, '<', 0.5,
		 60, 1, 1, 'critical', 'mark_no_data', true, 1, 'INSTANCES', 5)`, ruleID); err != nil {
		t.Fatalf("create snapshot rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot, created_at)
		VALUES ($1, 1, '{"metric_id":"pg.session.blocked_count","threshold":1}', $2)`, ruleID, now); err != nil {
		t.Fatalf("create snapshot rule version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_scope_instance (rule_id, instance_id) VALUES ($1, $2)`, ruleID, instanceID); err != nil {
		t.Fatalf("scope snapshot rule: %v", err)
	}
	if err := metric.EnsurePartitions(ctx, pool, now); err != nil {
		t.Fatalf("ensure metric partitions: %v", err)
	}
	seriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true}, MetricID: "pg.session.blocked_count",
		Labels: []byte(`{}`), LabelsKey: "{}", LastSeen: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("create blocked-session series: %v", err)
	}
	insertSnapshotSample(t, ctx, pool, seriesID, now)

	blocker, err := pgx.Connect(ctx, snapshotConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open blocker connection: %v", err)
	}
	defer blocker.Close(context.Background())
	if _, err := blocker.Exec(ctx, "CREATE TABLE snapshot_lock_target (id integer)"); err != nil {
		t.Fatalf("create lock target: %v", err)
	}
	if _, err := blocker.Exec(ctx, "BEGIN"); err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	if _, err := blocker.Exec(ctx, "LOCK TABLE snapshot_lock_target IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock target: %v", err)
	}
	var blockerPID int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatalf("read blocker PID: %v", err)
	}
	const waiterCount = 5
	waiters := make([]*pgx.Conn, 0, waiterCount)
	waiterPIDs := make([]int32, 0, waiterCount)
	waiterDone := make(chan error, waiterCount)
	for index := 0; index < waiterCount; index++ {
		waiter, err := pgx.Connect(ctx, snapshotConnectionString(databaseName))
		if err != nil {
			t.Fatalf("open waiter connection %d: %v", index, err)
		}
		waiters = append(waiters, waiter)
		t.Cleanup(func() { waiter.Close(context.Background()) })
		if _, err := waiter.Exec(ctx, "BEGIN"); err != nil {
			t.Fatalf("begin waiter %d: %v", index, err)
		}
		var waiterPID int32
		if err := waiter.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&waiterPID); err != nil {
			t.Fatalf("read waiter PID %d: %v", index, err)
		}
		waiterPIDs = append(waiterPIDs, waiterPID)
		go func(waitingConnection *pgx.Conn) {
			_, waitErr := waitingConnection.Exec(ctx, "SELECT * FROM snapshot_lock_target")
			waiterDone <- waitErr
		}(waiter)
		waitForBlocker(t, ctx, pool, waiterPID, blockerPID)
	}

	snapshotConnections := collect.New(platform, monitorpg.DirectDialer{}, currentClock, keyring)
	service, err := NewWithConfig(platform, currentClock, snapshotConnections.WithTriggerSnapshotConnection, Config{TriggerSnapshotSessionLimit: 5})
	if err != nil {
		t.Fatalf("configure evaluator: %v", err)
	}
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate blocking alert: %v", err)
	}
	snapshotConnections.Close()
	if _, err := blocker.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("release blocker: %v", err)
	}
	for index := range waiters {
		if err := <-waiterDone; err != nil {
			t.Fatalf("waiting query %d after lock release: %v", index, err)
		}
	}
	for index, waiter := range waiters {
		if err := waiter.Close(ctx); err != nil {
			t.Fatalf("close waiter connection %d: %v", index, err)
		}
	}
	if err := blocker.Close(ctx); err != nil {
		t.Fatalf("close blocker connection: %v", err)
	}

	var alertInstanceID, snapshotID, eventSnapshotID uuid.UUID
	var originalMatchCount, sessionCount int
	var truncated bool
	if err := pool.QueryRow(ctx, `SELECT alert.id, snapshot.id, snapshot.original_match_count, snapshot.truncated,
		count(session.pid)
		FROM alert_instance alert
		JOIN alert_trigger_snapshot snapshot ON snapshot.alert_instance_id = alert.id AND snapshot.result = 'SUCCESS'
		JOIN alert_trigger_snapshot_session session ON session.snapshot_id = snapshot.id
		WHERE alert.rule_id = $1
		GROUP BY alert.id, snapshot.id`, ruleID).
		Scan(&alertInstanceID, &snapshotID, &originalMatchCount, &truncated, &sessionCount); err != nil {
		t.Fatalf("read successful trigger snapshot: %v", err)
	}
	if originalMatchCount != 6 || sessionCount != 5 || !truncated {
		t.Fatalf("blocking snapshot = original %d sessions %d truncated %t, want 6/5/true", originalMatchCount, sessionCount, truncated)
	}
	var retainedBlockerCount, retainedWaiterCount int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE pid = $2),
		count(*) FILTER (WHERE pid = ANY($3) AND blocking_pids = ARRAY[$2]::integer[])
		FROM alert_trigger_snapshot_session WHERE snapshot_id = $1`, snapshotID, blockerPID, waiterPIDs).
		Scan(&retainedBlockerCount, &retainedWaiterCount); err != nil {
		t.Fatalf("read retained blocking chain: %v", err)
	}
	if retainedBlockerCount != 1 || retainedWaiterCount != 4 {
		t.Fatalf("retained blocking chain = blocker %d waiters %d, want 1/4", retainedBlockerCount, retainedWaiterCount)
	}
	if err := pool.QueryRow(ctx, `SELECT trigger_snapshot_id FROM alert_event
		WHERE alert_instance_id = $1 AND kind = 'FIRED'`, alertInstanceID).Scan(&eventSnapshotID); err != nil {
		t.Fatalf("read FIRED event snapshot: %v", err)
	}
	if eventSnapshotID != snapshotID {
		t.Fatalf("FIRED event snapshot = %s, want %s", eventSnapshotID, snapshotID)
	}
	var performanceEventID uuid.UUID
	var performanceEventType string
	var performanceEventDerivedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT id, event_type, derived_at FROM performance_event
		WHERE alert_instance_id = $1`, alertInstanceID).
		Scan(&performanceEventID, &performanceEventType, &performanceEventDerivedAt); err != nil {
		t.Fatalf("read derived performance event: %v", err)
	}
	if performanceEventType != "LOCK_BLOCKING" || !performanceEventDerivedAt.Equal(now) {
		t.Fatalf("derived performance event = %s at %s, want LOCK_BLOCKING at %s",
			performanceEventType, performanceEventDerivedAt, now)
	}
	if err := httpapi.SeedAdmin(ctx, platform, "snapshot-admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed snapshot API administrator: %v", err)
	}
	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	apiClient, err := api.NewClientWithResponses(server.URL, api.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("create generated snapshot API client: %v", err)
	}
	login, err := apiClient.CreateSessionWithResponse(ctx, api.CreateSessionJSONRequestBody{
		Username: "snapshot-admin", Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("log in with generated snapshot API client: %v", err)
	}
	if login.StatusCode() != http.StatusNoContent {
		t.Fatalf("snapshot API login status = %d, want 204", login.StatusCode())
	}
	snapshotResponse, err := apiClient.GetAlertTriggerSnapshotWithResponse(ctx, alertInstanceID)
	if err != nil {
		t.Fatalf("read snapshot with generated API client: %v", err)
	}
	if snapshotResponse.StatusCode() != http.StatusOK || snapshotResponse.JSON200 == nil ||
		snapshotResponse.JSON200.Result != api.TriggerSnapshotSuccess ||
		snapshotResponse.JSON200.OriginalMatchCount != 6 || len(snapshotResponse.JSON200.Sessions) != 5 ||
		!snapshotResponse.JSON200.Truncated {
		t.Fatalf("successful snapshot API response = status %d body %+v", snapshotResponse.StatusCode(), snapshotResponse.JSON200)
	}
	eventListURL := fmt.Sprintf("%s/api/v1/instances/%s/performance-events?from=%s&to=%s",
		server.URL, instanceID, now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(time.Minute).Format(time.RFC3339Nano))
	eventPage := readPerformanceEventPage(t, client, eventListURL)
	if eventPage.Total != 1 || len(eventPage.Items) != 1 {
		t.Fatalf("performance event page = %+v", eventPage)
	}
	eventItem := eventPage.Items[0]
	if eventItem.Id != performanceEventID || eventItem.AlertInstanceId != alertInstanceID ||
		eventItem.EventType != api.EventLockBlocking || eventItem.AlertStatus != api.FIRING ||
		eventItem.Disposition != api.AlertDispositionNONE || eventItem.TriggerSnapshotResult != api.TriggerSnapshotSuccess ||
		!eventItem.InMaintenance || eventItem.MaintenanceWindowId == nil || *eventItem.MaintenanceWindowId != maintenanceWindowID {
		t.Fatalf("derived performance event projection = %+v", eventItem)
	}
	for _, contextValue := range []string{"pg.session.blocked_count", "1"} {
		if !strings.Contains(eventItem.CauseSummary, contextValue) {
			t.Errorf("performance event cause %q does not contain %q", eventItem.CauseSummary, contextValue)
		}
	}

	eventDetailURL := server.URL + "/api/v1/performance-events/" + performanceEventID.String()
	eventDetail := readPerformanceEvent(t, client, eventDetailURL)
	if eventDetail.Id != performanceEventID || eventDetail.SuggestedAction == "" ||
		!eventDetail.InMaintenance || eventDetail.MaintenanceWindowId == nil || *eventDetail.MaintenanceWindowId != maintenanceWindowID {
		t.Fatalf("performance event detail = %+v", eventDetail)
	}
	dispositionResponse := snapshotRequestJSON(t, client, http.MethodPut,
		server.URL+"/api/v1/alert-instances/"+alertInstanceID.String()+"/disposition",
		map[string]any{"disposition": "ACKED", "note": "investigating"})
	dispositionResponse.Body.Close()
	if dispositionResponse.StatusCode != http.StatusOK {
		t.Fatalf("performance event alert disposition status = %d, want 200", dispositionResponse.StatusCode)
	}
	eventDetail = readPerformanceEvent(t, client, eventDetailURL)
	if eventDetail.Disposition != api.AlertDispositionACKED {
		t.Fatalf("performance event disposition = %s, want ACKED", eventDetail.Disposition)
	}
	var storedDisposition string
	if err := pool.QueryRow(ctx, "SELECT disposition FROM alert_instance WHERE id = $1", alertInstanceID).Scan(&storedDisposition); err != nil {
		t.Fatalf("read event-backed alert disposition: %v", err)
	}
	if storedDisposition != "ACKED" {
		t.Fatalf("event-backed alert disposition = %s, want ACKED", storedDisposition)
	}
	eventPage = readPerformanceEventPage(t, client, eventListURL+"&disposition=ACKED")
	if eventPage.Total != 1 {
		t.Fatalf("acknowledged performance event page = %+v", eventPage)
	}

	currentClock.now = currentClock.now.Add(5 * time.Second)
	insertSnapshotSample(t, ctx, pool, seriesID, currentClock.now)
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("reevaluate sustained blocking alert: %v", err)
	}
	var snapshotCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_trigger_snapshot WHERE alert_instance_id = $1`, alertInstanceID).Scan(&snapshotCount); err != nil {
		t.Fatalf("count trigger snapshots: %v", err)
	}
	if snapshotCount != 1 {
		t.Fatalf("trigger snapshot count after sustained firing = %d, want 1", snapshotCount)
	}
	var performanceEventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM performance_event WHERE alert_instance_id = $1`, alertInstanceID).Scan(&performanceEventCount); err != nil {
		t.Fatalf("count performance events: %v", err)
	}
	if performanceEventCount != 1 {
		t.Fatalf("performance event count after sustained firing = %d, want 1", performanceEventCount)
	}
	currentClock.now = currentClock.now.Add(5 * time.Second)
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 0)", seriesID, currentClock.now); err != nil {
		t.Fatalf("insert recovered blocked-session sample: %v", err)
	}
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("recover blocking alert: %v", err)
	}
	eventDetail = readPerformanceEvent(t, client, eventDetailURL)
	if eventDetail.AlertStatus != api.RECOVERED || eventDetail.RecoveredAt == nil || eventDetail.DurationMs != 10_000 {
		t.Fatalf("recovered performance event projection = %+v, want RECOVERED after 10000ms", eventDetail)
	}
	eventPage = readPerformanceEventPage(t, client, eventListURL+"&recovered=true")
	if eventPage.Total != 1 {
		t.Fatalf("recovered performance event page = %+v", eventPage)
	}

	nonApplicableRuleID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule
		(id, name, metric_id, aggregation, operator, threshold, recovery_operator, recovery_threshold,
		 window_seconds, consecutive_count, recovery_consecutive_count, severity, no_data_policy,
		 enabled, version, scope, evaluation_interval_seconds)
		VALUES ($1, 'total connections', 'pg.connection.total', 'latest', '>=', 1, '<', 0.5,
		 60, 1, 1, 'warning', 'mark_no_data', true, 1, 'INSTANCES', 5)`, nonApplicableRuleID); err != nil {
		t.Fatalf("create non-applicable rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot, created_at)
		VALUES ($1, 1, '{"metric_id":"pg.connection.total"}', $2)`, nonApplicableRuleID, currentClock.now); err != nil {
		t.Fatalf("create non-applicable rule version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO alert_rule_scope_instance (rule_id, instance_id) VALUES ($1, $2)`, nonApplicableRuleID, instanceID); err != nil {
		t.Fatalf("scope non-applicable rule: %v", err)
	}
	nonApplicableSeriesID, err := metric.New(pool).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: pgtype.UUID{Bytes: instanceID, Valid: true}, MetricID: "pg.connection.total",
		Labels: []byte(`{}`), LabelsKey: "{}", LastSeen: pgtype.Timestamptz{Time: currentClock.now, Valid: true},
	})
	if err != nil {
		t.Fatalf("create non-applicable series: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 2)", nonApplicableSeriesID, currentClock.now); err != nil {
		t.Fatalf("insert non-applicable metric sample: %v", err)
	}
	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("evaluate non-applicable alert: %v", err)
	}
	var nonApplicableSnapshotCount, nonApplicableEventReferences, nonApplicablePerformanceEvents int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM alert_trigger_snapshot snapshot WHERE snapshot.alert_instance_id = alert.id),
		(SELECT count(*) FROM alert_event event WHERE event.alert_instance_id = alert.id
		 AND event.kind = 'FIRED' AND event.trigger_snapshot_id IS NOT NULL),
		(SELECT count(*) FROM performance_event event WHERE event.alert_instance_id = alert.id)
		FROM alert_instance alert WHERE alert.rule_id = $1`, nonApplicableRuleID).
		Scan(&nonApplicableSnapshotCount, &nonApplicableEventReferences, &nonApplicablePerformanceEvents); err != nil {
		t.Fatalf("read non-applicable snapshot state: %v", err)
	}
	if nonApplicableSnapshotCount != 0 || nonApplicableEventReferences != 0 || nonApplicablePerformanceEvents != 0 {
		t.Fatalf("non-applicable derived state = snapshots %d references %d events %d, want 0/0/0",
			nonApplicableSnapshotCount, nonApplicableEventReferences, nonApplicablePerformanceEvents)
	}

	var forbiddenColumns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'alert_trigger_snapshot_session'
		  AND (column_name = 'query' OR column_name LIKE '%sql%')`).Scan(&forbiddenColumns); err != nil {
		t.Fatalf("inspect snapshot session columns: %v", err)
	}
	if forbiddenColumns != 0 {
		t.Fatalf("snapshot session table has %d SQL text columns", forbiddenColumns)
	}
}

func TestTriggerSnapshotQueryPGMatrix(t *testing.T) {
	ports := strings.Fields(os.Getenv("SNAPSHOT_MATRIX_PORTS"))
	if len(ports) == 0 {
		t.Skip("SNAPSHOT_MATRIX_PORTS is not set")
	}
	wantNames := []string{
		"pid", "username", "database_name", "client_address", "state",
		"query_started_at", "transaction_started_at", "query_duration_ms",
		"transaction_duration_ms", "wait_event_type", "wait_event", "blocking_pids",
	}
	wantOIDs := []uint32{23, 25, 25, 25, 25, 1184, 1184, 20, 20, 25, 25, 1007}
	for _, port := range ports {
		t.Run("port_"+port, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			config, err := pgx.ParseConfig(fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
				snapshotEnv("SNAPSHOT_MATRIX_HOST", "localhost"), port,
				snapshotEnv("SNAPSHOT_MATRIX_USER", "monitored"), snapshotEnv("SNAPSHOT_MATRIX_PASSWORD", "monitored"),
				snapshotEnv("SNAPSHOT_MATRIX_DATABASE", "monitored")))
			if err != nil {
				t.Fatalf("parse matrix connection: %v", err)
			}
			conn, err := pgx.ConnectConfig(ctx, config)
			if err != nil {
				t.Fatalf("connect matrix database: %v", err)
			}
			defer conn.Close(context.Background())
			rows, err := conn.Query(ctx, triggerSnapshotSessionsSQL)
			if err != nil {
				t.Fatalf("run trigger snapshot query: %v", err)
			}
			defer rows.Close()
			fields := rows.FieldDescriptions()
			if len(fields) != len(wantNames) {
				t.Fatalf("snapshot query columns = %d, want %d", len(fields), len(wantNames))
			}
			for index, field := range fields {
				if field.Name != wantNames[index] || field.DataTypeOID != wantOIDs[index] {
					t.Errorf("column %d = %s/%d, want %s/%d", index, field.Name, field.DataTypeOID, wantNames[index], wantOIDs[index])
				}
			}
		})
	}
}

func waitForBlocker(t *testing.T, ctx context.Context, pool *pgxpool.Pool, waiterPID, blockerPID int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var blockers []int32
		if err := pool.QueryRow(ctx, "SELECT pg_blocking_pids($1)", waiterPID).Scan(&blockers); err == nil && len(blockers) == 1 && blockers[0] == blockerPID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("PID %d did not become blocked by PID %d", waiterPID, blockerPID)
}

func insertSnapshotSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seriesID int64, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 1)", seriesID, at); err != nil {
		t.Fatalf("insert blocked-session sample: %v", err)
	}
}

func snapshotRequestJSON(t *testing.T, client *http.Client, method, address string, body any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode snapshot API request: %v", err)
	}
	request, err := http.NewRequest(method, address, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("create snapshot API request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send snapshot API request: %v", err)
	}
	return response
}

func readPerformanceEvent(t *testing.T, client *http.Client, address string) api.PerformanceEvent {
	t.Helper()
	response := snapshotRequestJSON(t, client, http.MethodGet, address, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("performance event detail status = %d, want 200", response.StatusCode)
	}
	var event api.PerformanceEvent
	if err := json.NewDecoder(response.Body).Decode(&event); err != nil {
		t.Fatalf("decode performance event detail: %v", err)
	}
	return event
}

func readPerformanceEventPage(t *testing.T, client *http.Client, address string) api.PerformanceEventPage {
	t.Helper()
	response := snapshotRequestJSON(t, client, http.MethodGet, address, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("performance event list status = %d, want 200", response.StatusCode)
	}
	var page api.PerformanceEventPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode performance event page: %v", err)
	}
	return page
}

func openSnapshotSQL(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", snapshotConnectionString(databaseName))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		t.Fatalf("ping database: %v", err)
	}
	return database
}

func snapshotConnectionString(databaseName string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		snapshotEnv("PGHOST", "localhost"), snapshotEnvInt("PGPORT", 55432),
		snapshotEnv("PGUSER", "dbs_monitor"), snapshotEnv("PGPASSWORD", "dbs_monitor"), databaseName)
}

func snapshotEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func snapshotEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(snapshotEnv(name, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

type snapshotClock struct{ now time.Time }

func (current *snapshotClock) Now() time.Time { return current.now }

func (*snapshotClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}

var _ clock.Clock = (*snapshotClock)(nil)
