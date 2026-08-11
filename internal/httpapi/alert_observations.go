package httpapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) ListCurrentAlerts(ctx context.Context, request api.ListCurrentAlertsRequestObject) (api.ListCurrentAlertsResponseObject, error) {
	includePaused := request.Params.IncludePaused != nil && *request.Params.IncludePaused
	page, invalid, err := handler.listAlertObservations(ctx, false, includePaused, request.Params.InstanceId, request.Params.Limit, request.Params.Offset)
	if err != nil {
		return nil, err
	}
	if invalid {
		return api.ListCurrentAlerts400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination")), nil
	}
	return api.ListCurrentAlerts200JSONResponse(page), nil
}

func (handler *Handler) ListAlertHistory(ctx context.Context, request api.ListAlertHistoryRequestObject) (api.ListAlertHistoryResponseObject, error) {
	page, invalid, err := handler.listAlertObservations(ctx, true, true, request.Params.InstanceId, request.Params.Limit, request.Params.Offset)
	if err != nil {
		return nil, err
	}
	if invalid {
		return api.ListAlertHistory400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination")), nil
	}
	return api.ListAlertHistory200JSONResponse(page), nil
}

func (handler *Handler) GetAlertDetail(ctx context.Context, request api.GetAlertDetailRequestObject) (api.GetAlertDetailResponseObject, error) {
	queries := New(handler.platform)
	row, err := queries.GetAlertObservation(ctx, databaseUUID(request.Id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetAlertDetail404JSONResponse(errorBody(api.NOTFOUND, "alert instance not found")), nil
	}
	if err != nil {
		return nil, err
	}
	observation, err := handler.alertObservationResponse(alertObservationProjection{
		id: row.ID, instanceID: row.InstanceID, instanceName: row.InstanceName,
		ruleID: row.RuleID, ruleName: row.RuleName, ruleVersion: row.RuleVersion,
		ruleSnapshot: row.RuleSnapshot, metricID: row.MetricID, status: row.Status,
		severity: row.Severity, disposition: row.Disposition, paused: row.Paused,
		pausedAt:     row.PausedAt,
		currentValue: row.CurrentValue, threshold: row.Threshold,
		firstTriggeredAt: row.FirstTriggeredAt, updatedAt: row.UpdatedAt,
		recoveredAt: row.RecoveredAt, unavailability: row.Unavailability,
	})
	if err != nil {
		return nil, err
	}
	versions, err := queries.ListAlertRuleVersionHistory(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	versionHistory := make([]api.AlertRuleVersionRecord, 0, len(versions))
	for _, version := range versions {
		var snapshot map[string]interface{}
		if err := json.Unmarshal(version.RuleSnapshot, &snapshot); err != nil {
			return nil, err
		}
		versionHistory = append(versionHistory, api.AlertRuleVersionRecord{
			Version: int(version.RuleVersion), Snapshot: snapshot,
			EvaluatedAt: version.EvaluatedAt.Time.UTC(),
		})
	}
	detail := api.AlertDetail{
		Id: observation.Id, InstanceId: observation.InstanceId, InstanceName: observation.InstanceName,
		RuleId: observation.RuleId, RuleName: observation.RuleName, RuleVersion: observation.RuleVersion,
		RuleSnapshot: observation.RuleSnapshot, MetricId: observation.MetricId, Status: observation.Status,
		Severity: observation.Severity, Disposition: observation.Disposition, Paused: observation.Paused,
		PausedAt:      observation.PausedAt,
		InMaintenance: observation.InMaintenance, CurrentValue: observation.CurrentValue,
		Threshold: observation.Threshold, FirstTriggeredAt: observation.FirstTriggeredAt,
		UpdatedAt: observation.UpdatedAt, RecoveredAt: observation.RecoveredAt,
		DurationMs: observation.DurationMs, Unavailability: observation.Unavailability,
		RuleVersionHistory: versionHistory, NotificationResults: []map[string]interface{}{},
	}
	return api.GetAlertDetail200JSONResponse(detail), nil
}

func (handler *Handler) listAlertObservations(
	ctx context.Context,
	recovered bool,
	includePaused bool,
	instanceID *uuid.UUID,
	requestedLimit *int,
	requestedOffset *int,
) (api.AlertObservationPage, bool, error) {
	limit, offset := 50, 0
	if requestedLimit != nil {
		limit = *requestedLimit
	}
	if requestedOffset != nil {
		offset = *requestedOffset
	}
	if limit < 1 || limit > 200 || offset < 0 {
		return api.AlertObservationPage{}, true, nil
	}
	filter := CountAlertObservationsParams{Recovered: recovered, IncludePaused: includePaused}
	if instanceID != nil {
		filter.HasInstance = true
		filter.InstanceID = databaseUUID(*instanceID)
	}
	queries := New(handler.platform)
	total, err := queries.CountAlertObservations(ctx, filter)
	if err != nil {
		return api.AlertObservationPage{}, false, err
	}
	rows, err := queries.ListAlertObservations(ctx, ListAlertObservationsParams{
		Recovered: filter.Recovered, HasInstance: filter.HasInstance, InstanceID: filter.InstanceID,
		IncludePaused: filter.IncludePaused, PageLimit: int32(limit), PageOffset: int32(offset),
	})
	if err != nil {
		return api.AlertObservationPage{}, false, err
	}
	items := make([]api.AlertObservation, 0, len(rows))
	for _, row := range rows {
		item, err := handler.alertObservationResponse(alertObservationProjection{
			id: row.ID, instanceID: row.InstanceID, instanceName: row.InstanceName,
			ruleID: row.RuleID, ruleName: row.RuleName, ruleVersion: row.RuleVersion,
			ruleSnapshot: row.RuleSnapshot, metricID: row.MetricID, status: row.Status,
			severity: row.Severity, disposition: row.Disposition, paused: row.Paused,
			pausedAt:     row.PausedAt,
			currentValue: row.CurrentValue, threshold: row.Threshold,
			firstTriggeredAt: row.FirstTriggeredAt, updatedAt: row.UpdatedAt,
			recoveredAt: row.RecoveredAt, unavailability: row.Unavailability,
		})
		if err != nil {
			return api.AlertObservationPage{}, false, err
		}
		items = append(items, item)
	}
	return api.AlertObservationPage{Total: int(total), Items: items}, false, nil
}

type alertObservationProjection struct {
	id               pgtype.UUID
	instanceID       pgtype.UUID
	instanceName     string
	ruleID           pgtype.UUID
	ruleName         string
	ruleVersion      int32
	ruleSnapshot     []byte
	metricID         string
	status           string
	severity         string
	disposition      string
	paused           bool
	pausedAt         pgtype.Timestamptz
	currentValue     pgtype.Float8
	threshold        float64
	firstTriggeredAt pgtype.Timestamptz
	updatedAt        pgtype.Timestamptz
	recoveredAt      pgtype.Timestamptz
	unavailability   pgtype.Text
}

func (handler *Handler) alertObservationResponse(row alertObservationProjection) (api.AlertObservation, error) {
	var snapshot map[string]interface{}
	if err := json.Unmarshal(row.ruleSnapshot, &snapshot); err != nil {
		return api.AlertObservation{}, err
	}
	end := handler.clock.Now().UTC()
	if row.recoveredAt.Valid {
		end = row.recoveredAt.Time.UTC()
	}
	start := row.updatedAt.Time.UTC()
	if row.firstTriggeredAt.Valid {
		start = row.firstTriggeredAt.Time.UTC()
	}
	duration := end.Sub(start)
	if duration < 0 {
		duration = 0
	}
	threshold := row.threshold
	response := api.AlertObservation{
		Id: uuid.UUID(row.id.Bytes), InstanceId: uuid.UUID(row.instanceID.Bytes), InstanceName: row.instanceName,
		RuleId: uuid.UUID(row.ruleID.Bytes), RuleName: row.ruleName, RuleVersion: int(row.ruleVersion),
		RuleSnapshot: snapshot, MetricId: row.metricID, Status: api.AlertStatus(row.status),
		Severity: api.AlertSeverity(row.severity), Disposition: api.AlertDisposition(row.disposition),
		Paused: row.paused, CurrentValue: floatPointer(row.currentValue), Threshold: &threshold,
		PausedAt:         timePointer(row.pausedAt),
		FirstTriggeredAt: timePointer(row.firstTriggeredAt), UpdatedAt: row.updatedAt.Time.UTC(),
		RecoveredAt: timePointer(row.recoveredAt), DurationMs: duration.Milliseconds(),
	}
	if row.unavailability.Valid {
		value := api.Unavailability(row.unavailability.String)
		response.Unavailability = &value
	}
	return response, nil
}
