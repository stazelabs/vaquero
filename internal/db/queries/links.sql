-- name: CreateLink :one
INSERT INTO ticket_links (id, from_id, to_id, kind, created_at) VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetLink :one
SELECT * FROM ticket_links WHERE id = ?;

-- name: DeleteLink :exec
DELETE FROM ticket_links WHERE id = ?;

-- name: ListLinksFrom :many
SELECT l.id, l.kind, t.id AS other_id, p.key AS other_key, t.number AS other_number,
       t.title AS other_title, s.category AS other_category
FROM ticket_links l
JOIN tickets t ON t.id = l.to_id
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
WHERE l.from_id = ?
ORDER BY l.kind, p.key, t.number;

-- name: ListLinksTo :many
SELECT l.id, l.kind, t.id AS other_id, p.key AS other_key, t.number AS other_number,
       t.title AS other_title, s.category AS other_category
FROM ticket_links l
JOIN tickets t ON t.id = l.from_id
JOIN projects p ON p.id = t.project_id
JOIN statuses s ON s.id = t.status_id
WHERE l.to_id = ?
ORDER BY l.kind, p.key, t.number;

-- name: ListBlockedBy :many
-- The tickets a ticket blocks, for the loop check.
SELECT l.to_id AS id, p.key, t.number
FROM ticket_links l JOIN tickets t ON t.id = l.to_id JOIN projects p ON p.id = t.project_id
WHERE l.from_id = ? AND l.kind = 'blocks';

-- name: LinkExists :one
SELECT count(*) FROM ticket_links WHERE from_id = ? AND to_id = ? AND kind = ?;
