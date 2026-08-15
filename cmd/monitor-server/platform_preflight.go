package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformdb"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func reportPlatformDatabasePreflight(
	report platformdb.Report,
	health *platformhealth.Store,
	logger *log.Logger,
	now time.Time,
) {
	if len(report.Warnings) == 0 {
		health.Update(now, platformhealth.DatabaseSource(nil))
		return
	}
	health.Update(now, platformhealth.SourceSnapshot{
		Source: platformhealth.SourcePlatformDatabase,
		Status: platformhealth.StatusDegraded,
		Code:   "PLATFORM_DATABASE_PREREQUISITES_DEGRADED",
	})
	for _, warning := range report.Warnings {
		event, err := json.Marshal(struct {
			Event      string          `json:"event"`
			Level      string          `json:"level"`
			Code       platformdb.Code `json:"code"`
			Message    string          `json:"message"`
			ObservedAt time.Time       `json:"observed_at"`
		}{
			Event:      "platform_database_prerequisite_warning",
			Level:      "WARN",
			Code:       warning.Code,
			Message:    warning.Message,
			ObservedAt: now.UTC(),
		})
		if err == nil {
			logger.Print(string(event))
		}
	}
}
