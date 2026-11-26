-- name: GetSetting :one
SELECT * FROM settings WHERE key = ?;

-- name: PutSetting :exec
INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: CountAttachments :one
SELECT count(*) FROM attachments;

-- name: AttachmentTotals :one
SELECT count(*) AS count, CAST(coalesce(sum(size), 0) AS INTEGER) AS size FROM attachments;
