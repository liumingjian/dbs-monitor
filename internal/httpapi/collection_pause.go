package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func (handler *Handler) GetCollectionPause(ctx context.Context, request api.GetCollectionPauseRequestObject) (api.GetCollectionPauseResponseObject, error) {
	row, err := New(handler.platform).GetCollectionPause(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	return api.GetCollectionPause200JSONResponse(toAPICollectionPauseStatus(
		row.CollectionPaused,
		row.CollectionPauseUpdatedBy,
		row.CollectionPauseUpdatedAt,
		row.CollectionPauseReason,
	)), nil
}

func (handler *Handler) UpdateCollectionPause(ctx context.Context, request api.UpdateCollectionPauseRequestObject) (api.UpdateCollectionPauseResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("collection pause body is required")
	}
	instanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	actorID := databaseUserID(authenticatedUserID(ctx))
	updatedAt := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	reason := pgtype.Text{}
	if request.Body.Reason != nil {
		reason = pgtype.Text{String: *request.Body.Reason, Valid: true}
	}

	var status api.CollectionPauseStatus
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := New(tx)
		updated, err := queries.SetCollectionPause(ctx, SetCollectionPauseParams{
			InstanceID:               instanceID,
			CollectionPaused:         request.Body.Paused,
			CollectionPauseUpdatedBy: actorID,
			CollectionPauseUpdatedAt: updatedAt,
			CollectionPauseReason:    reason,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			current, err := queries.GetCollectionPause(ctx, instanceID)
			if err != nil {
				return err
			}
			status = toAPICollectionPauseStatus(
				current.CollectionPaused,
				current.CollectionPauseUpdatedBy,
				current.CollectionPauseUpdatedAt,
				current.CollectionPauseReason,
			)
			return nil
		}
		if err != nil {
			return err
		}
		status = toAPICollectionPauseStatus(
			updated.CollectionPaused,
			updated.CollectionPauseUpdatedBy,
			updated.CollectionPauseUpdatedAt,
			updated.CollectionPauseReason,
		)
		kind := alerting.EventUnfrozen
		if request.Body.Paused {
			kind = alerting.EventFrozen
		}
		return queries.CreateCollectionPauseEvents(ctx, CreateCollectionPauseEventsParams{
			InstanceID:  instanceID,
			Kind:        string(kind),
			EvaluatedAt: updatedAt,
			ActorID:     actorID,
		})
	})
	if err != nil {
		return nil, err
	}
	return api.UpdateCollectionPause200JSONResponse(status), nil
}

func toAPICollectionPauseStatus(paused bool, updatedBy pgtype.UUID, updatedAt pgtype.Timestamptz, reason pgtype.Text) api.CollectionPauseStatus {
	status := api.CollectionPauseStatus{Paused: paused}
	if updatedBy.Valid {
		value := uuid.UUID(updatedBy.Bytes)
		status.UpdatedBy = &value
	}
	if updatedAt.Valid {
		value := updatedAt.Time
		status.UpdatedAt = &value
	}
	if reason.Valid {
		status.Reason = &reason.String
	}
	return status
}
