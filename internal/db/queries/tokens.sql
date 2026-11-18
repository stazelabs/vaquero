-- name: CreateToken :one
INSERT INTO api_tokens (id, actor_id, name, prefix, token_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetToken :one
SELECT * FROM api_tokens WHERE id = ?;

-- name: GetTokenByHash :one
SELECT * FROM api_tokens WHERE token_hash = ?;

-- name: ListTokens :many
SELECT t.*, a.name AS actor_name FROM api_tokens t JOIN actors a ON a.id = t.actor_id
ORDER BY t.revoked_at IS NOT NULL, t.created_at DESC;

-- name: RevokeToken :exec
UPDATE api_tokens SET revoked_at = ? WHERE id = ?;

-- name: TouchToken :exec
-- Records use at most once per window: before is now minus the window.
UPDATE api_tokens SET last_used_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < sqlc.arg(before));
