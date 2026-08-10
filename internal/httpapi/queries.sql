-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM app_user WHERE enabled AND role = 'PLATFORM_ADMIN');

-- name: CreateAdmin :exec
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, 'PLATFORM_ADMIN')
ON CONFLICT (username) DO NOTHING;

-- name: GetUserForLogin :one
SELECT id, password_hash, role FROM app_user WHERE username = $1 AND enabled;

-- name: GetUserPassword :one
SELECT password_hash FROM app_user WHERE id = $1 AND enabled;

-- name: CreateSession :exec
INSERT INTO user_session (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetSessionUser :one
SELECT u.id, u.role
FROM user_session session
JOIN app_user u ON u.id = session.user_id
WHERE session.token_hash = $1 AND session.expires_at > $2 AND u.enabled;

-- name: GetCurrentUser :one
SELECT id, username, role, enabled, created_at
FROM app_user
WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, role, enabled, created_at
FROM app_user
ORDER BY username;

-- name: CreateUser :one
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING id, username, role, enabled, created_at;

-- name: LockEnabledPlatformAdmins :many
SELECT id
FROM app_user
WHERE enabled AND role = 'PLATFORM_ADMIN'
ORDER BY id
FOR UPDATE;

-- name: GetUserForUpdate :one
SELECT id, username, role, enabled, created_at
FROM app_user
WHERE id = $1
FOR UPDATE;

-- name: SetUserEnabled :one
UPDATE app_user
SET enabled = $2
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserRole :one
UPDATE app_user
SET role = $2
WHERE id = $1
RETURNING id, username, role, enabled, created_at;

-- name: SetUserPassword :execrows
UPDATE app_user
SET password_hash = $2
WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM user_session WHERE user_id = $1;

-- name: GetAgentTokenHash :one
SELECT agent_token_hash FROM instance WHERE id = $1;

-- name: GetInstanceAlertStatus :one
SELECT status FROM alert_instance WHERE instance_id = $1;
