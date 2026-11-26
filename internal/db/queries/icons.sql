-- name: GetProjectIcon :one
SELECT * FROM project_icons WHERE project_id = ?;

-- name: GetProjectIconEtag :one
-- Whether a project has a stored image, without reading it.
SELECT etag FROM project_icons WHERE project_id = ?;

-- name: SaveProjectIcon :exec
INSERT INTO project_icons (project_id, type, data, etag, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (project_id) DO UPDATE SET type = excluded.type, data = excluded.data, etag = excluded.etag, updated_at = excluded.updated_at;

-- name: SetProjectIconKind :exec
UPDATE projects SET icon_kind = ?, icon_glyph = ?, updated_at = ? WHERE id = ?;

-- name: DeleteProjectIcon :exec
DELETE FROM project_icons WHERE project_id = ?;
