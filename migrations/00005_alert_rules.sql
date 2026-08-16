-- +goose Up
CREATE TABLE alert_rule (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (name <> ''),
    metric_id text NOT NULL,
    aggregation text NOT NULL CHECK (aggregation IN ('latest', 'avg', 'max', 'min', 'sum', 'count')),
    operator text NOT NULL CHECK (operator IN ('>', '>=', '<', '<=', '=', '!=')),
    threshold double precision NOT NULL,
    recovery_operator text NOT NULL CHECK (recovery_operator IN ('>', '>=', '<', '<=', '=', '!=')),
    recovery_threshold double precision NOT NULL,
    window_seconds integer NOT NULL CHECK (window_seconds > 0),
    consecutive_count integer NOT NULL CHECK (consecutive_count > 0),
    recovery_consecutive_count integer NOT NULL CHECK (recovery_consecutive_count > 0),
    severity text NOT NULL CHECK (severity IN ('critical', 'warning', 'info')),
    no_data_policy text NOT NULL CHECK (no_data_policy IN ('ignore', 'mark_no_data')),
    enabled boolean NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alert_rule_version (
    rule_id uuid NOT NULL REFERENCES alert_rule(id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, version)
);

INSERT INTO alert_rule (
    id, name, metric_id, aggregation, operator, threshold,
    recovery_operator, recovery_threshold, window_seconds,
    consecutive_count, recovery_consecutive_count, severity,
    no_data_policy, enabled, version
) VALUES (
    '00000000-0000-0000-0000-000000000061',
    'PostgreSQL connection total', 'pg.connection.total', 'latest', '>=', 20,
    '<', 15, 5, 2, 2, 'critical', 'mark_no_data', true, 1
);

INSERT INTO alert_rule_version (rule_id, version, snapshot)
SELECT id, version, jsonb_build_object(
    'id', id, 'name', name, 'metric_id', metric_id,
    'aggregation', aggregation, 'operator', operator, 'threshold', threshold,
    'recovery_operator', recovery_operator, 'recovery_threshold', recovery_threshold,
    'window_seconds', window_seconds, 'consecutive_count', consecutive_count,
    'recovery_consecutive_count', recovery_consecutive_count, 'severity', severity,
    'no_data_policy', no_data_policy, 'enabled', enabled, 'version', version
)
FROM alert_rule;

ALTER TABLE alert_instance
    ADD COLUMN id uuid DEFAULT gen_random_uuid(),
    ADD COLUMN rule_id uuid REFERENCES alert_rule(id),
    ADD COLUMN rule_version integer,
    ADD COLUMN severity text,
    ADD COLUMN current_value double precision,
    ADD COLUMN rule_snapshot jsonb,
    ADD COLUMN metric_dimension_key text NOT NULL DEFAULT '{}';

UPDATE alert_instance
SET rule_id = '00000000-0000-0000-0000-000000000061',
    rule_version = 1,
    severity = 'critical',
    rule_snapshot = (SELECT snapshot FROM alert_rule_version
                     WHERE rule_id = '00000000-0000-0000-0000-000000000061' AND version = 1);

ALTER TABLE alert_instance
    DROP CONSTRAINT alert_instance_pkey,
    DROP CONSTRAINT alert_instance_metric_id_check,
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN rule_id SET NOT NULL,
    ALTER COLUMN rule_version SET NOT NULL,
    ALTER COLUMN severity SET NOT NULL,
    ALTER COLUMN rule_snapshot SET NOT NULL,
    ADD PRIMARY KEY (id),
    ADD UNIQUE (rule_id, instance_id, metric_dimension_key);

CREATE TABLE alert_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_instance_id uuid NOT NULL REFERENCES alert_instance(id) ON DELETE CASCADE,
    rule_id uuid NOT NULL REFERENCES alert_rule(id),
    rule_version integer NOT NULL CHECK (rule_version > 0),
    kind text NOT NULL CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED'
    )),
    from_state text NOT NULL CHECK (from_state IN ('OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED')),
    to_state text NOT NULL CHECK (to_state IN ('OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED')),
    current_value double precision,
    unavailability text,
    rule_snapshot jsonb NOT NULL,
    evaluated_at timestamptz NOT NULL
);

CREATE INDEX alert_event_instance_evaluated_idx
    ON alert_event (alert_instance_id, evaluated_at DESC);
