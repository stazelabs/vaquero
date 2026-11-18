-- +goose Up
-- Instance-wide key/value settings.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 100),
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
SELECT 1;
