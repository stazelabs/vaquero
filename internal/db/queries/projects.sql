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
UPDATE projects SET name = ?, description = ?, workflow = ?, tier_map = ?, updated_at = ? WHERE id = ?;

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

-- The projects page's stats, one query per stat across every project
-- (board.LoadProjectStats).

-- name: ListProjectStats :many
-- Per project: tickets in review, open tickets, and the newest event's time
-- ('' when it has none).
SELECT p.id AS project_id,
       CAST(coalesce(sum(s.category = 'in_review'), 0) AS INTEGER) AS in_review,
       CAST(coalesce(sum(s.category NOT IN ('done', 'cancelled')), 0) AS INTEGER) AS open_count,
       CAST(coalesce((SELECT e.at FROM events e WHERE e.project_id = p.id ORDER BY e.seq DESC LIMIT 1), '') AS TEXT) AS last_change
FROM projects p
LEFT JOIN tickets t ON t.project_id = p.id
LEFT JOIN statuses s ON s.id = t.status_id
GROUP BY p.id;

-- name: ListBoardReadyTickets :many
-- Every project's tickets that are ready by the board's rule, the ready
-- column of ListBoardTickets (TestProjectStats holds the two the same):
-- a todo ticket, unassigned and unclaimed, with no open blocker and no
-- open sub-ticket. With the project's workflow on, readiness decides too.
SELECT t.id, t.project_id, p.workflow
FROM tickets t
JOIN statuses s ON s.id = t.status_id
JOIN projects p ON p.id = t.project_id
WHERE s.category = 'todo' AND t.assignee_id IS NULL AND t.claimed_by IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM ticket_links l
      JOIN tickets bt ON bt.id = l.from_id
      JOIN statuses bs ON bs.id = bt.status_id
      WHERE l.to_id = t.id AND l.kind = 'blocks' AND bs.category NOT IN ('done', 'cancelled'))
  AND NOT EXISTS (
      SELECT 1 FROM tickets ct
      JOIN statuses cs ON cs.id = ct.status_id
      WHERE ct.parent_id = t.id AND cs.category NOT IN ('done', 'cancelled'))
ORDER BY t.id;

-- name: ListOpenMilestoneProgress :many
-- Every project's open milestones with ListMilestoneProgress's counts, in
-- its order within each project: earliest due first, undated last.
SELECT m.project_id, m.id, m.name, m.due_on,
       CAST(coalesce(sum(s.category = 'done'), 0) AS INTEGER) AS done,
       CAST(coalesce(sum(s.category <> 'cancelled'), 0) AS INTEGER) AS total
FROM milestones m
LEFT JOIN tickets t ON t.milestone_id = m.id
LEFT JOIN statuses s ON s.id = t.status_id
WHERE m.closed_at IS NULL
GROUP BY m.id
ORDER BY m.project_id, m.due_on IS NULL, m.due_on, m.name;
