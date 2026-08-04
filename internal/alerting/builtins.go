package alerting

type BuiltinRule struct {
	Name      string
	MetricID  string
	Severity  string
	Enabled   bool
	Deletable bool
}

var BuiltinCollectionRules = []BuiltinRule{
	{Name: "数据库不可达", MetricID: "pg.availability.reachable", Severity: "critical", Enabled: true, Deletable: false},
	{Name: "Agent 离线", MetricID: "agent.status", Severity: "critical", Enabled: true, Deletable: false},
	{Name: "数据过期", MetricID: "collector.last_success_time", Severity: "warning", Enabled: true, Deletable: false},
}
