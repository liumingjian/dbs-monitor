package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestValidateDisposition(t *testing.T) {
	note := "  Investigating  "
	reasonDetail := "  Expected maintenance load  "
	blankReasonDetail := "  "
	knownIssue := api.KNOWNISSUE
	other := api.OTHER
	unsupportedReason := api.IgnoreReasonCode("UNSUPPORTED")

	tests := []struct {
		name       string
		input      api.AlertDispositionInput
		wantFields dispositionFields
		wantField  string
	}{
		{
			name:  "acknowledged with note",
			input: api.AlertDispositionInput{Disposition: api.AlertDispositionACKED, Note: &note},
			wantFields: dispositionFields{
				note: pgtype.Text{String: "Investigating", Valid: true},
			},
		},
		{
			name:  "ignored for known issue",
			input: api.AlertDispositionInput{Disposition: api.AlertDispositionIGNORED, IgnoreReasonCode: &knownIssue},
			wantFields: dispositionFields{
				ignoreReasonCode: pgtype.Text{String: "KNOWN_ISSUE", Valid: true},
			},
		},
		{
			name: "ignored for other reason with detail",
			input: api.AlertDispositionInput{
				Disposition:        api.AlertDispositionIGNORED,
				IgnoreReasonCode:   &other,
				IgnoreReasonDetail: &reasonDetail,
			},
			wantFields: dispositionFields{
				ignoreReasonCode:   pgtype.Text{String: "OTHER", Valid: true},
				ignoreReasonDetail: pgtype.Text{String: "Expected maintenance load", Valid: true},
			},
		},
		{
			name:      "none cannot be submitted",
			input:     api.AlertDispositionInput{Disposition: api.AlertDispositionNONE},
			wantField: "disposition",
		},
		{
			name: "acknowledged omits ignore reason",
			input: api.AlertDispositionInput{
				Disposition:      api.AlertDispositionACKED,
				IgnoreReasonCode: &knownIssue,
			},
			wantField: "ignore_reason_code",
		},
		{
			name: "acknowledged omits ignore detail",
			input: api.AlertDispositionInput{
				Disposition:        api.AlertDispositionACKED,
				IgnoreReasonDetail: &reasonDetail,
			},
			wantField: "ignore_reason_detail",
		},
		{
			name:      "ignored omits note",
			input:     api.AlertDispositionInput{Disposition: api.AlertDispositionIGNORED, Note: &note},
			wantField: "note",
		},
		{
			name:      "ignored requires reason",
			input:     api.AlertDispositionInput{Disposition: api.AlertDispositionIGNORED},
			wantField: "ignore_reason_code",
		},
		{
			name: "ignored rejects unsupported reason",
			input: api.AlertDispositionInput{
				Disposition:      api.AlertDispositionIGNORED,
				IgnoreReasonCode: &unsupportedReason,
			},
			wantField: "ignore_reason_code",
		},
		{
			name: "other reason requires nonblank detail",
			input: api.AlertDispositionInput{
				Disposition:        api.AlertDispositionIGNORED,
				IgnoreReasonCode:   &other,
				IgnoreReasonDetail: &blankReasonDetail,
			},
			wantField: "ignore_reason_detail",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields, fieldErrors := validateDisposition(test.input)
			if test.wantField == "" {
				if len(fieldErrors) != 0 {
					t.Fatalf("validation errors = %+v, want none", fieldErrors)
				}
				if fields != test.wantFields {
					t.Fatalf("validated fields = %+v, want %+v", fields, test.wantFields)
				}
				return
			}
			if len(fieldErrors) != 1 || fieldErrors[0].field != test.wantField {
				t.Fatalf("validation errors = %+v, want field %q", fieldErrors, test.wantField)
			}
		})
	}
}
