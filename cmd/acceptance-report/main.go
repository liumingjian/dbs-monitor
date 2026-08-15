package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/acceptancereport"
)

var candidateSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type manifest struct {
	CandidateSHA string                   `json:"candidate_sha"`
	Version      string                   `json:"version"`
	GeneratedAt  time.Time                `json:"generated_at"`
	Environment  json.RawMessage          `json:"environment"`
	Profile      acceptancereport.Profile `json:"profile"`
	Rounds       []roundManifest          `json:"rounds"`
}

type roundManifest struct {
	Sequence     int             `json:"sequence"`
	CandidateSHA string          `json:"candidate_sha"`
	Valid        bool            `json:"valid"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
	Environment  json.RawMessage `json:"environment"`
	Evidence     evidencePaths   `json:"evidence"`
	ManualReview string          `json:"manual_review"`
}

type evidencePaths struct {
	Package     string `json:"package"`
	CheckFull   string `json:"check_full"`
	Matrix      string `json:"matrix"`
	PGRange     string `json:"pg_range"`
	RTC         string `json:"rt_c"`
	LinuxNative string `json:"linux_native"`
}

func main() {
	manifestPath := flag.String("manifest", "", "path to the report manifest")
	outputPath := flag.String("output", "acceptance-report.json", "path for the generated report")
	flag.Parse()
	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "-manifest is required")
		os.Exit(2)
	}
	githubActions := strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true")
	if err := generate(*manifestPath, *outputPath, githubActions); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(manifestPath, outputPath string, githubActions bool) error {
	if filepath.Ext(outputPath) != ".json" {
		return errors.New("acceptance report output must be a .json file")
	}
	if githubActions && isValidationArchive(outputPath) {
		return errors.New("GitHub Actions reports cannot be written to docs/validation")
	}

	input, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	if !candidateSHAPattern.MatchString(input.CandidateSHA) {
		return errors.New("candidate_sha must be a 40-character lowercase commit SHA")
	}
	if input.Version == "" || input.GeneratedAt.IsZero() || len(input.Environment) == 0 {
		return errors.New("version, generated_at, and environment are required")
	}
	if input.Profile != acceptancereport.ProfileCI && input.Profile != acceptancereport.ProfileFinal {
		return fmt.Errorf("unsupported report profile %q", input.Profile)
	}

	manifestDirectory := filepath.Dir(manifestPath)
	rounds := make([]acceptancereport.Round, 0, len(input.Rounds))
	for _, source := range input.Rounds {
		round, err := loadRound(manifestDirectory, source)
		if err != nil {
			return fmt.Errorf("round %d: %w", source.Sequence, err)
		}
		rounds = append(rounds, round)
	}
	decision := acceptancereport.Evaluate(acceptancereport.Input{
		CandidateSHA:  input.CandidateSHA,
		Profile:       input.Profile,
		GitHubActions: githubActions,
		Rounds:        rounds,
	})
	report := acceptancereport.Report{
		CandidateSHA: input.CandidateSHA,
		Version:      input.Version,
		GeneratedAt:  input.GeneratedAt,
		Environment:  input.Environment,
		Profile:      input.Profile,
		Rounds:       rounds,
		Verdict:      decision.Verdict,
		Provisional:  decision.Provisional,
		ReasonCodes:  decision.ReasonCodes,
	}
	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode acceptance report: %w", err)
	}
	contents = append(contents, '\n')
	if err := scanSecrets(contents); err != nil {
		return fmt.Errorf("acceptance report secret scan: %w", err)
	}
	if err := writeAtomic(outputPath, contents); err != nil {
		return fmt.Errorf("write acceptance report: %w", err)
	}
	return nil
}

func readManifest(path string) (manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	var result manifest
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return result, nil
}

func loadRound(manifestDirectory string, source roundManifest) (acceptancereport.Round, error) {
	evidence := acceptancereport.Evidence{}
	evidenceFiles := []struct {
		reference   string
		destination **acceptancereport.Block
	}{
		{source.Evidence.Package, &evidence.Package},
		{source.Evidence.CheckFull, &evidence.CheckFull},
		{source.Evidence.Matrix, &evidence.Matrix},
		{source.Evidence.PGRange, &evidence.PGRange},
		{source.Evidence.RTC, &evidence.RTC},
		{source.Evidence.LinuxNative, &evidence.LinuxNative},
	}
	for _, file := range evidenceFiles {
		if file.reference == "" {
			continue
		}
		block, err := loadBlock(manifestDirectory, file.reference)
		if err != nil {
			return acceptancereport.Round{}, err
		}
		*file.destination = block
	}
	var manualReview *acceptancereport.ManualReview
	if source.ManualReview != "" {
		review, err := loadManualReview(manifestDirectory, source.ManualReview)
		if err != nil {
			return acceptancereport.Round{}, err
		}
		manualReview = &review
	}
	return acceptancereport.Round{
		Sequence:     source.Sequence,
		CandidateSHA: source.CandidateSHA,
		Valid:        source.Valid,
		StartedAt:    source.StartedAt,
		FinishedAt:   source.FinishedAt,
		Environment:  source.Environment,
		Evidence:     evidence,
		ManualReview: manualReview,
	}, nil
}

func loadBlock(base, reference string) (*acceptancereport.Block, error) {
	path, err := artifactPath(base, reference)
	if err != nil {
		return nil, err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read evidence %q: %w", reference, err)
	}
	var artifact struct {
		CandidateSHA string                   `json:"candidate_sha"`
		Result       acceptancereport.Status  `json:"result"`
		Entries      []acceptancereport.Entry `json:"entries"`
		Exceptions   []json.RawMessage        `json:"exceptions"`
		Summary      json.RawMessage          `json:"summary"`
	}
	if err := json.Unmarshal(contents, &artifact); err != nil {
		return nil, fmt.Errorf("decode evidence %q: %w", reference, err)
	}
	artifact.Result = normalizeStatus(artifact.Result)
	for index := range artifact.Entries {
		artifact.Entries[index].Status = normalizeStatus(artifact.Entries[index].Status)
	}
	summary := artifact.Summary
	if len(summary) == 0 {
		summary, err = remainingSummary(contents)
		if err != nil {
			return nil, fmt.Errorf("summarize evidence %q: %w", reference, err)
		}
	}
	digest := sha256.Sum256(contents)
	return &acceptancereport.Block{
		CandidateSHA:   artifact.CandidateSHA,
		Source:         filepath.ToSlash(filepath.Clean(reference)),
		ArtifactSHA256: hex.EncodeToString(digest[:]),
		Result:         artifact.Result,
		Entries:        artifact.Entries,
		Exceptions:     artifact.Exceptions,
		Summary:        summary,
	}, nil
}

func remainingSummary(contents []byte) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		return nil, err
	}
	for _, name := range []string{"candidate_sha", "result", "entries", "exceptions"} {
		delete(fields, name)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return json.Marshal(fields)
}

func loadManualReview(base, reference string) (acceptancereport.ManualReview, error) {
	path, err := artifactPath(base, reference)
	if err != nil {
		return acceptancereport.ManualReview{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return acceptancereport.ManualReview{}, fmt.Errorf("open manual review %q: %w", reference, err)
	}
	defer file.Close()
	var review acceptancereport.ManualReview
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return acceptancereport.ManualReview{}, fmt.Errorf("decode manual review %q: %w", reference, err)
	}
	if err := requireEOF(decoder); err != nil {
		return acceptancereport.ManualReview{}, fmt.Errorf("decode manual review %q: %w", reference, err)
	}
	review.Result = normalizeStatus(review.Result)
	return review, nil
}

func artifactPath(base, reference string) (string, error) {
	if filepath.IsAbs(reference) {
		return "", fmt.Errorf("evidence path %q must be relative to the manifest", reference)
	}
	if filepath.Ext(reference) != ".json" {
		return "", fmt.Errorf("evidence path %q must be a .json file", reference)
	}
	return filepath.Join(base, filepath.Clean(reference)), nil
}

func normalizeStatus(status acceptancereport.Status) acceptancereport.Status {
	normalized := strings.ToLower(string(status))
	switch normalized {
	case "pass", "passed":
		return acceptancereport.StatusPass
	case "fail", "failed":
		return acceptancereport.StatusFail
	default:
		return acceptancereport.Status(normalized)
	}
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func isValidationArchive(path string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(absolute)), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == "docs" && parts[index+1] == "validation" {
			return true
		}
	}
	return false
}

var secretValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)postgres(?:ql)?://[^:/\s]+:[^@\s]+@`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
}

func scanSecrets(contents []byte) error {
	var value any
	if err := json.Unmarshal(contents, &value); err != nil {
		return err
	}
	return scanValue(value)
}

func scanValue(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if forbiddenArchiveField(key) {
				return fmt.Errorf("forbidden field %q", key)
			}
			if err := scanValue(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := scanValue(child); err != nil {
				return err
			}
		}
	case string:
		for _, pattern := range secretValuePatterns {
			if pattern.MatchString(typed) {
				return errors.New("secret-like value detected")
			}
		}
	}
	return nil
}

func forbiddenArchiveField(key string) bool {
	name := strings.ToLower(key)
	switch name {
	case "stdout", "stderr", "database_dump", "database_dump_path":
		return true
	}
	for _, suffix := range []string{"_mode", "_owner", "_sha256", "_present", "_redacted", "_path", "_count", "_status"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	for _, part := range strings.FieldsFunc(name, func(character rune) bool {
		return character < 'a' || character > 'z'
	}) {
		switch part {
		case "password", "token", "secret", "credential", "credentials", "dsn":
			return true
		}
	}
	return strings.Contains(name, "private_key") || strings.Contains(name, "database_url")
}

func writeAtomic(path string, contents []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".acceptance-report-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(contents)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
