package metric

import (
	"math"
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	tests := []struct {
		name       string
		previous   float64
		current    float64
		elapsed    time.Duration
		want       float64
		wantOK     bool
		wantReason ResetReason
	}{
		{"increases", 100, 160, 30 * time.Second, 2, true, ResetNone},
		{"counter reset", 160, 10, 30 * time.Second, 0, false, ResetCounter},
		{"unchanged", 100, 100, 30 * time.Second, 0, true, ResetNone},
		{"non-positive interval", 100, 160, 0, 0, false, ResetInvalidInterval},
		{"non-finite input", math.NaN(), 160, 30 * time.Second, 0, false, ResetNonFinite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, reason := Rate(tt.previous, tt.current, tt.elapsed)
			if got != tt.want || ok != tt.wantOK || reason != tt.wantReason {
				t.Fatalf("Rate() = (%v, %v, %q), want (%v, %v, %q)", got, ok, reason, tt.want, tt.wantOK, tt.wantReason)
			}
			if got < 0 {
				t.Fatalf("Rate() returned negative value %v", got)
			}
		})
	}
}
