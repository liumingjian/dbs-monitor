package metric

// NonNumericMetricEncodings maps state values stored in metric_sample to float8 codes.
var NonNumericMetricEncodings = map[string]map[string]float64{
	MetricAvailabilityReachable.String(): {
		"unreachable": 0,
		"reachable":   1,
	},
}
