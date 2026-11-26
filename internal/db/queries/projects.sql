-- name: CreateProject :one
INSERT INTO projects (id, key, name, description, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = ?;

-- name: ListProjects :many
-- Active projects first, by name; then archived ones. has_image: the
-- project has a stored icon image, in use or not.
SELECT p.*,
       (SELECT count(*) FROM tickets t JOIN statuses s ON s.id = t.status_id
         WHERE t.project_id = p.id AND s.category NOT IN ('done', 'cancelled')) AS open_count,
       CAST(EXISTS (SELECT 1 FROM project_icons i WHERE i.project_id = p.id) AS BOOLEAN) AS has_image
FROM projects p
ORDER BY p.archived_at IS NOT NULL, p.name;

-- name: UpdateProject :exec
UPDATE projects SET name = ?, description = ?, workflow = ?, tier_map = ?,
       repo_path = ?, main_branch = ?, remote = ?, fetch_minutes = ?, updated_at = ? WHERE id = ?;

-- name: SetProjectArchived :exec
UPDATE projects SET archived_at = ?, updated_at = ? WHERE id = ?;

-- name: NextTicketNumber :one
UPDATE projects SET next_number = next_number + 1 WHERE id = ? RETURNING next_number - 1;

-- TakeTicketNumber gives an imported ticket the number before next and
-- numbers on from next; it changes no row when that number was given before.
-- name: TakeTicketNumber :execrows
UPDATE projects SET next_number = sqlc.arg(next) WHERE id = sqlc.arg(id) AND next_number < sqlc.arg(next);

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

-- name: DeleteProjectOldKeys :exec
-- Old keys in the project (of tickets that moved away) and the old keys
-- elsewhere of the project's tickets (that moved in).
DELETE FROM ticket_old_keys
WHERE ticket_old_keys.project_id = sqlc.arg(pid)
   OR ticket_old_keys.ticket_id IN (SELECT t.id FROM tickets t WHERE t.project_id = sqlc.arg(pid));

-- name: RehomeMovedTicketEvents :exec
-- The project's events on tickets that have since moved to another project
-- go with the ticket, so its history outlives the old project.
UPDATE events SET project_id = (SELECT t.project_id FROM tickets t WHERE t.id = events.ticket_id)
WHERE events.project_id = sqlc.arg(pid)
  AND events.ticket_id IN (SELECT t.id FROM tickets t WHERE t.project_id <> sqlc.arg(pid));

-- name: DeleteProjectEventTickets :exec
-- The lookup rows of the events DeleteProjectEvents removes (ruling PR-2).
DELETE FROM event_tickets
WHERE event_tickets.seq IN (
    SELECT e.seq FROM events e
    WHERE e.project_id = sqlc.arg(pid)
       OR e.ticket_id IN (SELECT t.id FROM tickets t WHERE t.project_id = sqlc.arg(pid)));

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

-- name: ListProjectStats :many
-- For the projects page (board.LoadProjectStats, which reads each stat in
-- one query across every project): per project, tickets in review, open
-- tickets, and the newest event's time ('' when it has none): its own
-- events, and the moves out of it (VAQ-147), which are recorded on the
-- project the ticket moved to (events_moved_from; +p.id drops the
-- column's affinity, which would keep the index from matching).
SELECT p.id AS project_id,
       CAST(coalesce(sum(s.category = 'in_review'), 0) AS INTEGER) AS in_review,
       CAST(coalesce(sum(s.category NOT IN ('done', 'cancelled')), 0) AS INTEGER) AS open_count,
       CAST(max(coalesce((SELECT e.at FROM events e WHERE e.project_id = p.id ORDER BY e.seq DESC LIMIT 1), ''),
                coalesce((SELECT mv.at FROM events mv
                          WHERE mv.kind = 'ticket.moved' AND json_extract(mv.payload, '$.from_project_id') = +p.id
                          ORDER BY mv.seq DESC LIMIT 1), '')) AS TEXT) AS last_change
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

-- name: ListProjectKeysByIDs :many
-- The key of each given project, in one read (a page of decisions).
SELECT id, key FROM projects WHERE id IN (sqlc.slice('ids'));
