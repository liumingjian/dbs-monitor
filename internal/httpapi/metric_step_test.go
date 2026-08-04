package httpapi

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestChooseMetricStep(t *testing.T) {
	from := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		requested api.GetMetricSeriesParamsStep
		span      time.Duration
		want      string
		wantError bool
	}{
		{"auto short", api.Auto, time.Hour, "15s", false},
		{"auto medium", api.Auto, 6 * time.Hour, "1m", false},
		{"auto long", api.Auto, 24 * time.Hour, "5m", false},
		{"raw six hours", api.Raw, 6 * time.Hour, "raw", false},
		{"raw over six hours", api.Raw, 6*time.Hour + time.Second, "", true},
		{"range over retention", api.N5m, 31 * 24 * time.Hour, "", true},
		{"reversed range", api.Auto, -time.Second, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chooseMetricStep(tt.requested, from, from.Add(tt.span))
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %v", err, tt.wantError)
			}
			if got.name != tt.want {
				t.Fatalf("step = %q, want %q", got.name, tt.want)
			}
		})
	}
}
