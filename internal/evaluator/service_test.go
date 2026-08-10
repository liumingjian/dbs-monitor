package evaluator

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestStructurallyNotApplicable(t *testing.T) {
	tests := []struct {
		name                string
		metricID            string
		unavailability      pgtype.Text
		agentMetricsEnabled bool
		want                bool
	}{
		{name: "role mismatch", metricID: "pg.replication.wal_lag_bytes", unavailability: pgtype.Text{String: "NOT_APPLICABLE_ROLE", Valid: true}, agentMetricsEnabled: true, want: true},
		{name: "unenrolled agent state", metricID: "agent.status", agentMetricsEnabled: false, want: true},
		{name: "unenrolled agent metric", metricID: "host.cpu.usage_percent", agentMetricsEnabled: false, want: true},
		{name: "server metric remains applicable", metricID: "pg.connection.total", agentMetricsEnabled: false, want: false},
		{name: "ordinary missing data remains applicable", metricID: "pg.connection.total", unavailability: pgtype.Text{String: "COLLECTION_FAILED", Valid: true}, agentMetricsEnabled: true, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := structurallyNotApplicable(test.metricID, test.unavailability, test.agentMetricsEnabled); got != test.want {
				t.Fatalf("structurallyNotApplicable() = %t, want %t", got, test.want)
			}
		})
	}
}
