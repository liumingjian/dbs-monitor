package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestRunCommandRejectsUnsupportedArguments(t *testing.T) {
	for _, arguments := range [][]string{
		{"unsupported"},
		{rotateMasterKeyCommand, "unexpected"},
	} {
		err := runCommand(context.Background(), arguments)
		if err == nil {
			t.Fatalf("runCommand(%q) succeeded", arguments)
		}
		if got, want := err.Error(), "usage: dbs-monitor-server [rotate-master-key]"; got != want {
			t.Fatalf("runCommand(%q) error = %q, want %q", arguments, got, want)
		}
	}
}

func TestDiskThresholdsFromEnvironment(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]string
		want      platformhealth.DiskThresholds
		wantError bool
	}{
		{name: "defaults", want: platformhealth.DefaultDiskThresholds()},
		{
			name: "deployment overrides",
			values: map[string]string{
				"DISK_WARNING_PERCENT": "75.5", "DISK_CRITICAL_PERCENT": "85", "DISK_EMERGENCY_PERCENT": "92.5",
			},
			want: platformhealth.DiskThresholds{Warning: 75.5, Critical: 85, Emergency: 92.5, Hysteresis: 2},
		},
		{name: "non numeric", values: map[string]string{"DISK_WARNING_PERCENT": "high"}, wantError: true},
		{name: "non finite", values: map[string]string{"DISK_WARNING_PERCENT": "NaN"}, wantError: true},
		{
			name:   "unordered thresholds",
			values: map[string]string{"DISK_WARNING_PERCENT": "91", "DISK_CRITICAL_PERCENT": "90"}, wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"DISK_WARNING_PERCENT", "DISK_CRITICAL_PERCENT", "DISK_EMERGENCY_PERCENT"} {
				t.Setenv(name, "")
			}
			for name, value := range test.values {
				t.Setenv(name, value)
			}
			got, err := diskThresholdsFromEnvironment()
			if (err != nil) != test.wantError {
				t.Fatalf("diskThresholdsFromEnvironment() error = %v, wantError %t", err, test.wantError)
			}
			if !test.wantError && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("disk thresholds = %+v, want %+v", got, test.want)
			}
		})
	}
}
