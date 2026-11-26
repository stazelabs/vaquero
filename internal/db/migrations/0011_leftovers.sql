-- +goose Up
-- Run N's whole schema (the leftovers): one index and two tables.

-- Tag changes in ticket history (VAQ-134). A tag rename, merge or delete
-- is one project event whose payload.ticket_ids lists the tickets it
-- changed; a ticket's history finds its project's tag events through this
-- index, then looks in each one's list (json_each). Tag events from before
-- ticket_ids existed name no ticket and show on no ticket's history.
CREATE INDEX events_tag ON events (project_id, seq)
WHERE kind IN ('tag.renamed', 'tag.merged', 'tag.deleted');

-- A ticket moved to another project keeps answering to its old key
-- (VAQ-18): the old project and number lead to the ticket. A project
-- whose key changes keeps its id, so its moved tickets' old keys follow
-- the new key. moved_at is when the move that re-keyed the ticket was
-- made (the ticket's own, or its ancestor's that carried it): a ticket's
-- newest moved_at is when it last arrived in its project, whatever its
-- parents are now, and an approval asked before then doesn't count
-- (ruling N-R7). Only internal/core writes this table.
CREATE TABLE ticket_old_keys (
    project_id TEXT NOT NULL REFERENCES projects (id),
    number     INTEGER NOT NULL,
    ticket_id  TEXT NOT NULL REFERENCES tickets (id),
    moved_at   TEXT NOT NULL,
    PRIMARY KEY (project_id, number)
);
CREATE INDEX ticket_old_keys_ticket ON ticket_old_keys (ticket_id);

-- Links between projects (VAQ-119): one project depends on another, or
-- the two relate. Only internal/core writes this table.
CREATE TABLE project_links (
    id           TEXT PRIMARY KEY,
    from_project TEXT NOT NULL REFERENCES projects (id),
    to_project   TEXT NOT NULL REFERENCES projects (id),
    kind         TEXT NOT NULL CHECK (kind IN ('depends on', 'relates')),
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL REFERENCES actors (id),
    CHECK (from_project <> to_project),
    UNIQUE (from_project, to_project, kind)
);
CREATE INDEX project_links_to ON project_links (to_project);

-- +goose Down
SELECT 1;
