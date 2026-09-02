package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/liumingjian/dbs-monitor/internal/alerting"
)

func reconcileAlertingSeeds(ctx context.Context, database *sql.DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alerting seed transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_policy (id, identifier, name, is_default)
		VALUES ($1, $2, $3, true)
		ON CONFLICT DO NOTHING`,
		alerting.DefaultNotificationPolicyID,
		alerting.DefaultNotificationPolicyIdentifier,
		alerting.DefaultNotificationPolicyName,
	); err != nil {
		return fmt.Errorf("seed default notification policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_policy_channel (policy_id, channel)
		SELECT id, 'SMTP' FROM notification_policy WHERE is_default
		ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("seed default notification policy channel: %w", err)
	}
	var defaultPolicyName string
	if err := tx.QueryRowContext(ctx, "SELECT name FROM notification_policy WHERE is_default").Scan(&defaultPolicyName); err != nil {
		return fmt.Errorf("read default notification policy: %w", err)
	}

	for _, rule := range alerting.BuiltinCollectionRules {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule (
			id, name, metric_id, aggregation, operator, threshold,
			recovery_operator, recovery_threshold, window_seconds,
			consecutive_count, recovery_consecutive_count, severity,
			no_data_policy, enabled, version, scope,
			evaluation_interval_seconds, builtin_identifier
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14, 1, 'ALL', $15, $16
		) ON CONFLICT DO NOTHING`,
			rule.ID, rule.Name, rule.MetricID, rule.Aggregation, rule.Operator, rule.Threshold,
			rule.RecoveryOperator, rule.RecoveryThreshold, rule.WindowSeconds,
			rule.ConsecutiveCount, rule.RecoveryConsecutiveCount, rule.Severity,
			rule.NoDataPolicy, rule.Enabled, rule.EvaluationIntervalSeconds, rule.Identifier,
		); err != nil {
			return fmt.Errorf("seed built-in alert rule %q: %w", rule.Identifier, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_version (rule_id, version, snapshot)
			SELECT id, version, to_jsonb(alert_rule) || jsonb_build_object(
				'instance_ids', jsonb_build_array(),
				'is_builtin', true,
				'effective_notification_policy_name', $2 || '（继承）'
			)
			FROM alert_rule
			WHERE id = $1
			ON CONFLICT DO NOTHING`, rule.ID, defaultPolicyName); err != nil {
			return fmt.Errorf("seed built-in alert rule version %q: %w", rule.Identifier, err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM alert_rule_template"); err != nil {
		return fmt.Errorf("replace alert rule templates: %w", err)
	}
	for _, template := range alerting.BuiltinRuleTemplates {
		var semanticSlot any
		if template.Slot != "" {
			semanticSlot = template.Slot
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_template (
			identifier, version, name, metric_id, engine, semantic_slot, aggregation, operator, threshold,
			recovery_operator, recovery_threshold, window_seconds, consecutive_count,
			recovery_consecutive_count, severity, no_data_policy, evaluation_interval_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
			template.Identifier, template.Version, template.Name, template.MetricID,
			template.Engine.String(), semanticSlot,
			template.Aggregation, template.Operator, template.Threshold,
			template.RecoveryOperator, template.RecoveryThreshold, template.WindowSeconds,
			template.ConsecutiveCount, template.RecoveryConsecutiveCount,
			template.Severity, template.NoDataPolicy, template.EvaluationIntervalSeconds,
		); err != nil {
			return fmt.Errorf("seed alert rule template %q: %w", template.Identifier, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alerting seeds: %w", err)
	}
	return nil
}
