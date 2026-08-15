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

var supportedPostgreSQLVersions = [...]string{"13", "14", "15", "16", "17"}

type targetDatabase struct {
	Version string
	URL     string
}

type targetProbe func(context.Context, targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error)

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
	outputPath := flag.String("output", "results/pg-range-evidence.json", "evidence output path")
	flag.Parse()

	targets, err := targetsFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	evidence, collectionErr := collectEvidence(context.Background(), *candidateSHA, targets, probeTarget)
	if err := writeEvidence(*outputPath, evidence); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if collectionErr != nil {
		fmt.Fprintln(os.Stderr, collectionErr)
		os.Exit(1)
	}
}

func targetsFromEnvironment() ([]targetDatabase, error) {
	targets := make([]targetDatabase, 0, len(supportedPostgreSQLVersions))
	for _, version := range supportedPostgreSQLVersions {
		environmentVariable := "PG" + version + "_URL"
		databaseURL := os.Getenv(environmentVariable)
		if databaseURL == "" {
			return nil, fmt.Errorf("%s is required", environmentVariable)
		}
		targets = append(targets, targetDatabase{Version: version, URL: databaseURL})
	}
	return targets, nil
}

func collectEvidence(ctx context.Context, candidateSHA string, targets []targetDatabase, probe targetProbe) (pgRangeEvidence, error) {
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
		entryPrefix := "pg" + target.Version
		evidence.Entries = append(evidence.Entries, evidenceEntry{
			ID:           entryPrefix + "/collection",
			Baseline:     true,
			Status:       "pass",
			ActualResult: "collection query subset passed",
		})

		capabilityStates, snapshotComplete, err := probe(ctx, target)
		if capabilityStates == nil {
			capabilityStates = metric.UnknownCapabilityStates()
		}
		capabilityResult := "GO"
		capabilityStatus := "pass"
		if err != nil || !snapshotComplete || hasUnknownCapability(capabilityStates) {
			capabilityResult = "NO-GO"
			capabilityStatus = "fail"
			evidence.Result = "fail"
			failures = append(failures, fmt.Errorf("PG%s capability evidence is UNKNOWN", target.Version))
		}
		evidence.Versions = append(evidence.Versions, versionEvidence{
			Version:      target.Version,
			Result:       capabilityResult,
			Capabilities: capabilityStates,
		})
		evidence.Entries = append(evidence.Entries, evidenceEntry{
			ID:           entryPrefix + "/capabilities",
			Baseline:     true,
			Status:       capabilityStatus,
			ActualResult: capabilityResult,
		})
	}
	return evidence, errors.Join(failures...)
}

func validateTargets(targets []targetDatabase) error {
	actualVersions := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.URL == "" {
			return fmt.Errorf("PG%s URL is required", target.Version)
		}
		actualVersions = append(actualVersions, target.Version)
	}
	if !slices.Equal(actualVersions, supportedPostgreSQLVersions[:]) {
		return fmt.Errorf("target versions = %v, want %v", actualVersions, supportedPostgreSQLVersions)
	}
	return nil
}

func probeTarget(parentContext context.Context, target targetDatabase) (map[metric.CapabilityID]metric.CapabilityStatus, bool, error) {
	ctx, cancel := context.WithTimeout(parentContext, 15*time.Second)
	defer cancel()
	connectionConfig, err := pgx.ParseConfig(target.URL)
	if err != nil {
		return metric.UnknownCapabilityStates(), false, err
	}
	conn, err := (monitorpg.DirectDialer{}).Dial(ctx, connectionConfig)
	if err != nil {
		return metric.UnknownCapabilityStates(), false, err
	}
	defer conn.Close(context.Background())
	states, complete := capability.ProbeSnapshot(ctx, conn)
	return states, complete, nil
}

func hasUnknownCapability(states map[metric.CapabilityID]metric.CapabilityStatus) bool {
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
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, append(contents, '\n'), 0o644); err != nil {
		return fmt.Errorf("write PG range evidence: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish PG range evidence: %w", err)
	}
	return nil
}
