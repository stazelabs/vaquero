-- name: CreateAttachment :one
INSERT INTO attachments (id, ticket_id, uploaded_by, name, content_type, size, blob_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = ?;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = ?;

-- name: DeleteProjectAttachments :exec
DELETE FROM attachments WHERE ticket_id IN (SELECT id FROM tickets WHERE project_id = ?);

-- name: AttachmentByBlobKey :one
SELECT * FROM attachments WHERE blob_key = ?;

-- name: ListTicketAttachments :many
SELECT a.*, u.name AS uploader_name
FROM attachments a JOIN actors u ON u.id = a.uploaded_by
WHERE a.ticket_id = ?
ORDER BY a.created_at, a.id;
