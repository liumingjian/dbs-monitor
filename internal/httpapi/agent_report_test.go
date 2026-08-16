package httpapi

import (
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestAgentReportHasClockSkew(t *testing.T) {
	now := time.Unix(1_000, 0)
	tests := []struct {
		name            string
		currentOffset   time.Duration
		backfillOffsets []time.Duration
		want            bool
	}{
		{name: "current sample at past boundary", currentOffset: -agentClockSkew},
		{name: "current sample at future boundary", currentOffset: agentClockSkew},
		{name: "current sample too old", currentOffset: -agentClockSkew - time.Nanosecond, want: true},
		{name: "current sample too far in future", currentOffset: agentClockSkew + time.Nanosecond, want: true},
		{name: "old backfill is accepted", backfillOffsets: []time.Duration{-10 * time.Minute}},
		{name: "backfill at future boundary", backfillOffsets: []time.Duration{agentClockSkew}},
		{name: "backfill too far in future", backfillOffsets: []time.Duration{agentClockSkew + time.Nanosecond}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := api.AgentReport{Timestamp: now.Add(tt.currentOffset)}
			if tt.backfillOffsets != nil {
				backfill := make([]api.AgentSample, 0, len(tt.backfillOffsets))
				for _, offset := range tt.backfillOffsets {
					backfill = append(backfill, api.AgentSample{Timestamp: now.Add(offset)})
				}
				report.Backfill = &backfill
			}
			if got := agentReportHasClockSkew(report, now); got != tt.want {
				t.Fatalf("agentReportHasClockSkew() = %v, want %v", got, tt.want)
			}
		})
	}
}
