-- name: CreateInstance :one
WITH created_identity AS (
    INSERT INTO instance_identity (id, name)
    VALUES ($1, $2)
    RETURNING id
), created AS (
    INSERT INTO instance (id, name, engine, host, port, database_name, username, password_ciphertext, password_key_version, created_by)
    SELECT created_identity.id, $2, $3, $4, $5, $6, $7, $8, $9, $10
    FROM created_identity
    RETURNING id, name, engine, host, port, database_name, username, agent_version, created_at
), configured AS (
    INSERT INTO instance_collection_config (instance_id)
    SELECT id FROM created
    RETURNING instance_id
)
SELECT created.id, created.name, created.engine, created.host, created.port, created.database_name, created.username, created.agent_version, created.created_at
FROM created
JOIN configured ON configured.instance_id = created.id;

-- name: ListInstances :many
SELECT instance.id, instance.name, instance.engine, instance.host, instance.port, instance.database_name,
       instance.username, instance.agent_version, instance.created_at,
       instance.agent_expected,
       config.agent_metrics_enabled,
       config.collection_paused, config.collection_pause_updated_by,
       config.collection_pause_updated_at, config.collection_pause_reason,
       server_state.last_success_at AS collector_last_success_at,
       agent_state.last_report_at AS agent_last_report_at,
       agent_state.last_error_code AS agent_last_error_code,
       capability.observed_at AS capability_observed_at,
       capability.states AS capability_states
FROM instance
JOIN instance_collection_config config ON config.instance_id = instance.id
LEFT JOIN instance_collect_state server_state
    ON server_state.instance_id = instance.id AND server_state.source = 'SERVER_DIRECT'
LEFT JOIN instance_collect_state agent_state
    ON agent_state.instance_id = instance.id AND agent_state.source = 'AGENT'
LEFT JOIN instance_capability_snapshot capability ON capability.instance_id = instance.id
ORDER BY name, id;

-- name: GetInstance :one
SELECT instance.id, instance.name, instance.engine, instance.host, instance.port, instance.database_name,
       instance.username, instance.agent_version, instance.created_at,
       instance.agent_expected,
       config.agent_metrics_enabled,
       config.collection_paused, config.collection_pause_updated_by,
       config.collection_pause_updated_at, config.collection_pause_reason,
       server_state.last_success_at AS collector_last_success_at,
       agent_state.last_report_at AS agent_last_report_at,
       agent_state.last_error_code AS agent_last_error_code,
       capability.observed_at AS capability_observed_at,
       capability.states AS capability_states
FROM instance
JOIN instance_collection_config config ON config.instance_id = instance.id
LEFT JOIN instance_collect_state server_state
    ON server_state.instance_id = instance.id AND server_state.source = 'SERVER_DIRECT'
LEFT JOIN instance_collect_state agent_state
    ON agent_state.instance_id = instance.id AND agent_state.source = 'AGENT'
LEFT JOIN instance_capability_snapshot capability ON capability.instance_id = instance.id
WHERE instance.id = $1;

-- name: GetInstanceForUpdate :one
SELECT engine, host, port, database_name, username, password_ciphertext, password_key_version
FROM instance
WHERE id = $1
FOR UPDATE;

-- name: LockInstanceForRemoval :one
SELECT id
FROM instance
WHERE id = $1
FOR UPDATE;

-- name: UpdateInstanceMetadata :one
WITH updated_identity AS (
    UPDATE instance_identity
    SET name = $2
    WHERE instance_identity.id = $1
    RETURNING instance_identity.id
)
UPDATE instance
SET name = $2,
    host = $3,
    port = $4,
    database_name = $5,
    credential_version = credential_version + CASE
        WHEN host <> $3 OR port <> $4 OR database_name IS DISTINCT FROM $5 THEN 1
        ELSE 0
    END
FROM updated_identity
WHERE instance.id = updated_identity.id
RETURNING instance.id, instance.name, instance.engine, instance.host, instance.port, instance.database_name,
          instance.username, instance.agent_version;

-- name: GetAgentRegistration :one
SELECT agent_expected, agent_token_issued_at, agent_token_revoked_at,
       agent_first_registered_at, agent_version,
       (SELECT state.last_report_at
        FROM instance_collect_state state
        WHERE state.instance_id = instance.id AND state.source = 'AGENT') AS last_reported_at
FROM instance
WHERE id = $1;

-- name: RegisterAgent :one
UPDATE instance
SET agent_expected = true,
    agent_token_hash = $2,
    agent_token_issued_at = $3,
    agent_token_revoked_at = NULL,
    agent_first_registered_at = COALESCE(agent_first_registered_at, $3)
WHERE id = $1
  AND NOT agent_expected
  AND agent_token_hash IS NULL
RETURNING agent_expected, agent_token_issued_at, agent_token_revoked_at,
          agent_first_registered_at, agent_version;

-- name: RotateAgentToken :one
UPDATE instance
SET agent_token_hash = $2,
    agent_token_issued_at = $3,
    agent_token_revoked_at = NULL
WHERE id = $1
  AND agent_expected
  AND agent_token_hash IS NOT NULL
  AND agent_token_revoked_at IS NULL
RETURNING agent_expected, agent_token_issued_at, agent_token_revoked_at,
          agent_first_registered_at, agent_version;

-- name: RevokeAgentToken :one
UPDATE instance
SET agent_token_hash = NULL,
    agent_token_revoked_at = $2
WHERE id = $1
  AND agent_expected
  AND agent_token_hash IS NOT NULL
  AND agent_token_revoked_at IS NULL
RETURNING agent_expected, agent_token_issued_at, agent_token_revoked_at,
          agent_first_registered_at, agent_version;

-- name: DisableAgent :one
UPDATE instance
SET agent_expected = false,
    agent_token_hash = NULL
WHERE id = $1
  AND agent_expected
RETURNING agent_expected, agent_token_issued_at, agent_token_revoked_at,
          agent_first_registered_at, agent_version;

-- name: UpdateInstanceCredential :one
UPDATE instance
SET username = $2,
    password_ciphertext = $3,
    password_key_version = $4,
    credential_updated_by = $5,
    credential_updated_at = $6,
    credential_version = credential_version + 1
WHERE id = $1
RETURNING username;

-- name: DeleteInstance :exec
WITH deleted_instance AS (
    DELETE FROM instance
    WHERE instance.id = $1
    RETURNING instance.id, instance.name
)
UPDATE instance_identity identity
SET name = deleted_instance.name,
    removed_at = $2
FROM deleted_instance
WHERE identity.id = deleted_instance.id;

-- name: ListCollectionTargets :many
SELECT id, host, port, database_name, username, password_ciphertext, password_key_version, credential_version
FROM instance
JOIN instance_collection_config config ON config.instance_id = instance.id
WHERE NOT config.collection_paused
ORDER BY id;

-- name: CountCredentialsNotUsingKeyVersion :one
SELECT (SELECT count(*) FROM instance WHERE password_key_version <> sqlc.arg(key_version))
     + (SELECT count(*) FROM smtp_channel
        WHERE auth_key_version IS NOT NULL AND auth_key_version <> sqlc.arg(key_version))
     + (SELECT count(*) FROM webhook_target
        WHERE signing_key_version <> sqlc.arg(key_version));

-- name: ListCredentialsForKeyRotation :many
SELECT id, password_ciphertext, password_key_version
FROM instance
ORDER BY id
FOR UPDATE;

-- name: UpdateCredentialKeyVersion :exec
UPDATE instance
SET password_ciphertext = $2,
    password_key_version = $3
WHERE id = $1;

-- name: CountCredentialKeyReferences :one
SELECT (SELECT count(*) FROM instance WHERE password_key_version = sqlc.arg(key_version))
     + (SELECT count(*) FROM smtp_channel WHERE auth_key_version = sqlc.arg(key_version))
     + (SELECT count(*) FROM webhook_target WHERE signing_key_version = sqlc.arg(key_version));

-- name: SetCollectSuccess :exec
INSERT INTO instance_collect_state (instance_id, source, last_success_at)
VALUES ($1, 'SERVER_DIRECT', $2)
ON CONFLICT (instance_id, source)
DO UPDATE SET last_success_at = EXCLUDED.last_success_at,
              last_error_code = NULL,
              last_error_message = NULL;

-- name: SetCollectFailure :exec
INSERT INTO instance_collect_state (instance_id, source, last_error_code, last_error_message)
VALUES ($1, 'SERVER_DIRECT', $2, $3)
ON CONFLICT (instance_id, source)
DO UPDATE SET last_error_code = EXCLUDED.last_error_code,
              last_error_message = EXCLUDED.last_error_message;

-- name: GetCollectState :one
SELECT last_success_at, last_error_code
FROM instance_collect_state
WHERE instance_id = $1 AND source = 'SERVER_DIRECT';
