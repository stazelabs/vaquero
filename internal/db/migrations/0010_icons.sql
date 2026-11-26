-- +goose Up
-- Project icons (VAQ-103). A project shows a monogram tile by default, or
-- a glyph from the curated set (core.ProjectGlyphs), or an uploaded image.
-- icon_glyph is the glyph's name when icon_kind is 'glyph', else ''.
ALTER TABLE projects ADD COLUMN icon_kind TEXT NOT NULL DEFAULT 'monogram' CHECK (icon_kind IN ('monogram', 'glyph', 'image'));
ALTER TABLE projects ADD COLUMN icon_glyph TEXT NOT NULL DEFAULT '';

-- A project's uploaded image: at most 256 KB, kept in the database as
-- base64 text (never in the blob store), so it exports as an ordinary
-- table. It stays when the project switches back to its monogram or a
-- glyph, so the image can be chosen again without an upload. etag is a
-- hash of the bytes. Only internal/core writes this table.
CREATE TABLE project_icons (
    project_id TEXT PRIMARY KEY REFERENCES projects (id),
    type       TEXT NOT NULL CHECK (type IN ('image/png', 'image/jpeg', 'image/webp')),
    data       TEXT NOT NULL,
    etag       TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
SELECT 1;
