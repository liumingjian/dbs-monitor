package httpapi

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func (handler *Handler) GetQueryStatisticsSnapshot(
	ctx context.Context,
	request api.GetQueryStatisticsSnapshotRequestObject,
) (api.GetQueryStatisticsSnapshotResponseObject, error) {
	instanceID := databaseUUID(request.Id)
	states, _, err := handler.currentCapabilitySnapshot(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	capabilityStatus := states[metric.CapabilityExtensionPGStatStatements]
	if capabilityStatus != metric.CapabilityPresent {
		reason := api.FEATUREDISABLED
		if capabilityStatus == metric.CapabilityMissing {
			reason = api.EXTENSIONMISSING
		}
		return unavailableQueryStatistics(reason), nil
	}

	queries := New(handler.platform)
	sampledAt, err := queries.GetLatestQueryStatisticsSnapshot(ctx, instanceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailableQueryStatistics(api.FEATUREDISABLED), nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := queries.ListQueryStatisticsSnapshotEntries(ctx, ListQueryStatisticsSnapshotEntriesParams{
		InstanceID: instanceID,
		SampledAt:  sampledAt,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.QueryStatisticsEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, api.QueryStatisticsEntry{
			Queryid:         strconv.FormatInt(entry.Queryid, 10),
			DatabaseOid:     int64(entry.DatabaseOid.Uint32),
			UserOid:         int64(entry.UserOid.Uint32),
			Calls:           entry.Calls,
			TotalExecTimeMs: entry.TotalExecTimeMs,
		})
	}
	sampledAtTime := sampledAt.Time.UTC()
	return api.GetQueryStatisticsSnapshot200JSONResponse{
		SampledAt: &sampledAtTime,
		Items:     items,
	}, nil
}

func unavailableQueryStatistics(reason api.Unavailability) api.GetQueryStatisticsSnapshot200JSONResponse {
	return api.GetQueryStatisticsSnapshot200JSONResponse{
		Items:          []api.QueryStatisticsEntry{},
		Unavailability: &reason,
	}
}
