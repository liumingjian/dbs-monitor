-- +goose Up
ALTER TABLE alert_rule
    ADD COLUMN scope text NOT NULL DEFAULT 'ALL' CHECK (scope IN ('ALL', 'INSTANCES')),
    ADD COLUMN evaluation_interval_seconds integer NOT NULL DEFAULT 5 CHECK (evaluation_interval_seconds >= 5),
    ADD COLUMN enabled_updated_by uuid REFERENCES app_user(id),
    ADD COLUMN enabled_updated_at timestamptz;

CREATE TABLE alert_rule_scope_instance (
    rule_id uuid NOT NULL REFERENCES alert_rule(id) ON DELETE CASCADE,
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, instance_id)
);

CREATE TABLE alert_rule_evaluation_state (
    rule_id uuid NOT NULL REFERENCES alert_rule(id) ON DELETE CASCADE,
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    metric_dimension_key text NOT NULL,
    last_evaluated_at timestamptz NOT NULL,
    PRIMARY KEY (rule_id, instance_id, metric_dimension_key)
);

ALTER TABLE alert_instance
    ADD COLUMN first_triggered_at timestamptz,
    ADD COLUMN first_rule_version integer CHECK (first_rule_version > 0),
    ADD COLUMN first_rule_snapshot jsonb,
    ADD COLUMN recovered_at timestamptz;

UPDATE alert_instance
SET first_triggered_at = updated_at,
    first_rule_version = rule_version,
    first_rule_snapshot = rule_snapshot
WHERE status IN ('FIRING', 'NO_DATA', 'RECOVERED');

UPDATE alert_instance
SET recovered_at = updated_at
WHERE status = 'RECOVERED';

ALTER TABLE alert_instance
    DROP CONSTRAINT alert_instance_rule_id_instance_id_metric_dimension_key_key;

CREATE UNIQUE INDEX alert_instance_unresolved_dedup_idx
    ON alert_instance (rule_id, instance_id, metric_dimension_key)
    WHERE status <> 'RECOVERED';
