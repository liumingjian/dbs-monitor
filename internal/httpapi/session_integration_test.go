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

func TestLoginPersistsConcurrentSessionsWithHostCookie(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	platform := openUserTestDatabase(t, ctx)
	if err := SeedAdmin(ctx, platform, "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("seed administrator: %v", err)
	}
	server := httptest.NewTLSServer(NewHandler(platform, clock.Real{}, nil).Routes())
	defer server.Close()

	clients := []*http.Client{userTestClient(t, server), userTestClient(t, server)}
	for index, client := range clients {
		response := userJSONRequest(t, client, http.MethodPost, server.URL+"/api/v1/login", map[string]any{
			"username": "admin", "password": "correct horse battery staple",
		})
		if response.StatusCode != http.StatusNoContent {
			response.Body.Close()
			t.Fatalf("login %d status = %d, want 204", index+1, response.StatusCode)
		}
		cookies := response.Cookies()
		response.Body.Close()
		if len(cookies) != 1 {
			t.Fatalf("login %d cookies = %d, want 1", index+1, len(cookies))
		}
		cookie := cookies[0]
		if cookie.Name != "__Host-dbs_monitor_session" || cookie.Path != "/" || cookie.Domain != "" ||
			!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("login %d cookie = %+v, want __Host- cookie with Path=/, Secure, HttpOnly, SameSite=Strict, and no Domain", index+1, cookie)
		}
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

	assertUserStatus(t, userJSONRequest(t, clients[0], http.MethodPut, server.URL+"/api/v1/password", map[string]any{
		"old_password": "correct horse battery staple", "new_password": "new horse battery staple",
	}), http.StatusNoContent)
	assertUserStatus(t, userJSONRequest(t, clients[0], http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusOK)
	assertUserStatus(t, userJSONRequest(t, clients[1], http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusUnauthorized)

	wrongMediaType, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/logout", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("create non-JSON logout request: %v", err)
	}
	wrongMediaType.Header.Set("Content-Type", "text/plain")
	wrongMediaTypeResponse, err := clients[0].Do(wrongMediaType)
	if err != nil {
		t.Fatalf("send non-JSON logout request: %v", err)
	}
	assertUserStatus(t, wrongMediaTypeResponse, http.StatusUnsupportedMediaType)
	assertUserStatus(t, userJSONRequest(t, clients[0], http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusOK)

	assertUserStatus(t, userJSONRequest(t, clients[0], http.MethodPost, server.URL+"/api/v1/logout", map[string]any{}), http.StatusNoContent)
	assertUserStatus(t, userJSONRequest(t, clients[0], http.MethodGet, server.URL+"/api/v1/me", nil), http.StatusUnauthorized)
}

func TestSessionConfiguredAbsoluteAndIdleDeadlines(t *testing.T) {
	tests := []struct {
		name     string
		exercise func(*testing.T, *sessionTestClock, *http.Client, string)
	}{
		{
			name: "absolute deadline is not extended by activity",
			exercise: func(t *testing.T, currentClock *sessionTestClock, client *http.Client, address string) {
				for range 4 {
					currentClock.Advance(20 * time.Second)
					assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, address+"/api/v1/me", nil), http.StatusOK)
				}
				currentClock.Advance(10 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, address+"/api/v1/me", nil), http.StatusUnauthorized)
			},
		},
		{
			name: "idle deadline is extended only by activity",
			exercise: func(t *testing.T, currentClock *sessionTestClock, client *http.Client, address string) {
				currentClock.Advance(29 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, address+"/api/v1/me", nil), http.StatusOK)
				currentClock.Advance(30 * time.Second)
				assertUserStatus(t, userJSONRequest(t, client, http.MethodGet, address+"/api/v1/me", nil), http.StatusUnauthorized)
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
			currentClock := &sessionTestClock{now: time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)}
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

type sessionTestClock struct {
	now time.Time
}

func (currentClock *sessionTestClock) Now() time.Time {
	return currentClock.now
}

func (currentClock *sessionTestClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	panic("session tests do not use tickers")
}

func (currentClock *sessionTestClock) Advance(elapsed time.Duration) {
	currentClock.now = currentClock.now.Add(elapsed)
}
