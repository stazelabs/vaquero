-- name: CreateAttachment :one
INSERT INTO attachments (id, ticket_id, uploaded_by, name, content_type, size, blob_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = ?;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = ?;

-- name: DeleteProjectAttachments :exec
DELETE FROM attachments WHERE ticket_id IN (SELECT id FROM tickets WHERE project_id = ?);
