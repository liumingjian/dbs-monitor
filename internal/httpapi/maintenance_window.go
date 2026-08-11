package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

type maintenanceWindowValues struct {
	instanceIDs []pgtype.UUID
	startsAt    pgtype.Timestamptz
	endsAt      pgtype.Timestamptz
	reason      string
}

var errMaintenanceInstanceNotFound = errors.New("maintenance window instance not found")

func (handler *Handler) ListMaintenanceWindows(ctx context.Context, _ api.ListMaintenanceWindowsRequestObject) (api.ListMaintenanceWindowsResponseObject, error) {
	queries := notify.New(handler.platform)
	rows, err := queries.ListMaintenanceWindows(ctx)
	if err != nil {
		return nil, err
	}
	now := handler.clock.Now().UTC()
	response := make(api.ListMaintenanceWindows200JSONResponse, 0, len(rows))
	for _, row := range rows {
		item, err := toAPIMaintenanceWindow(ctx, queries, row, now)
		if err != nil {
			return nil, err
		}
		response = append(response, item)
	}
	return response, nil
}

func (handler *Handler) CreateMaintenanceWindow(ctx context.Context, request api.CreateMaintenanceWindowRequestObject) (api.CreateMaintenanceWindowResponseObject, error) {
	now := handler.clock.Now().UTC()
	values, ok := maintenanceWindowInputValues(request.Body, now)
	if !ok {
		return api.CreateMaintenanceWindow400JSONResponse(errorBody(api.VALIDATIONFAILED, "instances, a future end time, ordered bounds, and a reason are required")), nil
	}
	windowID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	var row notify.MaintenanceWindow
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := maintenanceWindowInstancesExist(ctx, queries, values.instanceIDs); err != nil {
			return err
		}
		var err error
		row, err = queries.CreateMaintenanceWindow(ctx, notify.CreateMaintenanceWindowParams{
			ID: windowID, StartsAt: values.startsAt, EndsAt: values.endsAt, Reason: values.reason,
			CreatedBy: databaseUUID(authenticatedUserID(ctx)),
			CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return err
		}
		return addMaintenanceWindowInstances(ctx, queries, windowID, values.instanceIDs)
	})
	if errors.Is(err, errMaintenanceInstanceNotFound) {
		return api.CreateMaintenanceWindow400JSONResponse(errorBody(api.VALIDATIONFAILED, "all instances must exist")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPIMaintenanceWindow(ctx, notify.New(handler.platform), row, now)
	if err != nil {
		return nil, err
	}
	return api.CreateMaintenanceWindow201JSONResponse(item), nil
}

func (handler *Handler) UpdateMaintenanceWindow(ctx context.Context, request api.UpdateMaintenanceWindowRequestObject) (api.UpdateMaintenanceWindowResponseObject, error) {
	now := handler.clock.Now().UTC()
	values, ok := maintenanceWindowInputValues(request.Body, now)
	if !ok {
		return api.UpdateMaintenanceWindow400JSONResponse(errorBody(api.VALIDATIONFAILED, "instances, a future end time, ordered bounds, and a reason are required")), nil
	}
	windowID := databaseUUID(request.Id)
	var row notify.MaintenanceWindow
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := maintenanceWindowInstancesExist(ctx, queries, values.instanceIDs); err != nil {
			return err
		}
		var err error
		row, err = queries.UpdateMaintenanceWindow(ctx, notify.UpdateMaintenanceWindowParams{
			ID: windowID, StartsAt: values.startsAt, EndsAt: values.endsAt, Reason: values.reason,
			UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return err
		}
		if err := queries.ClearMaintenanceWindowInstances(ctx, windowID); err != nil {
			return err
		}
		return addMaintenanceWindowInstances(ctx, queries, windowID, values.instanceIDs)
	})
	if errors.Is(err, errMaintenanceInstanceNotFound) {
		return api.UpdateMaintenanceWindow400JSONResponse(errorBody(api.VALIDATIONFAILED, "all instances must exist")), nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_, lookupErr := notify.New(handler.platform).GetMaintenanceWindow(ctx, windowID)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return api.UpdateMaintenanceWindow404JSONResponse(errorBody(api.NOTFOUND, "maintenance window not found")), nil
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		return api.UpdateMaintenanceWindow400JSONResponse(errorBody(api.CONFLICT, "ended maintenance windows cannot be edited")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPIMaintenanceWindow(ctx, notify.New(handler.platform), row, now)
	if err != nil {
		return nil, err
	}
	return api.UpdateMaintenanceWindow200JSONResponse(item), nil
}

func (handler *Handler) EndMaintenanceWindow(ctx context.Context, request api.EndMaintenanceWindowRequestObject) (api.EndMaintenanceWindowResponseObject, error) {
	now := handler.clock.Now().UTC()
	windowID := databaseUUID(request.Id)
	row, err := notify.New(handler.platform).EndMaintenanceWindow(ctx, notify.EndMaintenanceWindowParams{
		ID: windowID, EndsAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, lookupErr := notify.New(handler.platform).GetMaintenanceWindow(ctx, windowID)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return api.EndMaintenanceWindow404JSONResponse(errorBody(api.NOTFOUND, "maintenance window not found")), nil
		}
		if lookupErr != nil {
			return nil, lookupErr
		}
		return api.EndMaintenanceWindow400JSONResponse(errorBody(api.CONFLICT, "maintenance window is not active")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPIMaintenanceWindow(ctx, notify.New(handler.platform), row, now)
	if err != nil {
		return nil, err
	}
	return api.EndMaintenanceWindow200JSONResponse(item), nil
}

func (handler *Handler) DeleteMaintenanceWindow(ctx context.Context, request api.DeleteMaintenanceWindowRequestObject) (api.DeleteMaintenanceWindowResponseObject, error) {
	deleted, err := notify.New(handler.platform).DeleteMaintenanceWindow(ctx, notify.DeleteMaintenanceWindowParams{
		ID: databaseUUID(request.Id), DeletedAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return api.DeleteMaintenanceWindow404JSONResponse(errorBody(api.NOTFOUND, "maintenance window not found")), nil
	}
	return api.DeleteMaintenanceWindow204Response{}, nil
}

func maintenanceWindowInputValues(input *api.MaintenanceWindowInput, now time.Time) (maintenanceWindowValues, bool) {
	if input == nil || len(input.InstanceIds) == 0 || !input.StartsAt.Before(input.EndsAt) || !now.Before(input.EndsAt) {
		return maintenanceWindowValues{}, false
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return maintenanceWindowValues{}, false
	}
	seen := make(map[uuid.UUID]bool, len(input.InstanceIds))
	instanceIDs := make([]pgtype.UUID, 0, len(input.InstanceIds))
	for _, instanceID := range input.InstanceIds {
		if seen[instanceID] {
			return maintenanceWindowValues{}, false
		}
		seen[instanceID] = true
		instanceIDs = append(instanceIDs, databaseUUID(instanceID))
	}
	return maintenanceWindowValues{
		instanceIDs: instanceIDs,
		startsAt:    pgtype.Timestamptz{Time: input.StartsAt.UTC(), Valid: true},
		endsAt:      pgtype.Timestamptz{Time: input.EndsAt.UTC(), Valid: true},
		reason:      reason,
	}, true
}

func maintenanceWindowInstancesExist(ctx context.Context, queries *notify.Queries, instanceIDs []pgtype.UUID) error {
	for _, instanceID := range instanceIDs {
		exists, err := queries.MaintenanceWindowInstanceExists(ctx, instanceID)
		if err != nil {
			return err
		}
		if !exists {
			return errMaintenanceInstanceNotFound
		}
	}
	return nil
}

func addMaintenanceWindowInstances(ctx context.Context, queries *notify.Queries, windowID pgtype.UUID, instanceIDs []pgtype.UUID) error {
	for _, instanceID := range instanceIDs {
		if err := queries.AddMaintenanceWindowInstance(ctx, notify.AddMaintenanceWindowInstanceParams{
			MaintenanceWindowID: windowID, InstanceID: instanceID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func toAPIMaintenanceWindow(ctx context.Context, queries *notify.Queries, row notify.MaintenanceWindow, now time.Time) (api.MaintenanceWindow, error) {
	instanceIDs, err := queries.ListMaintenanceWindowInstances(ctx, row.ID)
	if err != nil {
		return api.MaintenanceWindow{}, err
	}
	apiInstanceIDs := make([]uuid.UUID, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		apiInstanceIDs = append(apiInstanceIDs, uuid.UUID(instanceID.Bytes))
	}
	status := api.MaintenanceEnded
	if now.Before(row.StartsAt.Time) {
		status = api.MaintenanceScheduled
	} else if now.Before(row.EndsAt.Time) {
		status = api.MaintenanceActive
	}
	return api.MaintenanceWindow{
		Id: uuid.UUID(row.ID.Bytes), InstanceIds: apiInstanceIDs,
		StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC(), Reason: row.Reason,
		CreatedBy: uuid.UUID(row.CreatedBy.Bytes), Status: status,
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}
