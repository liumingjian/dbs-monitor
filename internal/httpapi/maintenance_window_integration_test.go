package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

func TestMaintenanceWindowManagementLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, keyring := notificationHTTPTestDatabase(t, ctx)
	defer platform.Close()

	now := time.Now().UTC().Truncate(time.Second)
	currentClock := &maintenanceClock{now: now}
	if err := httpapi.SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	instanceIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for index, instanceID := range instanceIDs {
		ciphertext, keyVersion, err := keyring.EncryptPassword(instanceID, "unused-test-value")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := instance.New(platform).CreateInstance(ctx, instance.CreateInstanceParams{
			ID: pgtype.UUID{Bytes: instanceID, Valid: true}, Name: "maintenance-" + string(rune('a'+index)),
			Host: "127.0.0.1", Port: 5432, DatabaseName: "postgres", Username: "monitor",
			PasswordCiphertext: ciphertext, PasswordKeyVersion: keyVersion,
		}); err != nil {
			t.Fatalf("create maintenance instance: %v", err)
		}
	}

	server := httptest.NewTLSServer(httpapi.NewHandler(platform, currentClock, keyring).Routes())
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

	created := decodeNotificationResponse[api.MaintenanceWindow](t, requestJSON(
		t, client, http.MethodPost, server.URL+"/api/v1/maintenance-windows", map[string]any{
			"instance_ids": []string{instanceIDs[0].String()},
			"starts_at":    now.Add(-time.Minute), "ends_at": now.Add(time.Hour), "reason": "  planned restart  ",
		}, "",
	), http.StatusCreated)
	if created.Status != api.MaintenanceActive || created.Reason != "planned restart" ||
		len(created.InstanceIds) != 1 || created.InstanceIds[0] != instanceIDs[0] {
		t.Fatalf("created maintenance window = %+v", created)
	}

	updated := decodeNotificationResponse[api.MaintenanceWindow](t, requestJSON(
		t, client, http.MethodPut, server.URL+"/api/v1/maintenance-windows/"+created.Id.String(), map[string]any{
			"instance_ids": []string{instanceIDs[0].String(), instanceIDs[1].String()},
			"starts_at":    now.Add(-2 * time.Minute), "ends_at": now.Add(2 * time.Hour), "reason": "minor upgrade",
		}, "",
	), http.StatusOK)
	if updated.Reason != "minor upgrade" || len(updated.InstanceIds) != 2 {
		t.Fatalf("updated maintenance window = %+v", updated)
	}

	ended := decodeNotificationResponse[api.MaintenanceWindow](t, requestJSON(
		t, client, http.MethodPost, server.URL+"/api/v1/maintenance-windows/"+created.Id.String()+"/end", nil, "",
	), http.StatusOK)
	if ended.Status != api.MaintenanceEnded || !ended.EndsAt.Equal(now) {
		t.Fatalf("ended maintenance window = %+v", ended)
	}

	endedUpdate := requestJSON(t, client, http.MethodPut, server.URL+"/api/v1/maintenance-windows/"+created.Id.String(), map[string]any{
		"instance_ids": []string{instanceIDs[0].String()},
		"starts_at":    now, "ends_at": now.Add(time.Hour), "reason": "cannot edit",
	}, "")
	endedUpdate.Body.Close()
	if endedUpdate.StatusCode != http.StatusBadRequest {
		t.Fatalf("update ended maintenance window status = %d, want 400", endedUpdate.StatusCode)
	}

	deleted := requestJSON(t, client, http.MethodDelete, server.URL+"/api/v1/maintenance-windows/"+created.Id.String(), nil, "")
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete maintenance window status = %d, want 204", deleted.StatusCode)
	}
	listed := decodeNotificationResponse[[]api.MaintenanceWindow](t, getResponse(t, client, server.URL+"/api/v1/maintenance-windows"), http.StatusOK)
	if len(listed) != 0 {
		t.Fatalf("maintenance windows after delete = %+v", listed)
	}
	var retainedWindow, retainedScopes int
	if err := platform.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM maintenance_window WHERE id = $1 AND deleted_at = $2),
		(SELECT count(*) FROM maintenance_window_instance WHERE maintenance_window_id = $1)`, created.Id, now).
		Scan(&retainedWindow, &retainedScopes); err != nil {
		t.Fatal(err)
	}
	if retainedWindow != 1 || retainedScopes != 2 {
		t.Fatalf("retained deleted history = %d window, %d scopes; want 1, 2", retainedWindow, retainedScopes)
	}
}

type maintenanceClock struct{ now time.Time }

func (clock *maintenanceClock) Now() time.Time { return clock.now }
func (*maintenanceClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}
