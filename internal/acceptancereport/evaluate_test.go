package acceptancereport

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"
)

const testCandidateSHA = "0123456789012345678901234567890123456789"

func TestEvaluateRules(t *testing.T) {
	tests := []struct {
		name            string
		mutate          func(*Input)
		wantVerdict     Verdict
		wantProvisional bool
		wantReason      ReasonCode
	}{
		{
			name: "missing evidence",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.PGRange = nil
			},
			wantVerdict:     VerdictNoGo,
			wantProvisional: true,
			wantReason:      ReasonEvidenceMissing,
		},
		{
			name: "evidence belongs to another candidate",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.RTC.CandidateSHA = "1111111111111111111111111111111111111111"
			},
			wantVerdict:     VerdictNoGo,
			wantProvisional: true,
			wantReason:      ReasonEvidenceSHAMismatch,
		},
		{
			name: "hard gate failed",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.Matrix.Entries[0].Status = StatusFail
			},
			wantVerdict:     VerdictNoGo,
			wantProvisional: true,
			wantReason:      ReasonHardGateFailed,
		},
		{
			name: "unexpected pending entry",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.Matrix.Entries = append(input.Rounds[0].Evidence.Matrix.Entries, Entry{
					ID: "AC-01-S2", Status: StatusPending,
				})
			},
			wantVerdict:     VerdictNoGo,
			wantProvisional: true,
			wantReason:      ReasonUnexpectedMatrixState,
		},
		{
			name: "matrix exception",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.Matrix.Exceptions = []json.RawMessage{json.RawMessage(`{"id":"AC-01-S1"}`)}
			},
			wantVerdict:     VerdictNoGo,
			wantProvisional: true,
			wantReason:      ReasonUnexpectedMatrixState,
		},
		{
			name: "GitHub Actions seals GO",
			mutate: func(input *Input) {
				*input = passingFinalInput()
				input.GitHubActions = true
			},
			wantVerdict:     VerdictProvisionalPass,
			wantProvisional: true,
			wantReason:      ReasonGitHubActions,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := passingCIInput()
			test.mutate(&input)

			decision := Evaluate(input)

			if decision.Verdict != test.wantVerdict {
				t.Fatalf("verdict = %q, want %q (reasons: %v)", decision.Verdict, test.wantVerdict, decision.ReasonCodes)
			}
			if decision.Provisional != test.wantProvisional {
				t.Errorf("provisional = %v, want %v", decision.Provisional, test.wantProvisional)
			}
			if !slices.Contains(decision.ReasonCodes, test.wantReason) {
				t.Errorf("reason codes = %v, want %q", decision.ReasonCodes, test.wantReason)
			}
		})
	}
}

func TestEvaluatePGRangeRequiresEveryEntryPass(t *testing.T) {
	for _, status := range []Status{StatusFail, StatusNA, StatusPending} {
		t.Run(string(status), func(t *testing.T) {
			input := passingCIInput()
			input.Rounds[0].Evidence.PGRange.Entries = append(input.Rounds[0].Evidence.PGRange.Entries, Entry{
				ID: "pg17/stat_statements", Status: status,
			})

			decision := Evaluate(input)

			if decision.Verdict != VerdictNoGo || !slices.Contains(decision.ReasonCodes, ReasonHardGateFailed) {
				t.Fatalf("decision = %+v, want NO-GO with %s", decision, ReasonHardGateFailed)
			}
		})
	}
}

func TestEvaluateFinalRoundsStartAtOne(t *testing.T) {
	input := passingFinalInput()
	input.Rounds[0].Sequence = 2
	input.Rounds[1].Sequence = 3

	decision := Evaluate(input)

	if decision.Verdict != VerdictNoGo || !slices.Contains(decision.ReasonCodes, ReasonRoundsInvalid) {
		t.Fatalf("decision = %+v, want NO-GO with %s", decision, ReasonRoundsInvalid)
	}
}

func TestEvaluateFinalRoundMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Round)
	}{
		{name: "environment missing", mutate: func(round *Round) { round.Environment = nil }},
		{name: "start missing", mutate: func(round *Round) { round.StartedAt = time.Time{} }},
		{name: "finish missing", mutate: func(round *Round) { round.FinishedAt = time.Time{} }},
		{name: "finish before start", mutate: func(round *Round) { round.FinishedAt = round.StartedAt.Add(-time.Second) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := passingFinalInput()
			test.mutate(&input.Rounds[0])
			decision := Evaluate(input)
			if decision.Verdict != VerdictNoGo || !slices.Contains(decision.ReasonCodes, ReasonRoundsInvalid) {
				t.Fatalf("decision = %+v, want NO-GO with %s", decision, ReasonRoundsInvalid)
			}
		})
	}
}

func TestEvaluateRejectsTruncatedMatrixEvidence(t *testing.T) {
	input := passingCIInput()
	input.Rounds[0].Evidence.Matrix.Entries = input.Rounds[0].Evidence.Matrix.Entries[:1]

	decision := Evaluate(input)

	if decision.Verdict != VerdictNoGo || !slices.Contains(decision.ReasonCodes, ReasonEvidenceMissing) {
		t.Fatalf("decision = %+v, want NO-GO with %s", decision, ReasonEvidenceMissing)
	}
}

func TestEvaluateRejectsEmptyHardGateEvidence(t *testing.T) {
	tests := []struct {
		name  string
		block func(*Evidence) **Block
	}{
		{name: "package", block: func(evidence *Evidence) **Block { return &evidence.Package }},
		{name: "check full", block: func(evidence *Evidence) **Block { return &evidence.CheckFull }},
		{name: "PG range", block: func(evidence *Evidence) **Block { return &evidence.PGRange }},
		{name: "Linux native", block: func(evidence *Evidence) **Block { return &evidence.LinuxNative }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := passingFinalInput()
			*test.block(&input.Rounds[0].Evidence) = &Block{CandidateSHA: testCandidateSHA}
			decision := Evaluate(input)
			if decision.Verdict != VerdictNoGo || !slices.Contains(decision.ReasonCodes, ReasonHardGateFailed) {
				t.Fatalf("decision = %+v, want NO-GO with %s", decision, ReasonHardGateFailed)
			}
		})
	}
}

func TestEvaluateFinalConditions(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Input)
		wantVerdict Verdict
		wantReason  ReasonCode
	}{
		{name: "complete report", mutate: func(*Input) {}, wantVerdict: VerdictGo},
		{
			name: "RT-C breach is recorded without a threshold verdict",
			mutate: func(input *Input) {
				for index := range input.Rounds {
					input.Rounds[index].Evidence.RTC.Result = StatusFail
				}
			},
			wantVerdict: VerdictGo,
		},
		{
			name: "RT-C data missing",
			mutate: func(input *Input) {
				input.Rounds[0].Evidence.RTC.Summary = nil
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonRTCDataMissing,
		},
		{
			name: "manual operator missing",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.Operator = ""
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualResultMissing,
		},
		{
			name: "manual time missing",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.At = time.Time{}
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualResultMissing,
		},
		{
			name: "manual result missing",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.Result = ""
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualResultMissing,
		},
		{
			name: "manual screenshot index missing",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.ScreenshotIndex = nil
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualResultMissing,
		},
		{
			name: "manual residual risks field missing",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.ResidualRisks = nil
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualResultMissing,
		},
		{
			name: "manual gate failed",
			mutate: func(input *Input) {
				input.Rounds[0].ManualReview.Result = StatusFail
			},
			wantVerdict: VerdictNoGo,
			wantReason:  ReasonManualGateFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := passingFinalInput()
			test.mutate(&input)
			decision := Evaluate(input)
			if decision.Verdict != test.wantVerdict {
				t.Fatalf("decision = %+v, want verdict %s", decision, test.wantVerdict)
			}
			if test.wantReason != "" && !slices.Contains(decision.ReasonCodes, test.wantReason) {
				t.Errorf("reason codes = %v, want %s", decision.ReasonCodes, test.wantReason)
			}
		})
	}
}

func passingCIInput() Input {
	return Input{
		CandidateSHA: testCandidateSHA,
		Profile:      ProfileCI,
		Rounds: []Round{{
			Sequence:     1,
			Valid:        true,
			CandidateSHA: testCandidateSHA,
			Evidence: Evidence{
				Matrix: &Block{
					CandidateSHA: testCandidateSHA,
					Entries:      passingMatrixEntries(),
				},
				PGRange: &Block{
					CandidateSHA: testCandidateSHA,
					Entries:      []Entry{{ID: "pg13/stat_database", Baseline: true, Status: StatusPass}},
				},
				RTC: &Block{
					CandidateSHA: testCandidateSHA,
					Summary:      json.RawMessage(`{"samples":100}`),
				},
			},
		}},
	}
}

func passingMatrixEntries() []Entry {
	entries := []Entry{
		{ID: "AC-01-S1", Baseline: true, Status: StatusPass},
		{ID: "AC-03-F2", Baseline: true, Status: StatusNA},
		{ID: "AC-04-F2", Baseline: true, Status: StatusNA},
		{ID: "AC-05-S4", Status: StatusNA},
		{ID: "AC-08-F2", Baseline: true, Status: StatusNA},
		{ID: "AC-09-F2", Baseline: true, Status: StatusNA},
		{ID: "AC-03-F6", Status: StatusPending},
		{ID: "AC-08-S8", Status: StatusPending},
	}
	for index := len(entries); index < 104; index++ {
		entries = append(entries, Entry{ID: "PASS-" + strconv.Itoa(index), Baseline: true, Status: StatusPass})
	}
	return entries
}

func passingFinalInput() Input {
	newRound := func(sequence int, startedAt, finishedAt time.Time) Round {
		ci := passingCIInput().Rounds[0]
		ci.Sequence = sequence
		ci.StartedAt = startedAt
		ci.FinishedAt = finishedAt
		ci.Environment = json.RawMessage(`{"os":"Kylin V10","arch":"amd64"}`)
		ci.Evidence.Package = &Block{CandidateSHA: testCandidateSHA, Result: StatusPass}
		ci.Evidence.CheckFull = &Block{CandidateSHA: testCandidateSHA, Result: StatusPass}
		ci.Evidence.LinuxNative = &Block{CandidateSHA: testCandidateSHA, Result: StatusPass}
		ci.ManualReview = &ManualReview{
			Operator: "release-operator", At: startedAt, Result: StatusPass,
			ScreenshotIndex: []string{"screenshots/ac-05-s4.png"}, ResidualRisks: []string{},
		}
		return ci
	}
	firstStart := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	firstFinish := firstStart.Add(time.Hour)
	secondStart := firstFinish.Add(time.Hour)
	return Input{
		CandidateSHA: testCandidateSHA,
		Profile:      ProfileFinal,
		Rounds: []Round{
			newRound(1, firstStart, firstFinish),
			newRound(2, secondStart, secondStart.Add(time.Hour)),
		},
	}
}
