-- name: CreateComment :one
INSERT INTO comments (id, ticket_id, author_id, body, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetComment :one
SELECT * FROM comments WHERE id = ?;

-- name: ListComments :many
SELECT c.*, a.name AS author_name, a.kind AS author_kind
FROM comments c JOIN actors a ON a.id = c.author_id
WHERE c.ticket_id = ? AND c.deleted_at IS NULL
ORDER BY c.created_at, c.id;

-- name: UpdateComment :exec
UPDATE comments SET body = ?, updated_at = ? WHERE id = ?;

-- name: SoftDeleteComment :exec
UPDATE comments SET deleted_at = ? WHERE id = ?;
