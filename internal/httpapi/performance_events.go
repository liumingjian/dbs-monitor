package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) ListPerformanceEvents(ctx context.Context, request api.ListPerformanceEventsRequestObject) (api.ListPerformanceEventsResponseObject, error) {
	if !request.Params.From.Before(request.Params.To) {
		return api.ListPerformanceEvents400JSONResponse(errorBody(api.VALIDATIONFAILED, "from must be before to")), nil
	}
	limit := 50
	offset := 0
	sortOrder := "-derived_at"
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	if request.Params.Sort != nil {
		sortOrder = *request.Params.Sort
	}
	if limit < 1 || limit > 200 || offset < 0 || !validPerformanceEventSort(sortOrder) {
		return api.ListPerformanceEvents400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination or sort")), nil
	}

	filter := CountPerformanceEventsParams{
		InstanceID: databaseUUID(request.Id),
		FromTime:   pgtype.Timestamptz{Time: request.Params.From.UTC(), Valid: true},
		ToTime:     pgtype.Timestamptz{Time: request.Params.To.UTC(), Valid: true},
	}
	if request.Params.Recovered != nil {
		filter.Recovered = pgtype.Bool{Bool: *request.Params.Recovered, Valid: true}
	}
	if request.Params.Disposition != nil {
		filter.Disposition = pgtype.Text{String: string(*request.Params.Disposition), Valid: true}
	}
	queries := New(handler.platform)
	total, err := queries.CountPerformanceEvents(ctx, filter)
	if err != nil {
		return nil, err
	}
	rows, err := queries.ListPerformanceEvents(ctx, ListPerformanceEventsParams{
		InstanceID: filter.InstanceID, FromTime: filter.FromTime, ToTime: filter.ToTime,
		Recovered: filter.Recovered, Disposition: filter.Disposition,
		SortOrder: sortOrder, PageLimit: int32(limit), PageOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.PerformanceEvent, 0, len(rows))
	for _, row := range rows {
		item, err := handler.performanceEventResponse(performanceEventProjection{
			id: row.ID, alertInstanceID: row.AlertInstanceID, eventType: row.EventType,
			derivedAt: row.DerivedAt, instanceID: row.InstanceID, alertStatus: row.AlertStatus,
			severity: row.Severity, disposition: row.Disposition, updatedAt: row.UpdatedAt,
			recoveredAt: row.RecoveredAt, metricID: row.MetricID, triggerValue: row.TriggerValue,
			threshold: row.Threshold, triggerSnapshotResult: row.TriggerSnapshotResult,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return api.ListPerformanceEvents200JSONResponse{Total: int(total), Items: items}, nil
}

func (handler *Handler) GetPerformanceEvent(ctx context.Context, request api.GetPerformanceEventRequestObject) (api.GetPerformanceEventResponseObject, error) {
	row, err := New(handler.platform).GetPerformanceEvent(ctx, databaseUUID(request.Id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetPerformanceEvent404JSONResponse(errorBody(api.NOTFOUND, "performance event not found")), nil
	}
	if err != nil {
		return nil, err
	}
	response, err := handler.performanceEventResponse(performanceEventProjection{
		id: row.ID, alertInstanceID: row.AlertInstanceID, eventType: row.EventType,
		derivedAt: row.DerivedAt, instanceID: row.InstanceID, alertStatus: row.AlertStatus,
		severity: row.Severity, disposition: row.Disposition, updatedAt: row.UpdatedAt,
		recoveredAt: row.RecoveredAt, metricID: row.MetricID, triggerValue: row.TriggerValue,
		threshold: row.Threshold, triggerSnapshotResult: row.TriggerSnapshotResult,
	})
	if err != nil {
		return nil, err
	}
	return api.GetPerformanceEvent200JSONResponse(response), nil
}

type performanceEventProjection struct {
	id                    pgtype.UUID
	alertInstanceID       pgtype.UUID
	eventType             string
	derivedAt             pgtype.Timestamptz
	instanceID            pgtype.UUID
	alertStatus           string
	severity              string
	disposition           string
	updatedAt             pgtype.Timestamptz
	recoveredAt           pgtype.Timestamptz
	metricID              string
	triggerValue          pgtype.Float8
	threshold             float64
	triggerSnapshotResult pgtype.Text
}

func (handler *Handler) performanceEventResponse(row performanceEventProjection) (api.PerformanceEvent, error) {
	if !row.triggerValue.Valid {
		return api.PerformanceEvent{}, fmt.Errorf("performance event %s has no trigger value", uuid.UUID(row.id.Bytes))
	}
	template, ok := alerting.KnowledgeTemplateForEventType(alerting.PerformanceEventType(row.eventType))
	if !ok {
		return api.PerformanceEvent{}, fmt.Errorf("performance event %s has unknown type %q", uuid.UUID(row.id.Bytes), row.eventType)
	}
	knowledge := template.Render(alerting.KnowledgeContext{
		MetricID:     row.metricID,
		Threshold:    strconv.FormatFloat(row.threshold, 'g', -1, 64),
		TriggerValue: strconv.FormatFloat(row.triggerValue.Float64, 'g', -1, 64),
	})
	end := handler.clock.Now().UTC()
	if row.recoveredAt.Valid {
		end = row.recoveredAt.Time.UTC()
	}
	duration := end.Sub(row.derivedAt.Time.UTC())
	if duration < 0 {
		duration = 0
	}
	snapshotResult := api.TriggerSnapshotNotApplicable
	if row.triggerSnapshotResult.Valid {
		snapshotResult = api.AlertTriggerSnapshotResult(row.triggerSnapshotResult.String)
	}
	return api.PerformanceEvent{
		Id: uuid.UUID(row.id.Bytes), InstanceId: uuid.UUID(row.instanceID.Bytes),
		AlertInstanceId: uuid.UUID(row.alertInstanceID.Bytes), EventType: api.PerformanceEventType(row.eventType),
		AlertStatus: api.AlertStatus(row.alertStatus), Severity: api.AlertSeverity(row.severity),
		Disposition: api.AlertDisposition(row.disposition), DerivedAt: row.derivedAt.Time.UTC(),
		UpdatedAt: row.updatedAt.Time.UTC(), RecoveredAt: timePointer(row.recoveredAt),
		DurationMs: duration.Milliseconds(), MetricId: row.metricID, Threshold: row.threshold,
		TriggerValue: row.triggerValue.Float64, CauseSummary: knowledge.CauseSummary,
		SuggestedAction: knowledge.SuggestedAction, TriggerSnapshotResult: snapshotResult,
	}, nil
}

func validPerformanceEventSort(value string) bool {
	switch value {
	case "derived_at", "-derived_at", "updated_at", "-updated_at":
		return true
	default:
		return false
	}
}
