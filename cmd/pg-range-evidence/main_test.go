package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

const testCandidateSHA = "0123456789012345678901234567890123456789"

func TestCollectEvidenceBuildsThreeStateTable(t *testing.T) {
	probe := func(_ context.Context, _ targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error) {
		return map[metric.CapabilityID]metric.CapabilityStatus{
			metric.CapabilityRolePGMonitor:             metric.CapabilityPresent,
			metric.CapabilityExtensionPGStatStatements: metric.CapabilityMissing,
			metric.CapabilityTopologyHasReplication:    metric.CapabilityNotApplicable,
			metric.CapabilityTopologyHasSlot:           metric.CapabilityNotApplicable,
		}, true, nil
	}

	evidence, err := collectEvidence(context.Background(), testCandidateSHA, testTargets(), probe)
	if err != nil {
		t.Fatalf("collect evidence: %v", err)
	}
	if evidence.Result != "pass" || len(evidence.Versions) != 5 || len(evidence.Entries) != 10 {
		t.Fatalf("evidence = %+v, want collection and capability entries for five passing versions", evidence)
	}
	if evidence.Entries[0].ID != "pg13/collection" || evidence.Entries[1].ID != "pg13/capabilities" {
		t.Fatalf("first version entries = %+v, want collection then capabilities", evidence.Entries[:2])
	}
	for _, version := range evidence.Versions {
		if version.Result != "GO" {
			t.Errorf("PG%s result = %s, want GO", version.Version, version.Result)
		}
		got := []metric.CapabilityStatus{
			version.Capabilities[metric.CapabilityRolePGMonitor],
			version.Capabilities[metric.CapabilityExtensionPGStatStatements],
			version.Capabilities[metric.CapabilityTopologyHasReplication],
		}
		want := []metric.CapabilityStatus{metric.CapabilityPresent, metric.CapabilityMissing, metric.CapabilityNotApplicable}
		if !slices.Equal(got, want) {
			t.Errorf("PG%s states = %v, want %v", version.Version, got, want)
		}
	}
	if _, err := json.Marshal(evidence); err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
}

func TestCollectEvidenceMakesUnknownVersionNoGo(t *testing.T) {
	probe := func(_ context.Context, target targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error) {
		states := metric.UnknownCapabilityStates()
		if target.Version != "15" {
			for id := range states {
				states[id] = metric.CapabilityPresent
			}
			return states, true, nil
		}
		return states, false, errors.New("probe failed")
	}

	evidence, err := collectEvidence(context.Background(), testCandidateSHA, testTargets(), probe)
	if err == nil {
		t.Fatal("collect evidence succeeded, want UNKNOWN failure")
	}
	if evidence.Result != "fail" || evidence.Versions[2].Result != "NO-GO" || evidence.Entries[5].Status != "fail" {
		t.Fatalf("evidence = %+v, want PG15 NO-GO", evidence)
	}
	for _, status := range evidence.Versions[2].Capabilities {
		if status != metric.CapabilityUnknown {
			t.Errorf("PG15 capability status = %s, want UNKNOWN", status)
		}
	}
}

func TestCollectEvidenceRejectsInvalidCandidateSHA(t *testing.T) {
	_, err := collectEvidence(context.Background(), "main", testTargets(), nil)
	if err == nil {
		t.Fatal("collect evidence accepted a moving candidate ref")
	}
}

func testTargets() []targetDatabase {
	return []targetDatabase{
		{Version: "13", URL: "postgres://pg13"},
		{Version: "14", URL: "postgres://pg14"},
		{Version: "15", URL: "postgres://pg15"},
		{Version: "16", URL: "postgres://pg16"},
		{Version: "17", URL: "postgres://pg17"},
	}
}
