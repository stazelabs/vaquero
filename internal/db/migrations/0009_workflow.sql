-- +goose Up
-- The agent workflow's records (part 1): the per-project workflow switch
-- and tier map, each ticket's execution tier and model, provenance on
-- events, plans with their revisions, decisions and approvals. Only
-- internal/core writes these tables.

-- The workflow is off (0) until the owner turns it on; tier_map is '' for
-- the shipped default map, else a JSON object harness -> tier -> model.
ALTER TABLE projects ADD COLUMN workflow INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN tier_map TEXT NOT NULL DEFAULT '' CHECK (tier_map = '' OR json_valid(tier_map));

-- '' is no tier; model is an exact model id, '' for the tier map's choice.
ALTER TABLE tickets ADD COLUMN tier TEXT NOT NULL DEFAULT '' CHECK (tier IN ('', 'design', 'light', 'standard', 'heavy'));
ALTER TABLE tickets ADD COLUMN model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 80);

-- Provenance, as reported and never verified: {"surface", "client",
-- "harness", "model", "run"}, each at most 80 characters, empty ones left
-- out. Events from before 0009 have {}.
ALTER TABLE events ADD COLUMN meta TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(meta));

-- A ticket's (or an epic's) plan: the current text and its revision.
-- plan_revisions keeps every revision, the current one included.
CREATE TABLE plans (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT NOT NULL UNIQUE REFERENCES tickets (id),
    revision   INTEGER NOT NULL CHECK (revision >= 1),
    body       TEXT NOT NULL CHECK (length(body) <= 200000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by TEXT NOT NULL REFERENCES actors (id)
);

CREATE TABLE plan_revisions (
    plan_id  TEXT NOT NULL REFERENCES plans (id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    body     TEXT NOT NULL CHECK (length(body) <= 200000),
    at       TEXT NOT NULL,
    actor_id TEXT NOT NULL REFERENCES actors (id),
    PRIMARY KEY (plan_id, revision)
);

-- A decision: a question an agent parks for the owner, a ruling an agent
-- records (answered when made), or an approval (of a plan, a merge, a
-- release). run_id gains its foreign key when runs arrive (part 3).
-- options is a JSON list of {"label", "tradeoff"}. relayed_by is the agent
-- that relayed the owner's answer, with the owner's words; NULL when the
-- owner answered directly.
CREATE TABLE decisions (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects (id),
    run_id         TEXT,
    kind           TEXT NOT NULL CHECK (kind IN ('question', 'ruling', 'approval')),
    subject        TEXT NOT NULL DEFAULT '' CHECK (subject IN ('', 'plan', 'merge', 'release')),
    question       TEXT NOT NULL CHECK (length(trim(question)) BETWEEN 1 AND 20000),
    options        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(options) AND json_type(options) = 'array'),
    recommendation TEXT NOT NULL DEFAULT '' CHECK (length(recommendation) <= 20000),
    why            TEXT NOT NULL DEFAULT '' CHECK (length(why) <= 20000),
    state          TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'answered', 'withdrawn')),
    answer         TEXT NOT NULL DEFAULT '' CHECK (length(answer) <= 20000),
    answer_note    TEXT NOT NULL DEFAULT '' CHECK (length(answer_note) <= 20000),
    answered_by    TEXT REFERENCES actors (id),
    answered_at    TEXT,
    relayed_by     TEXT REFERENCES actors (id),
    relayed_words  TEXT NOT NULL DEFAULT '' CHECK (length(relayed_words) <= 20000),
    asked_by       TEXT NOT NULL REFERENCES actors (id),
    asked_at       TEXT NOT NULL
);
CREATE INDEX decisions_project ON decisions (project_id, state);

-- requirement_hash and plan_revision are what a plan approval asked about
-- for each ticket, pinned when asked: an Approve is refused if either has
-- changed since. Other decisions leave them '' and 0.
CREATE TABLE decision_tickets (
    decision_id      TEXT NOT NULL REFERENCES decisions (id),
    ticket_id        TEXT NOT NULL REFERENCES tickets (id),
    requirement_hash TEXT NOT NULL DEFAULT '',
    plan_revision    INTEGER NOT NULL DEFAULT 0 CHECK (plan_revision >= 0),
    PRIMARY KEY (decision_id, ticket_id)
);
CREATE INDEX decision_tickets_ticket ON decision_tickets (ticket_id);

-- An approval of a ticket's requirement and plan, as they stood when the
-- owner approved: requirement_hash is hex(sha256(NFC(title) + "\n" +
-- NFC(description))), plan_revision the plan approved (0: no plan), pins
-- a JSON value: {"children": [ticket ids]}, the sub-tickets it had when
-- approved, the only ones its approval covers (repo pins arrive in part 2).
CREATE TABLE approvals (
    id               TEXT PRIMARY KEY,
    ticket_id        TEXT NOT NULL REFERENCES tickets (id),
    decision_id      TEXT NOT NULL REFERENCES decisions (id),
    requirement_hash TEXT NOT NULL,
    plan_revision    INTEGER NOT NULL DEFAULT 0 CHECK (plan_revision >= 0),
    pins             TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(pins)),
    at               TEXT NOT NULL
);
CREATE INDEX approvals_ticket ON approvals (ticket_id, at);
CREATE INDEX approvals_decision ON approvals (decision_id);

-- +goose Down
SELECT 1;
