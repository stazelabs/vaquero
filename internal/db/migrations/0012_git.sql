-- +goose Up
-- Agent workflow part 2's whole schema (VAQ-136): a project's repo
-- settings, a ticket's references to repo files and URLs, and the files an
-- approval was asked about.

-- A project's repo (ruling P2-R1: repo_path, main_branch and remote are
-- the owner's, like the workflow switch). repo_path is an absolute path to
-- a git work tree, '' for none. remote '' means origin when the repo has
-- it, else no remote. fetch_minutes 0 is never; otherwise Vaquero fetches
-- the remote every N minutes, 5 to 1440.
ALTER TABLE projects ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN main_branch TEXT NOT NULL DEFAULT 'main';
ALTER TABLE projects ADD COLUMN remote TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN fetch_minutes INTEGER NOT NULL DEFAULT 0
    CHECK (fetch_minutes = 0 OR fetch_minutes BETWEEN 5 AND 1440);

-- A ticket's typed references: target is a repo path (relative, checked by
-- gitread.ValidPath) or an http(s) URL; anchor is a heading for the link
-- only (pins stay per file). Only internal/core writes this table.
CREATE TABLE ticket_refs (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL REFERENCES tickets (id),
    kind       TEXT NOT NULL CHECK (kind IN ('requirement', 'design', 'plan', 'spec', 'other')),
    target     TEXT NOT NULL,
    anchor     TEXT NOT NULL DEFAULT '',
    label      TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL REFERENCES actors (id),
    created_at TEXT NOT NULL,
    UNIQUE (ticket_id, target, anchor)
);
CREATE INDEX ticket_refs_ticket ON ticket_refs (ticket_id);

-- The repo files a plan approval pins for each ticket, as asked (ruling
-- P2-R2): a JSON array of {"path", "commit", "blob"}; '[]' for none and for
-- every decision asked before part 2.
ALTER TABLE decision_tickets ADD COLUMN files TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(files) AND json_type(files) = 'array');

-- +goose Down
SELECT 1;
