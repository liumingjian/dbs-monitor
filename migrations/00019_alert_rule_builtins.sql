-- +goose Up
CREATE TABLE notification_policy (
    id uuid PRIMARY KEY,
    identifier text NOT NULL UNIQUE CHECK (identifier <> ''),
    name text NOT NULL CHECK (name <> ''),
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notification_policy_single_default_idx
    ON notification_policy (is_default)
    WHERE is_default;

-- +goose StatementBegin
CREATE FUNCTION protect_default_notification_policy() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.is_default THEN
        RAISE EXCEPTION 'the default notification policy cannot be removed'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.is_default AND NOT NEW.is_default THEN
        RAISE EXCEPTION 'the default notification policy cannot be removed'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER notification_policy_default_guard
BEFORE DELETE OR UPDATE OF is_default ON notification_policy
FOR EACH ROW EXECUTE FUNCTION protect_default_notification_policy();

ALTER TABLE alert_rule
    ADD COLUMN builtin_identifier text UNIQUE,
    ADD COLUMN notification_policy_id uuid REFERENCES notification_policy(id) ON DELETE RESTRICT,
    ADD COLUMN source_template_id text,
    ADD COLUMN source_template_version integer CHECK (source_template_version > 0),
    ADD CONSTRAINT alert_rule_template_source_pair CHECK (
        (source_template_id IS NULL) = (source_template_version IS NULL)
    );

CREATE TABLE alert_rule_template (
    identifier text PRIMARY KEY CHECK (identifier <> ''),
    version integer NOT NULL CHECK (version > 0),
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
    evaluation_interval_seconds integer NOT NULL CHECK (evaluation_interval_seconds >= 5)
);
