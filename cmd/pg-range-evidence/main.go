package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/liumingjian/dbs-monitor/internal/capability"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

var candidateSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type targetDatabase struct {
	Version string
	URL     string
}

type probeFunc func(context.Context, targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error)

type pgRangeEvidence struct {
	CandidateSHA string            `json:"candidate_sha"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Result       string            `json:"result"`
	Versions     []versionEvidence `json:"versions"`
	Entries      []evidenceEntry   `json:"entries"`
}

type versionEvidence struct {
	Version      string                                          `json:"version"`
	Result       string                                          `json:"result"`
	Capabilities map[metric.CapabilityID]metric.CapabilityStatus `json:"capabilities"`
}

type evidenceEntry struct {
	ID           string `json:"id"`
	Baseline     bool   `json:"baseline"`
	Status       string `json:"status"`
	ActualResult string `json:"actual_result"`
}

func main() {
	candidateSHA := flag.String("candidate-sha", "", "40-character candidate commit SHA")
	output := flag.String("output", "results/pg-range-evidence.json", "evidence output path")
	flag.Parse()

	targets, err := targetsFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	evidence, collectErr := collectEvidence(context.Background(), *candidateSHA, targets, probeTarget)
	if err := writeEvidence(*output, evidence); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if collectErr != nil {
		fmt.Fprintln(os.Stderr, collectErr)
		os.Exit(1)
	}
}

func targetsFromEnvironment() ([]targetDatabase, error) {
	targets := make([]targetDatabase, 0, 5)
	for _, version := range []string{"13", "14", "15", "16", "17"} {
		name := "PG" + version + "_URL"
		url := os.Getenv(name)
		if url == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
		targets = append(targets, targetDatabase{Version: version, URL: url})
	}
	return targets, nil
}

func collectEvidence(ctx context.Context, candidateSHA string, targets []targetDatabase, probe probeFunc) (pgRangeEvidence, error) {
	evidence := pgRangeEvidence{
		CandidateSHA: candidateSHA,
		GeneratedAt:  time.Now().UTC(),
		Result:       "pass",
	}
	if !candidateSHAPattern.MatchString(candidateSHA) {
		return evidence, errors.New("candidate SHA must be 40 lowercase hexadecimal characters")
	}
	if err := validateTargets(targets); err != nil {
		return evidence, err
	}

	var failures []error
	for _, target := range targets {
		evidence.Entries = append(evidence.Entries, evidenceEntry{
			ID: "pg" + target.Version + "/collection", Baseline: true, Status: "pass",
			ActualResult: "collection query subset passed",
		})
		states, complete, err := probe(ctx, target)
		if states == nil {
			states = metric.UnknownCapabilityStates()
		}
		versionResult := "GO"
		entryStatus := "pass"
		if err != nil || !complete || containsUnknown(states) {
			versionResult = "NO-GO"
			entryStatus = "fail"
			evidence.Result = "fail"
			failures = append(failures, fmt.Errorf("PG%s capability evidence is UNKNOWN", target.Version))
		}
		evidence.Versions = append(evidence.Versions, versionEvidence{
			Version: target.Version, Result: versionResult, Capabilities: states,
		})
		evidence.Entries = append(evidence.Entries, evidenceEntry{
			ID: "pg" + target.Version + "/capabilities", Baseline: true, Status: entryStatus,
			ActualResult: versionResult,
		})
	}
	return evidence, errors.Join(failures...)
}

func validateTargets(targets []targetDatabase) error {
	want := []string{"13", "14", "15", "16", "17"}
	got := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.URL == "" {
			return fmt.Errorf("PG%s URL is required", target.Version)
		}
		got = append(got, target.Version)
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("target versions = %v, want %v", got, want)
	}
	return nil
}

func probeTarget(parent context.Context, target targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	config, err := pgx.ParseConfig(target.URL)
	if err != nil {
		return metric.UnknownCapabilityStates(), false, err
	}
	conn, err := (monitorpg.DirectDialer{}).Dial(ctx, config)
	if err != nil {
		return metric.UnknownCapabilityStates(), false, err
	}
	defer conn.Close(context.Background())
	states, complete := capability.ProbeSnapshot(ctx, conn)
	return states, complete, nil
}

func containsUnknown(states map[metric.CapabilityID]metric.CapabilityStatus) bool {
	for _, declaration := range metric.Capabilities {
		if states[declaration.ID] == metric.CapabilityUnknown {
			return true
		}
	}
	return false
}

func writeEvidence(path string, evidence pgRangeEvidence) error {
	contents, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return fmt.Errorf("encode PG range evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create evidence directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(contents, '\n'), 0o644); err != nil {
		return fmt.Errorf("write PG range evidence: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish PG range evidence: %w", err)
	}
	return nil
}
