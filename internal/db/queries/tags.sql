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

-- name: ListProjectTags :many
-- Every tag a project's tickets carry, with how many of its tickets do.
SELECT g.id, g.name, count(*) AS tickets FROM tags g
JOIN ticket_tags tt ON tt.tag_id = g.id
JOIN tickets t ON t.id = tt.ticket_id
WHERE t.project_id = ?
GROUP BY g.id, g.name
ORDER BY g.name;

-- name: GetTag :one
SELECT * FROM tags WHERE id = ?;

-- name: CountProjectTagTickets :one
-- How many of a project's tickets carry the tag.
SELECT count(*) FROM ticket_tags tt JOIN tickets t ON t.id = tt.ticket_id
WHERE tt.tag_id = ? AND t.project_id = ?;

-- name: CountTagTickets :one
-- How many tickets, in any project, carry the tag.
SELECT count(*) FROM ticket_tags WHERE tag_id = ?;

-- name: SetTagName :exec
UPDATE tags SET name = ? WHERE id = ?;

-- name: TouchTaggedTickets :exec
-- Raises the version of every ticket of a project that carries a tag about
-- to be renamed, merged or removed: its tags change.
UPDATE tickets SET version = version + 1, updated_at = sqlc.arg(updated_at)
WHERE project_id = sqlc.arg(project_id)
  AND id IN (SELECT ticket_id FROM ticket_tags WHERE tag_id = sqlc.arg(tag_id));

-- name: CopyProjectTicketTags :exec
-- Gives every ticket of a project tagged from_id the tag to_id too; a
-- ticket that has it already keeps its one row.
INSERT INTO ticket_tags (ticket_id, tag_id)
SELECT tt.ticket_id, sqlc.arg(to_id) FROM ticket_tags tt
JOIN tickets t ON t.id = tt.ticket_id
WHERE tt.tag_id = sqlc.arg(from_id) AND t.project_id = sqlc.arg(project_id)
ON CONFLICT DO NOTHING;

-- name: RemoveProjectTicketTags :exec
-- Takes a tag off every ticket of a project.
DELETE FROM ticket_tags
WHERE tag_id = sqlc.arg(tag_id)
  AND ticket_id IN (SELECT id FROM tickets WHERE project_id = sqlc.arg(project_id));

-- name: DeleteUnusedTag :exec
-- Removes a tag no ticket carries any more.
DELETE FROM tags WHERE id = sqlc.arg(id) AND NOT EXISTS (SELECT 1 FROM ticket_tags WHERE tag_id = sqlc.arg(id));

-- name: ListProjectTaggedTicketIDs :many
-- The project's tickets that carry a tag, by number.
SELECT t.id FROM tickets t JOIN ticket_tags tt ON tt.ticket_id = t.id
WHERE tt.tag_id = ? AND t.project_id = ?
ORDER BY t.number;
