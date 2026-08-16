package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func (handler *Handler) ListAlertRules(ctx context.Context, _ api.ListAlertRulesRequestObject) (api.ListAlertRulesResponseObject, error) {
	queries := alerting.New(handler.platform)
	rules, err := queries.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListAlertRules200JSONResponse, 0, len(rules))
	for _, rule := range rules {
		instanceIDs, err := queries.ListAlertRuleScopeInstances(ctx, rule.ID)
		if err != nil {
			return nil, err
		}
		item, err := toAPIAlertRule(ctx, queries, rule, instanceIDs)
		if err != nil {
			return nil, err
		}
		response = append(response, item)
	}
	return response, nil
}

func (handler *Handler) ListAlertRuleTemplates(ctx context.Context, _ api.ListAlertRuleTemplatesRequestObject) (api.ListAlertRuleTemplatesResponseObject, error) {
	templates, err := alerting.New(handler.platform).ListAlertRuleTemplates(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListAlertRuleTemplates200JSONResponse, 0, len(templates))
	for _, template := range templates {
		response = append(response, toAPIAlertRuleTemplate(template))
	}
	return response, nil
}

func (handler *Handler) CreateAlertRule(ctx context.Context, request api.CreateAlertRuleRequestObject) (api.CreateAlertRuleResponseObject, error) {
	if request.Body == nil {
		return invalidAlertRule([]fieldError{{field: "body", message: "is required"}}), nil
	}
	created, fieldErrors, err := handler.createAlertRule(ctx, *request.Body, "", 0)
	if err != nil {
		return nil, err
	}
	if len(fieldErrors) > 0 {
		return invalidAlertRule(fieldErrors), nil
	}
	return api.CreateAlertRule201JSONResponse(created), nil
}

func (handler *Handler) UpdateAlertRule(ctx context.Context, request api.UpdateAlertRuleRequestObject) (api.UpdateAlertRuleResponseObject, error) {
	if request.Body == nil {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError([]fieldError{{field: "body", message: "is required"}})), nil
	}
	input := *request.Body
	if fieldErrors := validateAlertRule(input); len(fieldErrors) > 0 {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError(fieldErrors)), nil
	}
	queries := alerting.New(handler.platform)
	existing, err := queries.GetAlertRule(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateAlertRule404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	if input.Enabled != existing.Enabled {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError([]fieldError{{field: "enabled", message: "must be changed through the enablement endpoint"}})), nil
	}
	if existing.BuiltinIdentifier.Valid && input.Severity == api.Info {
		return api.UpdateAlertRule400JSONResponse(errorBody(api.BUILTINRULESEVERITYTOOLOW, "built-in collection rules must remain warning or critical")), nil
	}
	if existing.BuiltinIdentifier.Valid && input.MetricId != existing.MetricID {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError([]fieldError{{field: "metric_id", message: "cannot be changed for a built-in rule"}})), nil
	}
	if existing.BuiltinIdentifier.Valid && input.Scope != api.ALL {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError([]fieldError{{field: "scope", message: "must remain ALL for a built-in rule"}})), nil
	}
	if fieldErrors, err := handler.validateAlertRuleReferences(ctx, input); err != nil {
		return nil, err
	} else if len(fieldErrors) > 0 {
		return api.UpdateAlertRule400JSONResponse(alertRuleValidationError(fieldErrors)), nil
	}

	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	actorID := databaseUserID(authenticatedUserID(ctx))
	scopedInstanceIDs := toDatabaseUUIDs(input.InstanceIds)
	var updated alerting.AlertRule
	err = handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		updated, err = queries.UpdateAlertRule(ctx, alerting.UpdateAlertRuleParams{
			ID:                        pgtype.UUID{Bytes: request.Id, Valid: true},
			Name:                      strings.TrimSpace(input.Name),
			MetricID:                  input.MetricId,
			Aggregation:               string(input.Aggregation),
			Operator:                  string(input.Operator),
			Threshold:                 input.Threshold,
			RecoveryOperator:          string(input.RecoveryOperator),
			RecoveryThreshold:         *input.RecoveryThreshold,
			WindowSeconds:             int32(input.WindowSeconds),
			ConsecutiveCount:          int32(input.ConsecutiveCount),
			RecoveryConsecutiveCount:  int32(recoveryConsecutiveCount(input)),
			Severity:                  string(input.Severity),
			NoDataPolicy:              string(input.NoDataPolicy),
			Scope:                     string(input.Scope),
			EvaluationIntervalSeconds: int32(input.EvaluationIntervalSeconds),
			NotificationPolicyID:      toDatabaseOptionalUUID(input.NotificationPolicyId),
			UpdatedAt:                 now,
			ActorID:                   actorID,
		})
		if err != nil {
			return err
		}
		response, err := toAPIAlertRule(ctx, queries, updated, scopedInstanceIDs)
		if err != nil {
			return err
		}
		return saveAlertRuleVersion(ctx, queries, updated, response, scopedInstanceIDs, now)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateAlertRule404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	response, err := toAPIAlertRule(ctx, alerting.New(handler.platform), updated, scopedInstanceIDs)
	if err != nil {
		return nil, err
	}
	return api.UpdateAlertRule200JSONResponse(response), nil
}

func (handler *Handler) UpdateAlertRuleEnabled(ctx context.Context, request api.UpdateAlertRuleEnabledRequestObject) (api.UpdateAlertRuleEnabledResponseObject, error) {
	if request.Body == nil {
		return api.UpdateAlertRuleEnabled400JSONResponse(alertRuleValidationError([]fieldError{{field: "body", message: "is required"}})), nil
	}
	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	queries := alerting.New(handler.platform)
	existing, err := queries.GetAlertRule(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateAlertRuleEnabled404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	if existing.BuiltinIdentifier.Valid && !request.Body.Enabled {
		return api.UpdateAlertRuleEnabled400JSONResponse(errorBody(api.BUILTINRULEDISABLEFORBIDDEN, "built-in collection rules cannot be disabled")), nil
	}
	rule, err := queries.SetAlertRuleEnabled(ctx, alerting.SetAlertRuleEnabledParams{
		ID:               pgtype.UUID{Bytes: request.Id, Valid: true},
		Enabled:          request.Body.Enabled,
		EnabledUpdatedBy: databaseUserID(authenticatedUserID(ctx)),
		EnabledUpdatedAt: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateAlertRuleEnabled404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	instanceIDs, err := queries.ListAlertRuleScopeInstances(ctx, rule.ID)
	if err != nil {
		return nil, err
	}
	response, err := toAPIAlertRule(ctx, queries, rule, instanceIDs)
	if err != nil {
		return nil, err
	}
	return api.UpdateAlertRuleEnabled200JSONResponse(response), nil
}

func (handler *Handler) DeleteAlertRule(ctx context.Context, request api.DeleteAlertRuleRequestObject) (api.DeleteAlertRuleResponseObject, error) {
	queries := alerting.New(handler.platform)
	ruleID := pgtype.UUID{Bytes: request.Id, Valid: true}
	rule, err := queries.GetAlertRule(ctx, ruleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.DeleteAlertRule404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	if rule.BuiltinIdentifier.Valid {
		return api.DeleteAlertRule409JSONResponse(errorBody(api.BUILTINRULEDELETEFORBIDDEN, "built-in collection rules cannot be deleted")), nil
	}
	if _, err := queries.DeleteAlertRule(ctx, alerting.DeleteAlertRuleParams{
		ID:        ruleID,
		ActorID:   databaseUserID(authenticatedUserID(ctx)),
		DeletedAt: pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true},
	}); err != nil {
		return nil, err
	}
	return api.DeleteAlertRule204Response{}, nil
}

func (handler *Handler) CreateAlertRuleFromTemplate(ctx context.Context, request api.CreateAlertRuleFromTemplateRequestObject) (api.CreateAlertRuleFromTemplateResponseObject, error) {
	template, err := alerting.New(handler.platform).GetAlertRuleTemplate(ctx, request.Id)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.CreateAlertRuleFromTemplate404JSONResponse(errorBody(api.NOTFOUND, "alert rule template not found")), nil
	}
	if err != nil {
		return nil, err
	}

	recoveryThreshold := template.RecoveryThreshold
	recoveryCount := int(template.RecoveryConsecutiveCount)
	input := api.AlertRuleInput{
		Name:                      template.Name,
		MetricId:                  template.MetricID,
		Aggregation:               api.AlertAggregation(template.Aggregation),
		Operator:                  api.AlertOperator(template.Operator),
		Threshold:                 template.Threshold,
		RecoveryOperator:          api.AlertOperator(template.RecoveryOperator),
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             int(template.WindowSeconds),
		ConsecutiveCount:          int(template.ConsecutiveCount),
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  api.AlertSeverity(template.Severity),
		NoDataPolicy:              api.NoDataPolicy(template.NoDataPolicy),
		Scope:                     api.ALL,
		InstanceIds:               []uuid.UUID{},
		EvaluationIntervalSeconds: int(template.EvaluationIntervalSeconds),
		Enabled:                   true,
	}
	if request.Body != nil {
		applyTemplateOverrides(&input, *request.Body)
	}
	created, fieldErrors, err := handler.createAlertRule(ctx, input, template.Identifier, template.Version)
	if err != nil {
		return nil, err
	}
	if len(fieldErrors) > 0 {
		return api.CreateAlertRuleFromTemplate400JSONResponse(alertRuleValidationError(fieldErrors)), nil
	}
	return api.CreateAlertRuleFromTemplate201JSONResponse(created), nil
}

func (handler *Handler) CopyAlertRule(ctx context.Context, request api.CopyAlertRuleRequestObject) (api.CopyAlertRuleResponseObject, error) {
	queries := alerting.New(handler.platform)
	rule, err := queries.GetAlertRule(ctx, pgtype.UUID{Bytes: request.Id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.CopyAlertRule404JSONResponse(errorBody(api.NOTFOUND, "alert rule not found")), nil
	}
	if err != nil {
		return nil, err
	}
	instanceIDs, err := queries.ListAlertRuleScopeInstances(ctx, rule.ID)
	if err != nil {
		return nil, err
	}
	recoveryThreshold := rule.RecoveryThreshold
	recoveryCount := int(rule.RecoveryConsecutiveCount)
	input := api.AlertRuleInput{
		Name:                      rule.Name + " 副本",
		MetricId:                  rule.MetricID,
		Aggregation:               api.AlertAggregation(rule.Aggregation),
		Operator:                  api.AlertOperator(rule.Operator),
		Threshold:                 rule.Threshold,
		RecoveryOperator:          api.AlertOperator(rule.RecoveryOperator),
		RecoveryThreshold:         &recoveryThreshold,
		WindowSeconds:             int(rule.WindowSeconds),
		ConsecutiveCount:          int(rule.ConsecutiveCount),
		RecoveryConsecutiveCount:  &recoveryCount,
		Severity:                  api.AlertSeverity(rule.Severity),
		NoDataPolicy:              api.NoDataPolicy(rule.NoDataPolicy),
		Scope:                     api.AlertRuleScope(rule.Scope),
		InstanceIds:               toAPIUUIDs(instanceIDs),
		EvaluationIntervalSeconds: int(rule.EvaluationIntervalSeconds),
		Enabled:                   rule.Enabled,
		NotificationPolicyId:      toAPIOptionalUUID(rule.NotificationPolicyID),
	}
	if request.Body != nil && request.Body.Name != nil {
		input.Name = *request.Body.Name
	}
	created, fieldErrors, err := handler.createAlertRule(ctx, input, "", 0)
	if err != nil {
		return nil, err
	}
	if len(fieldErrors) > 0 {
		return api.CopyAlertRule400JSONResponse(alertRuleValidationError(fieldErrors)), nil
	}
	return api.CopyAlertRule201JSONResponse(created), nil
}

func (handler *Handler) createAlertRule(ctx context.Context, input api.AlertRuleInput, sourceTemplateID string, sourceTemplateVersion int32) (api.AlertRule, []fieldError, error) {
	if fieldErrors := validateAlertRule(input); len(fieldErrors) > 0 {
		return api.AlertRule{}, fieldErrors, nil
	}
	if fieldErrors, err := handler.validateAlertRuleReferences(ctx, input); err != nil {
		return api.AlertRule{}, nil, err
	} else if len(fieldErrors) > 0 {
		return api.AlertRule{}, fieldErrors, nil
	}

	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	actorID := databaseUserID(authenticatedUserID(ctx))
	ruleID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	scopedInstanceIDs := toDatabaseUUIDs(input.InstanceIds)
	databaseSourceTemplateID := pgtype.Text{}
	databaseSourceTemplateVersion := pgtype.Int4{}
	if sourceTemplateID != "" {
		databaseSourceTemplateID = pgtype.Text{String: sourceTemplateID, Valid: true}
		databaseSourceTemplateVersion = pgtype.Int4{Int32: sourceTemplateVersion, Valid: true}
	}
	var response api.AlertRule
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		rule, err := queries.CreateAlertRule(ctx, alerting.CreateAlertRuleParams{
			ID:                        ruleID,
			Name:                      strings.TrimSpace(input.Name),
			MetricID:                  input.MetricId,
			Aggregation:               string(input.Aggregation),
			Operator:                  string(input.Operator),
			Threshold:                 input.Threshold,
			RecoveryOperator:          string(input.RecoveryOperator),
			RecoveryThreshold:         *input.RecoveryThreshold,
			WindowSeconds:             int32(input.WindowSeconds),
			ConsecutiveCount:          int32(input.ConsecutiveCount),
			RecoveryConsecutiveCount:  int32(recoveryConsecutiveCount(input)),
			Severity:                  string(input.Severity),
			NoDataPolicy:              string(input.NoDataPolicy),
			Scope:                     string(input.Scope),
			EvaluationIntervalSeconds: int32(input.EvaluationIntervalSeconds),
			Enabled:                   input.Enabled,
			CreatedAt:                 now,
			NotificationPolicyID:      toDatabaseOptionalUUID(input.NotificationPolicyId),
			SourceTemplateID:          databaseSourceTemplateID,
			SourceTemplateVersion:     databaseSourceTemplateVersion,
			ActorID:                   actorID,
		})
		if err != nil {
			return err
		}
		response, err = toAPIAlertRule(ctx, queries, rule, scopedInstanceIDs)
		if err != nil {
			return err
		}
		return saveAlertRuleVersion(ctx, queries, rule, response, scopedInstanceIDs, now)
	})
	if err != nil {
		return api.AlertRule{}, nil, err
	}
	return response, nil, nil
}

func applyTemplateOverrides(input *api.AlertRuleInput, overrides api.AlertRuleTemplateInstantiationInput) {
	if overrides.Name != nil {
		input.Name = *overrides.Name
	}
	if overrides.Threshold != nil {
		input.Threshold = *overrides.Threshold
	}
	if overrides.RecoveryThreshold != nil {
		input.RecoveryThreshold = overrides.RecoveryThreshold
	}
	if overrides.ConsecutiveCount != nil {
		input.ConsecutiveCount = *overrides.ConsecutiveCount
	}
	if overrides.RecoveryConsecutiveCount != nil {
		input.RecoveryConsecutiveCount = overrides.RecoveryConsecutiveCount
	}
	if overrides.Severity != nil {
		input.Severity = *overrides.Severity
	}
	if overrides.Scope != nil {
		input.Scope = *overrides.Scope
	}
	if overrides.InstanceIds != nil {
		input.InstanceIds = *overrides.InstanceIds
	}
	if overrides.NotificationPolicyId != nil {
		input.NotificationPolicyId = overrides.NotificationPolicyId
	}
	if overrides.Enabled != nil {
		input.Enabled = *overrides.Enabled
	}
}

func validateAlertRule(rule api.AlertRuleInput) []fieldError {
	fieldErrors := make([]fieldError, 0)
	if strings.TrimSpace(rule.Name) == "" {
		fieldErrors = append(fieldErrors, fieldError{field: "name", message: "must not be blank"})
	}
	definition, exists := metricDefinition(metric.MetricID(rule.MetricId))
	if !exists || definition.Alertability == metric.AlertabilityNo {
		fieldErrors = append(fieldErrors, fieldError{field: "metric_id", message: "must identify an alertable metric"})
	}
	if !validAggregation(rule.Aggregation) {
		fieldErrors = append(fieldErrors, fieldError{field: "aggregation", message: "is not supported"})
	}
	if !validOperator(rule.Operator) {
		fieldErrors = append(fieldErrors, fieldError{field: "operator", message: "is not supported"})
	}
	if !validOperator(rule.RecoveryOperator) {
		fieldErrors = append(fieldErrors, fieldError{field: "recovery_operator", message: "is not supported"})
	}
	if rule.WindowSeconds < 1 {
		fieldErrors = append(fieldErrors, fieldError{field: "window_seconds", message: "must be positive"})
	}
	if rule.ConsecutiveCount < 1 {
		fieldErrors = append(fieldErrors, fieldError{field: "consecutive_count", message: "must be positive"})
	}
	if rule.RecoveryConsecutiveCount != nil && *rule.RecoveryConsecutiveCount < 1 {
		fieldErrors = append(fieldErrors, fieldError{field: "recovery_consecutive_count", message: "must be positive"})
	}
	if rule.Severity != api.Critical && rule.Severity != api.Warning && rule.Severity != api.Info {
		fieldErrors = append(fieldErrors, fieldError{field: "severity", message: "is not supported"})
	}
	if rule.NoDataPolicy != api.Ignore && rule.NoDataPolicy != api.MarkNoData {
		fieldErrors = append(fieldErrors, fieldError{field: "no_data_policy", message: "is not supported"})
	}
	if rule.EvaluationIntervalSeconds < 5 {
		fieldErrors = append(fieldErrors, fieldError{field: "evaluation_interval_seconds", message: "must be at least 5 seconds"})
	}
	switch rule.Scope {
	case api.ALL:
		if len(rule.InstanceIds) != 0 {
			fieldErrors = append(fieldErrors, fieldError{field: "instance_ids", message: "must be empty for ALL scope"})
		}
	case api.INSTANCES:
		if len(rule.InstanceIds) == 0 {
			fieldErrors = append(fieldErrors, fieldError{field: "instance_ids", message: "must contain at least one instance"})
		}
		seen := make(map[uuid.UUID]struct{}, len(rule.InstanceIds))
		for _, instanceID := range rule.InstanceIds {
			if _, duplicate := seen[instanceID]; duplicate {
				fieldErrors = append(fieldErrors, fieldError{field: "instance_ids", message: "must not contain duplicates"})
				break
			}
			seen[instanceID] = struct{}{}
		}
	default:
		fieldErrors = append(fieldErrors, fieldError{field: "scope", message: "is not supported"})
	}
	if rule.RecoveryThreshold == nil {
		fieldErrors = append(fieldErrors, fieldError{field: "recovery_threshold", message: "is required"})
		return fieldErrors
	}
	encodings, categorical := metric.NonNumericMetricEncodings[rule.MetricId]
	if exists && !categorical && validOperator(rule.Operator) && validOperator(rule.RecoveryOperator) {
		if !validHysteresis(rule.Operator, rule.Threshold, rule.RecoveryOperator, *rule.RecoveryThreshold) {
			fieldErrors = append(fieldErrors, fieldError{field: "recovery_threshold", message: "must define a separate recovery range"})
		}
	}
	if exists && categorical {
		if rule.Aggregation != api.Latest {
			fieldErrors = append(fieldErrors, fieldError{field: "aggregation", message: "categorical metrics require latest aggregation"})
		}
		if !validCategoricalRecovery(encodings, rule.Operator, rule.Threshold, rule.RecoveryOperator, *rule.RecoveryThreshold) {
			fieldErrors = append(fieldErrors, fieldError{field: "recovery_threshold", message: "must define the opposite recovery state"})
		}
	}
	return fieldErrors
}

func recoveryConsecutiveCount(rule api.AlertRuleInput) int {
	if rule.RecoveryConsecutiveCount == nil {
		return rule.ConsecutiveCount
	}
	return *rule.RecoveryConsecutiveCount
}

func validCategoricalRecovery(encodings map[string]float64, operator api.AlertOperator, threshold float64, recoveryOperator api.AlertOperator, recoveryThreshold float64) bool {
	hasTrigger := false
	hasRecovery := false
	for _, value := range encodings {
		trigger := alerting.Compare(value, string(operator), threshold)
		recovery := alerting.Compare(value, string(recoveryOperator), recoveryThreshold)
		if trigger && recovery {
			return false
		}
		hasTrigger = hasTrigger || trigger
		hasRecovery = hasRecovery || recovery
	}
	return hasTrigger && hasRecovery
}

func (handler *Handler) validateAlertRuleReferences(ctx context.Context, rule api.AlertRuleInput) ([]fieldError, error) {
	queries := alerting.New(handler.platform)
	if rule.Scope == api.INSTANCES {
		for _, instanceID := range rule.InstanceIds {
			exists, err := queries.AlertRuleTargetInstanceExists(ctx, pgtype.UUID{Bytes: instanceID, Valid: true})
			if err != nil {
				return nil, err
			}
			if !exists {
				return []fieldError{{field: "instance_ids", message: "contains an unknown instance"}}, nil
			}
		}
	}
	if rule.NotificationPolicyId != nil {
		_, err := queries.GetNotificationPolicy(ctx, toDatabaseOptionalUUID(rule.NotificationPolicyId))
		if errors.Is(err, pgx.ErrNoRows) {
			return []fieldError{{field: "notification_policy_id", message: "must identify an existing notification policy"}}, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func saveAlertRuleVersion(ctx context.Context, queries *alerting.Queries, rule alerting.AlertRule, response api.AlertRule, instanceIDs []pgtype.UUID, createdAt pgtype.Timestamptz) error {
	if err := replaceAlertRuleScope(ctx, queries, rule.ID, instanceIDs); err != nil {
		return err
	}
	snapshot, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return queries.CreateAlertRuleVersion(ctx, alerting.CreateAlertRuleVersionParams{
		RuleID:    rule.ID,
		Version:   rule.Version,
		Snapshot:  snapshot,
		CreatedAt: createdAt,
	})
}

func replaceAlertRuleScope(ctx context.Context, queries *alerting.Queries, ruleID pgtype.UUID, instanceIDs []pgtype.UUID) error {
	if err := queries.DeleteAlertRuleScopeInstances(ctx, ruleID); err != nil {
		return err
	}
	for _, instanceID := range instanceIDs {
		if err := queries.AddAlertRuleScopeInstance(ctx, alerting.AddAlertRuleScopeInstanceParams{
			RuleID: ruleID, InstanceID: instanceID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func toDatabaseUUIDs(instanceIDs []uuid.UUID) []pgtype.UUID {
	result := make([]pgtype.UUID, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		result = append(result, pgtype.UUID{Bytes: instanceID, Valid: true})
	}
	return result
}

func toAPIUUIDs(instanceIDs []pgtype.UUID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		result = append(result, instanceID.Bytes)
	}
	return result
}

func toDatabaseOptionalUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}

func toAPIOptionalUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := uuid.UUID(value.Bytes)
	return &result
}

func metricDefinition(id metric.MetricID) (metric.Metric, bool) {
	for _, definition := range metric.Metrics {
		if definition.ID == id {
			return definition, true
		}
	}
	return metric.Metric{}, false
}

func validAggregation(value api.AlertAggregation) bool {
	switch value {
	case api.Latest, api.Avg, api.Max, api.Min, api.Sum, api.Count:
		return true
	default:
		return false
	}
}

func validOperator(value api.AlertOperator) bool {
	switch value {
	case api.GreaterThan, api.GreaterThanEqual, api.LessThan, api.LessThanEqual, api.Equal, api.NotEqual:
		return true
	default:
		return false
	}
}

func validHysteresis(operator api.AlertOperator, threshold float64, recoveryOperator api.AlertOperator, recoveryThreshold float64) bool {
	switch operator {
	case api.GreaterThan:
		return ((recoveryOperator == api.LessThan || recoveryOperator == api.LessThanEqual) && recoveryThreshold < threshold) ||
			(recoveryOperator == api.Equal && recoveryThreshold == threshold)
	case api.GreaterThanEqual:
		return (recoveryOperator == api.LessThan || recoveryOperator == api.LessThanEqual) && recoveryThreshold < threshold
	case api.LessThan, api.LessThanEqual:
		return (recoveryOperator == api.GreaterThan || recoveryOperator == api.GreaterThanEqual) && recoveryThreshold > threshold
	case api.Equal, api.NotEqual:
		return operator != recoveryOperator || threshold != recoveryThreshold
	default:
		return false
	}
}

func invalidAlertRule(fieldErrors []fieldError) api.CreateAlertRule400JSONResponse {
	return api.CreateAlertRule400JSONResponse(alertRuleValidationError(fieldErrors))
}

func alertRuleValidationError(fieldErrors []fieldError) api.Error {
	return validationErrorBody("alert rule validation failed", fieldErrors)
}

func toAPIAlertRule(ctx context.Context, queries *alerting.Queries, rule alerting.AlertRule, scopedInstanceIDs []pgtype.UUID) (api.AlertRule, error) {
	var policy alerting.NotificationPolicy
	var err error
	if rule.NotificationPolicyID.Valid {
		policy, err = queries.GetNotificationPolicy(ctx, rule.NotificationPolicyID)
	} else {
		policy, err = queries.GetDefaultNotificationPolicy(ctx)
	}
	if err != nil {
		return api.AlertRule{}, err
	}
	stats, err := queries.GetAlertRuleStats(ctx, rule.ID)
	if err != nil {
		return api.AlertRule{}, err
	}
	policyName := policy.Name
	if !rule.NotificationPolicyID.Valid {
		policyName += "（继承）"
	}
	result := api.AlertRule{
		Id:                              rule.ID.Bytes,
		Name:                            rule.Name,
		MetricId:                        rule.MetricID,
		Aggregation:                     api.AlertAggregation(rule.Aggregation),
		Operator:                        api.AlertOperator(rule.Operator),
		Threshold:                       rule.Threshold,
		RecoveryOperator:                api.AlertOperator(rule.RecoveryOperator),
		RecoveryThreshold:               rule.RecoveryThreshold,
		WindowSeconds:                   int(rule.WindowSeconds),
		ConsecutiveCount:                int(rule.ConsecutiveCount),
		RecoveryConsecutiveCount:        int(rule.RecoveryConsecutiveCount),
		Severity:                        api.AlertSeverity(rule.Severity),
		NoDataPolicy:                    api.NoDataPolicy(rule.NoDataPolicy),
		Scope:                           api.AlertRuleScope(rule.Scope),
		InstanceIds:                     toAPIUUIDs(scopedInstanceIDs),
		EvaluationIntervalSeconds:       int(rule.EvaluationIntervalSeconds),
		Enabled:                         rule.Enabled,
		IsBuiltin:                       rule.BuiltinIdentifier.Valid,
		EffectiveNotificationPolicyName: policyName,
		CurrentAlertCount:               int(stats.CurrentAlertCount),
		Version:                         int(rule.Version),
		CreatedAt:                       rule.CreatedAt.Time,
		UpdatedAt:                       rule.UpdatedAt.Time,
		CreatedBy:                       toAPIOptionalUUID(rule.CreatedBy),
		UpdatedBy:                       toAPIOptionalUUID(rule.UpdatedBy),
		EnabledUpdatedBy:                toAPIOptionalUUID(rule.EnabledUpdatedBy),
	}
	if stats.LastTriggeredAt.Valid {
		value := stats.LastTriggeredAt.Time
		result.LastTriggeredAt = &value
	}
	if rule.EnabledUpdatedAt.Valid {
		value := rule.EnabledUpdatedAt.Time
		result.EnabledUpdatedAt = &value
	}
	if rule.BuiltinIdentifier.Valid {
		value := rule.BuiltinIdentifier.String
		result.BuiltinIdentifier = &value
	}
	if rule.NotificationPolicyID.Valid {
		value := openapi_types.UUID(rule.NotificationPolicyID.Bytes)
		result.NotificationPolicyId = &value
	}
	if rule.SourceTemplateID.Valid {
		value := rule.SourceTemplateID.String
		result.SourceTemplateId = &value
	}
	if rule.SourceTemplateVersion.Valid {
		value := int(rule.SourceTemplateVersion.Int32)
		result.SourceTemplateVersion = &value
	}
	return result, nil
}

func toAPIAlertRuleTemplate(template alerting.AlertRuleTemplate) api.AlertRuleTemplate {
	return api.AlertRuleTemplate{
		Id:                        template.Identifier,
		Version:                   int(template.Version),
		Name:                      template.Name,
		MetricId:                  template.MetricID,
		Aggregation:               api.AlertAggregation(template.Aggregation),
		Operator:                  api.AlertOperator(template.Operator),
		Threshold:                 template.Threshold,
		RecoveryOperator:          api.AlertOperator(template.RecoveryOperator),
		RecoveryThreshold:         template.RecoveryThreshold,
		WindowSeconds:             int(template.WindowSeconds),
		ConsecutiveCount:          int(template.ConsecutiveCount),
		RecoveryConsecutiveCount:  int(template.RecoveryConsecutiveCount),
		Severity:                  api.AlertSeverity(template.Severity),
		NoDataPolicy:              api.NoDataPolicy(template.NoDataPolicy),
		EvaluationIntervalSeconds: int(template.EvaluationIntervalSeconds),
	}
}
