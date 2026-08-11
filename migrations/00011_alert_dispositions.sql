-- +goose Up
ALTER TABLE alert_instance
    ADD COLUMN disposition text NOT NULL DEFAULT 'NONE'
        CHECK (disposition IN ('NONE', 'ACKED', 'IGNORED')),
    ADD COLUMN disposition_by uuid REFERENCES app_user(id),
    ADD COLUMN disposition_at timestamptz,
    ADD COLUMN disposition_note text,
    ADD COLUMN ignore_reason_code text
        CHECK (ignore_reason_code IS NULL OR ignore_reason_code IN (
            'KNOWN_ISSUE', 'FALSE_POSITIVE', 'DUPLICATE', 'IMPACT_ACCEPTABLE', 'OTHER'
        )),
    ADD COLUMN ignore_reason_detail text,
    ADD CONSTRAINT alert_instance_disposition_summary_check CHECK (
        (disposition = 'NONE'
            AND disposition_by IS NULL AND disposition_at IS NULL
            AND disposition_note IS NULL AND ignore_reason_code IS NULL AND ignore_reason_detail IS NULL)
        OR (disposition = 'ACKED'
            AND disposition_by IS NOT NULL AND disposition_at IS NOT NULL
            AND ignore_reason_code IS NULL AND ignore_reason_detail IS NULL)
        OR (disposition = 'IGNORED'
            AND disposition_by IS NOT NULL AND disposition_at IS NOT NULL
            AND disposition_note IS NULL AND ignore_reason_code IS NOT NULL
            AND (ignore_reason_code <> 'OTHER'
                OR (ignore_reason_detail IS NOT NULL AND btrim(ignore_reason_detail) <> '')))
    );

ALTER TABLE alert_event
    DROP CONSTRAINT alert_event_kind_check,
    ADD CONSTRAINT alert_event_kind_check CHECK (kind IN (
        'PENDING_STARTED', 'FIRED', 'UPDATED', 'RECOVERED',
        'NO_DATA_ENTERED', 'NO_DATA_EXITED', 'ACKED', 'IGNORED'
    )),
    ADD COLUMN actor_id uuid REFERENCES app_user(id),
    ADD COLUMN acted_at timestamptz,
    ADD COLUMN from_disposition text
        CHECK (from_disposition IS NULL OR from_disposition IN ('NONE', 'ACKED', 'IGNORED')),
    ADD COLUMN to_disposition text
        CHECK (to_disposition IS NULL OR to_disposition IN ('ACKED', 'IGNORED')),
    ADD COLUMN disposition_note text,
    ADD COLUMN ignore_reason_code text
        CHECK (ignore_reason_code IS NULL OR ignore_reason_code IN (
            'KNOWN_ISSUE', 'FALSE_POSITIVE', 'DUPLICATE', 'IMPACT_ACCEPTABLE', 'OTHER'
        )),
    ADD COLUMN ignore_reason_detail text,
    ADD CONSTRAINT alert_event_disposition_check CHECK (
        (kind NOT IN ('ACKED', 'IGNORED')
            AND actor_id IS NULL AND acted_at IS NULL
            AND from_disposition IS NULL AND to_disposition IS NULL
            AND disposition_note IS NULL AND ignore_reason_code IS NULL AND ignore_reason_detail IS NULL)
        OR (kind = 'ACKED'
            AND actor_id IS NOT NULL AND acted_at IS NOT NULL
            AND from_disposition IS NOT NULL AND to_disposition = 'ACKED'
            AND ignore_reason_code IS NULL AND ignore_reason_detail IS NULL)
        OR (kind = 'IGNORED'
            AND actor_id IS NOT NULL AND acted_at IS NOT NULL
            AND from_disposition IS NOT NULL AND to_disposition = 'IGNORED'
            AND disposition_note IS NULL AND ignore_reason_code IS NOT NULL
            AND (ignore_reason_code <> 'OTHER'
                OR (ignore_reason_detail IS NOT NULL AND btrim(ignore_reason_detail) <> '')))
    );

CREATE INDEX alert_event_disposition_history_idx
    ON alert_event (alert_instance_id, acted_at, id)
    WHERE kind IN ('ACKED', 'IGNORED');
