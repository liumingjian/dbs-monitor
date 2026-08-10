-- name: CreateInstance :one
WITH created AS (
    INSERT INTO instance (id, name, host, port, database_name, username, password, agent_token_hash)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    RETURNING id, name, host, port, database_name, username, created_at
), configured AS (
    INSERT INTO instance_collection_config (instance_id)
    SELECT id FROM created
    RETURNING instance_id
)
SELECT created.id, created.name, created.host, created.port, created.database_name, created.username, created.created_at
FROM created
JOIN configured ON configured.instance_id = created.id;

-- name: ListInstances :many
SELECT id, name, host, port, database_name, username, created_at
FROM instance
ORDER BY name, id;

-- name: GetInstance :one
SELECT id, name, host, port, database_name, username, password, agent_token_hash, created_at
FROM instance
WHERE id = $1;

-- name: UpdateInstance :one
UPDATE instance
SET name = $2, host = $3, port = $4, database_name = $5, username = $6, password = $7
WHERE id = $1
RETURNING id, name, host, port, database_name, username, created_at;

-- name: DeleteInstance :exec
DELETE FROM instance WHERE id = $1;

-- name: ListCollectionTargets :many
SELECT id, host, port, database_name, username, password
FROM instance
ORDER BY id;

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
