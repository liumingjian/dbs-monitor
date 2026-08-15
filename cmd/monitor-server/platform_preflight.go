package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformdb"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

type platformDatabasePrerequisiteEvent struct {
	Event      string          `json:"event"`
	Level      string          `json:"level"`
	Code       platformdb.Code `json:"code"`
	Message    string          `json:"message"`
	ObservedAt time.Time       `json:"observed_at"`
}

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
		event, err := json.Marshal(platformDatabasePrerequisiteEvent{
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

func handlePlatformDatabasePreflightFailure(
	report platformdb.Report,
	recovering bool,
	health *platformhealth.Store,
	logger *log.Logger,
	now time.Time,
) (bool, error) {
	fatalErr := report.FatalError()
	if fatalErr == nil {
		return false, nil
	}
	if !recovering {
		return false, fatalErr
	}

	health.Update(now, platformhealth.SourceSnapshot{
		Source: platformhealth.SourcePlatformDatabase,
		Status: platformhealth.StatusFailed,
		Code:   string(report.Fatal[0].Code),
	})
	for _, finding := range report.Fatal {
		event, err := json.Marshal(platformDatabasePrerequisiteEvent{
			Event:      "platform_database_prerequisite_failure",
			Level:      "ERROR",
			Code:       finding.Code,
			Message:    finding.Message,
			ObservedAt: now.UTC(),
		})
		if err == nil {
			logger.Print(string(event))
		}
	}
	return true, nil
}
