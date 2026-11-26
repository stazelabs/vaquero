-- Links between projects (VAQ-119), written only by internal/core.

-- name: CreateProjectLink :one
INSERT INTO project_links (id, from_project, to_project, kind, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetProjectLink :one
SELECT * FROM project_links WHERE id = ?;

-- name: DeleteProjectLink :exec
DELETE FROM project_links WHERE id = ?;

-- name: ListAllProjectLinks :many
-- Every project link with both projects' keys and names: the loop check
-- reads it in core, and board.LoadProjectLinks spreads it over both ends,
-- so the projects page and the API read every project's links in one
-- query.
SELECT l.id, l.kind, l.from_project, f.key AS from_key, f.name AS from_name,
       l.to_project, t.key AS to_key, t.name AS to_name
FROM project_links l
JOIN projects f ON f.id = l.from_project
JOIN projects t ON t.id = l.to_project
ORDER BY l.id;

-- name: DeleteProjectProjectLinks :exec
-- DeleteProject's step: the project's links, either way round.
DELETE FROM project_links WHERE from_project = sqlc.arg(pid) OR to_project = sqlc.arg(pid);
