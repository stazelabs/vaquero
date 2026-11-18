-- +goose Up
-- When a ticket last changed status (the board's Done column shows the
-- last week), and full-text indexes over tickets and live comments, kept
-- current by triggers on core's writes.

ALTER TABLE tickets ADD COLUMN status_changed_at TEXT NOT NULL DEFAULT '';
UPDATE tickets SET status_changed_at = updated_at;

CREATE VIRTUAL TABLE tickets_fts USING fts5(
    title, body,
    content = 'tickets', content_rowid = 'rowid',
    tokenize = 'unicode61 remove_diacritics 2'
);
INSERT INTO tickets_fts (tickets_fts) VALUES ('rebuild');

-- +goose StatementBegin
CREATE TRIGGER tickets_fts_insert AFTER INSERT ON tickets BEGIN
    INSERT INTO tickets_fts (rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER tickets_fts_update AFTER UPDATE OF title, body ON tickets BEGIN
    INSERT INTO tickets_fts (tickets_fts, rowid, title, body) VALUES ('delete', old.rowid, old.title, old.body);
    INSERT INTO tickets_fts (rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
-- +goose StatementEnd

CREATE VIRTUAL TABLE comments_fts USING fts5(
    body,
    content = 'comments', content_rowid = 'rowid',
    tokenize = 'unicode61 remove_diacritics 2'
);
INSERT INTO comments_fts (comments_fts) VALUES ('rebuild');
INSERT INTO comments_fts (comments_fts, rowid, body)
    SELECT 'delete', rowid, body FROM comments WHERE deleted_at IS NOT NULL;

-- +goose StatementBegin
CREATE TRIGGER comments_fts_insert AFTER INSERT ON comments WHEN new.deleted_at IS NULL BEGIN
    INSERT INTO comments_fts (rowid, body) VALUES (new.rowid, new.body);
END;
-- +goose StatementEnd

-- An edit replaces the indexed text; a soft delete removes it.
-- +goose StatementBegin
CREATE TRIGGER comments_fts_update AFTER UPDATE OF body, deleted_at ON comments BEGIN
    INSERT INTO comments_fts (comments_fts, rowid, body)
        SELECT 'delete', old.rowid, old.body WHERE old.deleted_at IS NULL;
    INSERT INTO comments_fts (rowid, body)
        SELECT new.rowid, new.body WHERE new.deleted_at IS NULL;
END;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
