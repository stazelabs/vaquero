-- name: AppendEvent :one
INSERT INTO events (at, actor_id, kind, project_id, ticket_id, payload)
VALUES (?, ?, ?, ?, ?, ?) RETURNING seq;

-- name: ListEvents :many
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
ORDER BY e.seq DESC
LIMIT ?;

-- name: ListTicketEvents :many
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
WHERE e.ticket_id = ?
ORDER BY e.seq DESC;

-- name: CountEvents :one
SELECT count(*) FROM events;

-- name: MaxEventSeq :one
SELECT CAST(coalesce(max(seq), 0) AS INTEGER) FROM events;

-- name: ListEventsAfter :many
-- Events after a sequence number, oldest first, for clients that poll.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
WHERE e.seq > ?
ORDER BY e.seq
LIMIT ?;

-- name: ListEventsNewest :many
-- The newest events, newest first (callers reverse them).
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
ORDER BY e.seq DESC
LIMIT ?;
