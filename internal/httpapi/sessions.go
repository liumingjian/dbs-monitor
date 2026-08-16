package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

const (
	sessionSnapshotReadLimit = 500
	sessionSnapshotLookback  = time.Hour
)

func (handler *Handler) GetSessionSnapshot(
	ctx context.Context,
	request api.GetSessionSnapshotRequestObject,
) (api.GetSessionSnapshotResponseObject, error) {
	instanceID := databaseUUID(request.Id)
	queries := New(handler.platform)
	snapshot, err := queries.GetRecentSessionSnapshot(ctx, GetRecentSessionSnapshotParams{
		InstanceID: instanceID,
		LowerBound: pgtype.Timestamptz{
			Time:  handler.clock.Now().UTC().Add(-sessionSnapshotLookback),
			Valid: true,
		},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return handler.unavailableSessionSnapshot(ctx, queries, instanceID)
	}
	if err != nil {
		return nil, err
	}

	rows, err := queries.ListSessionSnapshotEntries(ctx, ListSessionSnapshotEntriesParams{
		InstanceID: instanceID,
		RowLimit:   sessionSnapshotReadLimit + 1,
	})
	if err != nil {
		return nil, err
	}
	truncated := snapshot.Truncated || snapshot.OriginalCount > sessionSnapshotReadLimit || len(rows) > sessionSnapshotReadLimit
	if len(rows) > sessionSnapshotReadLimit {
		rows = rows[:sessionSnapshotReadLimit]
	}
	items := make([]api.SessionSnapshotEntry, 0, len(rows))
	for _, row := range rows {
		blockingPIDs := row.BlockingPids
		if blockingPIDs == nil {
			blockingPIDs = []int32{}
		}
		items = append(items, api.SessionSnapshotEntry{
			Pid:                   row.Pid,
			Username:              textPointer(row.Username),
			DatabaseName:          textPointer(row.DatabaseName),
			ClientAddress:         textPointer(row.ClientAddress),
			State:                 textPointer(row.State),
			QueryStartedAt:        timePointer(row.QueryStartedAt),
			TransactionStartedAt:  timePointer(row.TransactionStartedAt),
			QueryDurationMs:       int64Pointer(row.QueryDurationMs),
			TransactionDurationMs: int64Pointer(row.TransactionDurationMs),
			WaitEventType:         textPointer(row.WaitEventType),
			WaitEvent:             textPointer(row.WaitEvent),
			BlockingPids:          blockingPIDs,
		})
	}
	sampledAt := snapshot.SampledAt.Time.UTC()
	originalCount := int(snapshot.OriginalCount)
	return api.GetSessionSnapshot200JSONResponse{
		SampledAt:     &sampledAt,
		OriginalCount: &originalCount,
		Truncated:     truncated,
		Items:         items,
	}, nil
}

func (handler *Handler) unavailableSessionSnapshot(
	ctx context.Context,
	queries *Queries,
	instanceID pgtype.UUID,
) (api.GetSessionSnapshotResponseObject, error) {
	hasSnapshot, err := queries.HasSessionSnapshot(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	reason := api.NOSAMPLESYET
	if hasSnapshot {
		reason = api.STALE
	}
	return api.GetSessionSnapshot200JSONResponse{
		Truncated:      false,
		Items:          []api.SessionSnapshotEntry{},
		Unavailability: &reason,
	}, nil
}
