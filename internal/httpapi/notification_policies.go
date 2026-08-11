package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/notify"
)

const (
	minimumNotificationRepeatIntervalSeconds = 900
	maximumNotificationRepeatIntervalSeconds = 86400
)

func (handler *Handler) ListNotificationContacts(ctx context.Context, _ api.ListNotificationContactsRequestObject) (api.ListNotificationContactsResponseObject, error) {
	rows, err := notify.New(handler.platform).ListNotificationContacts(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListNotificationContacts200JSONResponse, 0, len(rows))
	for _, row := range rows {
		response = append(response, toAPINotificationContact(row))
	}
	return response, nil
}

func (handler *Handler) CreateNotificationContact(ctx context.Context, request api.CreateNotificationContactRequestObject) (api.CreateNotificationContactResponseObject, error) {
	values, ok := notificationContactValues(request.Body)
	if !ok {
		return api.CreateNotificationContact400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid contact name and email are required")), nil
	}
	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	row, err := notify.New(handler.platform).CreateNotificationContact(ctx, notify.CreateNotificationContactParams{
		ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:       values.name,
		Email:      values.email,
		ExternalID: values.externalID,
		CreatedAt:  now,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateNotificationContact201JSONResponse(toAPINotificationContact(row)), nil
}

func (handler *Handler) UpdateNotificationContact(ctx context.Context, request api.UpdateNotificationContactRequestObject) (api.UpdateNotificationContactResponseObject, error) {
	values, ok := notificationContactValues(request.Body)
	if !ok {
		return api.UpdateNotificationContact400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid contact name and email are required")), nil
	}
	row, err := notify.New(handler.platform).UpdateNotificationContact(ctx, notify.UpdateNotificationContactParams{
		ID:         pgtype.UUID{Bytes: request.Id, Valid: true},
		Name:       values.name,
		Email:      values.email,
		ExternalID: values.externalID,
		UpdatedAt:  pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateNotificationContact404JSONResponse(errorBody(api.NOTFOUND, "notification contact not found")), nil
	}
	if err != nil {
		return nil, err
	}
	return api.UpdateNotificationContact200JSONResponse(toAPINotificationContact(row)), nil
}

func (handler *Handler) DeleteNotificationContact(ctx context.Context, request api.DeleteNotificationContactRequestObject) (api.DeleteNotificationContactResponseObject, error) {
	deleted, err := notify.New(handler.platform).DeleteNotificationContact(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return api.DeleteNotificationContact404JSONResponse(errorBody(api.NOTFOUND, "notification contact not found")), nil
	}
	return api.DeleteNotificationContact204Response{}, nil
}

func (handler *Handler) ListNotificationContactGroups(ctx context.Context, _ api.ListNotificationContactGroupsRequestObject) (api.ListNotificationContactGroupsResponseObject, error) {
	queries := notify.New(handler.platform)
	rows, err := queries.ListNotificationContactGroups(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListNotificationContactGroups200JSONResponse, 0, len(rows))
	for _, row := range rows {
		item, err := toAPINotificationContactGroup(ctx, queries, row)
		if err != nil {
			return nil, err
		}
		response = append(response, item)
	}
	return response, nil
}

func (handler *Handler) CreateNotificationContactGroup(ctx context.Context, request api.CreateNotificationContactGroupRequestObject) (api.CreateNotificationContactGroupResponseObject, error) {
	values, ok := notificationContactGroupValues(request.Body)
	if !ok {
		return api.CreateNotificationContactGroup400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid group name and unique contacts are required")), nil
	}
	groupID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	var row notify.NotificationContactGroup
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := notificationContactsExist(ctx, queries, values.contactIDs); err != nil {
			return err
		}
		var err error
		row, err = queries.CreateNotificationContactGroup(ctx, notify.CreateNotificationContactGroupParams{
			ID:        groupID,
			Name:      values.name,
			CreatedAt: now,
		})
		if err != nil {
			return err
		}
		return replaceNotificationContactGroupMembers(ctx, queries, groupID, values.contactIDs)
	})
	if errors.Is(err, errNotificationReferenceNotFound) {
		return api.CreateNotificationContactGroup400JSONResponse(errorBody(api.VALIDATIONFAILED, "contact_ids contains an unknown contact")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPINotificationContactGroup(ctx, notify.New(handler.platform), row)
	if err != nil {
		return nil, err
	}
	return api.CreateNotificationContactGroup201JSONResponse(item), nil
}

func (handler *Handler) UpdateNotificationContactGroup(ctx context.Context, request api.UpdateNotificationContactGroupRequestObject) (api.UpdateNotificationContactGroupResponseObject, error) {
	values, ok := notificationContactGroupValues(request.Body)
	if !ok {
		return api.UpdateNotificationContactGroup400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid group name and unique contacts are required")), nil
	}
	groupID := pgtype.UUID{Bytes: request.Id, Valid: true}
	var row notify.NotificationContactGroup
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := notificationContactsExist(ctx, queries, values.contactIDs); err != nil {
			return err
		}
		var err error
		row, err = queries.UpdateNotificationContactGroup(ctx, notify.UpdateNotificationContactGroupParams{
			ID:        groupID,
			Name:      values.name,
			UpdatedAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		return replaceNotificationContactGroupMembers(ctx, queries, groupID, values.contactIDs)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateNotificationContactGroup404JSONResponse(errorBody(api.NOTFOUND, "notification contact group not found")), nil
	}
	if errors.Is(err, errNotificationReferenceNotFound) {
		return api.UpdateNotificationContactGroup400JSONResponse(errorBody(api.VALIDATIONFAILED, "contact_ids contains an unknown contact")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPINotificationContactGroup(ctx, notify.New(handler.platform), row)
	if err != nil {
		return nil, err
	}
	return api.UpdateNotificationContactGroup200JSONResponse(item), nil
}

func (handler *Handler) DeleteNotificationContactGroup(ctx context.Context, request api.DeleteNotificationContactGroupRequestObject) (api.DeleteNotificationContactGroupResponseObject, error) {
	deleted, err := notify.New(handler.platform).DeleteNotificationContactGroup(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return api.DeleteNotificationContactGroup404JSONResponse(errorBody(api.NOTFOUND, "notification contact group not found")), nil
	}
	return api.DeleteNotificationContactGroup204Response{}, nil
}

func (handler *Handler) ListNotificationPolicies(ctx context.Context, _ api.ListNotificationPoliciesRequestObject) (api.ListNotificationPoliciesResponseObject, error) {
	queries := notify.New(handler.platform)
	rows, err := queries.ListNotificationPolicies(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListNotificationPolicies200JSONResponse, 0, len(rows))
	for _, row := range rows {
		item, err := toAPINotificationPolicy(ctx, queries, row)
		if err != nil {
			return nil, err
		}
		response = append(response, item)
	}
	return response, nil
}

func (handler *Handler) CreateNotificationPolicy(ctx context.Context, request api.CreateNotificationPolicyRequestObject) (api.CreateNotificationPolicyResponseObject, error) {
	values, ok := notificationPolicyValues(request.Body)
	if !ok {
		return api.CreateNotificationPolicy400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid policy recipients, channels, severity filter, and repeat interval are required")), nil
	}
	policyID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	var row notify.NotificationPolicy
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := notificationPolicyReferencesExist(ctx, queries, values); err != nil {
			return err
		}
		var err error
		row, err = queries.CreateNotificationPolicy(ctx, notify.CreateNotificationPolicyParams{
			ID:               policyID,
			Identifier:       uuid.UUID(policyID.Bytes).String(),
			Name:             values.name,
			SeverityFilter:   values.severityFilter,
			NotifyOnFire:     values.notifyOnFire,
			NotifyOnRecovery: values.notifyOnRecovery,
			RepeatInterval:   values.repeatInterval,
			TemplateID:       values.templateID,
			CreatedAt:        now,
		})
		if err != nil {
			return err
		}
		return replaceNotificationPolicyAssociations(ctx, queries, policyID, values)
	})
	if errors.Is(err, errNotificationReferenceNotFound) {
		return api.CreateNotificationPolicy400JSONResponse(errorBody(api.VALIDATIONFAILED, "policy contains an unknown contact, group, or Webhook target")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPINotificationPolicy(ctx, notify.New(handler.platform), row)
	if err != nil {
		return nil, err
	}
	return api.CreateNotificationPolicy201JSONResponse(item), nil
}

func (handler *Handler) UpdateNotificationPolicy(ctx context.Context, request api.UpdateNotificationPolicyRequestObject) (api.UpdateNotificationPolicyResponseObject, error) {
	values, ok := notificationPolicyValues(request.Body)
	if !ok {
		return api.UpdateNotificationPolicy400JSONResponse(errorBody(api.VALIDATIONFAILED, "valid policy recipients, channels, severity filter, and repeat interval are required")), nil
	}
	policyID := pgtype.UUID{Bytes: request.Id, Valid: true}
	var row notify.NotificationPolicy
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := notify.New(tx)
		if err := notificationPolicyReferencesExist(ctx, queries, values); err != nil {
			return err
		}
		var err error
		row, err = queries.UpdateNotificationPolicy(ctx, notify.UpdateNotificationPolicyParams{
			ID:               policyID,
			Name:             values.name,
			SeverityFilter:   values.severityFilter,
			NotifyOnFire:     values.notifyOnFire,
			NotifyOnRecovery: values.notifyOnRecovery,
			RepeatInterval:   values.repeatInterval,
			TemplateID:       values.templateID,
			UpdatedAt:        pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
		})
		if err != nil {
			return err
		}
		return replaceNotificationPolicyAssociations(ctx, queries, policyID, values)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateNotificationPolicy404JSONResponse(errorBody(api.NOTFOUND, "notification policy not found")), nil
	}
	if errors.Is(err, errNotificationReferenceNotFound) {
		return api.UpdateNotificationPolicy400JSONResponse(errorBody(api.VALIDATIONFAILED, "policy contains an unknown contact, group, or Webhook target")), nil
	}
	if err != nil {
		return nil, err
	}
	item, err := toAPINotificationPolicy(ctx, notify.New(handler.platform), row)
	if err != nil {
		return nil, err
	}
	return api.UpdateNotificationPolicy200JSONResponse(item), nil
}

func (handler *Handler) DeleteNotificationPolicy(ctx context.Context, request api.DeleteNotificationPolicyRequestObject) (api.DeleteNotificationPolicyResponseObject, error) {
	queries := notify.New(handler.platform)
	policyID := pgtype.UUID{Bytes: request.Id, Valid: true}
	policy, err := queries.GetNotificationPolicy(ctx, policyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.DeleteNotificationPolicy404JSONResponse(errorBody(api.NOTFOUND, "notification policy not found")), nil
	}
	if err != nil {
		return nil, err
	}
	if policy.IsDefault {
		return api.DeleteNotificationPolicy409JSONResponse(errorBody(api.CONFLICT, "the default notification policy cannot be deleted")), nil
	}
	deleted, err := queries.DeleteNotificationPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return api.DeleteNotificationPolicy404JSONResponse(errorBody(api.NOTFOUND, "notification policy not found")), nil
	}
	return api.DeleteNotificationPolicy204Response{}, nil
}

type notificationContactInputValues struct {
	name       string
	email      string
	externalID pgtype.Text
}

func notificationContactValues(input *api.NotificationContactInput) (notificationContactInputValues, bool) {
	if input == nil {
		return notificationContactInputValues{}, false
	}
	values := notificationContactInputValues{name: strings.TrimSpace(input.Name), email: strings.TrimSpace(string(input.Email))}
	if values.name == "" || !validEmail(values.email) {
		return notificationContactInputValues{}, false
	}
	if input.ExternalId != nil {
		externalID := strings.TrimSpace(*input.ExternalId)
		if externalID == "" {
			return notificationContactInputValues{}, false
		}
		values.externalID = pgtype.Text{String: externalID, Valid: true}
	}
	return values, true
}

type notificationContactGroupInputValues struct {
	name       string
	contactIDs []pgtype.UUID
}

func notificationContactGroupValues(input *api.NotificationContactGroupInput) (notificationContactGroupInputValues, bool) {
	if input == nil {
		return notificationContactGroupInputValues{}, false
	}
	values := notificationContactGroupInputValues{name: strings.TrimSpace(input.Name)}
	contactIDs, unique := uniqueDatabaseUUIDs(input.ContactIds)
	if values.name == "" || !unique {
		return notificationContactGroupInputValues{}, false
	}
	values.contactIDs = contactIDs
	return values, true
}

type notificationPolicyInputValues struct {
	name             string
	contactIDs       []pgtype.UUID
	contactGroupIDs  []pgtype.UUID
	channels         []api.NotificationPolicyChannel
	severityFilter   []string
	notifyOnFire     bool
	notifyOnRecovery bool
	repeatInterval   int32
	templateID       pgtype.Text
}

func notificationPolicyValues(input *api.NotificationPolicyInput) (notificationPolicyInputValues, bool) {
	if input == nil || input.RepeatInterval < minimumNotificationRepeatIntervalSeconds || input.RepeatInterval > maximumNotificationRepeatIntervalSeconds {
		return notificationPolicyInputValues{}, false
	}
	values := notificationPolicyInputValues{
		name:             strings.TrimSpace(input.Name),
		channels:         input.Channels,
		notifyOnFire:     input.NotifyOnFire,
		notifyOnRecovery: input.NotifyOnRecovery,
		repeatInterval:   int32(input.RepeatInterval),
	}
	var unique bool
	values.contactIDs, unique = uniqueDatabaseUUIDs(input.ContactIds)
	if values.name == "" || !unique {
		return notificationPolicyInputValues{}, false
	}
	values.contactGroupIDs, unique = uniqueDatabaseUUIDs(input.ContactGroupIds)
	if !unique {
		return notificationPolicyInputValues{}, false
	}
	values.severityFilter, unique = notificationPolicySeverityFilter(input.SeverityFilter)
	if !unique || !validNotificationPolicyChannels(input.Channels) {
		return notificationPolicyInputValues{}, false
	}
	if input.TemplateId != nil {
		templateID := strings.TrimSpace(*input.TemplateId)
		if templateID == "" {
			return notificationPolicyInputValues{}, false
		}
		values.templateID = pgtype.Text{String: templateID, Valid: true}
	}
	return values, true
}

func notificationPolicySeverityFilter(severities []api.AlertSeverity) ([]string, bool) {
	if len(severities) == 0 {
		return nil, false
	}
	seen := make(map[api.AlertSeverity]bool, len(severities))
	result := make([]string, 0, len(severities))
	for _, severity := range severities {
		if seen[severity] || (severity != api.Critical && severity != api.Warning && severity != api.Info) {
			return nil, false
		}
		seen[severity] = true
		result = append(result, string(severity))
	}
	return result, true
}

func validNotificationPolicyChannels(channels []api.NotificationPolicyChannel) bool {
	if len(channels) == 0 {
		return false
	}
	seen := make(map[string]bool, len(channels))
	for _, channel := range channels {
		if !validNotificationPolicyChannel(channel) {
			return false
		}
		key := string(channel.Channel)
		if channel.TargetId != nil {
			key += ":" + channel.TargetId.String()
		}
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func validNotificationPolicyChannel(channel api.NotificationPolicyChannel) bool {
	switch channel.Channel {
	case api.PolicySMTP:
		return channel.TargetId == nil
	case api.PolicyWebhook:
		return channel.TargetId != nil
	default:
		return false
	}
}

var errNotificationReferenceNotFound = errors.New("notification reference not found")

func notificationContactsExist(ctx context.Context, queries *notify.Queries, contactIDs []pgtype.UUID) error {
	for _, contactID := range contactIDs {
		_, err := queries.GetNotificationContact(ctx, contactID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotificationReferenceNotFound
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func notificationPolicyReferencesExist(ctx context.Context, queries *notify.Queries, values notificationPolicyInputValues) error {
	if err := notificationContactsExist(ctx, queries, values.contactIDs); err != nil {
		return err
	}
	for _, groupID := range values.contactGroupIDs {
		_, err := queries.GetNotificationContactGroup(ctx, groupID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotificationReferenceNotFound
		}
		if err != nil {
			return err
		}
	}
	for _, channel := range values.channels {
		if channel.Channel != api.PolicyWebhook {
			continue
		}
		_, err := queries.GetWebhookTarget(ctx, pgtype.UUID{Bytes: *channel.TargetId, Valid: true})
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotificationReferenceNotFound
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func replaceNotificationContactGroupMembers(ctx context.Context, queries *notify.Queries, groupID pgtype.UUID, contactIDs []pgtype.UUID) error {
	if err := queries.ClearNotificationContactGroupMembers(ctx, groupID); err != nil {
		return err
	}
	for _, contactID := range contactIDs {
		if err := queries.AddNotificationContactGroupMember(ctx, notify.AddNotificationContactGroupMemberParams{GroupID: groupID, ContactID: contactID}); err != nil {
			return err
		}
	}
	return nil
}

func replaceNotificationPolicyAssociations(ctx context.Context, queries *notify.Queries, policyID pgtype.UUID, values notificationPolicyInputValues) error {
	if err := queries.ClearNotificationPolicyContacts(ctx, policyID); err != nil {
		return err
	}
	if err := queries.ClearNotificationPolicyContactGroups(ctx, policyID); err != nil {
		return err
	}
	if err := queries.ClearNotificationPolicyChannels(ctx, policyID); err != nil {
		return err
	}
	for _, contactID := range values.contactIDs {
		if err := queries.AddNotificationPolicyContact(ctx, notify.AddNotificationPolicyContactParams{
			PolicyID:  policyID,
			ContactID: contactID,
		}); err != nil {
			return err
		}
	}
	for _, groupID := range values.contactGroupIDs {
		if err := queries.AddNotificationPolicyContactGroup(ctx, notify.AddNotificationPolicyContactGroupParams{
			PolicyID: policyID,
			GroupID:  groupID,
		}); err != nil {
			return err
		}
	}
	for _, channel := range values.channels {
		targetID := pgtype.UUID{}
		if channel.TargetId != nil {
			targetID = pgtype.UUID{Bytes: *channel.TargetId, Valid: true}
		}
		if err := queries.AddNotificationPolicyChannel(ctx, notify.AddNotificationPolicyChannelParams{
			PolicyID:        policyID,
			Channel:         string(channel.Channel),
			ChannelTargetID: targetID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func toAPINotificationContact(row notify.NotificationContact) api.NotificationContact {
	item := api.NotificationContact{
		Id:        uuid.UUID(row.ID.Bytes),
		Name:      row.Name,
		Email:     openapi_types.Email(row.Email),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.ExternalID.Valid {
		item.ExternalId = &row.ExternalID.String
	}
	return item
}

func toAPINotificationContactGroup(ctx context.Context, queries *notify.Queries, row notify.NotificationContactGroup) (api.NotificationContactGroup, error) {
	contactIDs, err := queries.ListNotificationContactGroupMembers(ctx, row.ID)
	if err != nil {
		return api.NotificationContactGroup{}, err
	}
	return api.NotificationContactGroup{
		Id:         uuid.UUID(row.ID.Bytes),
		Name:       row.Name,
		ContactIds: toOpenAPIUUIDs(contactIDs),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}, nil
}

func toAPINotificationPolicy(ctx context.Context, queries *notify.Queries, row notify.NotificationPolicy) (api.NotificationPolicy, error) {
	contactIDs, err := queries.ListNotificationPolicyContacts(ctx, row.ID)
	if err != nil {
		return api.NotificationPolicy{}, err
	}
	groupIDs, err := queries.ListNotificationPolicyContactGroups(ctx, row.ID)
	if err != nil {
		return api.NotificationPolicy{}, err
	}
	channelRows, err := queries.ListNotificationPolicyChannels(ctx, row.ID)
	if err != nil {
		return api.NotificationPolicy{}, err
	}
	channels := make([]api.NotificationPolicyChannel, 0, len(channelRows))
	for _, channelRow := range channelRows {
		channel := api.NotificationPolicyChannel{Channel: api.NotificationPolicyChannelChannel(channelRow.Channel)}
		if channelRow.ChannelTargetID.Valid {
			targetID := openapi_types.UUID(channelRow.ChannelTargetID.Bytes)
			channel.TargetId = &targetID
		}
		channels = append(channels, channel)
	}
	severityFilter := make([]api.AlertSeverity, 0, len(row.SeverityFilter))
	for _, severity := range row.SeverityFilter {
		severityFilter = append(severityFilter, api.AlertSeverity(severity))
	}
	item := api.NotificationPolicy{
		Id:               uuid.UUID(row.ID.Bytes),
		Name:             row.Name,
		IsDefault:        row.IsDefault,
		ContactIds:       toOpenAPIUUIDs(contactIDs),
		ContactGroupIds:  toOpenAPIUUIDs(groupIDs),
		Channels:         channels,
		SeverityFilter:   severityFilter,
		NotifyOnFire:     row.NotifyOnFire,
		NotifyOnRecovery: row.NotifyOnRecovery,
		RepeatInterval:   int(row.RepeatInterval),
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}
	if row.TemplateID.Valid {
		item.TemplateId = &row.TemplateID.String
	}
	return item, nil
}

func uniqueDatabaseUUIDs(ids []openapi_types.UUID) ([]pgtype.UUID, bool) {
	seen := make(map[uuid.UUID]bool, len(ids))
	result := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		value := uuid.UUID(id)
		if seen[value] {
			return nil, false
		}
		seen[value] = true
		result = append(result, pgtype.UUID{Bytes: value, Valid: true})
	}
	return result, true
}

func toOpenAPIUUIDs(ids []pgtype.UUID) []openapi_types.UUID {
	result := make([]openapi_types.UUID, 0, len(ids))
	for _, id := range ids {
		result = append(result, openapi_types.UUID(id.Bytes))
	}
	return result
}
