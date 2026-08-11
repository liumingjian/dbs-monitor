-- +goose Up
ALTER TABLE instance
    ADD COLUMN agent_expected boolean NOT NULL DEFAULT false,
    ADD COLUMN agent_token_issued_at timestamptz,
    ADD COLUMN agent_token_revoked_at timestamptz,
    ADD COLUMN agent_first_registered_at timestamptz;

UPDATE instance
SET agent_expected = true,
    agent_token_issued_at = created_at,
    agent_first_registered_at = created_at
WHERE agent_token_hash IS NOT NULL;

ALTER TABLE instance
    ADD CONSTRAINT instance_agent_token_hash_size_check
        CHECK (agent_token_hash IS NULL OR octet_length(agent_token_hash) = 32),
    ADD CONSTRAINT instance_agent_registration_check CHECK (
        (agent_first_registered_at IS NULL AND NOT agent_expected
            AND agent_token_hash IS NULL AND agent_token_issued_at IS NULL
            AND agent_token_revoked_at IS NULL)
        OR agent_first_registered_at IS NOT NULL
    ),
    ADD CONSTRAINT instance_agent_disabled_token_check
        CHECK (agent_expected OR agent_token_hash IS NULL),
    ADD CONSTRAINT instance_agent_expected_state_check
        CHECK (NOT agent_expected OR agent_token_hash IS NOT NULL OR agent_token_revoked_at IS NOT NULL),
    ADD CONSTRAINT instance_agent_active_token_check
        CHECK (agent_token_hash IS NULL OR
            (agent_expected AND agent_token_issued_at IS NOT NULL AND agent_token_revoked_at IS NULL));

CREATE UNIQUE INDEX instance_agent_token_hash_unique_idx
    ON instance (agent_token_hash)
    WHERE agent_token_hash IS NOT NULL;
