package notify

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type MaintenanceWindowScope struct {
	ID          uuid.UUID
	InstanceIDs []uuid.UUID
	StartsAt    time.Time
	EndsAt      time.Time
	Deleted     bool
}

func FindActiveMaintenanceWindow(ctx context.Context, queries *Queries, instanceID pgtype.UUID, at time.Time) (pgtype.UUID, bool, error) {
	rows, err := queries.ListMaintenanceWindowsForInstance(ctx, instanceID)
	if err != nil {
		return pgtype.UUID{}, false, err
	}
	id := uuid.UUID(instanceID.Bytes)
	window, active := ActiveMaintenanceWindow(id, at.UTC(), MaintenanceWindowScopes(id, rows))
	if !active {
		return pgtype.UUID{}, false, nil
	}
	return pgtype.UUID{Bytes: window.ID, Valid: true}, true, nil
}

func ActiveMaintenanceWindow(instanceID uuid.UUID, at time.Time, windows []MaintenanceWindowScope) (MaintenanceWindowScope, bool) {
	var active MaintenanceWindowScope
	found := false
	for _, window := range windows {
		if window.Deleted || at.Before(window.StartsAt) || !at.Before(window.EndsAt) || !containsInstance(window.InstanceIDs, instanceID) {
			continue
		}
		if !found || window.EndsAt.Before(active.EndsAt) ||
			(window.EndsAt.Equal(active.EndsAt) && window.ID.String() < active.ID.String()) {
			active = window
			found = true
		}
	}
	return active, found
}

func MaintenanceWindowScopes(instanceID uuid.UUID, rows []MaintenanceWindow) []MaintenanceWindowScope {
	windows := make([]MaintenanceWindowScope, 0, len(rows))
	for _, row := range rows {
		windows = append(windows, MaintenanceWindowScope{
			ID:          uuid.UUID(row.ID.Bytes),
			InstanceIDs: []uuid.UUID{instanceID},
			StartsAt:    row.StartsAt.Time.UTC(),
			EndsAt:      row.EndsAt.Time.UTC(),
			Deleted:     row.DeletedAt.Valid,
		})
	}
	return windows
}

func containsInstance(instanceIDs []uuid.UUID, instanceID uuid.UUID) bool {
	for _, candidate := range instanceIDs {
		if candidate == instanceID {
			return true
		}
	}
	return false
}
