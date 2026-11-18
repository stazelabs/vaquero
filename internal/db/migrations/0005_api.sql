-- +goose Up
-- API tokens (only a hash is stored), and a version on tickets so a save
-- based on an old read can be refused.

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    actor_id     TEXT NOT NULL REFERENCES actors (id),
    name         TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    prefix       TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);

ALTER TABLE tickets ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

-- +goose Down
SELECT 1;
