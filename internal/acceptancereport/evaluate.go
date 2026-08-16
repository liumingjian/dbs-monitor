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

const (
	matrixEntryCount    = 104
	matrixBaselineCount = 101
)

var allowedMatrixNA = map[string]struct{}{
	"AC-03-F2": {},
	"AC-04-F2": {},
	"AC-05-S4": {},
	"AC-08-F2": {},
	"AC-09-F2": {},
}

var allowedMatrixPending = map[string]struct{}{
	"AC-03-F6": {},
	"AC-08-S8": {},
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

	verdict := VerdictGo
	if reasons.hasFailure() {
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
		timingValid := !round.StartedAt.IsZero() && !round.FinishedAt.IsZero() && round.FinishedAt.After(round.StartedAt)
		if !hasJSONData(round.Environment) || !timingValid {
			reasons.add(ReasonRoundsInvalid)
		}
	}
	if !first.FinishedAt.IsZero() && !second.StartedAt.IsZero() && second.StartedAt.Before(first.FinishedAt) {
		reasons.add(ReasonRoundsInvalid)
	}
}

func evaluateEvidence(profile Profile, candidateSHA string, evidence Evidence, reasons reasonSet) {
	requiredEvidence := []*Block{evidence.Matrix, evidence.PGRange, evidence.RTC}
	if profile == ProfileFinal {
		requiredEvidence = []*Block{evidence.Package, evidence.CheckFull, evidence.Matrix, evidence.PGRange, evidence.RTC, evidence.LinuxNative}
	}
	for _, block := range requiredEvidence {
		if block == nil {
			reasons.add(ReasonEvidenceMissing)
			continue
		}
		if block.CandidateSHA != candidateSHA {
			reasons.add(ReasonEvidenceSHAMismatch)
		}
	}

	hardGateEvidence := []*Block{evidence.Package, evidence.CheckFull, evidence.Matrix, evidence.PGRange, evidence.LinuxNative}
	for _, block := range hardGateEvidence {
		if block == nil {
			continue
		}
		isMatrix := block == evidence.Matrix
		if block.Result != "" && normalizeStatus(block.Result) != StatusPass {
			reasons.add(ReasonHardGateFailed)
		}
		if !isMatrix && block.Result == "" && len(block.Entries) == 0 {
			reasons.add(ReasonHardGateFailed)
		}
		for _, entry := range block.Entries {
			status := normalizeStatus(entry.Status)
			_, allowsNA := allowedMatrixNA[entry.ID]
			isAllowedMatrixNA := isMatrix && status == StatusNA && allowsNA
			isRequired := !isMatrix || entry.Baseline
			if isRequired && status != StatusPass && !isAllowedMatrixNA {
				reasons.add(ReasonHardGateFailed)
			}
		}
	}

	evaluateMatrix(evidence.Matrix, reasons)
	if evidence.RTC != nil && !hasJSONData(evidence.RTC.Summary) {
		reasons.add(ReasonRTCDataMissing)
	}
}

func evaluateMatrix(matrix *Block, reasons reasonSet) {
	if matrix == nil {
		return
	}
	if !matrixIsComplete(matrix.Entries) {
		reasons.add(ReasonEvidenceMissing)
	}
	if len(matrix.Exceptions) > 0 {
		reasons.add(ReasonUnexpectedMatrixState)
	}
	for _, entry := range matrix.Entries {
		switch normalizeStatus(entry.Status) {
		case StatusPending:
			_, allowed := allowedMatrixPending[entry.ID]
			if !allowed || entry.Baseline {
				reasons.add(ReasonUnexpectedMatrixState)
			}
		case StatusNA:
			if _, allowed := allowedMatrixNA[entry.ID]; !allowed {
				reasons.add(ReasonUnexpectedMatrixState)
			}
		}
	}
}

func matrixIsComplete(entries []Entry) bool {
	if len(entries) != matrixEntryCount {
		return false
	}
	baselineCount := 0
	ids := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return false
		}
		if _, duplicate := ids[entry.ID]; duplicate {
			return false
		}
		ids[entry.ID] = struct{}{}
		if entry.Baseline {
			baselineCount++
		}
	}
	return baselineCount == matrixBaselineCount
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
	if review == nil {
		reasons.add(ReasonManualResultMissing)
		return
	}
	complete := strings.TrimSpace(review.Operator) != "" && !review.At.IsZero() && review.Result != "" &&
		len(review.ScreenshotIndex) > 0 && review.ResidualRisks != nil
	if !complete {
		reasons.add(ReasonManualResultMissing)
		return
	}
	if normalizeStatus(review.Result) != StatusPass {
		reasons.add(ReasonManualGateFailed)
	}
}

func normalizeStatus(status Status) Status {
	normalized := strings.ToLower(string(status))
	switch normalized {
	case "pass", "passed":
		return StatusPass
	case "fail", "failed":
		return StatusFail
	default:
		return Status(normalized)
	}
}

type reasonSet map[ReasonCode]struct{}

func (reasons reasonSet) add(reason ReasonCode) {
	reasons[reason] = struct{}{}
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
	order := [...]ReasonCode{
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
		if _, exists := reasons[reason]; exists {
			result = append(result, reason)
		}
	}
	return result
}
