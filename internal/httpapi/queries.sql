-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM app_user WHERE role = 'PLATFORM_ADMIN');

-- name: CreateAdmin :exec
INSERT INTO app_user (id, username, password_hash, role)
VALUES ($1, $2, $3, 'PLATFORM_ADMIN')
ON CONFLICT (username) DO NOTHING;

-- name: GetUserForLogin :one
SELECT id, password_hash, role FROM app_user WHERE username = $1;

-- name: CreateSession :exec
INSERT INTO user_session (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetSessionRole :one
SELECT u.role
FROM user_session session
JOIN app_user u ON u.id = session.user_id
WHERE session.token_hash = $1 AND session.expires_at > $2;

-- name: GetAgentTokenHash :one
SELECT agent_token_hash FROM instance WHERE id = $1;

-- name: GetInstanceAlertStatus :one
SELECT status FROM alert_instance WHERE instance_id = $1;
