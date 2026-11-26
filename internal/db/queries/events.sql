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
-- and the events whose payload.ticket_ids lists it, through event_tickets
-- (ruling P-R1): a group decision, a tag rename, merge or delete in any
-- project it was in, a move that carried it along, whatever its parents
-- are now. Three indexed lookups (events_ticket, events_link_to,
-- event_tickets' key), never a scan of events; a link never names its own
-- ticket, and event_tickets never holds an event's own ticket, so no event
-- comes twice.
SELECT e.*, a.name AS actor_name, coalesce(p.key, '') AS project_key,
       coalesce(t.number, 0) AS ticket_number, coalesce(t.title, '') AS ticket_title,
       coalesce(tp.key, '') AS ticket_project_key
FROM (
    SELECT o.seq FROM events o WHERE o.ticket_id = sqlc.arg(ticket_id)
    UNION ALL
    SELECT l.seq FROM events l
    WHERE l.kind IN ('link.created', 'link.deleted') AND json_extract(l.payload, '$.to_id') = sqlc.arg(ticket_id)
    UNION ALL
    SELECT x.seq FROM event_tickets x WHERE x.ticket_id = sqlc.arg(ticket_id)
) m
JOIN events e ON e.seq = m.seq
JOIN actors a ON a.id = e.actor_id
LEFT JOIN projects p ON p.id = e.project_id
LEFT JOIN tickets t ON t.id = e.ticket_id
LEFT JOIN projects tp ON tp.id = t.project_id
ORDER BY e.seq DESC;

-- name: AddEventTickets :exec
-- An event's event_tickets rows: every id in its payload.ticket_ids other
-- than its own ticket, as migration 0013's backfill writes them.
INSERT OR IGNORE INTO event_tickets (seq, ticket_id)
SELECT e.seq, j.value FROM events e, json_each(e.payload, '$.ticket_ids') j
WHERE e.seq = sqlc.arg(seq) AND json_type(e.payload, '$.ticket_ids') = 'array' AND j.type = 'text'
  AND j.value IS NOT e.ticket_id;

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
