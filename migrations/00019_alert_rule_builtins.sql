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

-- 模板带引擎归属：模板引用的指标属于哪个引擎，模板就属于哪个引擎，填的是哪个语义位就带哪个位。
-- 两列都是 metric_catalog 那一行的转写，由 alerting_seed.go 从 internal/alerting 的模板表落进来。
-- 可见性由这两列一起决定：带位的模板一份两用（任何绑定了这个位的引擎都看得见），
-- AGNOSTIC 的模板处处可见，其余只在本引擎的实例上可见。
CREATE TABLE alert_rule_template (
    identifier text PRIMARY KEY CHECK (identifier <> ''),
    version integer NOT NULL CHECK (version > 0),
    name text NOT NULL CHECK (name <> ''),
    metric_id text NOT NULL,
    engine text NOT NULL CHECK (engine IN ('POSTGRESQL', 'AGNOSTIC')),
    -- 与引擎无关的指标（host.* / agent.* / collector.*）不占位：它们本来就处处可用，
    -- 再给一个位只会让「容量水位」这个位在同一台实例上指向两个指标。
    semantic_slot text REFERENCES metric_semantic_slot(slot_id),
    CONSTRAINT alert_rule_template_agnostic_has_no_slot CHECK (
        engine <> 'AGNOSTIC' OR semantic_slot IS NULL
    ),
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
