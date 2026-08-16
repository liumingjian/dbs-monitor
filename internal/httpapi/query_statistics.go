package httpapi

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	if states[metric.CapabilityRolePGMonitor] == metric.CapabilityMissing {
		return unavailableQueryStatistics(api.PERMISSIONDENIED), nil
	}
	if capabilityStatus != metric.CapabilityPresent {
		reason := api.FEATUREDISABLED
		if capabilityStatus == metric.CapabilityMissing {
			reason = api.EXTENSIONMISSING
		}
		return unavailableQueryStatistics(reason), nil
	}

	queries := New(handler.platform)
	state, err := handler.queryStatisticsCollectionState(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if state.unavailability != nil {
		return unavailableQueryStatistics(*state.unavailability), nil
	}
	sampledAt, err := queries.GetLatestQueryStatisticsSnapshot(ctx, GetLatestQueryStatisticsSnapshotParams{
		InstanceID: instanceID,
		LowerBound: pgtype.Timestamptz{
			Time:  handler.clock.Now().UTC().Add(-state.freshness),
			Valid: true,
		},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		hasSnapshot, snapshotErr := queries.HasQueryStatisticsSnapshot(ctx, instanceID)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if hasSnapshot {
			return unavailableQueryStatistics(api.STALE), nil
		}
		return unavailableQueryStatistics(api.NOSAMPLESYET), nil
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
	if len(entries) == 0 {
		return unavailableQueryStatisticsAt(api.NODATAINRANGE, sampledAt.Time.UTC()), nil
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

type queryStatisticsState struct {
	freshness      time.Duration
	unavailability *api.Unavailability
}

func (handler *Handler) queryStatisticsCollectionState(ctx context.Context, instanceID pgtype.UUID) (queryStatisticsState, error) {
	defaultInterval := 5 * time.Minute
	for _, task := range metric.Tasks {
		if task.ID == metric.TaskQueryStatistics {
			defaultInterval = task.Interval
			break
		}
	}
	state := queryStatisticsState{freshness: defaultInterval * 5 / 2}
	var result, code pgtype.Text
	var intervalSeconds int32
	err := handler.platform.QueryRow(ctx, `SELECT collection.last_result, collection.last_error_code,
		COALESCE(config.interval_seconds, $3)::integer
		FROM instance_collection_task_state collection
		LEFT JOIN collection_task_config config
			ON config.instance_id = collection.instance_id AND config.task_id = collection.task_id
		WHERE collection.instance_id = $1 AND collection.task_id = $2`,
		instanceID, metric.TaskQueryStatistics, int32(defaultInterval/time.Second),
	).Scan(&result, &code, &intervalSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return queryStatisticsState{}, err
	}
	state.freshness = time.Duration(intervalSeconds) * time.Second * 5 / 2
	if code.Valid && code.String == string(metric.ResetCounter) {
		reason := api.COUNTERRESET
		state.unavailability = &reason
		return state, nil
	}
	if result.Valid {
		switch api.CollectionTaskResult(result.String) {
		case api.FAILED, api.TIMEDOUT, api.SKIPPEDBACKPRESSURE, api.BACKOFF:
			reason := api.COLLECTIONFAILED
			state.unavailability = &reason
		}
	}
	return state, nil
}

func unavailableQueryStatistics(reason api.Unavailability) api.GetQueryStatisticsSnapshot200JSONResponse {
	return api.GetQueryStatisticsSnapshot200JSONResponse{
		Items:          []api.QueryStatisticsEntry{},
		Unavailability: &reason,
	}
}

func unavailableQueryStatisticsAt(reason api.Unavailability, sampledAt time.Time) api.GetQueryStatisticsSnapshot200JSONResponse {
	response := unavailableQueryStatistics(reason)
	response.SampledAt = &sampledAt
	return response
}
