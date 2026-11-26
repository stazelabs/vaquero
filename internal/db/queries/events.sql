-- name: AppendEvent :one
INSERT INTO events (at, actor_id, kind, project_id, ticket_id, payload, meta)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING seq;

-- name: ListEvents :many
-- The activity feed, newest first. Reorders within a column are left out
-- (they are still events, and still advance the sequence).
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
WHERE e.kind <> 'ticket.reordered'
ORDER BY e.seq DESC
LIMIT ?;

-- name: ListTicketEvents :many
-- A ticket's history: its own events, link events that name it as the
-- "to" side by id (a link is one event, recorded on the "from" ticket),
-- and its project's tag events whose payload.ticket_ids lists it (a tag
-- change is one project event). Three indexed lookups (events_ticket,
-- events_link_to, events_tag), never a scan of events; a link never names
-- its own ticket and a tag event names no ticket, so no event comes twice.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM (
    SELECT o.seq FROM events o WHERE o.ticket_id = sqlc.arg(ticket_id)
    UNION ALL
    SELECT l.seq FROM events l
    WHERE l.kind IN ('link.created', 'link.deleted') AND json_extract(l.payload, '$.to_id') = sqlc.arg(ticket_id)
    UNION ALL
    SELECT g.seq FROM events g
    WHERE g.kind IN ('tag.renamed', 'tag.merged', 'tag.deleted')
      AND g.project_id = (SELECT tp.project_id FROM tickets tp WHERE tp.id = sqlc.arg(ticket_id))
      AND EXISTS (SELECT 1 FROM json_each(g.payload, '$.ticket_ids') j WHERE j.value = sqlc.arg(ticket_id))
) m
JOIN events e ON e.seq = m.seq
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
ORDER BY e.seq DESC;

-- name: CountEvents :one
SELECT count(*) FROM events;

-- name: MaxEventSeq :one
SELECT CAST(coalesce(max(seq), 0) AS INTEGER) FROM events;

-- name: ListEventsAfter :many
-- Events after a sequence number, oldest first, for clients that poll;
-- reorders left out, as on the activity feed.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
WHERE e.seq > ? AND e.kind <> 'ticket.reordered'
ORDER BY e.seq
LIMIT ?;

-- name: ListEventsNewest :many
-- The newest events, newest first (callers reverse them); reorders left
-- out, as on the activity feed.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
WHERE e.kind <> 'ticket.reordered'
ORDER BY e.seq DESC
LIMIT ?;
