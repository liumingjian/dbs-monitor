package api_test

import (
	"encoding/json"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestMetricPointNullDoesNotBecomeZero(t *testing.T) {
	payload := []byte(`{
		"from":"2026-08-03T00:00:00Z",
		"to":"2026-08-03T00:01:00Z",
		"step":"1m",
		"metrics":[{
			"metric":"pg.connection.total",
			"unit":"count",
			"unavailability":null,
			"series":[{"labels":{},"points":[[1754179200,null]]}]
		}]
	}`)

	var response api.MetricSeriesResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode metric response: %v", err)
	}

	point := response.Metrics[0].Series[0].Points[0]
	if len(point) != 2 {
		t.Fatalf("point length = %d, want 2", len(point))
	}
	if point[1] != nil {
		t.Fatalf("missing value became %v, want nil", *point[1])
	}
}
