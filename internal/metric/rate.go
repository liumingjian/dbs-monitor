package metric

import (
	"math"
	"time"
)

type ResetReason string

const (
	ResetNone            ResetReason = ""
	ResetCounter         ResetReason = "COUNTER_RESET"
	ResetInvalidInterval ResetReason = "INVALID_INTERVAL"
	ResetNonFinite       ResetReason = "NON_FINITE"
)

func Rate(previous, current float64, elapsed time.Duration) (float64, bool, ResetReason) {
	if math.IsNaN(previous) || math.IsNaN(current) || math.IsInf(previous, 0) || math.IsInf(current, 0) {
		return 0, false, ResetNonFinite
	}
	if elapsed <= 0 {
		return 0, false, ResetInvalidInterval
	}
	if current < previous {
		return 0, false, ResetCounter
	}
	return (current - previous) / elapsed.Seconds(), true, ResetNone
}
