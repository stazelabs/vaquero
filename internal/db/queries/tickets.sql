-- name: CreateTicket :one
INSERT INTO tickets (id, project_id, number, title, body, status_id, priority, assignee_id,
                     parent_id, milestone_id, position, created_by, created_at, updated_at, status_changed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetTicket :one
SELECT * FROM tickets WHERE id = ?;

-- name: GetTicketByNumber :one
SELECT * FROM tickets WHERE project_id = ? AND number = ?;

-- name: ListTicketsByProject :many
SELECT t.*, s.name AS status_name, s.category AS status_category,
       coalesce(a.name, '') AS assignee_name
FROM tickets t
JOIN statuses s ON s.id = t.status_id
LEFT JOIN actors a ON a.id = t.assignee_id
WHERE t.project_id = ?
ORDER BY s.position, t.position;

-- name: ListSubTickets :many
SELECT t.*, s.name AS status_name, s.category AS status_category
FROM tickets t JOIN statuses s ON s.id = t.status_id
WHERE t.parent_id = ?
ORDER BY t.number;

-- name: UpdateTicket :exec
UPDATE tickets SET title = ?, body = ?, status_id = ?, priority = ?, assignee_id = ?,
       parent_id = ?, milestone_id = ?, position = ?, updated_at = ?, status_changed_at = ?
WHERE id = ?;

-- name: MaxPositionInStatus :one
SELECT CAST(coalesce(max(position), 0) AS REAL) FROM tickets WHERE status_id = ?;

-- name: ListAncestorIDs :many
-- The ticket's parent, its parent's parent, and so on, nearest first.
WITH RECURSIVE up(id, parent_id, depth) AS (
    SELECT t.id, t.parent_id, 0 FROM tickets t WHERE t.id = ?
    UNION ALL
    SELECT t.id, t.parent_id, up.depth + 1 FROM tickets t JOIN up ON t.id = up.parent_id
    WHERE up.depth < 1000
)
SELECT CAST(id AS TEXT) FROM up WHERE depth > 0 ORDER BY depth;
