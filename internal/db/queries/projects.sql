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

-- name: SetProjectKey :exec
UPDATE projects SET key = ?, updated_at = ? WHERE id = ?;

-- The statements DeleteProject runs, in order, inside one transaction.

-- name: DeleteProjectTicketTags :exec
DELETE FROM ticket_tags WHERE ticket_id IN (SELECT id FROM tickets WHERE project_id = ?);

-- name: DeleteProjectComments :exec
DELETE FROM comments WHERE ticket_id IN (SELECT id FROM tickets WHERE project_id = ?);

-- name: DeleteProjectLinks :exec
-- Links touching the project's tickets, from any project.
DELETE FROM ticket_links
WHERE ticket_links.from_id IN (SELECT t.id FROM tickets t WHERE t.project_id = sqlc.arg(pid))
   OR ticket_links.to_id IN (SELECT t.id FROM tickets t WHERE t.project_id = sqlc.arg(pid));

-- name: DeleteProjectEvents :exec
DELETE FROM events
WHERE events.project_id = sqlc.arg(pid)
   OR events.ticket_id IN (SELECT t.id FROM tickets t WHERE t.project_id = sqlc.arg(pid));

-- name: DeleteProjectTickets :exec
DELETE FROM tickets WHERE project_id = ?;

-- name: DeleteProjectMilestones :exec
DELETE FROM milestones WHERE project_id = ?;

-- name: DeleteProjectStatuses :exec
DELETE FROM statuses WHERE project_id = ?;

-- name: DeleteProjectRow :exec
DELETE FROM projects WHERE id = ?;
