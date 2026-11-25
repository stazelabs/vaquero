-- +goose Up
-- Attachments: files on tickets. The bytes live in a blob store, chosen by
-- the operator (the blobs table here, a directory or an S3 bucket). The
-- attachments rows are tracker data and are written only by internal/core;
-- blobs is written only through internal/blobs.

CREATE TABLE blobs (
    key          TEXT PRIMARY KEY,
    bytes        BLOB NOT NULL,
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL,
    modified     TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE attachments (
    id           TEXT PRIMARY KEY,
    ticket_id    TEXT NOT NULL REFERENCES tickets (id),
    uploaded_by  TEXT NOT NULL REFERENCES actors (id),
    name         TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL,
    blob_key     TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL
);
CREATE INDEX attachments_ticket ON attachments (ticket_id);

-- +goose Down
SELECT 1;
