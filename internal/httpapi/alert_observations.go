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
	pagination, valid := parseAlertObservationPagination(request.Params.Limit, request.Params.Offset)
	if !valid {
		return api.ListCurrentAlerts400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination")), nil
	}

	includePaused := request.Params.IncludePaused != nil && *request.Params.IncludePaused
	page, err := handler.listAlertObservations(ctx, alertObservationListOptions{
		recovered:     false,
		includePaused: includePaused,
		instanceID:    request.Params.InstanceId,
		pagination:    pagination,
	})
	if err != nil {
		return nil, err
	}
	return api.ListCurrentAlerts200JSONResponse(page), nil
}

func (handler *Handler) ListAlertHistory(ctx context.Context, request api.ListAlertHistoryRequestObject) (api.ListAlertHistoryResponseObject, error) {
	pagination, valid := parseAlertObservationPagination(request.Params.Limit, request.Params.Offset)
	if !valid {
		return api.ListAlertHistory400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination")), nil
	}

	page, err := handler.listAlertObservations(ctx, alertObservationListOptions{
		recovered:     true,
		includePaused: true,
		instanceID:    request.Params.InstanceId,
		pagination:    pagination,
	})
	if err != nil {
		return nil, err
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
		id:                  row.ID,
		instanceID:          row.InstanceID,
		instanceName:        row.InstanceName,
		ruleID:              row.RuleID,
		ruleName:            row.RuleName,
		ruleVersion:         row.RuleVersion,
		ruleSnapshot:        row.RuleSnapshot,
		metricID:            row.MetricID,
		status:              row.Status,
		severity:            row.Severity,
		disposition:         row.Disposition,
		inMaintenance:       row.InMaintenance,
		maintenanceWindowID: row.MaintenanceWindowID,
		paused:              row.Paused,
		pausedAt:            row.PausedAt,
		currentValue:        row.CurrentValue,
		threshold:           row.Threshold,
		firstTriggeredAt:    row.FirstTriggeredAt,
		updatedAt:           row.UpdatedAt,
		recoveredAt:         row.RecoveredAt,
		unavailability:      row.Unavailability,
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
			Version:     int(version.RuleVersion),
			Snapshot:    snapshot,
			EvaluatedAt: version.EvaluatedAt.Time.UTC(),
		})
	}
	notificationEvents, err := queries.ListAlertNotificationResults(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	notificationResults := make([]map[string]interface{}, 0, len(notificationEvents))
	for _, event := range notificationEvents {
		notificationResults = append(notificationResults, map[string]interface{}{
			"kind":         event.Kind,
			"evaluated_at": event.EvaluatedAt.Time.UTC(),
		})
	}
	detail := api.AlertDetail{
		Id:                  observation.Id,
		InstanceId:          observation.InstanceId,
		InstanceName:        observation.InstanceName,
		RuleId:              observation.RuleId,
		RuleName:            observation.RuleName,
		RuleVersion:         observation.RuleVersion,
		RuleSnapshot:        observation.RuleSnapshot,
		MetricId:            observation.MetricId,
		Status:              observation.Status,
		Severity:            observation.Severity,
		Disposition:         observation.Disposition,
		Paused:              observation.Paused,
		PausedAt:            observation.PausedAt,
		InMaintenance:       observation.InMaintenance,
		MaintenanceWindowId: observation.MaintenanceWindowId,
		CurrentValue:        observation.CurrentValue,
		Threshold:           observation.Threshold,
		FirstTriggeredAt:    observation.FirstTriggeredAt,
		UpdatedAt:           observation.UpdatedAt,
		RecoveredAt:         observation.RecoveredAt,
		DurationMs:          observation.DurationMs,
		Unavailability:      observation.Unavailability,
		RuleVersionHistory:  versionHistory,
		NotificationResults: notificationResults,
	}
	return api.GetAlertDetail200JSONResponse(detail), nil
}

func (handler *Handler) ListAlertEvents(ctx context.Context, request api.ListAlertEventsRequestObject) (api.ListAlertEventsResponseObject, error) {
	queries := New(handler.platform)
	alertID := databaseUUID(request.Id)
	_, err := queries.GetAlertObservation(ctx, alertID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.ListAlertEvents404JSONResponse(errorBody(api.NOTFOUND, "alert instance not found")), nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := queries.ListAlertEvents(ctx, alertID)
	if err != nil {
		return nil, err
	}
	response := make(api.ListAlertEvents200JSONResponse, 0, len(rows))
	for _, row := range rows {
		event, err := toAPIAlertEvent(row)
		if err != nil {
			return nil, err
		}
		response = append(response, event)
	}
	return response, nil
}

func toAPIAlertEvent(row AlertEvent) (api.AlertEvent, error) {
	var ruleSnapshot map[string]interface{}
	if err := json.Unmarshal(row.RuleSnapshot, &ruleSnapshot); err != nil {
		return api.AlertEvent{}, err
	}

	event := api.AlertEvent{
		Id:                  row.ID,
		Kind:                api.AlertEventKind(row.Kind),
		FromState:           api.AlertStatus(row.FromState),
		ToState:             api.AlertStatus(row.ToState),
		RuleVersion:         int(row.RuleVersion),
		CurrentValue:        floatPointer(row.CurrentValue),
		RuleSnapshot:        ruleSnapshot,
		EvaluatedAt:         row.EvaluatedAt.Time.UTC(),
		ActorId:             uuidPointer(row.ActorID),
		ActedAt:             timePointer(row.ActedAt),
		DispositionNote:     textPointer(row.DispositionNote),
		IgnoreReasonCode:    ignoreReasonPointer(row.IgnoreReasonCode),
		IgnoreReasonDetail:  textPointer(row.IgnoreReasonDetail),
		TriggerSnapshotId:   uuidPointer(row.TriggerSnapshotID),
		InMaintenance:       row.InMaintenance,
		MaintenanceWindowId: uuidPointer(row.MaintenanceWindowID),
	}
	if row.Unavailability.Valid {
		value := api.Unavailability(row.Unavailability.String)
		event.Unavailability = &value
	}
	if row.FromDisposition.Valid {
		value := api.AlertDisposition(row.FromDisposition.String)
		event.FromDisposition = &value
	}
	if row.ToDisposition.Valid {
		value := api.AlertDisposition(row.ToDisposition.String)
		event.ToDisposition = &value
	}
	return event, nil
}

const (
	defaultAlertObservationLimit = 50
	maxAlertObservationLimit     = 200
)

type alertObservationPagination struct {
	limit  int
	offset int
}

func parseAlertObservationPagination(requestedLimit, requestedOffset *int) (alertObservationPagination, bool) {
	pagination := alertObservationPagination{limit: defaultAlertObservationLimit}
	if requestedLimit != nil {
		pagination.limit = *requestedLimit
	}
	if requestedOffset != nil {
		pagination.offset = *requestedOffset
	}
	valid := pagination.limit >= 1 && pagination.limit <= maxAlertObservationLimit && pagination.offset >= 0
	return pagination, valid
}

type alertObservationListOptions struct {
	recovered     bool
	includePaused bool
	instanceID    *uuid.UUID
	pagination    alertObservationPagination
}

func (handler *Handler) listAlertObservations(ctx context.Context, options alertObservationListOptions) (api.AlertObservationPage, error) {
	filter := CountAlertObservationsParams{
		Recovered:     options.recovered,
		IncludePaused: options.includePaused,
	}
	if options.instanceID != nil {
		filter.HasInstance = true
		filter.InstanceID = databaseUUID(*options.instanceID)
	}

	queries := New(handler.platform)
	total, err := queries.CountAlertObservations(ctx, filter)
	if err != nil {
		return api.AlertObservationPage{}, err
	}
	rows, err := queries.ListAlertObservations(ctx, ListAlertObservationsParams{
		Recovered:     filter.Recovered,
		HasInstance:   filter.HasInstance,
		InstanceID:    filter.InstanceID,
		IncludePaused: filter.IncludePaused,
		PageLimit:     int32(options.pagination.limit),
		PageOffset:    int32(options.pagination.offset),
	})
	if err != nil {
		return api.AlertObservationPage{}, err
	}

	items := make([]api.AlertObservation, 0, len(rows))
	for _, row := range rows {
		item, err := handler.alertObservationResponse(alertObservationProjection{
			id:                  row.ID,
			instanceID:          row.InstanceID,
			instanceName:        row.InstanceName,
			ruleID:              row.RuleID,
			ruleName:            row.RuleName,
			ruleVersion:         row.RuleVersion,
			ruleSnapshot:        row.RuleSnapshot,
			metricID:            row.MetricID,
			status:              row.Status,
			severity:            row.Severity,
			disposition:         row.Disposition,
			inMaintenance:       row.InMaintenance,
			maintenanceWindowID: row.MaintenanceWindowID,
			paused:              row.Paused,
			pausedAt:            row.PausedAt,
			currentValue:        row.CurrentValue,
			threshold:           row.Threshold,
			firstTriggeredAt:    row.FirstTriggeredAt,
			updatedAt:           row.UpdatedAt,
			recoveredAt:         row.RecoveredAt,
			unavailability:      row.Unavailability,
		})
		if err != nil {
			return api.AlertObservationPage{}, err
		}
		items = append(items, item)
	}
	return api.AlertObservationPage{Total: int(total), Items: items}, nil
}

type alertObservationProjection struct {
	id                  pgtype.UUID
	instanceID          pgtype.UUID
	instanceName        string
	ruleID              pgtype.UUID
	ruleName            string
	ruleVersion         int32
	ruleSnapshot        []byte
	metricID            string
	status              string
	severity            string
	disposition         string
	inMaintenance       bool
	maintenanceWindowID pgtype.UUID
	paused              bool
	pausedAt            pgtype.Timestamptz
	currentValue        pgtype.Float8
	threshold           float64
	firstTriggeredAt    pgtype.Timestamptz
	updatedAt           pgtype.Timestamptz
	recoveredAt         pgtype.Timestamptz
	unavailability      pgtype.Text
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
		Id:               uuid.UUID(row.id.Bytes),
		InstanceId:       uuid.UUID(row.instanceID.Bytes),
		InstanceName:     row.instanceName,
		RuleId:           uuid.UUID(row.ruleID.Bytes),
		RuleName:         row.ruleName,
		RuleVersion:      int(row.ruleVersion),
		RuleSnapshot:     snapshot,
		MetricId:         row.metricID,
		Status:           api.AlertStatus(row.status),
		Severity:         api.AlertSeverity(row.severity),
		Disposition:      api.AlertDisposition(row.disposition),
		InMaintenance:    row.inMaintenance,
		Paused:           row.paused,
		PausedAt:         timePointer(row.pausedAt),
		CurrentValue:     floatPointer(row.currentValue),
		Threshold:        &threshold,
		FirstTriggeredAt: timePointer(row.firstTriggeredAt),
		UpdatedAt:        row.updatedAt.Time.UTC(),
		RecoveredAt:      timePointer(row.recoveredAt),
		DurationMs:       duration.Milliseconds(),
	}
	if row.maintenanceWindowID.Valid {
		maintenanceWindowID := uuid.UUID(row.maintenanceWindowID.Bytes)
		response.MaintenanceWindowId = &maintenanceWindowID
	}
	if row.unavailability.Valid {
		value := api.Unavailability(row.unavailability.String)
		response.Unavailability = &value
	}
	return response, nil
}
