-- name: AppendEvent :one
INSERT INTO events (at, actor_id, kind, project_id, ticket_id, payload, meta)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING seq;

-- name: ListEvents :many
-- The activity feed, newest first. Reorders within a column are left out
-- (they are still events, and still advance the sequence).
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title,
       coalesce(tp.key, '') AS ticket_project_key
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
LEFT JOIN projects tp ON tp.id = t.project_id
WHERE e.kind <> 'ticket.reordered'
ORDER BY e.seq DESC
LIMIT ?;

-- name: ListTicketEvents :many
-- A ticket's history: its own events, link events that name it as the
-- "to" side by id (a link is one event, recorded on the "from" ticket),
-- its project's tag events whose payload.ticket_ids lists it (a tag
-- change is one project event), its old projects' too (a ticket moved
-- from them keeps their tag history), and the moves of its ancestors that
-- took it along (payload.ticket_ids: a move is one event, on the ticket
-- moved). Four indexed lookups (events_ticket, events_link_to, events_tag,
-- events_ticket again), never a scan of events; a link never names its
-- own ticket, a tag event names no ticket, and a move's ticket_ids never
-- name the ticket it is on, so no event comes twice.
WITH RECURSIVE up(id, depth) AS (
    SELECT ut.parent_id, 1 FROM tickets ut WHERE ut.id = sqlc.arg(ticket_id) AND ut.parent_id IS NOT NULL
    UNION ALL
    SELECT ut.parent_id, up.depth + 1 FROM tickets ut JOIN up ON ut.id = up.id
    WHERE ut.parent_id IS NOT NULL AND up.depth < 1000
)
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title,
       coalesce(tp.key, '') AS ticket_project_key
FROM (
    SELECT o.seq FROM events o WHERE o.ticket_id = sqlc.arg(ticket_id)
    UNION ALL
    SELECT l.seq FROM events l
    WHERE l.kind IN ('link.created', 'link.deleted') AND json_extract(l.payload, '$.to_id') = sqlc.arg(ticket_id)
    UNION ALL
    SELECT g.seq FROM events g
    WHERE g.kind IN ('tag.renamed', 'tag.merged', 'tag.deleted')
      AND g.project_id IN (SELECT tc.project_id FROM tickets tc WHERE tc.id = sqlc.arg(ticket_id)
                           UNION SELECT ok.project_id FROM ticket_old_keys ok WHERE ok.ticket_id = sqlc.arg(ticket_id))
      AND EXISTS (SELECT 1 FROM json_each(g.payload, '$.ticket_ids') j WHERE j.value = sqlc.arg(ticket_id))
    UNION ALL
    SELECT v.seq FROM events v
    WHERE v.kind = 'ticket.moved'
      AND v.ticket_id IN (SELECT up.id FROM up)
      AND EXISTS (SELECT 1 FROM json_each(v.payload, '$.ticket_ids') j WHERE j.value = sqlc.arg(ticket_id))
) m
JOIN events e ON e.seq = m.seq
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
LEFT JOIN projects tp ON tp.id = t.project_id
ORDER BY e.seq DESC;

-- name: CountEvents :one
SELECT count(*) FROM events;

-- name: MaxEventSeq :one
SELECT CAST(coalesce(max(seq), 0) AS INTEGER) FROM events;

-- name: ListEventsAfter :many
-- Events after a sequence number, oldest first, for clients that poll;
-- reorders left out, as on the activity feed.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title,
       coalesce(tp.key, '') AS ticket_project_key
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
LEFT JOIN projects tp ON tp.id = t.project_id
WHERE e.seq > ? AND e.kind <> 'ticket.reordered'
ORDER BY e.seq
LIMIT ?;

-- name: ListEventsNewest :many
-- The newest events, newest first (callers reverse them); reorders left
-- out, as on the activity feed.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title,
       coalesce(tp.key, '') AS ticket_project_key
FROM events e
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
LEFT JOIN projects tp ON tp.id = t.project_id
WHERE e.kind <> 'ticket.reordered'
ORDER BY e.seq DESC
LIMIT ?;

-- name: GetTicketEvent :one
-- One event recorded on a ticket, for the history's "Show changes".
SELECT seq, kind, payload FROM events WHERE seq = ? AND ticket_id = ?;
