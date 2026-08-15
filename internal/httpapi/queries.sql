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

-- name: GetSessionPrincipal :one
SELECT u.id, u.role
FROM user_session session
JOIN app_user u ON u.id = session.user_id
WHERE session.token_hash = $1 AND session.expires_at > $2;

-- name: GetAgentTokenHash :one
SELECT agent_token_hash FROM instance WHERE id = $1;

-- name: GetInstanceAlertStatus :one
SELECT status FROM alert_instance WHERE instance_id = $1;

-- name: ListPersistedCollectionTaskStates :many
SELECT task.task_id, task.last_due_at, task.last_started_at, task.last_finished_at, task.last_success_at,
       task.last_result, task.consecutive_failures,
       CASE
           WHEN task.next_eligible_at IS NULL THEN connection.next_eligible_at
           WHEN connection.next_eligible_at IS NULL THEN task.next_eligible_at
           ELSE GREATEST(task.next_eligible_at, connection.next_eligible_at)
       END::timestamptz AS next_eligible_at,
       task.last_error_code, task.last_error_message
FROM instance_collection_task_state task
LEFT JOIN instance_collection_connection_state connection ON connection.instance_id = task.instance_id
WHERE task.instance_id = $1
ORDER BY task.task_id;
