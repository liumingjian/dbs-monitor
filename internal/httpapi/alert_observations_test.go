package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestAlertObservationResponsePreservesStoredFacts(t *testing.T) {
	firstTriggeredAt := time.Date(2026, 8, 11, 10, 15, 0, 0, time.UTC)
	now := firstTriggeredAt.Add(45 * time.Minute)
	pausedAt := firstTriggeredAt.Add(-8 * 24 * time.Hour)
	alertID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	instanceID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	ruleID := uuid.MustParse("30000000-0000-4000-8000-000000000001")
	handler := &Handler{clock: projectionClock{now: now}}

	got, err := handler.alertObservationResponse(alertObservationProjection{
		id:               pgtype.UUID{Bytes: alertID, Valid: true},
		instanceID:       pgtype.UUID{Bytes: instanceID, Valid: true},
		instanceName:     "payments-primary",
		ruleID:           pgtype.UUID{Bytes: ruleID, Valid: true},
		ruleName:         "Connection pressure",
		ruleVersion:      2,
		ruleSnapshot:     []byte(`{"name":"Connection pressure","threshold":80}`),
		metricID:         "pg.connection.total",
		status:           "NO_DATA",
		severity:         "critical",
		disposition:      "ACKED",
		paused:           true,
		pausedAt:         pgtype.Timestamptz{Time: pausedAt, Valid: true},
		currentValue:     pgtype.Float8{},
		threshold:        80,
		firstTriggeredAt: pgtype.Timestamptz{Time: firstTriggeredAt, Valid: true},
		updatedAt:        pgtype.Timestamptz{Time: now, Valid: true},
		unavailability:   pgtype.Text{String: "AGENT_OFFLINE", Valid: true},
	})
	if err != nil {
		t.Fatalf("project alert observation: %v", err)
	}
	if got.Id != alertID || got.InstanceId != instanceID || got.RuleId != ruleID {
		t.Fatalf("projected identities = %s/%s/%s", got.Id, got.InstanceId, got.RuleId)
	}
	if got.Status != api.AlertStatus("NO_DATA") || got.CurrentValue != nil {
		t.Fatalf("status/current value = %s/%v, want NO_DATA/nil", got.Status, got.CurrentValue)
	}
	if got.Unavailability == nil || *got.Unavailability != api.Unavailability("AGENT_OFFLINE") {
		t.Fatalf("unavailability = %v, want AGENT_OFFLINE", got.Unavailability)
	}
	if got.DurationMs != (45 * time.Minute).Milliseconds() {
		t.Fatalf("duration = %d, want %d", got.DurationMs, (45 * time.Minute).Milliseconds())
	}
	if !got.Paused || got.PausedAt == nil || !got.PausedAt.Equal(pausedAt) {
		t.Fatalf("pause projection = %t at %v", got.Paused, got.PausedAt)
	}
}

type projectionClock struct{ now time.Time }

func (clock projectionClock) Now() time.Time { return clock.now }

func (projectionClock) Ticker(time.Duration) (<-chan time.Time, func()) {
	return make(chan time.Time), func() {}
}
