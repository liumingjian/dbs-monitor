package acceptancereport

import (
	"encoding/json"
	"strings"
	"time"
)

type Verdict string

const (
	VerdictGo              Verdict = "GO"
	VerdictNoGo            Verdict = "NO-GO"
	VerdictProvisionalPass Verdict = "PROVISIONAL-PASS"
)

type Profile string

const (
	ProfileCI    Profile = "ci"
	ProfileFinal Profile = "final"
)

type Status string

const (
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusPending Status = "pending"
	StatusNA      Status = "n-a"
)

type ReasonCode string

const (
	ReasonEvidenceMissing       ReasonCode = "EVIDENCE_MISSING"
	ReasonEvidenceSHAMismatch   ReasonCode = "EVIDENCE_SHA_MISMATCH"
	ReasonHardGateFailed        ReasonCode = "HARD_GATE_FAILED"
	ReasonUnexpectedMatrixState ReasonCode = "UNEXPECTED_MATRIX_STATE"
	ReasonGitHubActions         ReasonCode = "GITHUB_ACTIONS_PROVISIONAL"
	ReasonRoundsInvalid         ReasonCode = "ROUNDS_INVALID"
	ReasonRTCDataMissing        ReasonCode = "RT_C_DATA_MISSING"
	ReasonManualResultMissing   ReasonCode = "MANUAL_RESULT_INCOMPLETE"
	ReasonManualGateFailed      ReasonCode = "MANUAL_GATE_FAILED"
)

type Input struct {
	CandidateSHA  string
	Profile       Profile
	GitHubActions bool
	Rounds        []Round
}

type Round struct {
	Sequence     int             `json:"sequence"`
	CandidateSHA string          `json:"candidate_sha"`
	Valid        bool            `json:"valid"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   time.Time       `json:"finished_at"`
	Environment  json.RawMessage `json:"environment"`
	Evidence     Evidence        `json:"evidence"`
	ManualReview *ManualReview   `json:"manual_review,omitempty"`
}

type Evidence struct {
	Package     *Block `json:"package,omitempty"`
	CheckFull   *Block `json:"check_full,omitempty"`
	Matrix      *Block `json:"matrix,omitempty"`
	PGRange     *Block `json:"pg_range,omitempty"`
	RTC         *Block `json:"rt_c,omitempty"`
	LinuxNative *Block `json:"linux_native,omitempty"`
}

type Block struct {
	CandidateSHA   string            `json:"candidate_sha"`
	Source         string            `json:"source"`
	ArtifactSHA256 string            `json:"artifact_sha256"`
	Result         Status            `json:"result,omitempty"`
	Entries        []Entry           `json:"entries,omitempty"`
	Exceptions     []json.RawMessage `json:"exceptions"`
	Summary        json.RawMessage   `json:"summary,omitempty"`
}

type Entry struct {
	ID           string  `json:"id"`
	Baseline     bool    `json:"baseline"`
	Status       Status  `json:"status"`
	TestRef      *string `json:"test_ref"`
	ActualResult string  `json:"actual_result,omitempty"`
	DurationMS   int64   `json:"duration_ms"`
}

type ManualReview struct {
	Operator        string    `json:"operator"`
	At              time.Time `json:"at"`
	Result          Status    `json:"result"`
	ScreenshotIndex []string  `json:"screenshot_index"`
	ResidualRisks   []string  `json:"residual_risks"`
}

type Decision struct {
	Verdict     Verdict      `json:"verdict"`
	Provisional bool         `json:"provisional"`
	ReasonCodes []ReasonCode `json:"reason_codes"`
}

type Report struct {
	CandidateSHA string          `json:"candidate_sha"`
	Version      string          `json:"version"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Environment  json.RawMessage `json:"environment"`
	Profile      Profile         `json:"profile"`
	Rounds       []Round         `json:"rounds"`
	Verdict      Verdict         `json:"verdict"`
	Provisional  bool            `json:"provisional"`
	ReasonCodes  []ReasonCode    `json:"reason_codes"`
}

var allowedNA = map[string]bool{
	"AC-03-F2": true,
	"AC-04-F2": true,
	"AC-05-S4": true,
	"AC-08-F2": true,
	"AC-09-F2": true,
}

var allowedPending = map[string]bool{
	"AC-03-F6": true,
	"AC-08-S8": true,
}

// Evaluate derives the only verdict from already-loaded evidence.
func Evaluate(input Input) Decision {
	provisional := input.Profile == ProfileCI || input.GitHubActions
	reasons := reasonSet{}
	if input.Profile == ProfileFinal {
		evaluateFinalRounds(input, reasons)
	} else if len(input.Rounds) == 0 {
		reasons.add(ReasonRoundsInvalid)
	}

	for _, round := range input.Rounds {
		if !round.Valid {
			reasons.add(ReasonRoundsInvalid)
		}
		if round.CandidateSHA != input.CandidateSHA {
			reasons.add(ReasonEvidenceSHAMismatch)
		}
		evaluateEvidence(input.Profile, input.CandidateSHA, round.Evidence, reasons)
		if input.Profile == ProfileFinal {
			evaluateManualReview(round.ManualReview, reasons)
		}
	}
	if input.GitHubActions {
		reasons.add(ReasonGitHubActions)
	}

	failure := reasons.hasFailure()
	verdict := VerdictGo
	if failure {
		verdict = VerdictNoGo
	} else if provisional {
		verdict = VerdictProvisionalPass
	}
	return Decision{Verdict: verdict, Provisional: provisional, ReasonCodes: reasons.list()}
}

func evaluateFinalRounds(input Input, reasons reasonSet) {
	if len(input.Rounds) != 2 || input.Rounds[0].Sequence != 1 || input.Rounds[1].Sequence != 2 {
		reasons.add(ReasonRoundsInvalid)
		return
	}
	first, second := input.Rounds[0], input.Rounds[1]
	for _, round := range input.Rounds {
		if !hasJSONData(round.Environment) || round.StartedAt.IsZero() || round.FinishedAt.IsZero() || !round.FinishedAt.After(round.StartedAt) {
			reasons.add(ReasonRoundsInvalid)
		}
	}
	if !first.FinishedAt.IsZero() && !second.StartedAt.IsZero() && second.StartedAt.Before(first.FinishedAt) {
		reasons.add(ReasonRoundsInvalid)
	}
}

func evaluateEvidence(profile Profile, candidateSHA string, evidence Evidence, reasons reasonSet) {
	required := []*Block{evidence.Matrix, evidence.PGRange, evidence.RTC}
	if profile == ProfileFinal {
		required = []*Block{evidence.Package, evidence.CheckFull, evidence.Matrix, evidence.PGRange, evidence.RTC, evidence.LinuxNative}
	}
	for _, block := range required {
		if block == nil {
			reasons.add(ReasonEvidenceMissing)
			continue
		}
		if block.CandidateSHA != candidateSHA {
			reasons.add(ReasonEvidenceSHAMismatch)
		}
	}

	for _, block := range []*Block{evidence.Package, evidence.CheckFull, evidence.Matrix, evidence.PGRange, evidence.LinuxNative} {
		if block == nil {
			continue
		}
		if block.Result != "" && normalizeStatus(block.Result) != StatusPass {
			reasons.add(ReasonHardGateFailed)
		}
		if block != evidence.Matrix && block.Result == "" && len(block.Entries) == 0 {
			reasons.add(ReasonHardGateFailed)
		}
		for _, entry := range block.Entries {
			status := normalizeStatus(entry.Status)
			matrixNA := block == evidence.Matrix && status == StatusNA && allowedNA[entry.ID]
			required := block != evidence.Matrix || entry.Baseline
			if required && status != StatusPass && !matrixNA {
				reasons.add(ReasonHardGateFailed)
			}
		}
	}

	if evidence.Matrix != nil {
		if !completeMatrix(evidence.Matrix.Entries) {
			reasons.add(ReasonEvidenceMissing)
		}
		if len(evidence.Matrix.Exceptions) > 0 {
			reasons.add(ReasonUnexpectedMatrixState)
		}
		for _, entry := range evidence.Matrix.Entries {
			switch normalizeStatus(entry.Status) {
			case StatusPending:
				if !allowedPending[entry.ID] || entry.Baseline {
					reasons.add(ReasonUnexpectedMatrixState)
				}
			case StatusNA:
				if !allowedNA[entry.ID] {
					reasons.add(ReasonUnexpectedMatrixState)
				}
			}
		}
	}
	if evidence.RTC != nil && !hasJSONData(evidence.RTC.Summary) {
		reasons.add(ReasonRTCDataMissing)
	}
}

func completeMatrix(entries []Entry) bool {
	if len(entries) != 104 {
		return false
	}
	baselineCount := 0
	ids := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.ID == "" || ids[entry.ID] {
			return false
		}
		ids[entry.ID] = true
		if entry.Baseline {
			baselineCount++
		}
	}
	return baselineCount == 101
}

func hasJSONData(contents json.RawMessage) bool {
	if len(contents) == 0 {
		return false
	}
	var value any
	if json.Unmarshal(contents, &value) != nil || value == nil {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return true
	}
}

func evaluateManualReview(review *ManualReview, reasons reasonSet) {
	if review == nil || strings.TrimSpace(review.Operator) == "" || review.At.IsZero() || review.Result == "" ||
		len(review.ScreenshotIndex) == 0 || review.ResidualRisks == nil {
		reasons.add(ReasonManualResultMissing)
		return
	}
	if normalizeStatus(review.Result) != StatusPass {
		reasons.add(ReasonManualGateFailed)
	}
}

func normalizeStatus(status Status) Status {
	switch strings.ToLower(string(status)) {
	case "pass", "passed":
		return StatusPass
	case "fail", "failed":
		return StatusFail
	default:
		return Status(strings.ToLower(string(status)))
	}
}

type reasonSet map[ReasonCode]bool

func (reasons reasonSet) add(reason ReasonCode) {
	reasons[reason] = true
}

func (reasons reasonSet) hasFailure() bool {
	for reason := range reasons {
		if reason != ReasonGitHubActions {
			return true
		}
	}
	return false
}

func (reasons reasonSet) list() []ReasonCode {
	order := []ReasonCode{
		ReasonEvidenceMissing,
		ReasonEvidenceSHAMismatch,
		ReasonHardGateFailed,
		ReasonUnexpectedMatrixState,
		ReasonRoundsInvalid,
		ReasonRTCDataMissing,
		ReasonManualResultMissing,
		ReasonManualGateFailed,
		ReasonGitHubActions,
	}
	result := make([]ReasonCode, 0, len(reasons))
	for _, reason := range order {
		if reasons[reason] {
			result = append(result, reason)
		}
	}
	return result
}
