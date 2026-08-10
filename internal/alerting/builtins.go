package alerting

import "github.com/liumingjian/dbs-monitor/internal/api"

type BuiltinRule struct {
	Name      string
	MetricID  string
	Severity  string
	Enabled   bool
	Deletable bool
}

var BuiltinCollectionRules = []BuiltinRule{
	{Name: "数据库不可达", MetricID: string(api.GetMetricSeriesParamsMetricPgAvailabilityReachable), Severity: "critical", Enabled: true, Deletable: false},
	{Name: "Agent 离线", MetricID: string(api.GetMetricSeriesParamsMetricAgentStatus), Severity: "critical", Enabled: true, Deletable: false},
	{Name: "数据过期", MetricID: string(api.GetMetricSeriesParamsMetricCollectorLastSuccessTime), Severity: "warning", Enabled: true, Deletable: false},
}
