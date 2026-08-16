package httpapi

import (
	"context"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) ListPlatformEvents(ctx context.Context, _ api.ListPlatformEventsRequestObject) (api.ListPlatformEventsResponseObject, error) {
	rows, err := New(handler.platform).ListPlatformEvents(ctx)
	if err != nil {
		return nil, err
	}
	events := make(api.ListPlatformEvents200JSONResponse, 0, len(rows))
	for _, row := range rows {
		event := api.PlatformEvent{
			Id: row.ID, Kind: api.PlatformEventKind(row.Kind), OccurredAt: row.OccurredAt.Time, Actor: row.Actor,
		}
		if row.SubjectID.Valid {
			subjectID := uuid.UUID(row.SubjectID.Bytes)
			event.SubjectId = &subjectID
		}
		events = append(events, event)
	}
	return events, nil
}
