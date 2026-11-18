-- name: CreateStatus :one
INSERT INTO statuses (id, project_id, name, category, position) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetStatus :one
SELECT * FROM statuses WHERE id = ?;

-- name: ListStatuses :many
SELECT * FROM statuses WHERE project_id = ? ORDER BY position;

-- name: RenameStatus :exec
UPDATE statuses SET name = ? WHERE id = ?;

-- name: SetStatusPosition :exec
UPDATE statuses SET position = ? WHERE id = ?;

-- name: DeleteStatus :exec
DELETE FROM statuses WHERE id = ?;

-- name: CountTicketsInStatus :one
SELECT count(*) FROM tickets WHERE status_id = ?;

-- name: CountStatusesInCategory :one
SELECT count(*) FROM statuses WHERE project_id = ? AND category = ?;

-- name: MaxStatusPosition :one
SELECT CAST(coalesce(max(position), 0) AS INTEGER) FROM statuses WHERE project_id = ?;
