-- +goose Up
CREATE TABLE query_statistics_snapshot (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    sampled_at timestamptz NOT NULL,
    PRIMARY KEY (instance_id, sampled_at)
);

CREATE TABLE query_statistics_snapshot_entry (
    instance_id uuid NOT NULL,
    sampled_at timestamptz NOT NULL,
    queryid bigint NOT NULL,
    database_oid oid NOT NULL,
    user_oid oid NOT NULL,
    calls bigint NOT NULL CHECK (calls >= 0),
    total_exec_time_ms double precision NOT NULL CHECK (total_exec_time_ms >= 0),
    PRIMARY KEY (instance_id, sampled_at, queryid, database_oid, user_oid),
    FOREIGN KEY (instance_id, sampled_at)
        REFERENCES query_statistics_snapshot(instance_id, sampled_at) ON DELETE CASCADE
);
CREATE INDEX query_statistics_snapshot_instance_sampled_at_idx
    ON query_statistics_snapshot (instance_id, sampled_at DESC);

-- 归一化 SQL 文本，按 (实例, queryid) 去重存一份。
--
-- **只存归一化文本。** 这里落的是 pg_stat_statements 的 query：字面量已经被换成 $1、$2
-- 占位符，这是该扩展的设计保证。pg_stat_activity 的语句是带真实字面量的原文，可能含
-- 身份证号、手机号、口令——**任何情况下都不落库**，长查询采样（long_query_sample）因此
-- 至今没有、也不会有存 SQL 文本的列。
--
-- 与快照分表而不是给 query_statistics_snapshot_entry 加一列：500 台 × 每台 Top 500 条 ×
-- 均值 300 字节，每五分钟一次快照都存全文会迅速失控（~75MB 起步、每天翻几番）。
-- 文本本身几乎不变，按 (instance_id, queryid) 去重后总量只随「见过多少条不同的语句」增长。
--
-- 表名与列名刻意不带引擎字样：MySQL 的 events_statements_summary_by_digest.digest_text
-- 是同性质的归一化文本，落到这张表上不需要再造一张。
--
-- 没有保留期清理任务，也不该有：这张表的行数由「见过多少条不同的语句」封顶，
-- 不随时间线性增长，而实例删除时它随 instance 一起 CASCADE 掉。
CREATE TABLE query_statement_text (
    instance_id uuid NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    queryid bigint NOT NULL,
    query_text text NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (instance_id, queryid)
);
