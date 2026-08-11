package httpapi

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) GetAlertTriggerSnapshot(ctx context.Context, request api.GetAlertTriggerSnapshotRequestObject) (api.GetAlertTriggerSnapshotResponseObject, error) {
	alertInstanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	queries := New(handler.platform)
	metricID, err := queries.GetAlertInstanceMetricID(ctx, alertInstanceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetAlertTriggerSnapshot404JSONResponse(errorBody(api.NOTFOUND, "alert instance not found")), nil
	}
	if err != nil {
		return nil, err
	}
	response := api.AlertTriggerSnapshot{
		MetricId: metricID,
		Result:   api.TriggerSnapshotNotApplicable,
		Sessions: []api.AlertTriggerSnapshotSession{},
	}
	if _, applicable := alerting.TriggerSnapshotScopeForMetric(metricID); !applicable {
		return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
	}

	snapshot, err := queries.GetAlertTriggerSnapshot(ctx, alertInstanceID)
	if errors.Is(err, pgx.ErrNoRows) {
		reason := "trigger snapshot capture result is unavailable"
		response.Result = api.TriggerSnapshotFailed
		response.FailureReason = &reason
		return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
	}
	if err != nil {
		return nil, err
	}
	response.Result = api.AlertTriggerSnapshotResult(snapshot.Result)
	response.CapturedAt = timePointer(snapshot.CapturedAt)
	response.OriginalMatchCount = int(snapshot.OriginalMatchCount)
	response.Truncated = snapshot.Truncated
	response.FailureReason = textPointer(snapshot.FailureReason)
	if response.Result != api.TriggerSnapshotSuccess {
		return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
	}

	snapshotSessions, err := queries.ListAlertTriggerSnapshotSessions(ctx, snapshot.ID)
	if err != nil {
		return nil, err
	}
	response.Sessions = make([]api.AlertTriggerSnapshotSession, 0, len(snapshotSessions))
	for _, session := range snapshotSessions {
		response.Sessions = append(response.Sessions, api.AlertTriggerSnapshotSession{
			Pid:                   session.Pid,
			Username:              textPointer(session.Username),
			DatabaseName:          textPointer(session.DatabaseName),
			ClientAddress:         textPointer(session.ClientAddress),
			State:                 textPointer(session.State),
			QueryStartedAt:        timePointer(session.QueryStartedAt),
			TransactionStartedAt:  timePointer(session.TransactionStartedAt),
			QueryDurationMs:       int64Pointer(session.QueryDurationMs),
			TransactionDurationMs: int64Pointer(session.TransactionDurationMs),
			WaitEventType:         textPointer(session.WaitEventType),
			WaitEvent:             textPointer(session.WaitEvent),
			BlockingPids:          session.BlockingPids,
		})
	}
	return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
}

func int64Pointer(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
