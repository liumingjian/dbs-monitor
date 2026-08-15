package main

import "testing"

func TestEvaluationConfigFromEnvironment(t *testing.T) {
	const setting = "ALERT_TRIGGER_SNAPSHOT_SESSION_LIMIT"
	tests := []struct {
		name      string
		value     string
		wantLimit int
		wantError bool
	}{
		{name: "default", wantLimit: 100},
		{name: "configured for acceptance", value: "5", wantLimit: 5},
		{name: "not an integer", value: "five", wantError: true},
		{name: "not positive", value: "0", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(setting, test.value)
			config, err := evaluationConfigFromEnvironment()
			if (err != nil) != test.wantError {
				t.Fatalf("evaluationConfigFromEnvironment() error = %v, want error %t", err, test.wantError)
			}
			if err == nil && config.TriggerSnapshotSessionLimit != test.wantLimit {
				t.Fatalf("trigger snapshot session limit = %d, want %d", config.TriggerSnapshotSessionLimit, test.wantLimit)
			}
		})
	}
}
