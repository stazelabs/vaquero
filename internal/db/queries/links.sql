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

-- name: BlocksReach :one
-- Whether start blocks target, directly or through a chain of "blocks"
-- links: the loop check's decision, with no cap (UNION visits each ticket
-- once).
WITH RECURSIVE reach(id) AS (
    SELECT CAST(sqlc.arg(start) AS TEXT)
    UNION
    SELECT l.to_id FROM ticket_links l JOIN reach r ON l.from_id = r.id WHERE l.kind = 'blocks'
)
SELECT CAST(EXISTS (SELECT 1 FROM reach WHERE id = sqlc.arg(target)) AS BOOLEAN);

-- name: ListLinkTargets :many
-- Candidates for the ticket page's link box: tickets other than self_id,
-- narrowed by an exact project key, a number prefix and a title fragment
-- (each empty for any; the title matches ignoring ASCII case): the home
-- project's first, then the exact number, then the newest.
SELECT t.number, t.title, p.key AS project_key,
       CAST(p.id = CAST(sqlc.arg(home_id) AS TEXT) AS BOOLEAN) AS home,
       CAST(CAST(t.number AS TEXT) = CAST(sqlc.arg(exact) AS TEXT) AS BOOLEAN) AS exact FROM tickets t
JOIN projects p ON p.id = t.project_id
WHERE t.id <> sqlc.arg(self_id)
  AND (CAST(sqlc.arg(key) AS TEXT) = '' OR p.key = sqlc.arg(key))
  AND substr(CAST(t.number AS TEXT), 1, length(CAST(sqlc.arg(num_prefix) AS TEXT))) = sqlc.arg(num_prefix)
  AND instr(lower(t.title), lower(CAST(sqlc.arg(title) AS TEXT))) > 0
ORDER BY home DESC, exact DESC, t.number DESC
LIMIT 10;
