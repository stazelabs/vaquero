-- name: ListBoardTickets :many
-- A project's tickets for the board and list views. Every filter is
-- optional (NULL means any). ready: a todo ticket, unassigned and unclaimed, with no open
-- blocker and no open sub-ticket. ticket_id and parent_id narrow it to one
-- ticket or one ticket's sub-tickets, for the API's ticket answer.
SELECT t.id, t.number, t.title, t.priority, t.status_id, t.position, t.status_changed_at, t.version, t.tier,
       s.name AS status_name, s.category AS status_category,
       coalesce(nullif(a.display_name, ''), a.name, '') AS assignee_name, coalesce(a.kind, '') AS assignee_kind,
       coalesce(nullif(ca.display_name, ''), ca.name, '') AS claimant_name, coalesce(ca.kind, '') AS claimant_kind,
       coalesce(t.claim_expires_at, '') AS claim_expires_at,
       coalesce(t.claimed_by, '') AS claimant_id,
       coalesce(t.assignee_id, '') AS assignee_id,
       CAST(s.category = 'todo' AND t.assignee_id IS NULL AND t.claimed_by IS NULL
            AND NOT EXISTS (
                SELECT 1 FROM ticket_links l
                JOIN tickets bt ON bt.id = l.from_id
                JOIN statuses bs ON bs.id = bt.status_id
                WHERE l.to_id = t.id AND l.kind = 'blocks' AND bs.category NOT IN ('done', 'cancelled'))
            AND NOT EXISTS (
                SELECT 1 FROM tickets ct
                JOIN statuses cs ON cs.id = ct.status_id
                WHERE ct.parent_id = t.id AND cs.category NOT IN ('done', 'cancelled'))
       AS BOOLEAN) AS ready
FROM tickets t
JOIN statuses s ON s.id = t.status_id
LEFT JOIN actors a ON a.id = t.assignee_id
LEFT JOIN actors ca ON ca.id = t.claimed_by
WHERE t.project_id = sqlc.arg(project_id)
  AND (sqlc.narg(ticket_id) IS NULL OR t.id = sqlc.narg(ticket_id))
  AND (sqlc.narg(parent_id) IS NULL OR t.parent_id = sqlc.narg(parent_id))
  AND (sqlc.narg(assignee_id) IS NULL OR t.assignee_id = sqlc.narg(assignee_id))
  AND (sqlc.narg(assignee_kind) IS NULL OR a.kind = sqlc.narg(assignee_kind))
  AND (sqlc.narg(priority) IS NULL OR t.priority = sqlc.narg(priority))
  AND (sqlc.narg(milestone_id) IS NULL OR t.milestone_id = sqlc.narg(milestone_id))
  AND (CAST(sqlc.arg(no_milestone) AS BOOLEAN) = 0 OR t.milestone_id IS NULL)
  AND (sqlc.narg(tag) IS NULL OR EXISTS (
        SELECT 1 FROM ticket_tags tt JOIN tags g ON g.id = tt.tag_id
        WHERE tt.ticket_id = t.id AND g.name = sqlc.narg(tag)))
ORDER BY s.position, t.position;

-- name: ListMilestoneProgress :many
-- Per milestone: tickets done, and all tickets except cancelled ones.
SELECT m.id, m.name, m.due_on, m.closed_at,
       CAST(coalesce(sum(s.category = 'done'), 0) AS INTEGER) AS done,
       CAST(coalesce(sum(s.category <> 'cancelled'), 0) AS INTEGER) AS total
FROM milestones m
LEFT JOIN tickets t ON t.milestone_id = m.id
LEFT JOIN statuses s ON s.id = t.status_id
WHERE m.project_id = ?
GROUP BY m.id
ORDER BY m.closed_at IS NOT NULL, m.due_on IS NULL, m.due_on, m.name;

-- name: CountOpenWithoutMilestone :one
SELECT count(*) FROM tickets t JOIN statuses s ON s.id = t.status_id
WHERE t.project_id = ? AND t.milestone_id IS NULL AND s.category NOT IN ('done', 'cancelled');

-- name: ListOpenBlockers :many
-- For the Focus view's waiting reasons: each of a project's tickets with
-- an open blocker (from any project), and that blocker's key.
SELECT l.to_id AS ticket_id, bp.key AS blocker_project, bt.number AS blocker_number
FROM ticket_links l
JOIN tickets t ON t.id = l.to_id
JOIN tickets bt ON bt.id = l.from_id
JOIN statuses bs ON bs.id = bt.status_id
JOIN projects bp ON bp.id = bt.project_id
WHERE t.project_id = ? AND l.kind = 'blocks' AND bs.category NOT IN ('done', 'cancelled')
ORDER BY l.to_id, bp.key, bt.number;

-- name: ListParentsWithOpenSubTickets :many
-- For the Focus view's waiting reasons: a project's tickets with an open
-- sub-ticket (sub-tickets share their parent's project).
SELECT DISTINCT CAST(ct.parent_id AS TEXT) AS parent_id
FROM tickets ct
JOIN statuses cs ON cs.id = ct.status_id
WHERE ct.project_id = ? AND ct.parent_id IS NOT NULL AND cs.category NOT IN ('done', 'cancelled')
ORDER BY 1;

-- name: ListMyWork :many
-- My work (VAQ-171, ruling T-R3): the actor's open tickets (not done, not
-- cancelled) in projects not archived, by project name, then priority
-- (urgent first, none last), then the newest change first.
SELECT t.id, t.number, t.title, t.priority, t.version,
       p.id AS project_id, p.key AS project_key, p.name AS project_name, p.icon_kind, p.icon_glyph,
       s.name AS status_name,
       coalesce(nullif(ca.display_name, ''), ca.name, '') AS claimant_name, coalesce(ca.kind, '') AS claimant_kind,
       coalesce(t.claim_expires_at, '') AS claim_expires_at,
       coalesce(t.claimed_by, '') AS claimant_id
FROM tickets t
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
LEFT JOIN actors ca ON ca.id = t.claimed_by
WHERE t.assignee_id = ? AND p.archived_at IS NULL AND s.category NOT IN ('done', 'cancelled')
ORDER BY p.name, p.key, CASE WHEN t.priority = 0 THEN 5 ELSE t.priority END, t.updated_at DESC, t.number;

-- name: ListInboxReview :many
-- The inbox's Review (ruling T-R5): every ticket in an in-review status of
-- a workflow-on project not archived, the longest waiting first, with who
-- last moved it (its newest status change, else who created or moved it
-- to this project).
SELECT t.id, t.number, t.title, t.priority, t.status_changed_at,
       p.id AS project_id, p.key AS project_key, s.name AS status_name,
       CAST(coalesce((SELECT coalesce(nullif(a.display_name, ''), a.name) FROM events e
                 JOIN actors a ON a.id = e.actor_id
                 WHERE e.ticket_id = t.id AND (e.kind IN ('ticket.created', 'ticket.moved')
                   OR (e.kind = 'ticket.updated' AND json_extract(e.payload, '$.changes.status') IS NOT NULL))
                 ORDER BY e.seq DESC LIMIT 1), '') AS TEXT) AS moved_by
FROM tickets t
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
WHERE s.category = 'in_review' AND p.workflow = 1 AND p.archived_at IS NULL
ORDER BY t.status_changed_at, p.key, t.number;

-- name: ListRecentLapses :many
-- The inbox's Check (ruling T-R5): claims that lapsed since the given
-- instant (events_claim_lapsed) on tickets not done or cancelled, in
-- projects not archived, oldest first.
SELECT e.seq, e.at, coalesce(nullif(a.display_name, ''), a.name) AS agent_name,
       t.id AS ticket_id, t.number, t.title, p.id AS project_id, p.key AS project_key
FROM events e
JOIN actors a ON a.id = e.actor_id
JOIN tickets t ON t.id = e.ticket_id
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
WHERE e.kind = 'ticket.claim_lapsed' AND e.at >= sqlc.arg(since)
  AND s.category NOT IN ('done', 'cancelled') AND p.archived_at IS NULL
ORDER BY e.seq;
