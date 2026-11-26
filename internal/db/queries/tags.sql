-- name: UpsertTag :one
INSERT INTO tags (id, name, created_at) VALUES (?, ?, ?)
ON CONFLICT (name) DO UPDATE SET name = tags.name
RETURNING *;

-- name: ListTags :many
SELECT * FROM tags ORDER BY name;

-- name: ListTicketTags :many
SELECT g.* FROM tags g JOIN ticket_tags tt ON tt.tag_id = g.id WHERE tt.ticket_id = ? ORDER BY g.name;

-- name: ListProjectTicketTags :many
-- Every (ticket, tag) pair in a project, for list pages.
SELECT tt.ticket_id, g.name FROM ticket_tags tt
JOIN tags g ON g.id = tt.tag_id
JOIN tickets t ON t.id = tt.ticket_id
WHERE t.project_id = ?
ORDER BY g.name;

-- name: AddTicketTag :exec
INSERT INTO ticket_tags (ticket_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING;

-- name: RemoveTicketTag :exec
DELETE FROM ticket_tags WHERE ticket_id = ? AND tag_id = ?;

-- name: ListTagsForTickets :many
-- Every (ticket, tag) pair for the given tickets, in one read (a ticket's
-- sub-tickets in the API's answer).
SELECT tt.ticket_id, g.name FROM ticket_tags tt
JOIN tags g ON g.id = tt.tag_id
WHERE tt.ticket_id IN (sqlc.slice('ticket_ids'))
ORDER BY tt.ticket_id, g.name;
