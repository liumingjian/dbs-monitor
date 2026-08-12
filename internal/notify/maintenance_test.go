package notify

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestActiveMaintenanceWindow(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	instanceID := uuid.MustParse("00000000-0000-4000-8000-000000000082")
	otherInstanceID := uuid.MustParse("00000000-0000-4000-8000-000000000083")
	activeID := uuid.MustParse("00000000-0000-4000-8000-000000000101")
	laterID := uuid.MustParse("00000000-0000-4000-8000-000000000102")

	tests := []struct {
		name    string
		at      time.Time
		windows []MaintenanceWindowScope
		wantID  uuid.UUID
		want    bool
	}{
		{name: "empty"},
		{
			name: "instance and half open bounds match",
			at:   now,
			windows: []MaintenanceWindowScope{{
				ID: activeID, InstanceIDs: []uuid.UUID{otherInstanceID, instanceID},
				StartsAt: now, EndsAt: now.Add(time.Hour),
			}},
			wantID: activeID, want: true,
		},
		{
			name: "end bound does not match",
			at:   now.Add(time.Hour),
			windows: []MaintenanceWindowScope{{
				ID: activeID, InstanceIDs: []uuid.UUID{instanceID},
				StartsAt: now, EndsAt: now.Add(time.Hour),
			}},
		},
		{
			name: "different instance does not match",
			at:   now,
			windows: []MaintenanceWindowScope{{
				ID: activeID, InstanceIDs: []uuid.UUID{otherInstanceID},
				StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
			}},
		},
		{
			name: "deleted window does not match",
			at:   now,
			windows: []MaintenanceWindowScope{{
				ID: activeID, InstanceIDs: []uuid.UUID{instanceID},
				StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Deleted: true,
			}},
		},
		{
			name: "overlap chooses the window ending first",
			at:   now,
			windows: []MaintenanceWindowScope{
				{ID: laterID, InstanceIDs: []uuid.UUID{instanceID}, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(2 * time.Hour)},
				{ID: activeID, InstanceIDs: []uuid.UUID{instanceID}, StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(time.Hour)},
			},
			wantID: activeID, want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window, ok := ActiveMaintenanceWindow(instanceID, test.at, test.windows)
			if ok != test.want || (ok && window.ID != test.wantID) {
				t.Fatalf("ActiveMaintenanceWindow() = (%v, %v), want id %s, match %v", window.ID, ok, test.wantID, test.want)
			}
		})
	}
}
