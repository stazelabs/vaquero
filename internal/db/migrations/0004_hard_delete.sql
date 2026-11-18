-- +goose Up
-- Deleting a project hard-deletes its tickets and comments; their rows
-- leave the search indexes too.

-- +goose StatementBegin
CREATE TRIGGER tickets_fts_delete AFTER DELETE ON tickets BEGIN
    INSERT INTO tickets_fts (tickets_fts, rowid, title, body) VALUES ('delete', old.rowid, old.title, old.body);
END;
-- +goose StatementEnd

-- A soft-deleted comment already left the index.
-- +goose StatementBegin
CREATE TRIGGER comments_fts_delete AFTER DELETE ON comments WHEN old.deleted_at IS NULL BEGIN
    INSERT INTO comments_fts (comments_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
END;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
