package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

var errRecoveredAlertDisposition = errors.New("recovered alert instances cannot be disposed")

type dispositionFields struct {
	note               pgtype.Text
	ignoreReasonCode   pgtype.Text
	ignoreReasonDetail pgtype.Text
}

func (handler *Handler) GetAlertDisposition(ctx context.Context, request api.GetAlertDispositionRequestObject) (api.GetAlertDispositionResponseObject, error) {
	var detail api.AlertDispositionDetail
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		alertInstance, err := queries.GetAlertDispositionForRead(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
		if err != nil {
			return err
		}
		detail, err = loadAlertDispositionDetail(ctx, queries, alertInstance)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetAlertDisposition404JSONResponse(errorBody(api.NOTFOUND, "alert instance not found")), nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetAlertDisposition200JSONResponse(detail), nil
}

func (handler *Handler) UpdateAlertDisposition(ctx context.Context, request api.UpdateAlertDispositionRequestObject) (api.UpdateAlertDispositionResponseObject, error) {
	if request.Body == nil {
		return api.UpdateAlertDisposition400JSONResponse(dispositionValidationError([]fieldError{{field: "body", message: "is required"}})), nil
	}
	input := *request.Body
	fields, fieldErrors := validateDisposition(input)
	if len(fieldErrors) > 0 {
		return api.UpdateAlertDisposition400JSONResponse(dispositionValidationError(fieldErrors)), nil
	}

	alertInstanceID := pgtype.UUID{Bytes: request.Id, Valid: true}
	actorID := databaseUserID(authenticatedUserID(ctx))
	actedAt := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	disposition := string(input.Disposition)
	var detail api.AlertDispositionDetail
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		current, err := queries.GetAlertDispositionForUpdate(ctx, alertInstanceID)
		if err != nil {
			return err
		}
		if current.Status == "RECOVERED" {
			return errRecoveredAlertDisposition
		}
		updated, err := queries.UpdateAlertDisposition(ctx, alerting.UpdateAlertDispositionParams{
			ID:                 alertInstanceID,
			Disposition:        disposition,
			DispositionBy:      actorID,
			DispositionAt:      actedAt,
			DispositionNote:    fields.note,
			IgnoreReasonCode:   fields.ignoreReasonCode,
			IgnoreReasonDetail: fields.ignoreReasonDetail,
		})
		if err != nil {
			return err
		}
		if err := queries.CreateAlertDispositionEvent(ctx, alerting.CreateAlertDispositionEventParams{
			AlertInstanceID:    current.ID,
			RuleID:             current.RuleID,
			RuleVersion:        current.RuleVersion,
			Kind:               disposition,
			FromState:          current.Status,
			ToState:            current.Status,
			CurrentValue:       current.CurrentValue,
			Unavailability:     current.Unavailability,
			RuleSnapshot:       current.RuleSnapshot,
			EvaluatedAt:        current.UpdatedAt,
			ActorID:            actorID,
			ActedAt:            actedAt,
			FromDisposition:    pgtype.Text{String: current.Disposition, Valid: true},
			ToDisposition:      pgtype.Text{String: disposition, Valid: true},
			DispositionNote:    fields.note,
			IgnoreReasonCode:   fields.ignoreReasonCode,
			IgnoreReasonDetail: fields.ignoreReasonDetail,
		}); err != nil {
			return err
		}
		detail, err = loadAlertDispositionDetail(ctx, queries, updated)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateAlertDisposition404JSONResponse(errorBody(api.NOTFOUND, "alert instance not found")), nil
	}
	if errors.Is(err, errRecoveredAlertDisposition) {
		return api.UpdateAlertDisposition409JSONResponse(errorBody(api.CONFLICT, err.Error())), nil
	}
	if err != nil {
		return nil, err
	}
	return api.UpdateAlertDisposition200JSONResponse(detail), nil
}

func validateDisposition(input api.AlertDispositionInput) (dispositionFields, []fieldError) {
	fields := dispositionFields{
		note:               trimmedText(input.Note),
		ignoreReasonDetail: trimmedText(input.IgnoreReasonDetail),
	}
	if input.IgnoreReasonCode != nil {
		fields.ignoreReasonCode = pgtype.Text{String: string(*input.IgnoreReasonCode), Valid: true}
	}
	switch input.Disposition {
	case api.AlertDispositionACKED:
		if input.IgnoreReasonCode != nil {
			return fields, []fieldError{{field: "ignore_reason_code", message: "must be omitted when acknowledging"}}
		}
		if input.IgnoreReasonDetail != nil {
			return fields, []fieldError{{field: "ignore_reason_detail", message: "must be omitted when acknowledging"}}
		}
		return fields, nil
	case api.AlertDispositionIGNORED:
		if input.Note != nil {
			return fields, []fieldError{{field: "note", message: "must be omitted when ignoring"}}
		}
		if input.IgnoreReasonCode == nil {
			return fields, []fieldError{{field: "ignore_reason_code", message: "is required when ignoring"}}
		}
		if !validIgnoreReason(*input.IgnoreReasonCode) {
			return fields, []fieldError{{field: "ignore_reason_code", message: "is not supported"}}
		}
		if *input.IgnoreReasonCode == api.OTHER && !fields.ignoreReasonDetail.Valid {
			return fields, []fieldError{{field: "ignore_reason_detail", message: "is required for OTHER"}}
		}
		return fields, nil
	default:
		return fields, []fieldError{{field: "disposition", message: "must be ACKED or IGNORED"}}
	}
}

func validIgnoreReason(reason api.IgnoreReasonCode) bool {
	switch reason {
	case api.KNOWNISSUE, api.FALSEPOSITIVE, api.DUPLICATE, api.IMPACTACCEPTABLE, api.OTHER:
		return true
	default:
		return false
	}
}

func trimmedText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: trimmed, Valid: true}
}

func dispositionValidationError(fieldErrors []fieldError) api.Error {
	return validationErrorBody("alert disposition validation failed", fieldErrors)
}

func loadAlertDispositionDetail(ctx context.Context, queries *alerting.Queries, alertInstance alerting.AlertInstance) (api.AlertDispositionDetail, error) {
	events, err := queries.ListAlertDispositionEvents(ctx, alertInstance.ID)
	if err != nil {
		return api.AlertDispositionDetail{}, err
	}
	history := make([]api.AlertDispositionEvent, 0, len(events))
	for _, event := range events {
		var ruleSnapshot map[string]interface{}
		if err := json.Unmarshal(event.RuleSnapshot, &ruleSnapshot); err != nil {
			return api.AlertDispositionDetail{}, err
		}
		history = append(history, api.AlertDispositionEvent{
			Kind:               api.AlertDispositionEventKind(event.Kind),
			FromDisposition:    api.AlertDisposition(event.FromDisposition.String),
			ToDisposition:      api.AlertDisposition(event.ToDisposition.String),
			ActorId:            event.ActorID.Bytes,
			Note:               textPointer(event.DispositionNote),
			IgnoreReasonCode:   ignoreReasonPointer(event.IgnoreReasonCode),
			IgnoreReasonDetail: textPointer(event.IgnoreReasonDetail),
			RuleVersion:        int(event.RuleVersion),
			CurrentValue:       floatPointer(event.CurrentValue),
			RuleSnapshot:       ruleSnapshot,
			EvaluatedAt:        event.EvaluatedAt.Time,
			ActedAt:            event.ActedAt.Time,
		})
	}
	detail := api.AlertDispositionDetail{
		AlertInstanceId:          alertInstance.ID.Bytes,
		Disposition:              api.AlertDisposition(alertInstance.Disposition),
		DispositionBy:            uuidPointer(alertInstance.DispositionBy),
		DispositionAt:            timePointer(alertInstance.DispositionAt),
		Note:                     textPointer(alertInstance.DispositionNote),
		IgnoreReasonCode:         ignoreReasonPointer(alertInstance.IgnoreReasonCode),
		IgnoreReasonDetail:       textPointer(alertInstance.IgnoreReasonDetail),
		StopsRepeatNotifications: alertInstance.Disposition == string(api.AlertDispositionACKED),
		ExcludedFromHealthRollup: alertInstance.Disposition == string(api.AlertDispositionIGNORED),
		History:                  history,
	}
	return detail, nil
}

func uuidPointer(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := uuid.UUID(value.Bytes)
	return &result
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func ignoreReasonPointer(value pgtype.Text) *api.IgnoreReasonCode {
	if !value.Valid {
		return nil
	}
	result := api.IgnoreReasonCode(value.String)
	return &result
}

func floatPointer(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}
