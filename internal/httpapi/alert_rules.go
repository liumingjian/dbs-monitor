package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func (handler *Handler) ListAlertRules(ctx context.Context, _ api.ListAlertRulesRequestObject) (api.ListAlertRulesResponseObject, error) {
	rules, err := alerting.New(handler.platform).ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	response := make(api.ListAlertRules200JSONResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, toAPIAlertRule(rule))
	}
	return response, nil
}

func (handler *Handler) CreateAlertRule(ctx context.Context, request api.CreateAlertRuleRequestObject) (api.CreateAlertRuleResponseObject, error) {
	if request.Body == nil {
		return invalidAlertRule([]fieldError{{field: "body", message: "is required"}}), nil
	}
	input := *request.Body
	if fieldErrors := validateAlertRule(input); len(fieldErrors) > 0 {
		return invalidAlertRule(fieldErrors), nil
	}

	now := pgtype.Timestamptz{Time: handler.clock.Now().UTC(), Valid: true}
	ruleID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	var createdRule alerting.AlertRule
	err := handler.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		rule, err := queries.CreateAlertRule(ctx, alerting.CreateAlertRuleParams{
			ID:                       ruleID,
			Name:                     strings.TrimSpace(input.Name),
			MetricID:                 input.MetricId,
			Aggregation:              string(input.Aggregation),
			Operator:                 string(input.Operator),
			Threshold:                input.Threshold,
			RecoveryOperator:         string(input.RecoveryOperator),
			RecoveryThreshold:        input.RecoveryThreshold,
			WindowSeconds:            int32(input.WindowSeconds),
			ConsecutiveCount:         int32(input.ConsecutiveCount),
			RecoveryConsecutiveCount: int32(input.RecoveryConsecutiveCount),
			Severity:                 string(input.Severity),
			NoDataPolicy:             string(input.NoDataPolicy),
			Enabled:                  input.Enabled,
			CreatedAt:                now,
		})
		if err != nil {
			return err
		}
		createdRule = rule
		snapshot, err := json.Marshal(toAPIAlertRule(rule))
		if err != nil {
			return err
		}
		return queries.CreateAlertRuleVersion(ctx, alerting.CreateAlertRuleVersionParams{
			RuleID:    rule.ID,
			Version:   rule.Version,
			Snapshot:  snapshot,
			CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return api.CreateAlertRule201JSONResponse(toAPIAlertRule(createdRule)), nil
}

type fieldError struct {
	field   string
	message string
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
	if rule.RecoveryConsecutiveCount < 1 {
		fieldErrors = append(fieldErrors, fieldError{field: "recovery_consecutive_count", message: "must be positive"})
	}
	if rule.Severity != api.Critical && rule.Severity != api.Warning && rule.Severity != api.Info {
		fieldErrors = append(fieldErrors, fieldError{field: "severity", message: "is not supported"})
	}
	if rule.NoDataPolicy != api.Ignore && rule.NoDataPolicy != api.MarkNoData {
		fieldErrors = append(fieldErrors, fieldError{field: "no_data_policy", message: "is not supported"})
	}
	if exists && definition.Type != metric.MetricTypeState && validOperator(rule.Operator) && validOperator(rule.RecoveryOperator) {
		if !validHysteresis(rule.Operator, rule.Threshold, rule.RecoveryOperator, rule.RecoveryThreshold) {
			fieldErrors = append(fieldErrors, fieldError{field: "recovery_threshold", message: "must define a separate recovery range"})
		}
	}
	if exists && definition.Type == metric.MetricTypeState &&
		rule.Operator == rule.RecoveryOperator && rule.Threshold == rule.RecoveryThreshold {
		fieldErrors = append(fieldErrors, fieldError{field: "recovery_threshold", message: "must define the opposite recovery state"})
	}
	return fieldErrors
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
	case api.GreaterThan, api.GreaterThanEqual:
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
	body := errorBody(api.VALIDATIONFAILED, "alert rule validation failed")
	errors := make([]struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}, 0, len(fieldErrors))
	for _, item := range fieldErrors {
		errors = append(errors, struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		}{Field: item.field, Message: item.message})
	}
	body.Error.FieldErrors = &errors
	return api.CreateAlertRule400JSONResponse(body)
}

func toAPIAlertRule(rule alerting.AlertRule) api.AlertRule {
	return api.AlertRule{
		Id:                       rule.ID.Bytes,
		Name:                     rule.Name,
		MetricId:                 rule.MetricID,
		Aggregation:              api.AlertAggregation(rule.Aggregation),
		Operator:                 api.AlertOperator(rule.Operator),
		Threshold:                rule.Threshold,
		RecoveryOperator:         api.AlertOperator(rule.RecoveryOperator),
		RecoveryThreshold:        rule.RecoveryThreshold,
		WindowSeconds:            int(rule.WindowSeconds),
		ConsecutiveCount:         int(rule.ConsecutiveCount),
		RecoveryConsecutiveCount: int(rule.RecoveryConsecutiveCount),
		Severity:                 api.AlertSeverity(rule.Severity),
		NoDataPolicy:             api.NoDataPolicy(rule.NoDataPolicy),
		Enabled:                  rule.Enabled,
		Version:                  int(rule.Version),
		CreatedAt:                rule.CreatedAt.Time,
		UpdatedAt:                rule.UpdatedAt.Time,
	}
}
