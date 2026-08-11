package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestRefreshPlatformDatabaseHealthRecordsFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, "postgres://dbs_monitor@127.0.0.1:1/dbs_monitor?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unavailable platform pool: %v", err)
	}
	defer pool.Close()

	var journal bytes.Buffer
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), log.New(&journal, "", 0))
	refreshPlatformDatabaseHealth(ctx, &db.Pool{Pool: pool}, health, now)

	database := health.Source(platformhealth.SourcePlatformDatabase)
	if database.Status != platformhealth.StatusFailed || database.Code != "PLATFORM_DATABASE_UNREACHABLE" {
		t.Fatalf("platform database health = %+v, want FAILED", database)
	}
	if !strings.Contains(journal.String(), `"event":"platform_health_change"`) ||
		!strings.Contains(journal.String(), `"code":"PLATFORM_DATABASE_UNREACHABLE"`) {
		t.Fatalf("platform database journal event = %q", journal.String())
	}
}

func TestPlatformFailureHandler(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		path             string
		failed           bool
		status           int
		contentType      string
		body             string
		bodyContains     bool
		platformFaultSet bool
	}{
		{
			name: "non-failed platform delegates",
			path: "/instances", status: http.StatusOK, body: "next",
		},
		{
			name: "diagnostics remains available during failure",
			path: "/api/v1/diagnostics/health", failed: true, status: http.StatusOK, body: "next",
		},
		{
			name: "API failure returns JSON",
			path: "/api/v1/instances", failed: true, status: http.StatusServiceUnavailable,
			contentType: "application/json; charset=utf-8",
			body:        `{"error":{"code":"INTERNAL","message":"平台自身故障"}}`, platformFaultSet: true,
		},
		{
			name: "page failure returns dedicated HTML",
			path: "/instances", failed: true, status: http.StatusServiceUnavailable,
			contentType: "text/html; charset=utf-8",
			body:        "平台自身故障", bodyContains: true, platformFaultSet: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), nil)
			if test.failed {
				health.Update(now, platformhealth.DatabaseSource(errors.New("unavailable")))
			}
			next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("next"))
			})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)

			platformFailureHandler(next, health).ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != test.contentType {
				t.Fatalf("content type = %q, want %q", contentType, test.contentType)
			}
			body := response.Body.String()
			bodyMatches := body == test.body
			if test.bodyContains {
				bodyMatches = strings.Contains(body, test.body)
			}
			if !bodyMatches {
				t.Fatalf("body = %q, want %q (contains=%t)", body, test.body, test.bodyContains)
			}
			if strings.Contains(body, "暂无数据") {
				t.Fatalf("body misrepresents platform failure as no data: %q", body)
			}
			if got := response.Header().Get("X-DBS-Platform-Fault") == "true"; got != test.platformFaultSet {
				t.Fatalf("platform fault header set = %t, want %t", got, test.platformFaultSet)
			}
		})
	}
}

func TestPartitionDaysRemaining(t *testing.T) {
	lastSuccess := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		elapsed time.Duration
		want    int
	}{
		{name: "initial window", want: 7},
		{name: "partial day", elapsed: 23*time.Hour + 59*time.Minute, want: 7},
		{name: "one full day", elapsed: 24 * time.Hour, want: 6},
		{name: "six full days", elapsed: 6 * 24 * time.Hour, want: 1},
		{name: "window exhausted", elapsed: 7 * 24 * time.Hour, want: 0},
		{name: "past window is clamped", elapsed: 8 * 24 * time.Hour, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := partitionDaysRemaining(lastSuccess, lastSuccess.Add(test.elapsed)); got != test.want {
				t.Fatalf("partitionDaysRemaining(%s) = %d, want %d", test.elapsed, got, test.want)
			}
		})
	}
}
