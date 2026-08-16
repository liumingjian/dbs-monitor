package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

func (handler *Handler) SetNotificationSnapshotStore(store *notify.ChannelSnapshotStore) {
	handler.notificationSnapshotStore = store
}

func (handler *Handler) refreshNotificationSnapshot(ctx context.Context) error {
	if handler.notificationSnapshotStore == nil {
		return nil
	}
	if err := handler.notificationSnapshotStore.Sync(ctx, handler.platform); err != nil {
		if errors.Is(err, notify.ErrLocalLargeWriteRejected) {
			return nil
		}
		return fmt.Errorf("refresh notification channel snapshot: %w", err)
	}
	return nil
}

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
		UpdatedBy:      databaseUserID(authenticatedUserID(ctx)),
	})
	if err != nil {
		return nil, err
	}
	if err := handler.refreshNotificationSnapshot(ctx); err != nil {
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

func (handler *Handler) ListWebhookTargets(ctx context.Context, _ api.ListWebhookTargetsRequestObject) (api.ListWebhookTargetsResponseObject, error) {
	rows, err := notify.New(handler.platform).ListWebhookTargets(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListWebhookTargets200JSONResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, toAPIWebhookTarget(row))
	}
	return response, nil
}

func (handler *Handler) CreateWebhookTarget(ctx context.Context, request api.CreateWebhookTargetRequestObject) (api.CreateWebhookTargetResponseObject, error) {
	if request.Body == nil || request.Body.SigningValue == nil || request.Body.SignatureHeader == nil {
		return api.CreateWebhookTarget400JSONResponse(errorBody(api.VALIDATIONFAILED, "Webhook target and signing configuration are required")), nil
	}
	values, ok := validWebhookInput(*request.Body)
	if !ok {
		return api.CreateWebhookTarget400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid name, HTTP(S) URL, and signing configuration are required")), nil
	}
	targetID := uuid.New()
	signingValueCiphertext, signingKeyVersion, err := handler.keyring.EncryptWebhookSigningValue(targetID, values.signingValue)
	if err != nil {
		return nil, err
	}
	signatureHeaderCiphertext, _, err := handler.keyring.EncryptWebhookSignatureHeader(targetID, values.signatureHeader)
	if err != nil {
		return nil, err
	}
	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	row, err := notify.New(handler.platform).CreateWebhookTarget(ctx, notify.CreateWebhookTargetParams{
		ID:                        pgtype.UUID{Bytes: targetID, Valid: true},
		Name:                      values.name,
		Enabled:                   request.Body.Enabled,
		Url:                       values.targetURL,
		SigningValueCiphertext:    signingValueCiphertext,
		SignatureHeaderCiphertext: signatureHeaderCiphertext,
		SigningKeyVersion:         signingKeyVersion,
		CreatedAt:                 now,
		UpdatedBy:                 databaseUserID(authenticatedUserID(ctx)),
	})
	if err != nil {
		return nil, err
	}
	if err := handler.refreshNotificationSnapshot(ctx); err != nil {
		return nil, err
	}
	return api.CreateWebhookTarget201JSONResponse(toAPIWebhookTarget(row)), nil
}

func (handler *Handler) UpdateWebhookTarget(ctx context.Context, request api.UpdateWebhookTargetRequestObject) (api.UpdateWebhookTargetResponseObject, error) {
	if request.Body == nil {
		return api.UpdateWebhookTarget400JSONResponse(errorBody(api.VALIDATIONFAILED, "Webhook target is required")), nil
	}
	values, ok := validWebhookInput(*request.Body)
	if !ok {
		return api.UpdateWebhookTarget400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid name, HTTP(S) URL, and signing configuration are required")), nil
	}
	targetID := uuid.UUID(request.Id)
	queries := notify.New(handler.platform)
	existing, err := queries.GetWebhookTarget(ctx, pgtype.UUID{Bytes: targetID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateWebhookTarget404JSONResponse(errorBody(api.NOTFOUND, "Webhook target not found")), nil
	}
	if err != nil {
		return nil, err
	}
	signingValueCiphertext := existing.SigningValueCiphertext
	signatureHeaderCiphertext := existing.SignatureHeaderCiphertext
	signingKeyVersion := existing.SigningKeyVersion
	if request.Body.SigningValue != nil || request.Body.SignatureHeader != nil {
		if request.Body.SigningValue == nil {
			values.signingValue, err = handler.keyring.DecryptWebhookSigningValue(targetID, signingValueCiphertext, signingKeyVersion)
			if err != nil {
				return nil, err
			}
		}
		if request.Body.SignatureHeader == nil {
			values.signatureHeader, err = handler.keyring.DecryptWebhookSignatureHeader(targetID, signatureHeaderCiphertext, signingKeyVersion)
			if err != nil {
				return nil, err
			}
		}
		signingValueCiphertext, signingKeyVersion, err = handler.keyring.EncryptWebhookSigningValue(targetID, values.signingValue)
		if err != nil {
			return nil, err
		}
		signatureHeaderCiphertext, _, err = handler.keyring.EncryptWebhookSignatureHeader(targetID, values.signatureHeader)
		if err != nil {
			return nil, err
		}
	}
	row, err := queries.UpdateWebhookTarget(ctx, notify.UpdateWebhookTargetParams{
		ID:                        existing.ID,
		Name:                      values.name,
		Enabled:                   request.Body.Enabled,
		Url:                       values.targetURL,
		SigningValueCiphertext:    signingValueCiphertext,
		SignatureHeaderCiphertext: signatureHeaderCiphertext,
		SigningKeyVersion:         signingKeyVersion,
		UpdatedAt:                 pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
		UpdatedBy:                 databaseUserID(authenticatedUserID(ctx)),
	})
	if err != nil {
		return nil, err
	}
	if err := handler.refreshNotificationSnapshot(ctx); err != nil {
		return nil, err
	}
	return api.UpdateWebhookTarget200JSONResponse(toAPIWebhookTarget(row)), nil
}

func (handler *Handler) DeleteWebhookTarget(ctx context.Context, request api.DeleteWebhookTargetRequestObject) (api.DeleteWebhookTargetResponseObject, error) {
	deleted, err := notify.New(handler.platform).DeleteWebhookTarget(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return api.DeleteWebhookTarget404JSONResponse(errorBody(api.NOTFOUND, "Webhook target not found")), nil
	}
	if err := handler.refreshNotificationSnapshot(ctx); err != nil {
		return nil, err
	}
	return api.DeleteWebhookTarget204Response{}, nil
}

func (handler *Handler) TestWebhookTarget(ctx context.Context, request api.TestWebhookTargetRequestObject) (api.TestWebhookTargetResponseObject, error) {
	queries := notify.New(handler.platform)
	targetID := pgtype.UUID{Bytes: request.Id, Valid: true}
	target, err := queries.GetWebhookTarget(ctx, targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.TestWebhookTarget404JSONResponse(errorBody(api.NOTFOUND, "Webhook target not found")), nil
	}
	if err != nil {
		return nil, err
	}
	if !target.Enabled {
		return api.TestWebhookTarget400JSONResponse(errorBody(api.VALIDATIONFAILED, "Webhook target is not enabled")), nil
	}
	id, err := queries.EnqueueTestWebhookNotification(ctx, notify.EnqueueTestWebhookNotificationParams{
		ID: targetID, NextAttemptAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return api.TestWebhookTarget202JSONResponse{Id: uuid.UUID(id.Bytes)}, nil
}

func (handler *Handler) GetChannelFailures(ctx context.Context, _ api.GetChannelFailuresRequestObject) (api.GetChannelFailuresResponseObject, error) {
	queries := notify.New(handler.platform)
	summaries, err := queries.ListActiveChannelFailureSummaries(ctx)
	if err != nil {
		return nil, err
	}
	records, err := queries.ListRecentActiveChannelFailures(ctx)
	if err != nil {
		return nil, err
	}
	byTarget := make(map[string][]api.ChannelFailureRecord)
	for _, row := range records {
		key := channelFailureKey(row.Channel, row.ChannelTargetID)
		byTarget[key] = append(byTarget[key], api.ChannelFailureRecord{
			FailedAt:   row.FailedAt.Time,
			Target:     row.Target,
			Reason:     row.FailureReason.String,
			RetryCount: int(row.RetryCount),
		})
	}
	channels := make([]api.ChannelFailureSummary, 0, len(summaries))
	for _, row := range summaries {
		var targetID *openapi_types.UUID
		if row.ChannelTargetID.Valid {
			value := openapi_types.UUID(row.ChannelTargetID.Bytes)
			targetID = &value
		}
		channels = append(channels, api.ChannelFailureSummary{
			Channel:            api.ChannelFailureSummaryChannel(row.Channel),
			TargetId:           targetID,
			Target:             row.Target,
			RecentFailureCount: int(row.RecentFailureCount),
			LastFailureReason:  row.LastFailureReason,
			LastFailedAt:       row.LastFailedAt.Time,
			RecentFailures:     byTarget[channelFailureKey(row.Channel, row.ChannelTargetID)],
		})
	}
	return api.GetChannelFailures200JSONResponse{HasFailures: len(channels) > 0, Channels: channels}, nil
}

type webhookTargetValues struct {
	name            string
	targetURL       string
	signingValue    string
	signatureHeader string
}

func validWebhookInput(input api.WebhookTargetInput) (webhookTargetValues, bool) {
	values := webhookTargetValues{
		name:      strings.TrimSpace(input.Name),
		targetURL: strings.TrimSpace(input.Url),
	}
	parsed, err := url.Parse(values.targetURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || values.name == "" {
		return webhookTargetValues{}, false
	}
	if input.SigningValue != nil {
		values.signingValue = strings.TrimSpace(*input.SigningValue)
		if values.signingValue == "" {
			return webhookTargetValues{}, false
		}
	}
	if input.SignatureHeader != nil {
		values.signatureHeader = strings.TrimSpace(*input.SignatureHeader)
		if textproto.CanonicalMIMEHeaderKey(values.signatureHeader) == "" {
			return webhookTargetValues{}, false
		}
	}
	return values, true
}

func channelFailureKey(channel string, targetID pgtype.UUID) string {
	if targetID.Valid {
		return notify.WebhookChannelKey(targetID)
	}
	return channel
}

func toAPIWebhookTarget(row notify.WebhookTarget) api.WebhookTarget {
	return api.WebhookTarget{
		Id:                uuid.UUID(row.ID.Bytes),
		Name:              row.Name,
		Enabled:           row.Enabled,
		Url:               row.Url,
		SigningConfigured: len(row.SigningValueCiphertext) > 0 && len(row.SignatureHeaderCiphertext) > 0,
		CreatedAt:         row.CreatedAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}
