package httpapi

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

func (handler *Handler) GetSMTPChannel(ctx context.Context, _ api.GetSMTPChannelRequestObject) (api.GetSMTPChannelResponseObject, error) {
	row, err := notify.New(handler.platform).GetSMTPChannel(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetSMTPChannel200JSONResponse{Configured: false, AuthConfigured: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetSMTPChannel200JSONResponse(toAPISMTPChannel(row)), nil
}

func (handler *Handler) UpdateSMTPChannel(ctx context.Context, request api.UpdateSMTPChannelRequestObject) (api.UpdateSMTPChannelResponseObject, error) {
	if request.Body == nil {
		return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "SMTP configuration is required")), nil
	}
	input := request.Body
	host := strings.TrimSpace(input.Host)
	if !validEmail(string(input.FromAddress)) || !validEmail(string(input.Recipient)) || host == "" || input.Port < 1 || input.Port > 65535 {
		return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "host and valid email addresses are required")), nil
	}
	if input.AuthType != api.SMTPAuthNone && input.AuthType != api.SMTPAuthPlain && input.AuthType != api.SMTPAuthLogin {
		return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "unsupported SMTP authentication type")), nil
	}
	if input.TlsMode != api.SMTPStartTLS && input.TlsMode != api.SMTPImplicitTLS {
		return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "unsupported SMTP transport security")), nil
	}

	var username pgtype.Text
	var authCiphertext []byte
	var authKeyVersion pgtype.Int4
	queries := notify.New(handler.platform)
	if input.AuthType != api.SMTPAuthNone {
		if input.Username == nil || strings.TrimSpace(*input.Username) == "" {
			return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "username is required for SMTP authentication")), nil
		}
		username = pgtype.Text{String: strings.TrimSpace(*input.Username), Valid: true}
		if input.Password != nil && *input.Password != "" {
			var version int32
			var err error
			authCiphertext, version, err = handler.keyring.EncryptSMTPPassword(*input.Password)
			if err != nil {
				return nil, err
			}
			authKeyVersion = pgtype.Int4{Int32: version, Valid: true}
		} else {
			existingChannel, err := queries.GetSMTPChannel(ctx)
			if err != nil || !existingChannel.AuthKeyVersion.Valid || len(existingChannel.AuthCiphertext) == 0 {
				return api.UpdateSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "authentication value is required until it has been configured")), nil
			}
			authCiphertext = existingChannel.AuthCiphertext
			authKeyVersion = existingChannel.AuthKeyVersion
		}
	}

	row, err := queries.UpsertSMTPChannel(ctx, notify.UpsertSMTPChannelParams{
		Enabled:        input.Enabled,
		Host:           host,
		Port:           int32(input.Port),
		FromAddress:    string(input.FromAddress),
		Recipient:      string(input.Recipient),
		AuthType:       string(input.AuthType),
		Username:       username,
		AuthCiphertext: authCiphertext,
		AuthKeyVersion: authKeyVersion,
		TlsMode:        string(input.TlsMode),
		UpdatedAt:      pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return api.UpdateSMTPChannel200JSONResponse(toAPISMTPChannel(row)), nil
}

func (handler *Handler) TestSMTPChannel(ctx context.Context, request api.TestSMTPChannelRequestObject) (api.TestSMTPChannelResponseObject, error) {
	if request.Body == nil || !validEmail(string(request.Body.Target)) {
		return api.TestSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid test target is required")), nil
	}
	queries := notify.New(handler.platform)
	channel, err := queries.GetSMTPChannel(ctx)
	if err != nil || !channel.Enabled {
		return api.TestSMTPChannel400JSONResponse(errorBody(api.VALIDATIONFAILED, "SMTP channel is not enabled")), nil
	}
	id, err := queries.EnqueueTestNotification(ctx, notify.EnqueueTestNotificationParams{
		Target:        string(request.Body.Target),
		NextAttemptAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return api.TestSMTPChannel202JSONResponse{Id: uuid.UUID(id.Bytes)}, nil
}

func (handler *Handler) ListAlertNotifications(ctx context.Context, request api.ListAlertNotificationsRequestObject) (api.ListAlertNotificationsResponseObject, error) {
	rows, err := notify.New(handler.platform).ListAlertNotificationAttempts(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	response := make(api.ListAlertNotifications200JSONResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, toAPINotificationAttempt(row))
	}
	return response, nil
}

func toAPINotificationAttempt(row notify.ListAlertNotificationAttemptsRow) api.NotificationAttempt {
	item := api.NotificationAttempt{
		Id:           uuid.UUID(row.ID.Bytes),
		EventType:    api.NotificationAttemptEventType(row.EventType),
		Channel:      api.NotificationAttemptChannel(row.Channel),
		Target:       row.Target,
		Status:       api.NotificationAttemptStatus(row.Status),
		AttemptCount: int(row.AttemptCount),
		CreatedAt:    row.CreatedAt.Time,
	}
	if row.TemplateID.Valid {
		item.TemplateId = &row.TemplateID.String
	}
	if row.CompletedAt.Valid {
		item.CompletedAt = &row.CompletedAt.Time
	}
	if row.AttemptedAt.Valid {
		item.AttemptedAt = &row.AttemptedAt.Time
	}
	if row.Result.Valid {
		value := api.NotificationAttemptResult(row.Result.String)
		item.Result = &value
	}
	if row.FailureReason.Valid {
		item.FailureReason = &row.FailureReason.String
	}
	if row.RetryCount.Valid {
		value := int(row.RetryCount.Int32)
		item.RetryCount = &value
	}
	return item
}

func toAPISMTPChannel(row notify.SmtpChannel) api.SMTPChannel {
	enabled := row.Enabled
	host := row.Host
	port := int(row.Port)
	fromAddress := openapi_types.Email(row.FromAddress)
	recipient := openapi_types.Email(row.Recipient)
	authType := api.SMTPAuthType(row.AuthType)
	tlsMode := api.SMTPTransportSecurity(row.TlsMode)
	response := api.SMTPChannel{
		Configured:     true,
		Enabled:        &enabled,
		Host:           &host,
		Port:           &port,
		FromAddress:    &fromAddress,
		Recipient:      &recipient,
		AuthType:       &authType,
		AuthConfigured: len(row.AuthCiphertext) > 0,
		TlsMode:        &tlsMode,
		UpdatedAt:      &row.UpdatedAt.Time,
	}
	if row.Username.Valid {
		response.Username = &row.Username.String
	}
	return response
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}
