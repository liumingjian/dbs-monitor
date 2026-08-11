package alerting

import (
	"reflect"
	"testing"
	"time"
)

func TestRollupInstanceHealth(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	early := now.Add(-2 * time.Hour)
	late := now.Add(-time.Hour)
	value := func(number float64) *float64 { return &number }
	recoveredRecently := now.Add(-23 * time.Hour)
	recoveredTooLongAgo := now.Add(-25 * time.Hour)

	tests := []struct {
		name  string
		input HealthRollupInput
		want  HealthRollup
	}{
		{
			name: "pause overrides critical alerts without hiding their facts",
			input: HealthRollupInput{Paused: true, EverCollected: true, Now: now, Alerts: []HealthAlert{{
				RuleName: "database unavailable", Severity: SeverityCritical, State: FIRING,
				FirstTriggeredAt: early, CurrentValue: value(0),
			}}},
			want: HealthRollup{
				Status:      HealthPaused,
				Attribution: &HealthAttribution{RuleName: "database unavailable", CurrentValue: value(0)},
				Counts:      HealthAlertCounts{Critical: 1},
			},
		},
		{
			name: "worst severity wins and earliest trigger attributes ties",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{
				{RuleName: "warning", Severity: SeverityWarning, State: FIRING, FirstTriggeredAt: early},
				{RuleName: "later critical", Severity: SeverityCritical, State: FIRING, FirstTriggeredAt: late, CurrentValue: value(92)},
				{RuleName: "earlier critical", Severity: SeverityCritical, State: NO_DATA, FirstTriggeredAt: early},
			}},
			want: HealthRollup{
				Status:      HealthCritical,
				Attribution: &HealthAttribution{RuleName: "earlier critical"},
				Counts:      HealthAlertCounts{Critical: 2, Warning: 1},
				Flags:       HealthFlags{NoData: true},
			},
		},
		{
			name: "warning colors but info only counts",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{
				{RuleName: "informational", Severity: SeverityInfo, State: FIRING, FirstTriggeredAt: early},
				{RuleName: "warning", Severity: SeverityWarning, State: FIRING, FirstTriggeredAt: late},
			}},
			want: HealthRollup{
				Status:      HealthWarning,
				Attribution: &HealthAttribution{RuleName: "warning"},
				Counts:      HealthAlertCounts{Warning: 1, Info: 1},
			},
		},
		{
			name: "pure info stays healthy and attributes the earliest info",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{
				{RuleName: "later info", Severity: SeverityInfo, State: FIRING, FirstTriggeredAt: late},
				{RuleName: "earlier info", Severity: SeverityInfo, State: FIRING, FirstTriggeredAt: early, CurrentValue: value(61)},
			}},
			want: HealthRollup{
				Status:      HealthHealthy,
				Attribution: &HealthAttribution{RuleName: "earlier info", CurrentValue: value(61)},
				Counts:      HealthAlertCounts{Info: 2},
			},
		},
		{
			name:  "never collected is unknown",
			input: HealthRollupInput{Now: now},
			want:  HealthRollup{Status: HealthUnknown},
		},
		{
			name:  "collection success is monotonic even after later loss",
			input: HealthRollupInput{EverCollected: true, Now: now},
			want:  HealthRollup{Status: HealthHealthy},
		},
		{
			name: "ignored alerts leave rollup and counts but remain marked",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{{
				RuleName: "ignored critical", Severity: SeverityCritical, State: NO_DATA,
				FirstTriggeredAt: early, Ignored: true,
			}}},
			want: HealthRollup{
				Status: HealthHealthy,
				Flags:  HealthFlags{NoData: true, Ignored: 1},
			},
		},
		{
			name: "pending recovered and old recovery do not become current alerts",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{
				{RuleName: "pending", Severity: SeverityCritical, State: PENDING, FirstTriggeredAt: early},
				{RuleName: "recent", Severity: SeverityCritical, State: RECOVERED, RecoveredAt: &recoveredRecently},
				{RuleName: "old", Severity: SeverityWarning, State: RECOVERED, RecoveredAt: &recoveredTooLongAgo},
			}},
			want: HealthRollup{Status: HealthHealthy, Flags: HealthFlags{RecentlyRecovered: true}},
		},
		{
			name:  "maintenance and configuration missing are orthogonal even when counts are zero",
			input: HealthRollupInput{EverCollected: true, Now: now, InMaintenance: true, ConfigurationMissing: 3},
			want:  HealthRollup{Status: HealthHealthy, Flags: HealthFlags{InMaintenance: true, ConfigurationMissing: 3}},
		},
		{
			name: "structural and configuration exclusions have zero health impact",
			input: HealthRollupInput{EverCollected: true, Now: now, Alerts: []HealthAlert{
				{RuleName: "not applicable", Severity: SeverityCritical, State: FIRING, Exclusion: HealthAlertStructurallyNotApplicable},
				{RuleName: "configuration missing", Severity: SeverityCritical, State: NO_DATA, Exclusion: HealthAlertConfigurationMissing},
			}},
			want: HealthRollup{Status: HealthHealthy},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RollupInstanceHealth(test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("RollupInstanceHealth() = %+v, want %+v", got, test.want)
			}
		})
	}
}
