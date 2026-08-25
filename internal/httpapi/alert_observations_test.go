package httpapi

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/clock"
)

func TestToAPIAlertEventPreservesStoredFacts(t *testing.T) {
	timezone := time.FixedZone("test", 8*60*60)
	evaluatedAt := time.Date(2026, 8, 16, 14, 30, 0, 0, timezone)
	actedAt := evaluatedAt.Add(time.Minute)
	actorID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	triggerSnapshotID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	maintenanceWindowID := uuid.MustParse("30000000-0000-4000-8000-000000000001")

	got, err := toAPIAlertEvent(AlertEvent{
		ID:                  42,
		RuleVersion:         3,
		Kind:                "IGNORED",
		FromState:           "FIRING",
		ToState:             "FIRING",
		CurrentValue:        pgtype.Float8{Float64: 91.5, Valid: true},
		Unavailability:      pgtype.Text{String: "AGENT_OFFLINE", Valid: true},
		RuleSnapshot:        []byte(`{"name":"Connection pressure","threshold":80}`),
		EvaluatedAt:         pgtype.Timestamptz{Time: evaluatedAt, Valid: true},
		ActorID:             pgtype.UUID{Bytes: actorID, Valid: true},
		ActedAt:             pgtype.Timestamptz{Time: actedAt, Valid: true},
		FromDisposition:     pgtype.Text{String: "NONE", Valid: true},
		ToDisposition:       pgtype.Text{String: "IGNORED", Valid: true},
		DispositionNote:     pgtype.Text{String: "Investigating", Valid: true},
		IgnoreReasonCode:    pgtype.Text{String: "KNOWN_ISSUE", Valid: true},
		IgnoreReasonDetail:  pgtype.Text{String: "Expected during rollout", Valid: true},
		TriggerSnapshotID:   pgtype.UUID{Bytes: triggerSnapshotID, Valid: true},
		InMaintenance:       true,
		MaintenanceWindowID: pgtype.UUID{Bytes: maintenanceWindowID, Valid: true},
	})
	if err != nil {
		t.Fatalf("project alert event: %v", err)
	}

	currentValue := 91.5
	unavailability := api.Unavailability("AGENT_OFFLINE")
	fromDisposition := api.AlertDisposition("NONE")
	toDisposition := api.AlertDisposition("IGNORED")
	dispositionNote := "Investigating"
	ignoreReasonCode := api.IgnoreReasonCode("KNOWN_ISSUE")
	ignoreReasonDetail := "Expected during rollout"
	actedAtUTC := actedAt.UTC()
	want := api.AlertEvent{
		Id:                  42,
		Kind:                api.AlertEventIgnored,
		FromState:           api.AlertStatus("FIRING"),
		ToState:             api.AlertStatus("FIRING"),
		RuleVersion:         3,
		CurrentValue:        &currentValue,
		Unavailability:      &unavailability,
		RuleSnapshot:        map[string]interface{}{"name": "Connection pressure", "threshold": float64(80)},
		EvaluatedAt:         evaluatedAt.UTC(),
		ActorId:             &actorID,
		ActedAt:             &actedAtUTC,
		FromDisposition:     &fromDisposition,
		ToDisposition:       &toDisposition,
		DispositionNote:     &dispositionNote,
		IgnoreReasonCode:    &ignoreReasonCode,
		IgnoreReasonDetail:  &ignoreReasonDetail,
		TriggerSnapshotId:   &triggerSnapshotID,
		InMaintenance:       true,
		MaintenanceWindowId: &maintenanceWindowID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projected alert event = %+v, want %+v", got, want)
	}
}

func TestToAPIAlertEventOmitsMissingFacts(t *testing.T) {
	got, err := toAPIAlertEvent(AlertEvent{RuleSnapshot: []byte(`{}`)})
	if err != nil {
		t.Fatalf("project alert event: %v", err)
	}
	if got.CurrentValue != nil || got.Unavailability != nil || got.ActorId != nil || got.ActedAt != nil ||
		got.FromDisposition != nil || got.ToDisposition != nil || got.DispositionNote != nil ||
		got.IgnoreReasonCode != nil || got.IgnoreReasonDetail != nil || got.TriggerSnapshotId != nil ||
		got.MaintenanceWindowId != nil {
		t.Fatalf("projected optional facts = %+v, want all omitted", got)
	}
}

func TestToAPIAlertEventRejectsMalformedRuleSnapshot(t *testing.T) {
	if _, err := toAPIAlertEvent(AlertEvent{RuleSnapshot: []byte(`{`)}); err == nil {
		t.Fatal("project malformed rule snapshot: expected error")
	}
}

func TestParseAlertObservationPagination(t *testing.T) {
	zero := 0
	negative := -1
	maxLimit := maxAlertObservationLimit
	overMaxLimit := maxAlertObservationLimit + 1
	offset := 25

	tests := []struct {
		name            string
		requestedLimit  *int
		requestedOffset *int
		want            alertObservationPagination
		valid           bool
	}{
		{
			name:  "defaults",
			want:  alertObservationPagination{limit: defaultAlertObservationLimit},
			valid: true,
		},
		{
			name:            "explicit maximum and offset",
			requestedLimit:  &maxLimit,
			requestedOffset: &offset,
			want:            alertObservationPagination{limit: maxLimit, offset: offset},
			valid:           true,
		},
		{
			name:           "zero limit",
			requestedLimit: &zero,
		},
		{
			name:           "limit over maximum",
			requestedLimit: &overMaxLimit,
		},
		{
			name:            "negative offset",
			requestedOffset: &negative,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := parseAlertObservationPagination(test.requestedLimit, test.requestedOffset)
			if valid != test.valid {
				t.Fatalf("pagination valid = %t, want %t", valid, test.valid)
			}
			if valid && got != test.want {
				t.Fatalf("pagination = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestAlertObservationResponsePreservesStoredFacts(t *testing.T) {
	firstTriggeredAt := time.Date(2026, 8, 11, 10, 15, 0, 0, time.UTC)
	now := firstTriggeredAt.Add(45 * time.Minute)
	pausedAt := firstTriggeredAt.Add(-8 * 24 * time.Hour)
	alertID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	instanceID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	ruleID := uuid.MustParse("30000000-0000-4000-8000-000000000001")
	handler := &Handler{clock: clock.NewManual(now)}

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
