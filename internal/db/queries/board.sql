-- name: ListBoardTickets :many
-- A project's tickets for the board and list views. Every filter is
-- optional (NULL means any). ready: a todo ticket, unassigned, with no open
-- blocker and no open sub-ticket.
SELECT t.id, t.number, t.title, t.priority, t.status_id, t.position, t.status_changed_at, t.version,
       s.name AS status_name, s.category AS status_category,
       coalesce(a.name, '') AS assignee_name, coalesce(a.kind, '') AS assignee_kind,
       CAST(s.category = 'todo' AND t.assignee_id IS NULL
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
WHERE t.project_id = sqlc.arg(project_id)
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
