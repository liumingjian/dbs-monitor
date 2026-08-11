package metric

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	AgentStatusOffline          = "offline"
	AgentStatusOnline           = "online"
	AgentStatusNotInstalled     = "not_installed"
	AgentStatusPermissionDenied = "permission_denied"
	AgentStatusError            = "error"

	AgentOfflineAfter = 60 * time.Second
)

type ControlPlaneFacts struct {
	CollectorLastSuccessAt time.Time
	CollectorLastErrorCode string
	AgentExpected          bool
	AgentMetricsEnabled    bool
	AgentLastReportAt      time.Time
	AgentLastErrorCode     string
}

type ControlPlaneProjection struct {
	ObservedAt time.Time
	Value      float64
	State      string
	Labels     map[string]string
}

func ReadControlPlaneFacts(ctx context.Context, database DBTX, instanceID pgtype.UUID) (ControlPlaneFacts, error) {
	var agentExpected, agentMetricsEnabled bool
	var collectorLastSuccessAt, agentLastReportAt pgtype.Timestamptz
	var collectorLastErrorCode, agentLastErrorCode pgtype.Text
	err := database.QueryRow(ctx, `SELECT instance.agent_expected,
		config.agent_metrics_enabled,
		server_state.last_success_at,
		server_state.last_error_code,
		agent_state.last_report_at,
		agent_state.last_error_code
		FROM instance
		JOIN instance_collection_config config ON config.instance_id = instance.id
		LEFT JOIN instance_collect_state server_state
			ON server_state.instance_id = instance.id AND server_state.source = 'SERVER_DIRECT'
		LEFT JOIN instance_collect_state agent_state
			ON agent_state.instance_id = instance.id AND agent_state.source = 'AGENT'
		WHERE instance.id = $1`, instanceID).Scan(
		&agentExpected,
		&agentMetricsEnabled,
		&collectorLastSuccessAt,
		&collectorLastErrorCode,
		&agentLastReportAt,
		&agentLastErrorCode,
	)
	if err != nil {
		return ControlPlaneFacts{}, err
	}
	return ControlPlaneFacts{
		CollectorLastSuccessAt: nullableTime(collectorLastSuccessAt),
		CollectorLastErrorCode: nullableText(collectorLastErrorCode),
		AgentExpected:          agentExpected,
		AgentMetricsEnabled:    agentMetricsEnabled,
		AgentLastReportAt:      nullableTime(agentLastReportAt),
		AgentLastErrorCode:     nullableText(agentLastErrorCode),
	}, nil
}

func ProjectControlPlaneMetric(metricID MetricID, facts ControlPlaneFacts, now time.Time) (ControlPlaneProjection, bool) {
	switch metricID {
	case MetricCollectorLastSuccessTime:
		if facts.CollectorLastSuccessAt.IsZero() {
			return ControlPlaneProjection{}, false
		}
		return ControlPlaneProjection{
			ObservedAt: now,
			Value:      float64(facts.CollectorLastSuccessAt.Unix()),
			Labels:     map[string]string{"source_type": "SERVER_DIRECT"},
		}, true
	case MetricAgentStatus:
		state := AgentStatusAt(facts, now)
		return ControlPlaneProjection{
			ObservedAt: now,
			Value:      AgentStatusEncodings[state],
			State:      state,
			Labels:     map[string]string{"node": "agent"},
		}, true
	default:
		return ControlPlaneProjection{}, false
	}
}

func AgentStatusAt(facts ControlPlaneFacts, now time.Time) string {
	switch {
	case !facts.AgentExpected:
		return AgentStatusNotInstalled
	case facts.AgentLastErrorCode == "PERMISSION_DENIED":
		return AgentStatusPermissionDenied
	case facts.AgentLastErrorCode != "":
		return AgentStatusError
	case !facts.AgentLastReportAt.IsZero() && now.Sub(facts.AgentLastReportAt) <= AgentOfflineAfter:
		return AgentStatusOnline
	default:
		return AgentStatusOffline
	}
}

func nullableTime(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func nullableText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
