-- +goose Up
-- The tracker: actors, projects and their statuses and milestones, tickets,
-- tags, comments, links between tickets, and the append-only event log.
-- Only internal/core writes these tables.

CREATE TABLE actors (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('human', 'agent')),
    name       TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    created_at TEXT NOT NULL,
    UNIQUE (kind, name)
);

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE CHECK (length(key) BETWEEN 2 AND 10 AND key GLOB '[A-Z]*' AND key NOT GLOB '*[^A-Z0-9]*'),
    name        TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 20000),
    next_number INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    archived_at TEXT
);

CREATE TABLE statuses (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects (id),
    name       TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 40),
    category   TEXT NOT NULL CHECK (category IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'cancelled')),
    position   INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE milestones (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id),
    name        TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    description TEXT NOT NULL DEFAULT '',
    due_on      TEXT,
    created_at  TEXT NOT NULL,
    closed_at   TEXT,
    UNIQUE (project_id, name)
);

CREATE TABLE tickets (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects (id),
    number       INTEGER NOT NULL,
    title        TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    body         TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 100000),
    status_id    TEXT NOT NULL REFERENCES statuses (id),
    -- 0 none, 1 urgent, 2 high, 3 medium, 4 low
    priority     INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    assignee_id  TEXT REFERENCES actors (id),
    parent_id    TEXT REFERENCES tickets (id),
    milestone_id TEXT REFERENCES milestones (id),
    -- order within a status column
    position     REAL NOT NULL,
    created_by   TEXT NOT NULL REFERENCES actors (id),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (project_id, number)
);
CREATE INDEX tickets_column ON tickets (project_id, status_id, position);
CREATE INDEX tickets_parent ON tickets (parent_id);
CREATE INDEX tickets_milestone ON tickets (milestone_id);

CREATE TABLE tags (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(name)) BETWEEN 1 AND 40),
    created_at TEXT NOT NULL
);

CREATE TABLE ticket_tags (
    ticket_id TEXT NOT NULL REFERENCES tickets (id),
    tag_id    TEXT NOT NULL REFERENCES tags (id),
    PRIMARY KEY (ticket_id, tag_id)
);

CREATE TABLE comments (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL REFERENCES tickets (id),
    author_id  TEXT NOT NULL REFERENCES actors (id),
    body       TEXT NOT NULL CHECK (length(trim(body)) BETWEEN 1 AND 20000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE INDEX comments_ticket ON comments (ticket_id, created_at);

CREATE TABLE ticket_links (
    id         TEXT PRIMARY KEY,
    from_id    TEXT NOT NULL REFERENCES tickets (id),
    to_id      TEXT NOT NULL REFERENCES tickets (id),
    kind       TEXT NOT NULL CHECK (kind IN ('blocks', 'relates', 'duplicates')),
    created_at TEXT NOT NULL,
    CHECK (from_id <> to_id),
    UNIQUE (from_id, to_id, kind)
);
CREATE INDEX ticket_links_to ON ticket_links (to_id);

CREATE TABLE events (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    at         TEXT NOT NULL,
    actor_id   TEXT NOT NULL REFERENCES actors (id),
    kind       TEXT NOT NULL,
    project_id TEXT REFERENCES projects (id),
    ticket_id  TEXT REFERENCES tickets (id),
    payload    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload))
);
CREATE INDEX events_ticket ON events (ticket_id, seq);
CREATE INDEX events_project ON events (project_id, seq);

-- +goose Down
SELECT 1;
