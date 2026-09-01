package httpapi

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

type metricStep struct {
	name   string
	bucket pgtype.Interval
	raw    bool
}

func chooseMetricStep(requested api.MetricStep, from, to time.Time) (metricStep, error) {
	span := to.Sub(from)
	if span <= 0 {
		return metricStep{}, fmt.Errorf("from must be before to")
	}
	if span > 30*24*time.Hour {
		return metricStep{}, fmt.Errorf("time range must not exceed 30 days")
	}
	if requested == "" || requested == api.Auto {
		switch {
		case span <= time.Hour:
			requested = api.N15s
		case span <= 6*time.Hour:
			requested = api.N1m
		default:
			requested = api.N5m
		}
	}
	switch requested {
	case api.Raw:
		if span > 6*time.Hour {
			return metricStep{}, fmt.Errorf("raw step requires a range of 6 hours or less")
		}
		return metricStep{name: "raw", raw: true}, nil
	case api.N15s:
		return metricStep{name: "15s", bucket: pgtype.Interval{Microseconds: int64(15 * time.Second / time.Microsecond), Valid: true}}, nil
	case api.N1m:
		return metricStep{name: "1m", bucket: pgtype.Interval{Microseconds: int64(time.Minute / time.Microsecond), Valid: true}}, nil
	case api.N5m:
		return metricStep{name: "5m", bucket: pgtype.Interval{Microseconds: int64(5 * time.Minute / time.Microsecond), Valid: true}}, nil
	default:
		return metricStep{}, fmt.Errorf("unsupported step %q", requested)
	}
}
