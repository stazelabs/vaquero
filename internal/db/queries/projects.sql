-- name: CreateProject :one
INSERT INTO projects (id, key, name, description, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = ?;

-- name: ListProjects :many
-- Active projects first, by name; then archived ones.
SELECT p.*,
       (SELECT count(*) FROM tickets t JOIN statuses s ON s.id = t.status_id
         WHERE t.project_id = p.id AND s.category NOT IN ('done', 'cancelled')) AS open_count
FROM projects p
ORDER BY p.archived_at IS NOT NULL, p.name;

-- name: UpdateProject :exec
UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?;

-- name: SetProjectArchived :exec
UPDATE projects SET archived_at = ?, updated_at = ? WHERE id = ?;

-- name: NextTicketNumber :one
UPDATE projects SET next_number = next_number + 1 WHERE id = ? RETURNING next_number - 1;
