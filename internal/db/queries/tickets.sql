-- name: CreateTicket :one
INSERT INTO tickets (id, project_id, number, title, body, status_id, priority, assignee_id,
                     parent_id, milestone_id, position, created_by, created_at, updated_at, status_changed_at,
                     tier, model)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

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
       parent_id = ?, milestone_id = ?, position = ?, updated_at = ?, status_changed_at = ?,
       tier = ?, model = ?, version = version + 1
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

-- name: ListColumn :many
-- A status column's tickets, top to bottom.
SELECT id, position FROM tickets WHERE status_id = ? ORDER BY position, number;

-- name: SetTicketPosition :exec
UPDATE tickets SET position = ? WHERE id = ?;

-- name: PlaceTicket :exec
UPDATE tickets SET status_id = ?, position = ?, status_changed_at = ?, updated_at = ?, version = version + 1 WHERE id = ?;

-- name: ListTicketKeys :many
-- The current key of each given ticket that still exists, in one read
-- (link targets in history, named by id).
SELECT t.id, p.key AS project_key, t.number FROM tickets t
JOIN projects p ON p.id = t.project_id
WHERE t.id IN (sqlc.slice('ids'));

-- name: ListTicketBriefs :many
-- The key and title of each given ticket that still exists, in one read
-- (what a plan approval asked about).
SELECT t.id, p.key AS project_key, t.number, t.title FROM tickets t
JOIN projects p ON p.id = t.project_id
WHERE t.id IN (sqlc.slice('ids'));

-- Moving a ticket to another project (VAQ-18): MoveTicketToProject runs
-- these inside one transaction.

-- name: GetTicketByOldKey :one
-- The ticket that once had this project's number, before it moved away.
SELECT t.* FROM ticket_old_keys k
JOIN tickets t ON t.id = k.ticket_id
WHERE k.project_id = ? AND k.number = ?;

-- name: AddTicketOldKey :exec
INSERT INTO ticket_old_keys (project_id, number, ticket_id, moved_at) VALUES (?, ?, ?, ?);

-- name: ListTicketOldKeys :many
-- The keys a ticket had before it moved, oldest project first by name.
SELECT p.key AS project_key, k.number FROM ticket_old_keys k
JOIN projects p ON p.id = k.project_id
WHERE k.ticket_id = ?
ORDER BY p.key, k.number;

-- name: ListDescendants :many
-- The ticket's sub-tickets, theirs, and so on, nearest first, then by
-- number.
WITH RECURSIVE down(id, depth) AS (
    SELECT c.id, 1 FROM tickets c WHERE c.parent_id = sqlc.arg(id)
    UNION ALL
    SELECT c.id, down.depth + 1 FROM tickets c JOIN down ON c.parent_id = down.id
    WHERE down.depth < 1000
)
SELECT t.* FROM down JOIN tickets t ON t.id = down.id ORDER BY down.depth, t.number;

-- name: SetTicketProject :exec
UPDATE tickets SET project_id = ?, number = ?, status_id = ?, milestone_id = ?, position = ?,
       claim_prev_status_id = ?, status_changed_at = ?, updated_at = ?, version = version + 1
WHERE id = ?;
