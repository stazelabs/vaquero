-- name: PutBlob :exec
INSERT INTO blobs (key, bytes, content_type, size, modified) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET bytes = excluded.bytes, content_type = excluded.content_type,
    size = excluded.size, modified = excluded.modified;

-- name: GetBlob :one
SELECT bytes, size FROM blobs WHERE key = ?;

-- name: StatBlob :one
SELECT size FROM blobs WHERE key = ?;

-- name: DeleteBlob :exec
DELETE FROM blobs WHERE key = ?;

-- name: ListBlobs :many
-- Keys under a directory prefix (with its trailing slash, or empty for all),
-- in key order. The bytes are not read.
SELECT key, modified FROM blobs WHERE substr(key, 1, length(sqlc.arg(prefix))) = sqlc.arg(prefix) ORDER BY key;
