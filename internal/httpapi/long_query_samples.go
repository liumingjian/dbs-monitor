package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) ListLongQuerySamples(ctx context.Context, request api.ListLongQuerySamplesRequestObject) (api.ListLongQuerySamplesResponseObject, error) {
	if !request.Params.From.Before(request.Params.To) {
		return api.ListLongQuerySamples400JSONResponse(errorBody(api.VALIDATIONFAILED, "from must be before to")), nil
	}
	limit, offset, sortOrder := 50, 0, "-sampled_at"
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	if request.Params.Sort != nil {
		sortOrder = *request.Params.Sort
	}
	if limit < 1 || limit > 200 || offset < 0 || !validLongQuerySort(sortOrder) {
		return api.ListLongQuerySamples400JSONResponse(errorBody(api.VALIDATIONFAILED, "invalid pagination or sort")), nil
	}

	queries := New(handler.platform)
	filter := CountLongQuerySamplesParams{
		InstanceID: databaseUUID(request.Id),
		FromTime:   pgtype.Timestamptz{Time: request.Params.From.UTC(), Valid: true},
		ToTime:     pgtype.Timestamptz{Time: request.Params.To.UTC(), Valid: true},
	}
	total, err := queries.CountLongQuerySamples(ctx, filter)
	if err != nil {
		return nil, err
	}
	rows, err := queries.ListLongQuerySamples(ctx, ListLongQuerySamplesParams{
		InstanceID: filter.InstanceID, FromTime: filter.FromTime, ToTime: filter.ToTime,
		SortOrder: sortOrder, PageLimit: int32(limit), PageOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.LongQuerySample, 0, len(rows))
	for _, row := range rows {
		blockingPIDs := row.BlockingPids
		if blockingPIDs == nil {
			blockingPIDs = []int32{}
		}
		items = append(items, api.LongQuerySample{
			SampledAt:             row.SampledAt.Time.UTC(),
			Pid:                   row.Pid,
			Username:              textPointer(row.Username),
			DatabaseName:          textPointer(row.DatabaseName),
			ClientAddress:         textPointer(row.ClientAddress),
			State:                 textPointer(row.State),
			QueryStartedAt:        row.QueryStartedAt.Time.UTC(),
			TransactionStartedAt:  timePointer(row.TransactionStartedAt),
			QueryDurationMs:       row.QueryDurationMs,
			TransactionDurationMs: int64Pointer(row.TransactionDurationMs),
			WaitEventType:         textPointer(row.WaitEventType),
			WaitEvent:             textPointer(row.WaitEvent),
			BlockingPids:          blockingPIDs,
			SnapshotOriginalCount: int(row.OriginalCount),
			SnapshotTruncated:     row.Truncated,
		})
	}
	return api.ListLongQuerySamples200JSONResponse{Total: int(total), Items: items}, nil
}

func validLongQuerySort(value string) bool {
	switch value {
	case "sampled_at", "-sampled_at", "query_started_at", "-query_started_at":
		return true
	default:
		return false
	}
}
