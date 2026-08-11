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
	if snapshot.Result != "SUCCESS" {
		return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
	}

	rows, err := queries.ListAlertTriggerSnapshotSessions(ctx, snapshot.ID)
	if err != nil {
		return nil, err
	}
	response.Sessions = make([]api.AlertTriggerSnapshotSession, 0, len(rows))
	for _, row := range rows {
		response.Sessions = append(response.Sessions, api.AlertTriggerSnapshotSession{
			Pid: row.Pid, BlockingPids: row.BlockingPids,
			Username: textPointer(row.Username), DatabaseName: textPointer(row.DatabaseName),
			ClientAddress: textPointer(row.ClientAddress), State: textPointer(row.State),
			QueryStartedAt: timePointer(row.QueryStartedAt), TransactionStartedAt: timePointer(row.TransactionStartedAt),
			QueryDurationMs: int64Pointer(row.QueryDurationMs), TransactionDurationMs: int64Pointer(row.TransactionDurationMs),
			WaitEventType: textPointer(row.WaitEventType), WaitEvent: textPointer(row.WaitEvent),
		})
	}
	return api.GetAlertTriggerSnapshot200JSONResponse(response), nil
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func int64Pointer(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
