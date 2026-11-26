-- +goose Up
-- Run P's whole schema: the event-to-ticket lookup and the owner profile.

-- The tickets an event concerns beyond its own ticket_id (VAQ-144,
-- VAQ-147, ruling P-R1): every id in its payload.ticket_ids, other than
-- the event's own ticket. A group decision, a tag rename, merge or delete,
-- and a move's carried sub-tickets each list their tickets there; a
-- ticket's history finds them through this table's primary key instead of
-- looking inside payloads. Derived data, written by internal/core with the
-- event in the same transaction; never exported, and rebuilt after an
-- import from the backfill below.
CREATE TABLE event_tickets (
    seq       INTEGER NOT NULL REFERENCES events (seq),
    ticket_id TEXT NOT NULL,
    PRIMARY KEY (ticket_id, seq)
) WITHOUT ROWID;
-- By seq too: deleting an event checks for rows here, and a project delete
-- removes its events' rows by seq.
CREATE INDEX event_tickets_seq ON event_tickets (seq);

-- The backfill below also runs after an import (db.BackfillEventTickets),
-- which reads it from this file between the two markers. It adds only
-- what is missing, so a second run changes nothing.
-- backfill:begin
INSERT OR IGNORE INTO event_tickets (seq, ticket_id)
SELECT e.seq, j.value FROM events e, json_each(e.payload, '$.ticket_ids') j
WHERE json_type(e.payload, '$.ticket_ids') = 'array' AND j.type = 'text'
  AND j.value IS NOT e.ticket_id;
-- backfill:end

-- The history no longer looks inside payloads for tag events, so run N's
-- partial index for that (0011) has no reader.
DROP INDEX events_tag;

-- A move is recorded on the project the ticket moved to; the project it
-- left counts it as a change too (the projects page's last change,
-- VAQ-147), found through this index.
CREATE INDEX events_moved_from ON events (json_extract(payload, '$.from_project_id'))
WHERE kind = 'ticket.moved';

-- The owner's profile (VAQ-149, ruling P-R3): the name every page shows
-- when set; actors.name stays the actor's identity.
ALTER TABLE actors ADD COLUMN display_name TEXT NOT NULL DEFAULT '' CHECK (length(display_name) <= 80);

-- An actor's uploaded picture, shaped like project_icons: base64 text in
-- the database, never in the blob store. etag is a hash of the bytes;
-- core checks the content type.
-- Only internal/core writes this table.
CREATE TABLE actor_avatars (
    actor_id     TEXT PRIMARY KEY REFERENCES actors (id),
    content_type TEXT NOT NULL,
    data         TEXT NOT NULL,
    etag         TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- +goose Down
SELECT 1;
