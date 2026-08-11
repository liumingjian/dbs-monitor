package alerting

import "time"

type HealthStatus string

const (
	HealthCritical HealthStatus = "CRITICAL"
	HealthWarning  HealthStatus = "WARNING"
	HealthUnknown  HealthStatus = "UNKNOWN"
	HealthHealthy  HealthStatus = "HEALTHY"
	HealthPaused   HealthStatus = "PAUSED"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type HealthAlertExclusion uint8

const (
	HealthAlertIncluded HealthAlertExclusion = iota
	HealthAlertStructurallyNotApplicable
	HealthAlertConfigurationMissing
)

type HealthAlert struct {
	RuleName         string
	Severity         Severity
	State            State
	FirstTriggeredAt time.Time
	CurrentValue     *float64
	RecoveredAt      *time.Time
	Ignored          bool
	Exclusion        HealthAlertExclusion
}

type HealthRollupInput struct {
	Paused               bool
	EverCollected        bool
	InMaintenance        bool
	ConfigurationMissing int
	Now                  time.Time
	Alerts               []HealthAlert
}

type HealthAttribution struct {
	RuleName     string
	CurrentValue *float64
}

type HealthAlertCounts struct {
	Critical int
	Warning  int
	Info     int
}

type HealthFlags struct {
	NoData               bool
	InMaintenance        bool
	RecentlyRecovered    bool
	Ignored              int
	ConfigurationMissing int
}

type HealthRollup struct {
	Status      HealthStatus
	Attribution *HealthAttribution
	Counts      HealthAlertCounts
	Flags       HealthFlags
}

func RollupInstanceHealth(input HealthRollupInput) HealthRollup {
	result := HealthRollup{Flags: HealthFlags{
		InMaintenance:        input.InMaintenance,
		ConfigurationMissing: input.ConfigurationMissing,
	}}
	var attributed *HealthAlert
	for index := range input.Alerts {
		alert := &input.Alerts[index]
		if alert.Exclusion != HealthAlertIncluded {
			continue
		}
		if alert.State == RECOVERED {
			if alert.RecoveredAt != nil && !alert.RecoveredAt.After(input.Now) &&
				!alert.RecoveredAt.Before(input.Now.Add(-24*time.Hour)) {
				result.Flags.RecentlyRecovered = true
			}
			continue
		}
		if alert.State != FIRING && alert.State != NO_DATA {
			continue
		}
		if alert.State == NO_DATA {
			result.Flags.NoData = true
		}
		if alert.Ignored {
			result.Flags.Ignored++
			continue
		}

		switch alert.Severity {
		case SeverityCritical:
			result.Counts.Critical++
		case SeverityWarning:
			result.Counts.Warning++
		case SeverityInfo:
			result.Counts.Info++
		}
		if attributed == nil || severityRank(alert.Severity) > severityRank(attributed.Severity) ||
			severityRank(alert.Severity) == severityRank(attributed.Severity) && alert.FirstTriggeredAt.Before(attributed.FirstTriggeredAt) {
			attributed = alert
		}
	}

	if attributed != nil {
		result.Attribution = &HealthAttribution{RuleName: attributed.RuleName, CurrentValue: attributed.CurrentValue}
	}
	switch {
	case input.Paused:
		result.Status = HealthPaused
	case result.Counts.Critical > 0:
		result.Status = HealthCritical
	case result.Counts.Warning > 0:
		result.Status = HealthWarning
	case !input.EverCollected:
		result.Status = HealthUnknown
	default:
		result.Status = HealthHealthy
	}
	return result
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}
