package metric

var AgentStatusEncodings = map[string]float64{
	AgentStatusOffline:          0,
	AgentStatusOnline:           1,
	AgentStatusNotInstalled:     2,
	AgentStatusPermissionDenied: 3,
	AgentStatusError:            4,
}

// NonNumericMetricEncodings maps metric state values to their stable float8 codes.
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
	MetricAgentStatus.String(): AgentStatusEncodings,
}
