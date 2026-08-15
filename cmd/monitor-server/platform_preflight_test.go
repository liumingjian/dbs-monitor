package main

import (
	"bytes"
	"log"
	"strings"
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
	output := journal.String()
	if got := strings.Count(output, `"event":"platform_database_prerequisite_warning"`); got != len(warnings) {
		t.Fatalf("warning event count = %d, want %d; journal=%q", got, len(warnings), output)
	}
	for _, warning := range warnings {
		if !strings.Contains(output, `"code":"`+string(warning.Code)+`"`) {
			t.Errorf("journal does not enumerate %s: %q", warning.Code, output)
		}
	}
	if !strings.Contains(output, `"level":"WARN"`) {
		t.Fatalf("journal warnings have no WARN level: %q", output)
	}
}
