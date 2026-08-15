package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformdb"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestReportPlatformDatabasePreflightEnumeratesWarningsAndDegradesHealth(t *testing.T) {
	warnings := []platformdb.Finding{
		{Code: platformdb.CodeLocaleNonstandard, Message: "locale warning"},
		{Code: platformdb.CodeTimeZoneNonUTC, Message: "timezone warning"},
		{Code: platformdb.CodeInstanceShared, Message: "shared instance warning"},
		{Code: platformdb.CodeMinorVersionOutdated, Message: "minor version warning"},
	}
	var journal bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)

	reportPlatformDatabasePreflight(
		platformdb.Report{Warnings: warnings},
		health,
		log.New(&journal, "", 0),
		now,
	)

	database := health.Source(platformhealth.SourcePlatformDatabase)
	if database.Status != platformhealth.StatusDegraded || database.Code != "PLATFORM_DATABASE_PREREQUISITES_DEGRADED" {
		t.Fatalf("platform database health = %+v, want degraded prerequisites", database)
	}
	decoder := json.NewDecoder(&journal)
	for _, want := range warnings {
		var got struct {
			Event      string          `json:"event"`
			Level      string          `json:"level"`
			Code       platformdb.Code `json:"code"`
			Message    string          `json:"message"`
			ObservedAt time.Time       `json:"observed_at"`
		}
		if err := decoder.Decode(&got); err != nil {
			t.Fatalf("decode warning event for %s: %v", want.Code, err)
		}
		if got.Event != "platform_database_prerequisite_warning" || got.Level != "WARN" ||
			got.Code != want.Code || got.Message != want.Message || !got.ObservedAt.Equal(now) {
			t.Errorf("warning event = %+v, want code=%s message=%q observed_at=%s", got, want.Code, want.Message, now)
		}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("decode event after expected warnings: %v", err)
	}
}

func TestReportPlatformDatabasePreflightReportsHealthyWithoutWarnings(t *testing.T) {
	var journal bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)

	reportPlatformDatabasePreflight(platformdb.Report{}, health, log.New(&journal, "", 0), now)

	database := health.Source(platformhealth.SourcePlatformDatabase)
	if database.Status != platformhealth.StatusOK || database.Code != "PLATFORM_DATABASE_REACHABLE" {
		t.Fatalf("platform database health = %+v, want reachable", database)
	}
	if journal.Len() != 0 {
		t.Fatalf("platform database journal = %q, want no warnings", journal.String())
	}
}

func TestHandlePlatformDatabasePreflightUsesRecoveryPolicy(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	healthy := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)
	if ready, err := handlePlatformDatabasePreflight(
		platformdb.Report{}, false, healthy, log.New(io.Discard, "", 0), now,
	); !ready || err != nil {
		t.Fatalf("healthy result = ready %t, error %v; want ready without error", ready, err)
	}

	report := platformdb.Report{Fatal: []platformdb.Finding{{
		Code:    platformdb.CodeVersionUnsupported,
		Message: "PostgreSQL 17 is required",
	}}}

	startupHealth := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)
	if ready, err := handlePlatformDatabasePreflight(
		report, false, startupHealth, log.New(io.Discard, "", 0), now,
	); ready || err == nil || !bytes.Contains([]byte(err.Error()), []byte(platformdb.CodeVersionUnsupported)) {
		t.Fatalf("startup result = ready %t, error %v; want named fatal error", ready, err)
	}

	var journal bytes.Buffer
	recoveryHealth := platformhealth.NewStore("3.0.0", now.Add(-time.Minute), nil)
	ready, err := handlePlatformDatabasePreflight(
		report, true, recoveryHealth, log.New(&journal, "", 0), now,
	)
	if ready || err != nil {
		t.Fatalf("recovery result = ready %t, error %v; want retry without exit", ready, err)
	}
	database := recoveryHealth.Source(platformhealth.SourcePlatformDatabase)
	if database.Status != platformhealth.StatusFailed || database.Code != string(platformdb.CodeVersionUnsupported) {
		t.Fatalf("recovery health = %+v, want named failed prerequisite", database)
	}
	var event struct {
		Event string          `json:"event"`
		Level string          `json:"level"`
		Code  platformdb.Code `json:"code"`
	}
	if err := json.NewDecoder(&journal).Decode(&event); err != nil {
		t.Fatalf("decode recovery failure event: %v", err)
	}
	if event.Event != "platform_database_prerequisite_failure" || event.Level != "ERROR" ||
		event.Code != platformdb.CodeVersionUnsupported {
		t.Fatalf("recovery failure event = %+v", event)
	}
}
