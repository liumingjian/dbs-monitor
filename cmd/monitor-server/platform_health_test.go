package main

import (
	"bytes"
	"context"
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

func TestPlatformDatabaseFailureRendersFaultPageAndJournal(t *testing.T) {
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

	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) })
	request := httptest.NewRequest(http.MethodGet, "/instances", nil)
	response := httptest.NewRecorder()
	platformFailureHandler(next, health).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("platform fault page status = %d, want 503", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("platform fault page content type = %q, want text/html", contentType)
	}
	if body := response.Body.String(); !strings.Contains(body, "平台自身故障") || strings.Contains(body, "暂无数据") {
		t.Fatalf("platform fault page body = %q", body)
	}
}
