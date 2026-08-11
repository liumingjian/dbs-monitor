package httpapi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

type instanceHealthProjection struct {
	health               api.InstanceHealth
	alertStatus          api.AlertStatus
	agentStatus          api.InstanceAgentStatus
	lastCollectedAt      *time.Time
	dataFreshnessSeconds *int
}

type instanceHealthProjectionInput struct {
	instanceID             uuid.UUID
	paused                 bool
	collectorLastSuccessAt pgtype.Timestamptz
	agentExpected          bool
	agentLastReportAt      pgtype.Timestamptz
	agentLastErrorCode     pgtype.Text
	capabilityObservedAt   pgtype.Timestamptz
	capabilityStates       []byte
	alerts                 []alerting.ListInstanceHealthAlertsRow
	now                    time.Time
}

func (handler *Handler) loadInstanceHealthProjection(ctx context.Context, input instanceHealthProjectionInput) (instanceHealthProjection, error) {
	now := handler.clock.Now().UTC()
	alertsByInstance, err := handler.loadInstanceHealthAlerts(ctx, now)
	if err != nil {
		return instanceHealthProjection{}, err
	}
	input.alerts = alertsByInstance[input.instanceID]
	input.now = now
	return projectInstanceHealth(input)
}

func (handler *Handler) loadInstanceHealthAlerts(ctx context.Context, now time.Time) (map[uuid.UUID][]alerting.ListInstanceHealthAlertsRow, error) {
	rows, err := alerting.New(handler.platform).ListInstanceHealthAlerts(ctx, pgtype.Timestamptz{
		Time:  now.Add(-alerting.RecentRecoveryWindow),
		Valid: true,
	})
	if err != nil {
		return nil, err
	}

	alertsByInstance := make(map[uuid.UUID][]alerting.ListInstanceHealthAlertsRow)
	for _, row := range rows {
		instanceID := uuid.UUID(row.InstanceID.Bytes)
		alertsByInstance[instanceID] = append(alertsByInstance[instanceID], row)
	}
	return alertsByInstance, nil
}

func projectInstanceHealth(input instanceHealthProjectionInput) (instanceHealthProjection, error) {
	configurationMissing, err := configurationMissingCount(
		input.capabilityStates,
		input.capabilityObservedAt,
		input.now,
	)
	if err != nil {
		return instanceHealthProjection{}, err
	}

	rollup := alerting.RollupInstanceHealth(alerting.HealthRollupInput{
		Paused:               input.paused,
		EverCollected:        input.collectorLastSuccessAt.Valid,
		ConfigurationMissing: configurationMissing,
		Now:                  input.now,
		Alerts:               toHealthAlerts(input.alerts),
	})
	projection := instanceHealthProjection{
		health:      toAPIInstanceHealth(rollup),
		alertStatus: legacyAlertStatus(input.alerts),
		agentStatus: api.InstanceAgentStatus(metric.AgentStatusAt(metric.ControlPlaneFacts{
			AgentExpected:      input.agentExpected,
			AgentLastReportAt:  controlPlaneTime(input.agentLastReportAt),
			AgentLastErrorCode: controlPlaneText(input.agentLastErrorCode),
		}, input.now)),
	}
	if input.collectorLastSuccessAt.Valid {
		collectedAt := input.collectorLastSuccessAt.Time.UTC()
		projection.lastCollectedAt = &collectedAt
		seconds := max(0, int(input.now.Sub(collectedAt)/time.Second))
		projection.dataFreshnessSeconds = &seconds
	}
	return projection, nil
}

func toHealthAlerts(rows []alerting.ListInstanceHealthAlertsRow) []alerting.HealthAlert {
	alerts := make([]alerting.HealthAlert, 0, len(rows))
	for _, row := range rows {
		var currentValue *float64
		if row.CurrentValue.Valid {
			value := row.CurrentValue.Float64
			currentValue = &value
		}
		alerts = append(alerts, alerting.HealthAlert{
			RuleName:         row.RuleName,
			Severity:         alerting.Severity(row.Severity),
			State:            alerting.State(row.Status),
			FirstTriggeredAt: row.FirstTriggeredAt.Time.UTC(),
			CurrentValue:     currentValue,
			RecoveredAt:      timePointer(row.RecoveredAt),
			Ignored:          row.Disposition == "IGNORED",
		})
	}
	return alerts
}

func legacyAlertStatus(rows []alerting.ListInstanceHealthAlertsRow) api.AlertStatus {
	status := api.OK
	statusRank := alertStateRank(string(status))
	for _, row := range rows {
		rowRank := alertStateRank(row.Status)
		if rowRank > statusRank {
			status = api.AlertStatus(row.Status)
			statusRank = rowRank
		}
	}
	return status
}

func toAPIInstanceHealth(rollup alerting.HealthRollup) api.InstanceHealth {
	health := api.InstanceHealth{
		Status: api.HealthStatus(rollup.Status),
		Counts: api.HealthAlertCounts{
			Critical: rollup.Counts.Critical,
			Warning:  rollup.Counts.Warning,
			Info:     rollup.Counts.Info,
		},
		Flags: api.HealthFlags{
			NoData:               rollup.Flags.NoData,
			InMaintenance:        rollup.Flags.InMaintenance,
			RecentlyRecovered:    rollup.Flags.RecentlyRecovered,
			Ignored:              rollup.Flags.Ignored,
			ConfigurationMissing: rollup.Flags.ConfigurationMissing,
		},
	}
	if rollup.Attribution != nil {
		health.Attribution = &api.HealthAttribution{
			RuleName:     rollup.Attribution.RuleName,
			CurrentValue: rollup.Attribution.CurrentValue,
		}
	}
	return health
}

func toAPIInstance(
	id pgtype.UUID,
	name, host string,
	port int32,
	database, username string,
	agentVersion pgtype.Text,
	agentMetricsEnabled bool,
	pause api.CollectionPauseStatus,
	projection instanceHealthProjection,
) api.Instance {
	result := api.Instance{
		Id:                   id.Bytes,
		Name:                 name,
		Host:                 host,
		Port:                 int(port),
		Database:             database,
		Username:             username,
		AgentMetricsEnabled:  agentMetricsEnabled,
		AlertStatus:          projection.alertStatus,
		Health:               projection.health,
		AgentStatus:          projection.agentStatus,
		LastCollectedAt:      projection.lastCollectedAt,
		DataFreshnessSeconds: projection.dataFreshnessSeconds,
		CollectionPause:      pause,
	}
	if agentVersion.Valid {
		result.AgentVersion = &agentVersion.String
	}
	return result
}

func configurationMissingCount(encoded []byte, observedAt pgtype.Timestamptz, now time.Time) (int, error) {
	if !observedAt.Valid || len(encoded) == 0 {
		return 0, nil
	}
	states, err := metric.DecodeCapabilitySnapshot(encoded)
	if err != nil {
		return 0, err
	}
	states = metric.ProjectCapabilitySnapshot(states, observedAt.Time.UTC(), now)
	count := 0
	for _, capability := range metric.Capabilities {
		if capability.Class == metric.CapabilityClassFixable && states[capability.ID] == metric.CapabilityMissing {
			count++
		}
	}
	return count, nil
}

func alertStateRank(state string) int {
	switch state {
	case "FIRING":
		return 5
	case "NO_DATA":
		return 4
	case "PENDING":
		return 3
	case "RECOVERED":
		return 2
	default:
		return 1
	}
}

func controlPlaneTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func controlPlaneText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
