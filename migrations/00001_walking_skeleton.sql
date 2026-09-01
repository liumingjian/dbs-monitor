-- +goose Up
CREATE TABLE app_user (
    id uuid PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash bytea NOT NULL,
    role text NOT NULL CHECK (role IN ('READONLY', 'ALERT_ADMIN', 'PLATFORM_ADMIN'))
);

CREATE TABLE user_session (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

-- 一个实例是一个数据库服务端点、一条连接，可以包含多个库；库只是指标的一个维度。
CREATE TABLE instance (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    -- 实例运行的数据库产品。决定哪些采集任务适用、哪些指标存在，接入之后不可改。
    engine text NOT NULL DEFAULT 'POSTGRESQL' CHECK (engine IN ('POSTGRESQL')),
    host text NOT NULL,
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    -- bootstrap database：建立连接用的库名，不限定被监控的范围。
    -- PostgreSQL 必须连到某个库（留空时按 'postgres' 落库）；MySQL 没有这个概念，留空。
    database_name text,
    username text NOT NULL,
    password text NOT NULL,
    agent_token_hash bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT instance_bootstrap_database_required
        CHECK (engine <> 'POSTGRESQL' OR database_name IS NOT NULL)
);

CREATE TABLE instance_collect_state (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    source text NOT NULL CHECK (source IN ('SERVER_DIRECT', 'AGENT')),
    last_success_at timestamptz,
    last_report_at timestamptz,
    last_error_code text,
    last_error_message text,
    PRIMARY KEY (instance_id, source)
);

-- 指标目录。原来这里是一串写死在 metric_series 上的 metric_id CHECK 枚举：加一个指标要动迁移，
-- 接入第二个引擎时会变成每周一次。目录改成数据，行由 migrations.reconcileMetricCatalog 从
-- internal/metric 的字典同步进来（内置告警规则也是这个路子，见 alerting_seed.go）。
CREATE TABLE metric_semantic_slot (
    slot_id text PRIMARY KEY,
    display_name text NOT NULL
);

CREATE TABLE metric_catalog (
    metric_id text PRIMARY KEY,
    engine text NOT NULL CHECK (engine IN ('POSTGRESQL', 'AGNOSTIC')),
    unit text NOT NULL,
    display_name text NOT NULL,
    semantic_slot text REFERENCES metric_semantic_slot(slot_id),
    level text NOT NULL CHECK (level IN ('INSTANCE', 'DATABASE')),
    aggregation text NOT NULL CHECK (aggregation IN ('NONE', 'SUM', 'WEIGHTED_AVERAGE')),
    -- 实例级指标没有可聚合的东西；库级指标必须说清楚怎么收敛成实例级。
    CONSTRAINT metric_catalog_aggregation_matches_level CHECK (
        (level = 'INSTANCE' AND aggregation = 'NONE')
        OR (level = 'DATABASE' AND aggregation <> 'NONE')
    ),
    -- 一个语义位在一个引擎下最多只能落到一个指标，否则「位 + 引擎 -> 指标 ID」不是函数。
    UNIQUE (semantic_slot, engine)
);

CREATE TABLE metric_series (
    series_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    metric_id text NOT NULL REFERENCES metric_catalog(metric_id),
    labels jsonb NOT NULL DEFAULT '{}',
    labels_key text NOT NULL,
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen timestamptz NOT NULL,
    UNIQUE (instance_id, metric_id, labels_key)
);

CREATE TABLE metric_sample (
    series_id bigint NOT NULL,
    ts timestamptz NOT NULL,
    value double precision NOT NULL
) PARTITION BY RANGE (ts);
CREATE INDEX metric_sample_series_ts_idx ON metric_sample (series_id, ts DESC);

CREATE TABLE alert_instance (
    instance_id uuid PRIMARY KEY REFERENCES instance(id) ON DELETE CASCADE,
    metric_id text NOT NULL CHECK (metric_id = 'pg.connection.total'),
    status text NOT NULL CHECK (status IN ('OK', 'PENDING', 'FIRING', 'NO_DATA', 'RECOVERED')),
    breach_count integer NOT NULL DEFAULT 0 CHECK (breach_count >= 0),
    recovery_count integer NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
    no_data_count integer NOT NULL DEFAULT 0 CHECK (no_data_count >= 0),
    state_before_no_data text CHECK (state_before_no_data IS NULL OR state_before_no_data IN ('OK', 'PENDING', 'FIRING')),
    unavailability text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
