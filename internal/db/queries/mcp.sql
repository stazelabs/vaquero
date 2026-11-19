-- name: PruneMCPCalls :execrows
DELETE FROM mcp_calls WHERE at < ?;

-- name: RecordMCPCall :exec
INSERT INTO mcp_calls (id, at, actor_id, token_prefix, tool, args, outcome, message, duration_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListMCPCalls :many
-- Newest first, optionally one agent (by name) and one tool, older than
-- the call with id before (ULIDs sort by time).
SELECT c.id, c.at, c.token_prefix, c.tool, c.args, c.outcome, c.message, c.duration_ms,
       coalesce(a.name, '') AS actor_name, coalesce(a.kind, '') AS actor_kind
FROM mcp_calls c
LEFT JOIN actors a ON a.id = c.actor_id
WHERE (sqlc.narg(actor) IS NULL OR a.name = sqlc.narg(actor))
  AND (sqlc.narg(tool) IS NULL OR c.tool = sqlc.narg(tool))
  AND (sqlc.narg(before) IS NULL OR c.id < sqlc.narg(before))
ORDER BY c.id DESC
LIMIT sqlc.arg(lim);

-- name: ListMCPCallActors :many
SELECT DISTINCT a.name FROM mcp_calls c JOIN actors a ON a.id = c.actor_id ORDER BY a.name;

-- name: ListMCPCallTools :many
SELECT DISTINCT tool FROM mcp_calls ORDER BY tool;

-- name: NewestMCPCall :one
SELECT CAST(coalesce(max(id), '') AS TEXT) FROM mcp_calls;
