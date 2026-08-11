package metric

// NonNumericMetricEncodings maps state values stored in metric_sample to float8 codes.
var NonNumericMetricEncodings = map[string]map[string]float64{
	MetricAvailabilityReachable.String(): {
		"unreachable": 0,
		"reachable":   1,
	},
	MetricReplicationRole.String(): {
		"standalone": 0,
		"primary":    1,
		"replica":    2,
	},
	MetricReplicationConnectionState.String(): {
		"stopped":    0,
		"starting":   1,
		"startup":    2,
		"catchup":    3,
		"streaming":  4,
		"backup":     5,
		"stopping":   6,
		"waiting":    7,
		"restarting": 8,
	},
}
