-- +goose Up
-- A link event names its target ticket by id (payload.to_id) since v0.1.2;
-- older ones carry only the key it had then (payload.to), which a renamed
-- or deleted project frees for another. A ticket's history finds the link
-- events aimed at it through this index, by id only.
CREATE INDEX events_link_to ON events (json_extract(payload, '$.to_id'))
WHERE kind IN ('link.created', 'link.deleted');

-- The backfill below also runs after an import (db.BackfillLinkTargets),
-- which reads it from this file between the two markers.
-- backfill:begin
-- First from the link row, when it still exists: it names the target by id.
UPDATE events SET payload = json_set(payload, '$.to_id', (SELECT l.to_id FROM ticket_links l
     WHERE l.id = json_extract(events.payload, '$.link_id') AND l.from_id = events.ticket_id))
WHERE kind IN ('link.created', 'link.deleted') AND json_extract(payload, '$.to_id') IS NULL
  AND (SELECT l.to_id FROM ticket_links l
     WHERE l.id = json_extract(events.payload, '$.link_id') AND l.from_id = events.ticket_id) IS NOT NULL;
-- Else from the key, only when it surely named the same ticket then: the
-- ticket already existed when the event was written, and its project has
-- not changed key since. A ticket made later took a freed key, and a
-- project renamed later may have taken the key from another; neither is
-- the one the event meant, and those events keep no to_id.
UPDATE events SET payload = json_set(payload, '$.to_id', (SELECT t.id FROM projects p
     JOIN tickets t ON t.project_id = p.id
      AND t.number = CAST(substr(json_extract(events.payload, '$.to'), instr(json_extract(events.payload, '$.to'), '-') + 1) AS INTEGER)
     WHERE p.key = substr(json_extract(events.payload, '$.to'), 1, instr(json_extract(events.payload, '$.to'), '-') - 1)
       AND t.created_at <= events.at
       AND NOT EXISTS (SELECT 1 FROM events k
                       WHERE k.project_id = p.id AND k.kind = 'project.updated' AND k.seq > events.seq
                         AND json_extract(k.payload, '$.changes.key') IS NOT NULL)))
WHERE kind IN ('link.created', 'link.deleted') AND json_extract(payload, '$.to_id') IS NULL
  AND (SELECT t.id FROM projects p
     JOIN tickets t ON t.project_id = p.id
      AND t.number = CAST(substr(json_extract(events.payload, '$.to'), instr(json_extract(events.payload, '$.to'), '-') + 1) AS INTEGER)
     WHERE p.key = substr(json_extract(events.payload, '$.to'), 1, instr(json_extract(events.payload, '$.to'), '-') - 1)
       AND t.created_at <= events.at
       AND NOT EXISTS (SELECT 1 FROM events k
                       WHERE k.project_id = p.id AND k.kind = 'project.updated' AND k.seq > events.seq
                         AND json_extract(k.payload, '$.changes.key') IS NOT NULL)) IS NOT NULL;
-- backfill:end

-- +goose Down
SELECT 1;
