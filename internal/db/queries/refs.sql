-- name: CreateTicketRef :one
INSERT INTO ticket_refs (id, ticket_id, kind, target, anchor, label, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetTicketRef :one
SELECT * FROM ticket_refs WHERE id = ?;

-- name: DeleteTicketRef :exec
DELETE FROM ticket_refs WHERE id = ?;

-- name: CountTicketRef :one
SELECT count(*) FROM ticket_refs WHERE ticket_id = ? AND target = ? AND anchor = ?;

-- name: ListTicketRefs :many
SELECT * FROM ticket_refs WHERE ticket_id = ? ORDER BY created_at, id;

-- name: DeleteProjectRefs :exec
DELETE FROM ticket_refs WHERE ticket_id IN (SELECT t.id FROM tickets t WHERE t.project_id = ?);
