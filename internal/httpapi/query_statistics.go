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
	if states[metric.CapabilityExtensionPGStatStatements] != metric.CapabilityPresent {
		reason := api.FEATUREDISABLED
		if states[metric.CapabilityExtensionPGStatStatements] == metric.CapabilityMissing {
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
	rows, err := queries.ListQueryStatisticsSnapshotEntries(ctx, ListQueryStatisticsSnapshotEntriesParams{
		InstanceID: instanceID,
		SampledAt:  sampledAt,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.QueryStatisticsEntry, 0, len(rows))
	for _, row := range rows {
		items = append(items, api.QueryStatisticsEntry{
			Queryid:         strconv.FormatInt(row.Queryid, 10),
			DatabaseOid:     int64(row.DatabaseOid.Uint32),
			UserOid:         int64(row.UserOid.Uint32),
			Calls:           row.Calls,
			TotalExecTimeMs: row.TotalExecTimeMs,
		})
	}
	observedAt := sampledAt.Time.UTC()
	return api.GetQueryStatisticsSnapshot200JSONResponse{
		SampledAt: &observedAt,
		Items:     items,
	}, nil
}

func unavailableQueryStatistics(reason api.Unavailability) api.GetQueryStatisticsSnapshot200JSONResponse {
	return api.GetQueryStatisticsSnapshot200JSONResponse{
		Items:          []api.QueryStatisticsEntry{},
		Unavailability: &reason,
	}
}
