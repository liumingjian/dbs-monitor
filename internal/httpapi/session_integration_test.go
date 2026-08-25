package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/clock"
)

func TestMultipleSessionLifecycleWithHostCookie(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const (
		username    = "admin"
		oldPassword = "correct horse battery staple"
		newPassword = "new horse battery staple"
	)
	platform := openUserTestDatabase(t, ctx)
	if err := SeedAdmin(ctx, platform, username, oldPassword); err != nil {
		t.Fatalf("seed administrator: %v", err)
	}
	server := httptest.NewTLSServer(NewHandler(platform, clock.Real{}, nil).Routes())
	defer server.Close()

	clients := []*http.Client{userTestClient(t, server), userTestClient(t, server)}
	for index, client := range clients {
		response := userJSONRequest(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
			"username": username, "password": oldPassword,
		})
		if response.StatusCode != http.StatusNoContent {
			response.Body.Close()
			t.Fatalf("login %d status = %d, want 204", index+1, response.StatusCode)
		}
		assertHostSessionCookie(t, response, 0)
		response.Body.Close()
	}

	var sessionCount int
	if err := platform.QueryRow(ctx, `SELECT count(*) FROM user_session`).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 2 {
		t.Fatalf("persisted sessions = %d, want 2", sessionCount)
	}
	for _, client := range clients {
		assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusOK)
	}

	currentSession, otherSession := clients[0], clients[1]
	assertUserStatus(t, userJSONRequest(t, currentSession, http.MethodPut, server.URL+"/api/v1/password", map[string]any{
		"old_password": oldPassword, "new_password": newPassword,
	}), http.StatusNoContent)
	assertUserStatus(t, userJSONRequest(t, currentSession, http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusOK)
	assertUserStatus(t, userJSONRequest(t, otherSession, http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusUnauthorized)

	wrongMediaType, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/logout", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("create non-JSON logout request: %v", err)
	}
	wrongMediaType.Header.Set("Content-Type", "text/plain")
	wrongMediaTypeResponse, err := currentSession.Do(wrongMediaType)
	if err != nil {
		t.Fatalf("send non-JSON logout request: %v", err)
	}
	assertUserStatus(t, wrongMediaTypeResponse, http.StatusUnsupportedMediaType)
	assertUserStatus(t, userJSONRequest(t, currentSession, http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusOK)

	logoutResponse := userJSONRequest(t, currentSession, http.MethodPost, server.URL+"/api/v1/logout", map[string]any{})
	assertUserStatus(t, logoutResponse, http.StatusNoContent)
	assertHostSessionCookie(t, logoutResponse, -1)
	assertUserStatus(t, userJSONRequest(t, currentSession, http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusUnauthorized)
}

func assertHostSessionCookie(t *testing.T, response *http.Response, wantMaxAge int) {
	t.Helper()
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("response cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-dbs_monitor_session" || cookie.Path != "/" || cookie.Domain != "" ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != wantMaxAge {
		t.Fatalf("session cookie = %+v, want __Host- cookie with Path=/, Secure, HttpOnly, SameSite=Strict, no Domain, and MaxAge=%d", cookie, wantMaxAge)
	}
}

func TestSessionConfiguredAbsoluteAndIdleDeadlines(t *testing.T) {
	tests := []struct {
		name     string
		exercise func(*testing.T, *clock.Manual, *http.Client, string)
	}{
		{
			name: "absolute deadline is not extended by activity",
			exercise: func(t *testing.T, currentClock *clock.Manual, client *http.Client, serverURL string) {
				for range 4 {
					currentClock.Advance(20 * time.Second)
					assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, serverURL+"/api/v1/me", nil), http.StatusOK)
				}
				currentClock.Advance(10 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, serverURL+"/api/v1/me", nil), http.StatusUnauthorized)
			},
		},
		{
			name: "idle deadline is extended only by activity",
			exercise: func(t *testing.T, currentClock *clock.Manual, client *http.Client, serverURL string) {
				currentClock.Advance(29 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, serverURL+"/api/v1/me", nil), http.StatusOK)
				currentClock.Advance(30 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, serverURL+"/api/v1/me", nil), http.StatusUnauthorized)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			platform := openUserTestDatabase(t, ctx)
			if err := SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
				t.Fatalf("seed administrator: %v", err)
			}
			currentClock := clock.NewManual(time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC))
			handler := NewHandlerWithSessionConfig(platform, currentClock, nil, SessionConfig{
				AbsoluteTTL: 90 * time.Second,
				IdleTTL:     30 * time.Second,
			})
			server := httptest.NewTLSServer(handler.Routes())
			defer server.Close()

			client := userTestClient(t, server)
			assertUserStatus(t, userJSONRequest(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
				"username": "admin", "password": "correct horse battery staple",
			}), http.StatusNoContent)
			test.exercise(t, currentClock, client, server.URL)
		})
	}
}
